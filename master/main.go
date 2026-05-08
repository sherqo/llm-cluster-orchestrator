/*
* The LB should listen to http in some port and then should try to response for it
* the request flow should be:
  * client calls the LB via HTTP
	* the LB should assign an id to this request and push the request to the DB non-blockingly
	* then the LB should figure out what worker node withh take this request
	* then it should add this info to the in-memory registry (worker, reqeustId)
	* the assigned worker node should finish the work and send back to the LB
	* the LB then needs to figure out how to send it back via the requestId and also save the response to the DB
	* and remove the requestId from the in-memory registry

* the previous flow is for the normal case, but we also need to consider the failure cases:
*/

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"master/lib"
)

func main() {
	router := lib.NewRouter()

	// auto-verify workers at startup
	workerPorts := []string{"localhost:50051", "localhost:50052", "localhost:50053"}
	for _, addr := range workerPorts {
		workerID := "worker-" + addr
		w, err := lib.NewWorker(workerID, addr)
		if err != nil {
			log.Printf("worker %s not available: %v", addr, err)
			continue
		}
		router.AddWorkerWithInstance(w)
		log.Printf("worker %s verified and added", addr)
	}

	if router.WorkerCount() == 0 {
		fmt.Println("no workers available at startup")
	}

	// start a goroutine to create a worker from registered agent after 10 seconds
	go func() {
		time.Sleep(10 * time.Second)
		tryCreateWorkerFromAgent(router)
	}()

	lib.Serve(router)
}

func tryCreateWorkerFromAgent(router *lib.Router) {
	agents := router.GetAgents()
	if len(agents) == 0 {
		log.Println("no agents registered, skipping worker creation")
		return
	}

	agent := agents[0]
	agentAddr := fmt.Sprintf("http://%s:%d", agent.Host, agent.Port)
	log.Printf("requesting worker from agent at %s", agentAddr)

	resp, err := http.Post(agentAddr+"/workers/create", "application/json", bytes.NewReader([]byte("{}")))
	if err != nil {
		log.Printf("failed to create worker: %v", err)
		return
	}
	defer resp.Body.Close()

	var result struct {
		WorkerID string `json:"worker_id"`
		Address  string `json:"address"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Printf("failed to decode worker response: %v", err)
		return
	}

	log.Printf("worker created: %s at %s", result.WorkerID, result.Address)

	// verify worker via gRPC
	workerID := "worker-" + result.Address
	w, err := lib.NewWorker(workerID, result.Address)
	if err != nil {
		log.Printf("failed to verify worker: %v", err)
		return
	}

	router.AddWorkerWithInstance(w)
	log.Printf("worker %s verified and added via gRPC", result.Address)
}
