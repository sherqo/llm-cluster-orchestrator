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
	http.HandleFunc("/chat", func(w http.ResponseWriter, r *http.Request) {
		chatRequestHandler(w, r, router)
	})

	http.HandleFunc("/agents/register", func(w http.ResponseWriter, r *http.Request) {
		agentRegisterHandler(w, r, router)
	})

	go autoscaleLoop(router)
	go healthCheckLoop(router)

	log.Println("LB listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

// TODO: this crap is just here for now, but it should be more trasparent with fault tolerance
// TODO: go routine for async stuff instead of blocking each other
// first LB flow
func chatRequestHandler(w http.ResponseWriter, r *http.Request, router *Router) {
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

	// generate a unique request ID
	requestID, err := uuid.NewV7()
	if err != nil {
		monitoring.Verbose("server", "failed to generate request id: "+err.Error())
		http.Error(w, "failed to assign request id", http.StatusInternalServerError)
		return
	}
	requestIDStr := requestID.String()
	monitoring.Verbose("server", "assigned requestId="+requestIDStr)

	// handle the request and send errors
	resp, err := router.HandleChat(r.Context(), requestIDStr, req)
	if err != nil {
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

func spawnWorkerForAgent(router *Router, req AgentRegisterRequest) {
	agentAddr := fmt.Sprintf("http://%s:%d", req.Host, req.Port)
	log.Printf("requesting worker from agent at %s", agentAddr)

	client := http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(agentAddr+"/workers/create", "application/json", bytes.NewReader([]byte("{}")))
	if err != nil {
		log.Printf("failed to create worker from agent: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		log.Printf("agent worker creation failed with status %d", resp.StatusCode)
		return
	}

	var result struct {
		WorkerID string `json:"worker_id"`
		Address  string `json:"address"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Printf("failed to decode worker response: %v", err)
		return
	}

	log.Printf("worker created: %s at %s", result.WorkerID, result.Address)

	workerID := "worker-" + result.Address
	w, err := NewWorker(workerID, result.Address)
	if err != nil {
		log.Printf("failed to verify worker: %v", err)
		return
	}

	router.AddWorkerWithInstance(w)
	log.Printf("worker %s verified and added via gRPC", result.Address)
}

func autoscaleLoop(router *Router) {
	for {
		time.Sleep(5 * time.Second)

		inFlight := router.InFlightCount()
		workerCount := router.WorkerCount()
		
		// If no workers, we can't autoscale properly unless we have agents
		if workerCount == 0 {
			continue
		}

		// Calculate load: if we have more than 10 inflight per worker, we need more workers
		if inFlight > workerCount * 10 {
			monitoring.Verbose("autoscale", fmt.Sprintf("high load detected: %d inflight, %d workers", inFlight, workerCount))
			
			agents := router.GetAgents()
			if len(agents) > 0 {
				// Pick the first agent for simplicity in scaling
				agent := agents[0]
				monitoring.Verbose("autoscale", "requesting new worker from agent "+agent.AgentID)
				
				// Create a fake request to reuse spawnWorkerForAgent
				req := AgentRegisterRequest{
					AgentID: agent.AgentID,
					Host:    agent.Host,
					Port:    agent.Port,
				}
				go spawnWorkerForAgent(router, req)
			}
		}
	}
}

func healthCheckLoop(router *Router) {
	for {
		time.Sleep(5 * time.Second)

		router.workersM.RLock()
		workersToPing := make([]*Worker, 0, len(router.workers))
		for _, w := range router.workers {
			workersToPing = append(workersToPing, w)
		}
		router.workersM.RUnlock()

		for _, w := range workersToPing {
			err := w.Ping()
			if err != nil {
				monitoring.Verbose("health", "worker "+w.ID()+" ping failed: "+err.Error())
				w.SetDraining()
			} else {
				w.MarkHealthy()
			}
		}
	}
}
