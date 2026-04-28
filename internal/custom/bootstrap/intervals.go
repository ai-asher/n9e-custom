package bootstrap

import "time"

// Refresh cadences. Picked to balance responsiveness against DB load:
//
//   - Rule tables (aggregate / inhibit / cron) drift at human speed.
//     5 seconds is the same cadence N9e's native memsto uses for
//     alert_mute, so we match it for consistency.
//   - Emergency mute is a kill-switch. When ops flips it on during a
//     real incident, every additional second of delay is more pages
//     fired. 1s is the minimum the refresher allows.
const (
	aggregateRefreshInterval = 5 * time.Second
	suppressRefreshInterval  = 5 * time.Second
	cronRefreshInterval      = 5 * time.Second
	emergencyRefreshInterval = 1 * time.Second
)

// newTicker is a thin wrapper around time.NewTicker. We keep it as a
// package function (rather than inline) so tests can stub it later if we
// add deterministic timing tests.
func newTicker(d time.Duration) *time.Ticker {
	return time.NewTicker(d)
}
