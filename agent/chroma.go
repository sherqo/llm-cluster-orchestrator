package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

func isChromaAlive(chromaURL string) bool {
	resp, err := http.Get(chromaURL + "/api/v1/heartbeat")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func startChroma(cfg AgentConfig) error {
	if cfg.ChromaURL == "" {
		return nil
	}

	if isChromaAlive(cfg.ChromaURL) {
		Verbose("chroma", "already running at "+cfg.ChromaURL)
		return nil
	}

	if _, err := exec.LookPath("docker"); err != nil {
		return fmt.Errorf("docker is required to start chroma")
	}

	if started, err := startChromaDocker(cfg); err != nil {
		return err
	} else if !started {
		return fmt.Errorf("vector-db docker compose not found")
	}

	return waitForChroma(cfg)
}

func waitForChroma(cfg AgentConfig) error {
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		if isChromaAlive(cfg.ChromaURL) {
			Verbose("chroma", "ready at "+cfg.ChromaURL)
			return nil
		}
		time.Sleep(1 * time.Second)
	}

	log.Printf("chroma did not become ready in time")
	return fmt.Errorf("chroma not ready at %s", cfg.ChromaURL)
}

func startChromaDocker(cfg AgentConfig) (bool, error) {
	vectorDir, ok := findVectorDBDir()
	if !ok {
		return false, nil
	}

	Verbose("chroma", "starting via docker compose in "+vectorDir)

	// Try docker compose (V2) first, then fall back to docker-compose (V1)
	cmds := [][]string{
		{"docker", "compose", "up", "-d", "--build"},
		{"docker-compose", "up", "-d", "--build"},
	}

	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = vectorDir
		output, err := cmd.CombinedOutput()
		if err == nil {
			Verbose("chroma", "started with "+args[0]+" "+args[1])
			return true, nil
		}
		Verbose("chroma", args[0]+" failed: "+string(output))
	}

	return false, fmt.Errorf("docker compose failed")
}

func findVectorDBDir() (string, bool) {
	wd, err := os.Getwd()
	if err != nil {
		return "", false
	}

	paths := []string{
		filepath.Join(wd, "vector-db"),
		filepath.Join(wd, "..", "vector-db"),
		filepath.Join(wd, "..", "..", "vector-db"),
	}

	for _, p := range paths {
		if fileExists(filepath.Join(p, "docker-compose.yaml")) {
			return p, true
		}
	}

	return "", false
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}
