package mute

import (
	"testing"
	"time"

	customModels "github.com/ccfos/nightingale/v6/internal/custom/models"
	"github.com/ccfos/nightingale/v6/models"
)

func TestEmergencyHolder_DisabledNeverMatches(t *testing.T) {
	h := NewEmergencyHolder()
	got, _ := h.Match(&models.AlertCurEvent{}, time.Now())
	if got {
		t.Fatalf("default state must be disabled")
	}
}

func TestEmergencyHolder_EnabledMatchesEverything(t *testing.T) {
	h := NewEmergencyHolder()
	h.Set(&EmergencyState{Enabled: true, Reason: "maintenance"})

	got, reason := h.Match(&models.AlertCurEvent{}, time.Now())
	if !got {
		t.Fatalf("enabled mute must match")
	}
	if reason != "maintenance" {
		t.Fatalf("expected reason 'maintenance', got %q", reason)
	}
}

func TestEmergencyHolder_ExpiredMuteIgnored(t *testing.T) {
	h := NewEmergencyHolder()
	now := mustParseTime(t, time.RFC3339, "2026-04-28T14:00:00Z")
	h.Set(&EmergencyState{
		Enabled:  true,
		Reason:   "expired window",
		ExpireAt: now.Add(-time.Hour).Unix(),
	})

	got, _ := h.Match(&models.AlertCurEvent{}, now)
	if got {
		t.Fatalf("expired mute must not match")
	}
}

func TestEmergencyHolder_UnexpiredMuteMatches(t *testing.T) {
	h := NewEmergencyHolder()
	now := mustParseTime(t, time.RFC3339, "2026-04-28T14:00:00Z")
	h.Set(&EmergencyState{
		Enabled:  true,
		ExpireAt: now.Add(time.Hour).Unix(),
	})

	got, _ := h.Match(&models.AlertCurEvent{}, now)
	if !got {
		t.Fatalf("non-expired mute must match")
	}
}

func TestEmergencyHolder_DatasourceScopeFiltersOut(t *testing.T) {
	h := NewEmergencyHolder()
	h.Set(&EmergencyState{
		Enabled:       true,
		DatasourceIds: []int64{1, 2, 3},
	})

	in := &models.AlertCurEvent{DatasourceId: 1}
	out := &models.AlertCurEvent{DatasourceId: 99}

	if got, _ := h.Match(in, time.Now()); !got {
		t.Fatalf("ds=1 must match scope [1,2,3]")
	}
	if got, _ := h.Match(out, time.Now()); got {
		t.Fatalf("ds=99 must not match scope [1,2,3]")
	}
}

func TestEmergencyHolder_GroupScopeFiltersOut(t *testing.T) {
	h := NewEmergencyHolder()
	h.Set(&EmergencyState{
		Enabled:  true,
		GroupIds: []int64{7},
	})

	in := &models.AlertCurEvent{GroupId: 7}
	out := &models.AlertCurEvent{GroupId: 8}

	if got, _ := h.Match(in, time.Now()); !got {
		t.Fatalf("group 7 must match")
	}
	if got, _ := h.Match(out, time.Now()); got {
		t.Fatalf("group 8 must not match scope [7]")
	}
}

func TestEmergencyHolder_NilStateMeansDisabled(t *testing.T) {
	h := NewEmergencyHolder()
	h.Set(nil) // Set should treat nil as "disabled"

	got, _ := h.Match(&models.AlertCurEvent{}, time.Now())
	if got {
		t.Fatalf("Set(nil) must produce disabled state")
	}
}

func TestEmergencyHolder_NilEventReturnsFalse(t *testing.T) {
	h := NewEmergencyHolder()
	h.Set(&EmergencyState{Enabled: true})
	if got, _ := h.Match(nil, time.Now()); got {
		t.Fatalf("nil event must not match")
	}
}

func TestFromModel_DisabledRow(t *testing.T) {
	row := &customModels.CustomEmergencyMute{Enabled: 0}
	s := FromModel(row)
	if s.Enabled {
		t.Fatalf("Enabled=0 row must yield disabled state")
	}
}

func TestFromModel_EnabledRow(t *testing.T) {
	row := &customModels.CustomEmergencyMute{
		Enabled:           1,
		Reason:            "maintenance",
		ExpireAt:          1000,
		DatasourceIdsJson: []int64{1, 2},
		GroupIdsJson:      []int64{7},
	}
	s := FromModel(row)
	if !s.Enabled || s.Reason != "maintenance" || s.ExpireAt != 1000 ||
		len(s.DatasourceIds) != 2 || len(s.GroupIds) != 1 {
		t.Fatalf("FromModel did not preserve all fields: %+v", s)
	}
}

func TestFromModel_NilRowYieldsDisabled(t *testing.T) {
	s := FromModel(nil)
	if s.Enabled {
		t.Fatalf("nil row must yield disabled state")
	}
}
