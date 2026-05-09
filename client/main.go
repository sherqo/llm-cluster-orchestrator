package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"client/tui"
)

func main() {
	cfg := tui.Config{
		MasterURL: envOrDefault("MASTER_URL", "http://localhost:8080"),
		UserID:    envOrDefault("USER_ID", "cli-user"),
		Tier:      envOrDefault("TIER", "standard"),
	}

	p := tea.NewProgram(
		tui.New(cfg),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
