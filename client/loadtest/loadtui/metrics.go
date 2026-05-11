package loadtui

import (
	"math"
	"sort"
	"time"
)

type Metrics struct {
	start         time.Time
	sent          int
	ok            int
	fail          int
	waiting       int
	maxWaiting    int
	latencies     []time.Duration
	lastSentAt    time.Time
	lastDoneAt    time.Time
	sentWindow    []time.Time
	completedWindow []time.Time
}

func NewMetrics() *Metrics {
	return &Metrics{start: time.Now()}
}

func (m *Metrics) MarkSent() {
	m.sent++
	m.waiting++
	if m.waiting > m.maxWaiting {
		m.maxWaiting = m.waiting
	}
	m.lastSentAt = time.Now()
	m.sentWindow = append(m.sentWindow, m.lastSentAt)
}

func (m *Metrics) MarkOK(latency time.Duration) {
	m.ok++
	if m.waiting > 0 {
		m.waiting--
	}
	m.latencies = append(m.latencies, latency)
	m.lastDoneAt = time.Now()
	m.completedWindow = append(m.completedWindow, m.lastDoneAt)
}

func (m *Metrics) MarkFail(latency time.Duration) {
	m.fail++
	if m.waiting > 0 {
		m.waiting--
	}
	m.latencies = append(m.latencies, latency)
	m.lastDoneAt = time.Now()
	m.completedWindow = append(m.completedWindow, m.lastDoneAt)
}

func (m *Metrics) Counters() Counters {
	return Counters{
		Waiting: m.waiting,
		OK:      m.ok,
		Failed:  m.fail,
		Total:   m.sent,
	}
}

func (m *Metrics) Snapshot(cfg Config) SystemStats {
	end := time.Now()
	lat := computeLatency(m.latencies)
	rate := computeRates(m.sentWindow, m.completedWindow, m.sent, m.ok+m.fail, end.Sub(m.start))
	return SystemStats{
		StartTime:      m.start,
		EndTime:        end,
		Duration:       end.Sub(m.start),
		DurationSeconds: end.Sub(m.start).Seconds(),
		RequestsSent:   m.sent,
		RequestsOK:     m.ok,
		RequestsFailed: m.fail,
		InFlightMax:    m.maxWaiting,
		Latency:        lat,
		Rate:           rate,
		FreePct:        cfg.FreePct,
		PaidTier:       cfg.PaidTier,
		BurstCount:     cfg.BurstCount,
		RateCount:      cfg.RateCount,
		RateEvery:      cfg.RateEvery,
	}
}

func computeLatency(samples []time.Duration) LatencyStats {
	if len(samples) == 0 {
		return LatencyStats{}
	}

	data := make([]time.Duration, len(samples))
	copy(data, samples)
	sort.Slice(data, func(i, j int) bool { return data[i] < data[j] })

	min := data[0]
	max := data[len(data)-1]
	var sum time.Duration
	for _, d := range data {
		sum += d
	}
	avg := time.Duration(int64(sum) / int64(len(data)))

	return LatencyStats{
		Count: len(data),
		Min:   min,
		Max:   max,
		Avg:   avg,
		P50:   percentile(data, 0.50),
		P95:   percentile(data, 0.95),
		P99:   percentile(data, 0.99),
		MinMs: min.Seconds() * 1000,
		MaxMs: max.Seconds() * 1000,
		AvgMs: avg.Seconds() * 1000,
		P50Ms: percentile(data, 0.50).Seconds() * 1000,
		P95Ms: percentile(data, 0.95).Seconds() * 1000,
		P99Ms: percentile(data, 0.99).Seconds() * 1000,
	}
}

func percentile(data []time.Duration, p float64) time.Duration {
	if len(data) == 0 {
		return 0
	}
	idx := int(math.Round((float64(len(data))-1.0) * p))
	if idx < 0 {
		idx = 0
	}
	if idx >= len(data) {
		idx = len(data) - 1
	}
	return data[idx]
}

func computeRates(sentTimes, doneTimes []time.Time, totalSent, totalDone int, duration time.Duration) RateStats {
	window := 10 * time.Second
	cut := time.Now().Add(-window)
	sent := 0
	for _, t := range sentTimes {
		if t.After(cut) {
			sent++
		}
	}
	completed := 0
	for _, t := range doneTimes {
		if t.After(cut) {
			completed++
		}
	}
	overallSent := 0.0
	overallDone := 0.0
	if duration.Seconds() > 0 {
		overallSent = float64(totalSent) / duration.Seconds()
		overallDone = float64(totalDone) / duration.Seconds()
	}
	return RateStats{
		SentPerSec:     float64(sent) / window.Seconds(),
		CompletedPerSec: float64(completed) / window.Seconds(),
		SentPerSecOverall: overallSent,
		CompletedPerSecOverall: overallDone,
		WindowSeconds: window.Seconds(),
	}
}
