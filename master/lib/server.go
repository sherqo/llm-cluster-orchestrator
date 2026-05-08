package lib

import (
	"encoding/json"
	"errors"
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
	Addr   string  `json:"addr"`
	Weight float64 `json:"weight"`
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
		Verbose("server", "invalid agent registration JSON")
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	Verbose("server", "agent registration: "+req.AgentID+" at "+req.Host+":"+strconv.Itoa(req.Port))

	router.RegisterAgent(AgentInfo{
		AgentID: req.AgentID,
		Host:    req.Host,
		Port:    req.Port,
		AddedAt: time.Now(),
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "registered"})
}
