package denoise

import (
	"sync"
	"time"
)

// stormCounter tracks how many events have been observed for a given
// (rule, incident) pair within a sliding window, in order to detect alert
// storms — bursts that warrant an extra "we're being flooded" notification.
//
// Why a ring of timestamps rather than a simple counter:
//
//	A counter would also need a reset timer; the ring naturally expires old
//	hits as we step forward. The footprint is bounded by `threshold` entries
//	(once we cross threshold we record but stop appending) per active key.
type stormCounter struct {
	timestamps []int64 // ring buffer of event times (seconds)
}

// StormDetector emits "storm" decisions when a single incident receives more
// than `threshold` events within `windowSec` seconds.
//
// One detector instance is shared across all aggregate rules — the per-rule
// thresholds are passed into Observe, so a single global detector can serve
// any number of rules with different storm settings.
type StormDetector struct {
	mu  sync.Mutex
	m   map[int64]*stormCounter // keyed by incident id
	now func() int64
}

func NewStormDetector() *StormDetector {
	return &StormDetector{
		m:   make(map[int64]*stormCounter),
		now: func() int64 { return time.Now().Unix() },
	}
}

// Observe records an event hitting the given incident, then returns whether
// this observation just crossed the storm threshold. Returns true at most
// once per incident per "storm episode" — the caller is expected to use
// this signal to fire a one-shot storm notification.
//
// Threshold semantics: trigger fires the first time `count >= threshold`
// within `windowSec`. Setting `threshold <= 0` disables detection entirely
// (returns false without any work).
func (s *StormDetector) Observe(incidentId int64, threshold int, windowSec int64) bool {
	if threshold <= 0 {
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	cutoff := now - windowSec

	c, ok := s.m[incidentId]
	if !ok {
		c = &stormCounter{timestamps: make([]int64, 0, threshold)}
		s.m[incidentId] = c
	}

	// Trim entries that fell out of the window. timestamps are appended in
	// monotonic order so a single forward-scan suffices.
	dropTo := 0
	for dropTo < len(c.timestamps) && c.timestamps[dropTo] < cutoff {
		dropTo++
	}
	if dropTo > 0 {
		c.timestamps = c.timestamps[dropTo:]
	}

	c.timestamps = append(c.timestamps, now)

	// Strict equality so we trigger exactly once per episode. After firing,
	// subsequent appends grow the slice but won't re-fire until we reset.
	if len(c.timestamps) == threshold {
		// Reset to empty (but keep the entry) so the same incident can
		// trigger a fresh storm if it floods again later.
		c.timestamps = c.timestamps[:0]
		return true
	}
	return false
}

// Forget drops storm-tracking state for an incident — call it when the
// incident resolves so the map cannot grow unboundedly across long uptimes.
func (s *StormDetector) Forget(incidentId int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, incidentId)
}
