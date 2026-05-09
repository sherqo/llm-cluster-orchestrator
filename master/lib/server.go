package lib

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	monitoring "master/monitoring"
)

// structs
type ChatRequest struct {
	UserID string `json:"userId"`
	Prompt string `json:"prompt"`
	Tier   string `json:"tier"`
}

type ChatResponse struct {
	RequestID string `json:"requestId"`
	Reply     string `json:"reply"`
}

type WorkerRegisterRequest struct {
	Addr string `json:"addr"`
}

type WorkerRegisterResponse struct {
	WorkerID string `json:"workerId"`
	Status   string `json:"status"`
}

type AgentRegisterRequest struct {
	AgentID string `json:"agent_id"`
	Host    string `json:"host"`
	Port    int    `json:"port"`
}

// main server loop
func Serve(router *Router) {
	// Initialize the metrics collector (#13)
	metricsCfg := DefaultMetricsCollectorConfig()
	metrics := NewMetricsCollector(metricsCfg)

	// Initialize the admission controller (#12)
	admissionCfg := DefaultAdmissionConfig()
	admission := NewAdmissionController(admissionCfg, metrics)

	// Initialize the autoscaler with production defaults (#1, #3, #4, #8, #9, #10, #11, #14)
	autoscalerCfg := DefaultAutoscalerConfig()
	autoscaler := NewAutoscaler(autoscalerCfg, router, metrics)

	http.HandleFunc("/chat", func(w http.ResponseWriter, r *http.Request) {
		chatRequestHandler(w, r, router, admission, metrics)
	})

	http.HandleFunc("/agents/register", func(w http.ResponseWriter, r *http.Request) {
		agentRegisterHandler(w, r, router)
	})

	// Start the autoscaler instead of the old autoscaleLoop
	go autoscaler.Run()
	go healthCheckLoop(router)
	go admissionUpdateLoop(router, admission)

	log.Println("LB listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

// chatRequestHandler with admission control (#12) and metrics recording (#2, #13)
func chatRequestHandler(w http.ResponseWriter, r *http.Request, router *Router, admission *AdmissionController, metrics *MetricsCollector) {
	// http checks
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		monitoring.Verbose("server", "invalid JSON from client")
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	monitoring.Verbose("server", "received chat request, userId="+req.UserID+", tier="+req.Tier)

	// Record the incoming request for metrics (#13)
	metrics.RecordRequest()

	// Admission control: gate the request (#12)
	release, admitErr := admission.Admit(r.Context())
	if admitErr != nil {
		switch {
		case errors.Is(admitErr, ErrAdmissionQueueFull):
			monitoring.Verbose("server", "request rejected: queue full")
			w.Header().Set("Retry-After", "5")
			http.Error(w, "server overloaded, try again later", http.StatusServiceUnavailable)
		case errors.Is(admitErr, ErrAdmissionTimeout):
			monitoring.Verbose("server", "request rejected: queue timeout")
			http.Error(w, "request timed out in queue", http.StatusGatewayTimeout)
		default:
			monitoring.Verbose("server", "request rejected: "+admitErr.Error())
			http.Error(w, "service unavailable", http.StatusServiceUnavailable)
		}
		metrics.RecordError()
		return
	}
	defer release()

	// generate a unique request ID
	requestID, err := uuid.NewV7()
	if err != nil {
		monitoring.Verbose("server", "failed to generate request id: "+err.Error())
		http.Error(w, "failed to assign request id", http.StatusInternalServerError)
		metrics.RecordError()
		return
	}
	requestIDStr := requestID.String()
	monitoring.Verbose("server", "assigned requestId="+requestIDStr)

	// Track latency for metrics (#2, #13)
	start := time.Now()

	// handle the request and send errors
	resp, err := router.HandleChat(r.Context(), requestIDStr, req)
	if err != nil {
		metrics.RecordError()

		switch {
		case errors.Is(err, ErrNoWorkersAvailable):
			monitoring.Verbose("server", "no workers available")
			http.Error(w, "no workers available", http.StatusServiceUnavailable)
		case errors.Is(err, ErrWorkerFailed):
			monitoring.Verbose("server", err.Error())
			http.Error(w, err.Error(), http.StatusBadGateway)
		default:
			monitoring.Verbose("server", err.Error())
			http.Error(w, "internal server error", http.StatusInternalServerError)
		}
		return
	}

	// Record latency (#2, #5, #13)
	latency := time.Since(start).Seconds()
	metrics.RecordLatency(latency)

	monitoring.Verbose("server", "request completed, reqId="+resp.RequestID+" reply="+resp.Reply)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp) // send the request back to the client
}

func agentRegisterHandler(w http.ResponseWriter, r *http.Request, router *Router) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req AgentRegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		monitoring.Verbose("server", "invalid agent registration JSON")
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	monitoring.Verbose("server", "agent registration: "+req.AgentID+" at "+req.Host+":"+strconv.Itoa(req.Port))

	router.RegisterAgent(AgentInfo{
		AgentID: req.AgentID,
		Host:    req.Host,
		Port:    req.Port,
		AddedAt: time.Now(),
	})

	go spawnWorkerForAgent(router, req)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "registered"})
}

