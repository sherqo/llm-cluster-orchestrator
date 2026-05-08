package main

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

var ollamaCmd *exec.Cmd

func isOllamaAlive(ollamaURL string) bool {
	resp, err := http.Get(ollamaURL + "/api/tags")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func startOllama(cfg AgentConfig) error {
	if cfg.OllamaURL == "" {
		return nil
	}

	if isOllamaAlive(cfg.OllamaURL) {
		Verbose("ollama", "already running at "+cfg.OllamaURL)
		return nil
	}

	parsed, err := url.Parse(cfg.OllamaURL)
	if err != nil {
		return fmt.Errorf("invalid ollama url: %w", err)
	}
	if parsed.Host == "" {
		return fmt.Errorf("invalid ollama url host: %s", cfg.OllamaURL)
	}

	logsDir := "logs"
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		return err
	}

	logPath := filepath.Join(logsDir, "ollama_"+cfg.AgentID+".log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}

	cmd := exec.Command("ollama", "serve")
	cmd.Env = append(
		os.Environ(),
		"OLLAMA_HOST="+parsed.Host,
		"OLLAMA_NUM_PARALLEL=32",
		"OLLAMA_MAX_LOADED_MODELS=1",
	)
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	Verbose("ollama", "starting ollama with host "+parsed.Host)
	if err := cmd.Start(); err != nil {
		return err
	}
	ollamaCmd = cmd

	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		if isOllamaAlive(cfg.OllamaURL) {
			Verbose("ollama", "ready at "+cfg.OllamaURL)
			return nil
		}
		time.Sleep(1 * time.Second)
	}

	log.Printf("ollama did not become ready in time, check %s", logPath)
	return fmt.Errorf("ollama not ready at %s", cfg.OllamaURL)
}
