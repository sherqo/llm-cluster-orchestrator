package loadtui

import "time"

type Config struct {
	MasterURL  string
	UserIDBase string
	FreePct    int
	PaidTier   string
	BurstCount int
	RateCount  int
	RateEvery  time.Duration
}

type LogEntry struct {
	At         time.Time
	UserID     string
	Tier       string
	Prompt     string
	ReplyShort string
	Latency    time.Duration
	Status     string
	Error      string
}

type Counters struct {
	Waiting int
	OK      int
	Failed  int
	Total   int
}

type LatencyStats struct {
	Count int
	Min   time.Duration
	Max   time.Duration
	Avg   time.Duration
	P50   time.Duration
	P95   time.Duration
	P99   time.Duration
	MinMs float64
	MaxMs float64
	AvgMs float64
	P50Ms float64
	P95Ms float64
	P99Ms float64
}

type RateStats struct {
	SentPerSec     float64
	CompletedPerSec float64
	SentPerSecOverall     float64
	CompletedPerSecOverall float64
	WindowSeconds          float64
}

type SystemStats struct {
	StartTime      time.Time
	EndTime        time.Time
	Duration       time.Duration
	DurationSeconds float64
	RequestsSent   int
	RequestsOK     int
	RequestsFailed int
	InFlightMax    int
	Latency        LatencyStats
	Rate           RateStats
	FreePct        int
	PaidTier       string
	BurstCount     int
	RateCount      int
	RateEvery      time.Duration
}
