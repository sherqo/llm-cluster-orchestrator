package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"client/loadtest/loadtui"
	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	masterURL := flag.String("master-url", envOrDefault("MASTER_URL", "http://localhost:8080"), "master base URL")
	userID := flag.String("user-id", envOrDefault("USER_ID", "load-user"), "base user ID")
	freePercent := flag.Int("free-percent", envOrDefaultInt("FREE_PERCENT", 70), "percent of free users (0-100)")
	paidTier := flag.String("paid-tier", envOrDefault("PAID_TIER", "pro"), "paid tier: pro or elite")
	burstCount := flag.Int("burst", envOrDefaultInt("BURST", 50), "requests per burst")
	rateCount := flag.Int("rate", envOrDefaultInt("RATE", 10), "requests per tick when rate mode on")
	rateEvery := flag.Duration("rate-every", envOrDefaultDuration("RATE_EVERY", time.Second), "tick interval (e.g. 1s, 500ms)")
	flag.Parse()

	cfg := loadtui.Config{
		MasterURL:  *masterURL,
		UserIDBase: *userID,
		FreePct:    clampPct(*freePercent),
		PaidTier:   *paidTier,
		BurstCount: max(1, *burstCount),
		RateCount:  max(1, *rateCount),
		RateEvery:  *rateEvery,
	}

	program := tea.NewProgram(
		loadtui.New(cfg),
		tea.WithAltScreen(),
	)

	finalModel, err := program.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	model, ok := finalModel.(loadtui.Model)
	if !ok {
		return
	}

	report := model.Report()
	if report == nil {
		return
	}

	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "report marshal error: %v\n", err)
		return
	}

	if err := os.WriteFile("./client-load-report.json", data, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "report write error: %v\n", err)
		return
	}

	fmt.Println(string(data))
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envOrDefaultInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		var out int
		if _, err := fmt.Sscanf(v, "%d", &out); err == nil {
			return out
		}
	}
	return fallback
}

func envOrDefaultDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if out, err := time.ParseDuration(v); err == nil {
			return out
		}
	}
	return fallback
}

func clampPct(v int) int {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
