package main

import (
	"flag"
	"fmt"
	"os"

	"client/tui"
	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	masterURL := flag.String("master-url", envOrDefault("MASTER_URL", "http://localhost:8080"), "master base URL")
	userID := flag.String("user-id", envOrDefault("USER_ID", "cli-user"), "user ID")
	tier := flag.String("tier", envOrDefault("TIER", "standard"), "request tier")
	flag.Parse()

	cfg := tui.Config{
		MasterURL: *masterURL,
		UserID:    *userID,
		Tier:      *tier,
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
