package monitoring

import (
	"fmt"
	"sync"
	"time"
)

type LogEntry struct {
	Time  time.Time
	Place string
	Msg   string
}

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
