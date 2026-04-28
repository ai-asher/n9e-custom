// Package mute extends N9e's native AlertMute with two capabilities the
// upstream model lacks:
//
//  1. Cron-expression based mute windows (e.g. "0 22 * * 1-5" + duration=3600
//     to silence weekday 22:00–23:00 windows).
//  2. A global emergency-mute switch — one toggle per cluster — for
//     maintenance windows or major outages.
//
// Architecture
//
//	The package is a self-contained set of evaluators plus a HookAdapter
//	that wires into dispatch.EventMuteHook. Bootstrap typically chains:
//
//	  mute.HookAdapter -> suppress.HookAdapter -> noop
//
//	so an event explicitly muted by an operator's rule short-circuits the
//	more expensive suppression machinery downstream.
//
// What we do NOT do here
//
//	Native AlertMute (alert/mute/mute.go) already covers TimeRange and
//	weekly Periodic windows along with severity/tag/datasource filtering.
//	This package adds the missing pieces — cron and emergency — and stays
//	out of the original mute path entirely.
package mute
