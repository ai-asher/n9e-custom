package denoise

import (
	"testing"

	"github.com/ccfos/nightingale/v6/models"
)

func TestBuildIncidentKey_StableSort(t *testing.T) {
	event := &models.AlertCurEvent{
		TagsMap: map[string]string{"service": "order", "cluster": "prod"},
	}

	got1 := BuildIncidentKey(event, []string{"service", "cluster"})
	got2 := BuildIncidentKey(event, []string{"cluster", "service"})

	if got1 != got2 {
		t.Fatalf("expected dimension order to not affect key; got %q vs %q", got1, got2)
	}
	if got1 != "cluster=prod|service=order" {
		t.Fatalf("unexpected key: %q", got1)
	}
}

func TestBuildIncidentKey_MissingLabelMapsToSentinel(t *testing.T) {
	a := &models.AlertCurEvent{TagsMap: map[string]string{"service": "order"}}
	b := &models.AlertCurEvent{TagsMap: map[string]string{"service": "order"}}

	keyA := BuildIncidentKey(a, []string{"service", "cluster"})
	keyB := BuildIncidentKey(b, []string{"service", "cluster"})

	// Two events with the same missing-label set should still group together.
	if keyA != keyB {
		t.Fatalf("expected identical keys for events with same missing labels; %q vs %q", keyA, keyB)
	}
	if keyA != "cluster=<absent>|service=order" {
		t.Fatalf("missing label not represented as sentinel: %q", keyA)
	}
}

func TestBuildIncidentKey_DifferentValuesProduceDifferentKeys(t *testing.T) {
	a := &models.AlertCurEvent{TagsMap: map[string]string{"service": "order"}}
	b := &models.AlertCurEvent{TagsMap: map[string]string{"service": "billing"}}

	if BuildIncidentKey(a, []string{"service"}) == BuildIncidentKey(b, []string{"service"}) {
		t.Fatalf("expected different services to produce different incident keys")
	}
}

func TestBuildIncidentKey_NilOrEmpty(t *testing.T) {
	if BuildIncidentKey(nil, []string{"service"}) != "" {
		t.Fatalf("nil event should produce empty key")
	}
	event := &models.AlertCurEvent{TagsMap: map[string]string{"service": "order"}}
	if BuildIncidentKey(event, nil) != "" {
		t.Fatalf("empty dimensions should produce empty key")
	}
}

func TestDimensionValuesMap_PreservesAllDimensions(t *testing.T) {
	event := &models.AlertCurEvent{TagsMap: map[string]string{"service": "order"}}
	got := DimensionValuesMap(event, []string{"service", "cluster"})

	if got["service"] != "order" {
		t.Errorf("service: want order, got %q", got["service"])
	}
	if got["cluster"] != "<absent>" {
		t.Errorf("missing cluster: want <absent>, got %q", got["cluster"])
	}
}