// spawnWorkerForAgent spawns a worker on a specific agent with retry (#11).
func spawnWorkerForAgent(router *Router, req AgentRegisterRequest) {
	agentAddr := fmt.Sprintf("http://%s:%d", req.Host, req.Port)
	log.Printf("requesting worker from agent at %s", agentAddr)

	// Retry with exponential backoff (#11)
	maxAttempts := 3
	backoff := 1 * time.Second

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		err := trySpawnWorker(router, agentAddr, req.AgentID)
		if err == nil {
			return
		}

		log.Printf("spawn attempt %d/%d failed on agent %s: %v", attempt, maxAttempts, req.AgentID, err)
		monitoring.Verbose("server", fmt.Sprintf("spawn attempt %d/%d failed: %v", attempt, maxAttempts, err))

		if attempt < maxAttempts {
			time.Sleep(backoff)
			backoff *= 2
		}
	}

	log.Printf("all %d spawn attempts failed for agent %s", maxAttempts, req.AgentID)
}

func trySpawnWorker(router *Router, agentAddr, agentID string) error {
	client := http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(agentAddr+"/workers/create", "application/json", bytes.NewReader([]byte("{}")))
	if err != nil {
		return fmt.Errorf("failed to create worker from agent: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("agent worker creation failed with status %d", resp.StatusCode)
	}

	var result struct {
		WorkerID string `json:"worker_id"`
		Address  string `json:"address"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to decode worker response: %w", err)
	}

	log.Printf("worker created: %s at %s", result.WorkerID, result.Address)

	workerID := "worker-" + result.Address
	w, err := NewWorker(workerID, result.Address)
	if err != nil {
		return fmt.Errorf("failed to verify worker: %w", err)
	}

	// Track which agent spawned this worker (#10)
	w.SetAgentID(agentID)

	router.AddWorkerWithInstance(w)
	log.Printf("worker %s verified and added via gRPC (agent: %s)", result.Address, agentID)
	return nil
}

func healthCheckLoop(router *Router) {
	for {
		time.Sleep(5 * time.Second)

		router.workersM.RLock()
		workersToPing := make([]*Worker, 0, len(router.workers))
		for _, w := range router.workers {
			// Only health-check workers that are in routable states
			state := w.GetLifecycleState()
			if state == StateHealthy || state == StateWarming {
				workersToPing = append(workersToPing, w)
			}
		}
		router.workersM.RUnlock()

		for _, w := range workersToPing {
			err := w.Ping()
			if err != nil {
				monitoring.Verbose("health", "worker "+w.ID()+" ping failed: "+err.Error())
				w.SetDraining()
			} else {
				// If worker was warming, promote to healthy
				if w.GetLifecycleState() == StateWarming {
					w.MarkHealthy()
					monitoring.Verbose("health", "worker "+w.ID()+" warmed up, now healthy")
				} else {
					w.MarkHealthy()
				}
			}
		}
	}
}

// admissionUpdateLoop periodically updates admission limits based on healthy worker count.
func admissionUpdateLoop(router *Router, admission *AdmissionController) {
	for {
		time.Sleep(2 * time.Second)
		healthyCount := router.HealthyWorkerCount()
		admission.UpdateLimits(healthyCount)
	}
}
