package mute

import (
	"fmt"
	"time"

	"github.com/robfig/cron/v3"
)

// cronParser is N9e's standard cron flavor: 5-field (minute hour dom month
// dow) with no seconds, no descriptors. We construct it once at package init
// because cron.NewParser is not free and the hot path (Match) calls Parse
// only when the rule set changes — but we still cache to be safe.
var cronParser = cron.NewParser(
	cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow,
)

// CompiledCron carries a parsed cron schedule plus the location-aware
// timezone the schedule was parsed in. Storing the schedule + tz together
// avoids re-parsing on every event evaluation.
type CompiledCron struct {
	Schedule    cron.Schedule
	Location    *time.Location
	DurationSec int64
}

// CompileCron parses a 5-field cron expression in the given timezone.
// Empty timezone defaults to time.Local — the typical operator expectation
// when they configure "0 22 * * *" without thinking about TZ.
//
// Returns an error rather than a partial result so a malformed cron string
// can never silently match (and silently mute) every event. The caller is
// expected to surface this error to the operator at config-save time.
func CompileCron(expr, timezone string, durationSec int64) (*CompiledCron, error) {
	if expr == "" {
		return nil, fmt.Errorf("cron expression is empty")
	}
	if durationSec <= 0 {
		return nil, fmt.Errorf("duration_sec must be > 0")
	}

	loc := time.Local
	if timezone != "" {
		l, err := time.LoadLocation(timezone)
		if err != nil {
			return nil, fmt.Errorf("invalid timezone %q: %w", timezone, err)
		}
		loc = l
	}

	sched, err := cronParser.Parse(expr)
	if err != nil {
		return nil, fmt.Errorf("invalid cron %q: %w", expr, err)
	}

	return &CompiledCron{
		Schedule:    sched,
		Location:    loc,
		DurationSec: durationSec,
	}, nil
}

// IsActive reports whether `now` falls inside any mute window produced by
// the cron schedule.
//
// Algorithm
//
//	robfig/cron only exposes Next(t time.Time) — no Prev. We exploit Next's
//	semantics: schedule.Next(start) returns the first firing time strictly
//	after `start`. By stepping start back by `DurationSec` and asking for
//	the next firing, we get either:
//
//	  - a firing time fire ∈ (now-duration, now]      → we ARE in a window
//	  - a firing time fire >  now                     → we are NOT in a window
//
//	One Next() call per evaluation; no allocation.
//
// Edge cases:
//   - schedule fires exactly at `now` → fire == now, returns true.
//   - schedule fires every minute and duration > 60s → consecutive windows
//     overlap, every check returns true. Caller's intent is preserved.
func (c *CompiledCron) IsActive(now time.Time) bool {
	if c == nil || c.Schedule == nil {
		return false
	}
	nowInTZ := now.In(c.Location)
	windowStart := nowInTZ.Add(-time.Duration(c.DurationSec) * time.Second)
	fire := c.Schedule.Next(windowStart)
	if fire.IsZero() {
		// Schedule has no future firings (shouldn't happen for standard
		// cron exprs but cron.Schedule allows it in principle).
		return false
	}
	// fire is strictly after windowStart by Next's contract; it is "in
	// the current mute window" iff fire is at or before now.
	return !fire.After(nowInTZ)
}
