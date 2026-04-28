package mute

import (
	"slices"
	"sync/atomic"
	"time"

	customModels "github.com/ccfos/nightingale/v6/internal/custom/models"
	"github.com/ccfos/nightingale/v6/models"
)

// EmergencyState is the in-memory snapshot the hook reads on every event.
// It is refreshed periodically by an EmergencyProvider implementation —
// the cache layer reads custom_emergency_mute (the singleton row) every
// second or so and atomic.StorePointers a new EmergencyState here.
//
// We use atomic pointer swap rather than a mutex because emergency-mute
// reads happen on every alert event in the system; under a major incident
// where mute is most useful, that's also when alert volume peaks. Lock-
// free reads keep the hook out of contention with the refresher.
type EmergencyState struct {
	Enabled       bool
	Reason        string
	ExpireAt      int64 // unix seconds; 0 = no expiry
	DatasourceIds []int64
	GroupIds      []int64
}

// EmergencyHolder is the live container for EmergencyState. Construct one
// per process; pass to the HookAdapter and the refresher.
type EmergencyHolder struct {
	state atomic.Pointer[EmergencyState]
}

func NewEmergencyHolder() *EmergencyHolder {
	h := &EmergencyHolder{}
	h.state.Store(&EmergencyState{Enabled: false})
	return h
}

// Set atomically installs a new state snapshot. Safe to call from any
// goroutine. Passing nil is treated as "disabled" — convenient for unit
// tests and DB read failures.
func (h *EmergencyHolder) Set(s *EmergencyState) {
	if s == nil {
		s = &EmergencyState{Enabled: false}
	}
	h.state.Store(s)
}

// FromModel converts the DB row to a runtime EmergencyState. Centralized
// here so callers (the periodic refresher AND the API write path that
// flips the toggle) produce identical in-memory shape.
func FromModel(row *customModels.CustomEmergencyMute) *EmergencyState {
	if row == nil {
		return &EmergencyState{Enabled: false}
	}
	return &EmergencyState{
		Enabled:       row.Enabled == 1,
		Reason:        row.Reason,
		ExpireAt:      row.ExpireAt,
		DatasourceIds: row.DatasourceIdsJson,
		GroupIds:      row.GroupIdsJson,
	}
}

// Match reports whether the global emergency mute applies to this event
// at time `now`. Returns true iff:
//
//  1. The toggle is enabled.
//  2. The toggle hasn't expired (or has no expiry).
//  3. The event falls within the optional datasource/group scope filters
//     (an empty scope means "all").
//
// Matches are intentionally permissive: a misconfigured emergency-mute
// (with conflicting filters) errs on the side of "do mute", because
// mid-incident is exactly when operators don't want to debug filter
// semantics — they want quiet.
func (h *EmergencyHolder) Match(event *models.AlertCurEvent, now time.Time) (bool, string) {
	s := h.state.Load()
	if s == nil || !s.Enabled || event == nil {
		return false, ""
	}

	if s.ExpireAt > 0 && s.ExpireAt < now.Unix() {
		return false, ""
	}

	if len(s.DatasourceIds) > 0 && s.DatasourceIds[0] != 0 &&
		event.DatasourceId != 0 &&
		!slices.Contains(s.DatasourceIds, event.DatasourceId) {
		return false, ""
	}

	if len(s.GroupIds) > 0 && s.GroupIds[0] != 0 &&
		event.GroupId != 0 &&
		!slices.Contains(s.GroupIds, event.GroupId) {
		return false, ""
	}

	return true, s.Reason
}
