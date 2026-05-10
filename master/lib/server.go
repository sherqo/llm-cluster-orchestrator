package lib

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
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

type RunningWorkerInfo struct {
	WorkerID    string `json:"worker_id"`
	Address     string `json:"address"`
	ContainerID string `json:"container_id"`
}

type AgentRegisterRequest struct {
	AgentID string            `json:"agent_id"`
	Host    string            `json:"host"`
	Port    int               `json:"port"`
	Workers []RunningWorkerInfo `json:"workers,omitempty"`
}

// WorkerReadyNotification is the callback payload the agent POSTs to /workers/ready
// once the worker container is up and its gRPC port is reachable.
type WorkerReadyNotification struct {
	WorkerID    string `json:"worker_id"`
	Address     string `json:"address"`
	AgentID     string `json:"agent_id"`
	ContainerID string `json:"container_id"`
}

// masterBaseURL returns the externally-reachable base URL for this master.
// Read from MASTER_URL env var; falls back to http://localhost:8080.
func masterBaseURL() string {
	if u := os.Getenv("MASTER_URL"); u != "" {
		return strings.TrimRight(u, "/")
	}
	return "http://localhost:8080"
}

// main server loop
func Serve(router *Router) {
	// Initialize the metrics collector (#13)
	metricsCfg := DefaultMetricsCollectorConfig()
	metrics := NewMetricsCollector(metricsCfg)

	// Initialize the admission controller (#12)
	admissionCfg := DefaultAdmissionConfig()
	admission := NewAdmissionController(admissionCfg, metrics)

	// Expose metrics and autoscaler to the router for TUI access
	router.SetMetrics(metrics)

	// Initialize the autoscaler with production defaults (#1,#3,#4,#8,#9,#10,#11,#14)
	autoscalerCfg := DefaultAutoscalerConfig()
	autoscalerCfg.CallbackBaseURL = masterBaseURL()
	autoscaler := NewAutoscaler(autoscalerCfg, router, metrics)
	router.SetAutoscaler(autoscaler)

	http.HandleFunc("/chat", func(w http.ResponseWriter, r *http.Request) {
		chatRequestHandler(w, r, router, admission, metrics)
	})

	http.HandleFunc("/agents/register", func(w http.ResponseWriter, r *http.Request) {
		agentRegisterHandler(w, r, router, autoscalerCfg.CallbackBaseURL)
	})

	// Async callback: agent POSTs here when a worker container is ready
	http.HandleFunc("/workers/ready", func(w http.ResponseWriter, r *http.Request) {
		workerReadyHandler(w, r, router)
	})

	go autoscaler.Run()
	go healthCheckLoop(router)
	go admissionUpdateLoop(router, admission)

	monitoring.Event("server", fmt.Sprintf("LB listening on :8080 (callback base: %s)", autoscalerCfg.CallbackBaseURL))
	if err := http.ListenAndServe(":8080", nil); err != nil {
		monitoring.Event("server", "ListenAndServe fatal: "+err.Error())
	}
}

// ---------------------------------------------------------------------------
// /chat
// ---------------------------------------------------------------------------

func chatRequestHandler(w http.ResponseWriter, r *http.Request, router *Router, admission *AdmissionController, metrics *MetricsCollector) {
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
	metrics.RecordRequest()

	// Admission control (#12)
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

	requestID, err := uuid.NewV7()
	if err != nil {
		monitoring.Verbose("server", "failed to generate request id: "+err.Error())
		http.Error(w, "failed to assign request id", http.StatusInternalServerError)
		metrics.RecordError()
		return
	}
	requestIDStr := requestID.String()
	monitoring.Verbose("server", "assigned requestId="+requestIDStr)

	start := time.Now()

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

	metrics.RecordLatency(time.Since(start).Seconds())
	monitoring.Verbose("server", "request completed, reqId="+resp.RequestID+" reply="+resp.Reply)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// ---------------------------------------------------------------------------
// /agents/register
// ---------------------------------------------------------------------------

func agentRegisterHandler(w http.ResponseWriter, r *http.Request, router *Router, callbackBase string) {
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

	// Re-adopt any surviving workers the agent reported
	adopted := 0
	for _, rw := range req.Workers {
		if rw.WorkerID == "" {
			continue
		}
		if router.WorkerExists(rw.WorkerID) {
			monitoring.Verbose("server", "worker "+rw.WorkerID+" already registered, skipping")
			continue
		}
		wk, err := NewWorker(rw.WorkerID, rw.Address)
		if err != nil {
			monitoring.Verbose("server", "failed to re-adopt worker "+rw.WorkerID+": "+err.Error())
			continue
		}
		wk.SetAgentID(req.AgentID)
		router.AddWorkerWithInstance(wk)
		adopted++
		monitoring.Event("server", fmt.Sprintf("re-adopted surviving worker %s (%s) from agent %s", rw.WorkerID, rw.Address, req.AgentID))
	}

	// Only spawn a new worker if no surviving workers were reported
	if len(req.Workers) == 0 {
		go fireSpawnRequest(req.AgentID, fmt.Sprintf("http://%s:%d", req.Host, req.Port), callbackBase, router)
	} else {
		monitoring.Verbose("server", fmt.Sprintf(
			"agent %s reported %d surviving workers, adopted %d — skipping initial spawn",
			req.AgentID, len(req.Workers), adopted,
		))
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "registered"})
}

