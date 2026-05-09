package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"runtime"
	"strconv"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/mem"
)

func SystemInfoHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	cpuPercent, _ := cpu.Percent(0, false)
	memInfo, _ := mem.VirtualMemory()

	totalMemMB := memInfo.Total / 1024 / 1024
	usedMemMB := (memInfo.Total - memInfo.Available) / 1024 / 1024

	resp := map[string]any{
		"os":         runtime.GOOS,
		"cpu_usage":  fmt.Sprintf("%.1f/100", cpuPercent[0]),
		"memory_mb":  fmt.Sprintf("%d/%d", usedMemMB, totalMemMB),
	}

	Verbose("system", "cpu: "+resp["cpu_usage"].(string)+", memory: "+resp["memory_mb"].(string))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func CreateWorkerHandler(
	docker *DockerManager,
	masterURL string,
	agentID string,
) http.HandlerFunc {

	return func(
		w http.ResponseWriter,
		r *http.Request,
	) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req CreateWorkerRequest

		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil && err != io.EOF {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		Verbose("worker", "creating worker")

		resp, err := docker.CreateWorker(req)
		if err != nil {
			Verbose("worker", "failed: "+err.Error())
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		Verbose("worker", "container started: "+resp.WorkerID+" at "+resp.Address)

		// Use master URL from config as priority, only fallback to request's callback URL if config is empty
		callbackURL := ""
		if masterURL != "" {
			callbackURL = masterURL + "/workers/ready"
		} else if req.CallbackURL != "" {
			callbackURL = req.CallbackURL
		}
		Verbose("worker", "final callbackURL: "+callbackURL)

		// If a callback URL is available, asynchronously wait for the worker
		// gRPC port to be ready, then POST the result back to the master.
		if callbackURL != "" {
			go waitAndCallback(resp, callbackURL, agentID)

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			json.NewEncoder(w).Encode(map[string]string{
				"status":    "accepted",
				"worker_id": resp.WorkerID,
				"address":   resp.Address,
			})
			return
		}

		// Legacy: synchronous response (no callback)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

// waitAndCallback polls until the worker's gRPC port is reachable (max 100s),
// then POSTs the worker-ready notification to the master's callback URL.
func waitAndCallback(resp CreateWorkerResponse, callbackURL, agentID string) {
	Verbose("worker", "waiting for worker "+resp.WorkerID+" to be ready at "+resp.Address)

	if err := waitForPort(resp.Address, 100*time.Second); err != nil {
		Verbose("worker", "worker "+resp.WorkerID+" never became ready: "+err.Error())
		return
	}

	Verbose("worker", "worker "+resp.WorkerID+" is ready, notifying master")

	payload, err := json.Marshal(map[string]string{
		"worker_id":    resp.WorkerID,
		"address":      resp.Address,
		"agent_id":     agentID,
		"container_id": resp.ContainerID,
	})
	if err != nil {
		Verbose("worker", "failed to marshal callback payload: "+err.Error())
		return
	}

	client := http.Client{Timeout: 10 * time.Second}
	httpResp, err := client.Post(callbackURL, "application/json", bytes.NewReader(payload))
	if err != nil {
		Verbose("worker", "callback to master failed: "+err.Error())
		return
	}
	defer httpResp.Body.Close()

	Verbose("worker", fmt.Sprintf("callback sent for %s, master responded %d", resp.WorkerID, httpResp.StatusCode))
}

// waitForPort TCP-polls addr (host:port) until it accepts connections or timeout elapses.
func waitForPort(addr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	backoff := 500 * time.Millisecond

	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
		if err == nil {
			conn.Close()
			return nil
		}
		time.Sleep(backoff)
		if backoff < 5*time.Second {
			backoff += 500 * time.Millisecond
		}
	}
	return fmt.Errorf("port %s not reachable after %s", addr, timeout)
}

func CleanWorkerHandler(
	docker *DockerManager,
) http.HandlerFunc {

	return func(
		w http.ResponseWriter,
		r *http.Request,
	) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		Verbose("clean", "cleaning all workers with image "+docker.cfg.WorkerImage)

		count, err := docker.CleanWorkers()
		if err != nil {
			Verbose("clean", "failed: "+err.Error())
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		Verbose("clean", "removed "+strconv.Itoa(count)+" containers")

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]int{"removed": count})
	}
}

func DestroyWorkerHandler(
	docker *DockerManager,
) http.HandlerFunc {
	return func(
		w http.ResponseWriter,
		r *http.Request,
	) {
		if r.Method != http.MethodDelete {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		workerID := r.URL.Query().Get("worker_id")
		if workerID == "" {
			http.Error(w, "worker_id is required", http.StatusBadRequest)
			return
		}

		Verbose("worker", "destroying worker "+workerID)

		err := docker.DestroyWorker(workerID)
		if err != nil {
			Verbose("worker", "failed to destroy: "+err.Error())
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		Verbose("worker", "destroyed worker "+workerID)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "destroyed"})
	}
}

func RegisterWithMaster(cfg AgentConfig) error {
	body, err := json.Marshal(AgentRegistrationRequest{
		AgentID: cfg.AgentID,
		Host:    cfg.AdvertiseHost,
		Port:    cfg.AdvertisePort,
	})
	if err != nil {
		return err
	}

	Verbose("register", "registering agent "+cfg.AgentID+" with master "+cfg.MasterURL)

	client := http.Client{Timeout: 5 * time.Second}
	maxAttempts := 6
	backoff := 1 * time.Second

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		resp, err := client.Post(
			cfg.MasterURL+"/agents/register",
			"application/json",
			bytes.NewReader(body),
		)
		if err == nil {
			if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
				resp.Body.Close()
				Verbose("register", "registered agent "+cfg.AgentID+" with master "+cfg.MasterURL)
				log.Printf("registered agent %s with master %s", cfg.AgentID, cfg.MasterURL)
				return nil
			}
			resp.Body.Close()
			err = fmt.Errorf("master registration failed with status %d", resp.StatusCode)
		}

		if attempt == maxAttempts {
			Verbose("register", "failed: "+err.Error())
			return err
		}

		Verbose("register", fmt.Sprintf("retry %d/%d after error: %v", attempt, maxAttempts, err))
		time.Sleep(backoff)
		backoff *= 2
	}

	return fmt.Errorf("master registration failed after retries")
}
