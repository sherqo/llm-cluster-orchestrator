package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
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

		Verbose("worker", "created worker "+resp.WorkerID+" at "+resp.Address)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
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