// ---------------------------------------------------------------------------
// /workers/ready  — async callback from the agent
// ---------------------------------------------------------------------------

func workerReadyHandler(w http.ResponseWriter, r *http.Request, router *Router) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var note WorkerReadyNotification
	if err := json.NewDecoder(r.Body).Decode(&note); err != nil {
		monitoring.Verbose("server", "invalid worker-ready JSON: "+err.Error())
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	monitoring.Verbose("server", fmt.Sprintf(
		"worker-ready callback: worker_id=%s address=%s agent=%s",
		note.WorkerID, note.Address, note.AgentID,
	))

	// Register the worker — the port is already open so NewWorker connects quickly
	workerID := note.WorkerID
	if workerID == "" {
		workerID = "worker-" + note.Address
	}
	wk, err := NewWorker(workerID, note.Address)
	if err != nil {
		monitoring.Verbose("server", "worker-ready: gRPC connect failed for "+note.Address+": "+err.Error())
		http.Error(w, "gRPC connect failed: "+err.Error(), http.StatusBadGateway)
		return
	}

	wk.SetAgentID(note.AgentID)
	router.AddWorkerWithInstance(wk)

	monitoring.Event("server", fmt.Sprintf("worker %s registered via callback (agent: %s)", note.Address, note.AgentID))
	monitoring.Verbose("server", "worker "+note.Address+" added via callback, agent="+note.AgentID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "registered", "worker_id": workerID})
}

// ---------------------------------------------------------------------------
// Fire-and-forget spawn helpers (used by registration path and autoscaler)
// ---------------------------------------------------------------------------

// fireSpawnRequest sends a /workers/create to the agent with a callback URL.
// It returns after the agent acknowledges receipt (202) — not after the worker is ready.
// Retries up to 3 times with 5s backoff on network errors.
func fireSpawnRequest(agentID, agentAddr, callbackBase string, router *Router) {
	callbackURL := callbackBase + "/workers/ready"
	monitoring.Event("server", fmt.Sprintf("firing spawn request to %s (callback: %s)", agentAddr, callbackURL))

	body, _ := json.Marshal(map[string]string{
		"callback_url": callbackURL,
		"agent_id":     agentID,
	})

	maxAttempts := 3
	backoff := 5 * time.Second
	client := http.Client{Timeout: 10 * time.Second} // just delivery ack

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		resp, err := client.Post(agentAddr+"/workers/create", "application/json", bytes.NewReader(body))
		if err != nil {
			monitoring.Verbose("server", fmt.Sprintf(
				"spawn fire attempt %d/%d to %s failed: %v", attempt, maxAttempts, agentID, err,
			))
			if attempt < maxAttempts {
				time.Sleep(backoff)
				backoff *= 2
			}
			continue
		}
		if resp.StatusCode == http.StatusAccepted || (resp.StatusCode >= 200 && resp.StatusCode < 300) {
			var result struct {
				WorkerID string `json:"worker_id"`
				Address  string `json:"address"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&result); err == nil && result.WorkerID != "" {
				w := NewStartingWorker(result.WorkerID, result.Address, agentID)
				router.AddWorkerWithInstance(w)
			}
			resp.Body.Close()
			monitoring.Verbose("server", fmt.Sprintf(
				"spawn request accepted by agent %s (status %d) — waiting for callback",
				agentID, resp.StatusCode,
			))
			return
		}
		resp.Body.Close()

		monitoring.Verbose("server", fmt.Sprintf(
			"spawn fire attempt %d/%d to %s got status %d", attempt, maxAttempts, agentID, resp.StatusCode,
		))
		if attempt < maxAttempts {
			time.Sleep(backoff)
			backoff *= 2
		}
	}

	monitoring.Event("server", fmt.Sprintf("all spawn fire attempts failed for agent %s", agentID))
}

// ---------------------------------------------------------------------------
// Background loops
// ---------------------------------------------------------------------------

func healthCheckLoop(router *Router) {
	for {
		time.Sleep(5 * time.Second)

		router.workersM.RLock()
		workersToPing := make([]*Worker, 0, len(router.workers))
		for _, w := range router.workers {
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

func admissionUpdateLoop(router *Router, admission *AdmissionController) {
	for {
		time.Sleep(2 * time.Second)
		healthyCount := router.HealthyWorkerCount()
		admission.UpdateLimits(healthyCount)
	}
}
