package main

import (
	"encoding/json"
	"io"
	"net/http"
	"runtime"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/mem"
)

func SystemInfoHandler(
	w http.ResponseWriter,
	r *http.Request,
) {

	cpuPercent, _ := cpu.Percent(0, false)
	memInfo, _ := mem.VirtualMemory()

	resp := map[string]any{
		"os":        runtime.GOOS,
		"cpu_usage": cpuPercent[0],
		"memory_mb": memInfo.Available / 1024 / 1024,
	}

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

		resp, err := docker.CreateWorker(req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}
