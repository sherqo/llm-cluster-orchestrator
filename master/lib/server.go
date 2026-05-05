package lib

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/google/uuid"
)

type ChatRequest struct {
	UserID string `json:"userId"`
	Prompt string `json:"prompt"`
	Tier   string `json:"tier"`
}

type ChatResponse struct {
	RequestID string `json:"requestId"`
	Reply     string `json:"reply"`
}

func Serve(router *Router) {
	http.HandleFunc("/chat", func(w http.ResponseWriter, r *http.Request) {
		chatRequestHandler(w, r, router)
	})
	log.Println("LB listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

// TODO: this crap is just here for now, but it should be more trasparent with fault tolerance
func chatRequestHandler(w http.ResponseWriter, r *http.Request, router *Router) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Verbose("server", "invalid JSON from client")
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	Verbose("server", "received chat request, userId="+req.UserID+", tier="+req.Tier)

	// generate a unique request ID 
	requestID, err := uuid.NewV7()
	if err != nil {
		Verbose("server", "failed to generate request id: "+err.Error())
		http.Error(w, "failed to assign request id", http.StatusInternalServerError)
		return
	}
	requestIDStr := requestID.String()
	Verbose("server", "assigned requestId="+requestIDStr)

	resp, err := router.HandleChat(r.Context(), requestIDStr, req)
	if err != nil {
		switch {
		case errors.Is(err, ErrNoWorkersAvailable):
			Verbose("server", "no workers available")
			http.Error(w, "no workers available", http.StatusServiceUnavailable)
		case errors.Is(err, ErrWorkerFailed):
			Verbose("server", err.Error())
			http.Error(w, err.Error(), http.StatusBadGateway)
		default:
			Verbose("server", err.Error())
			http.Error(w, "internal server error", http.StatusInternalServerError)
		}
		return
	}

	Verbose("server", "request completed, reqId="+resp.RequestID+" reply="+resp.Reply)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
