package monitoring

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// ── LogEntry ──────────────────────────────────────────────────────────────────

type LogEntry struct {
	Time  time.Time
	Place string
	Msg   string
}

// ── Verbose log (internal trace) ──────────────────────────────────────────────

var verboseEnabled = true
var stdoutEnabled = false

var verboseMu sync.Mutex
var logEntries = make([]LogEntry, 0, 2048)

func SetVerboseEnabled(enabled bool) {
	verboseMu.Lock()
	defer verboseMu.Unlock()
	verboseEnabled = enabled
}

func SetStdoutEnabled(enabled bool) {
	verboseMu.Lock()
	defer verboseMu.Unlock()
	stdoutEnabled = enabled
}

func LogSnapshot(limit int) []LogEntry {
	verboseMu.Lock()
	defer verboseMu.Unlock()

	if limit <= 0 || limit >= len(logEntries) {
		out := make([]LogEntry, len(logEntries))
		copy(out, logEntries)
		return out
	}

	start := len(logEntries) - limit
	out := make([]LogEntry, len(logEntries[start:]))
	copy(out, logEntries[start:])
	return out
}

func Verbose(place, msg string) {
	entry := LogEntry{Time: time.Now(), Place: place, Msg: msg}

	verboseMu.Lock()
	defer verboseMu.Unlock()

	if !verboseEnabled {
		return
	}

	logEntries = append(logEntries, entry)
	if len(logEntries) > 4000 {
		logEntries = append([]LogEntry(nil), logEntries[len(logEntries)-2000:]...)
	}

	if stdoutEnabled {
		fmt.Printf("[%s] %s\n", place, msg)
	}
}

// ── Server Events (important notifications) ───────────────────────────────────
// These are high-level events like "worker registered", "agent connected" etc.
// They appear in the dedicated Notifications tab in the TUI.

var eventsMu sync.Mutex
var eventEntries = make([]LogEntry, 0, 512)

// Event records a high-level server notification (shown in the Notifications tab).
func Event(place, msg string) {
	entry := LogEntry{Time: time.Now(), Place: place, Msg: msg}

	eventsMu.Lock()
	eventEntries = append(eventEntries, entry)
	if len(eventEntries) > 1000 {
		eventEntries = append([]LogEntry(nil), eventEntries[len(eventEntries)-500:]...)
	}
	eventsMu.Unlock()

	// Also mirror into verbose log
	Verbose(place, msg)
}

// EventsSnapshot returns the last `limit` event entries.
func EventsSnapshot(limit int) []LogEntry {
	eventsMu.Lock()
	defer eventsMu.Unlock()

	if limit <= 0 || limit >= len(eventEntries) {
		out := make([]LogEntry, len(eventEntries))
		copy(out, eventEntries)
		return out
	}
	start := len(eventEntries) - limit
	out := make([]LogEntry, len(eventEntries[start:]))
	copy(out, eventEntries[start:])
	return out
}

// ── StdLogWriter ──────────────────────────────────────────────────────────────
// Implements io.Writer so that log.SetOutput(monitoring.NewStdLogWriter()) will
// capture all log.Printf/log.Fatal output into the TUI instead of printing to
// the terminal and breaking the layout.

type StdLogWriter struct{}

func NewStdLogWriter() *StdLogWriter { return &StdLogWriter{} }

func (w *StdLogWriter) Write(p []byte) (n int, err error) {
	line := strings.TrimRight(string(p), "\n\r")
	if line == "" {
		return len(p), nil
	}

	// Parse out the bracketed place tag if present: "[server] some msg"
	place := "system"
	msg := line

	// Strip leading timestamp that log package adds (e.g. "2006/01/02 15:04:05 ")
	// Format is: "YYYY/MM/DD HH:MM:SS message"
	if len(line) > 20 && line[4] == '/' && line[7] == '/' {
		msg = strings.TrimLeft(line[20:], " ")
	}

	if strings.HasPrefix(msg, "[") {
		end := strings.Index(msg, "]")
		if end > 0 {
			place = msg[1:end]
			msg = strings.TrimLeft(msg[end+1:], " ")
		}
	}

	Event(place, msg)
	return len(p), nil
}
