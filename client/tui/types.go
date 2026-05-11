package tui

import (
	"time"
)

// Role identifies who produced a message.
type Role int

const (
	RoleUser Role = iota
	RoleAssistant
	RoleError
	RoleSystem
)

// Message is a single entry in the conversation history.
type Message struct {
	Role      Role
	Content   string
	RequestID string    // populated for assistant/error messages
	At        time.Time // wall-clock time the message arrived
}

// PerfStats tracks end-user perceived performance.
type PerfStats struct {
	Total     int
	OK        int
	Errors    int
	Latencies []time.Duration
	Last      time.Duration
	LastState string // "ok" or "err"
}

// Session represents a named conversation.
type Session struct {
	ID        string
	Title     string
	Messages  []Message
	CreatedAt time.Time
}

// newSession creates a new session with a generated ID.
func newSession(id, title string) Session {
	return Session{
		ID:        id,
		Title:     title,
		CreatedAt: time.Now(),
	}
}
