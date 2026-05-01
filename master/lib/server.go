package lib

import (
	"encoding/json"
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

	worker, err := router.Pick(req)
	if err != nil {
		Verbose("server", "no workers available")
		http.Error(w, "no workers available", http.StatusServiceUnavailable)
		return
	}

	Verbose("server", "picked worker="+worker.addr)

	requestID, err := uuid.NewV7()
	if err != nil {
		Verbose("server", "failed to generate UUID: "+err.Error())
		http.Error(w, "failed to assign request id", http.StatusInternalServerError)
		return
	}

	requestIDStr := requestID.String()
	Verbose("server", "assigned requestId="+requestIDStr)

	router.AddInFlight(requestIDStr, worker.addr)
	defer router.RemoveInFlight(requestIDStr)
	Verbose("server", "added in-flight: reqId="+requestIDStr+" worker="+worker.addr)

	reply, err := worker.Send(r.Context(), requestIDStr, req)
	if err != nil {
		Verbose("server", "worker failed: "+err.Error())
		http.Error(w, "worker failed: "+err.Error(), http.StatusBadGateway)
		return
	}

	Verbose("server", "request completed, reqId="+requestIDStr+" reply="+reply)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ChatResponse{
		RequestID: requestIDStr,
		Reply:     reply,
	})
}
