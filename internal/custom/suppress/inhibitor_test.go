package suppress

import (
	"encoding/json"
	"testing"

	customModels "github.com/ccfos/nightingale/v6/internal/custom/models"
	"github.com/ccfos/nightingale/v6/models"
)

// helper: build a CompiledRule from in-memory descriptors so tests don't
// have to hand-craft JSON payloads.
func buildRule(t *testing.T, id int64, src, tgt []models.TagFilter, equal []string) *CompiledRule {
	t.Helper()

	srcJSON, _ := json.Marshal(src)
	tgtJSON, _ := json.Marshal(tgt)
	eqJSON, _ := json.Marshal(equal)

	raw := &customModels.CustomInhibitRule{
		Id:          id,
		SourceMatch: srcJSON,
		TargetMatch: tgtJSON,
		EqualLabels: string(eqJSON),
	}
	c, err := CompileRule(raw)
	if err != nil {
		t.Fatalf("CompileRule: %v", err)
	}
	return c
}

func TestInhibitor_RootCauseSuppressesDerived(t *testing.T) {
	rule := buildRule(t, 1,
		[]models.TagFilter{{Key: "alertname", Op: "==", Func: "==", Value: "HostDown"}},
		[]models.TagFilter{{Key: "category", Op: "==", Func: "==", Value: "service"}},
		[]string{"host"},
	)

	idx := NewRootCauseIndex(60)
	inh := NewInhibitor(&StaticRuleProvider{Rules: []*CompiledRule{rule}}, idx)

	// 1. Source event arrives — must NOT be suppressed; must register.
	src := &models.AlertCurEvent{
		Hash: "src-hash",
		TagsMap: map[string]string{
			"alertname": "HostDown",
			"host":      "db01",
		},
	}
	d := inh.Decide(src)
	if d.Suppressed {
		t.Fatalf("root-cause event should not be suppressed")
	}
	if len(d.NewRegistrations) != 1 || d.NewRegistrations[0].RuleId != 1 {
		t.Fatalf("expected single registration under rule 1, got %+v", d.NewRegistrations)
	}

	// 2. Derived event on the SAME host — should be suppressed.
	tgt := &models.AlertCurEvent{
		Hash: "tgt-hash",
		TagsMap: map[string]string{
			"category": "service",
			"host":     "db01",
		},
	}
	d = inh.Decide(tgt)
	if !d.Suppressed {
		t.Fatalf("derived event on same host should be suppressed")
	}
	if d.BySourceHash != "src-hash" || d.ByRuleId != 1 {
		t.Fatalf("wrong attribution: got source=%q rule=%d", d.BySourceHash, d.ByRuleId)
	}

	// 3. Derived event on a DIFFERENT host — must pass through.
	other := &models.AlertCurEvent{
		Hash: "other-hash",
		TagsMap: map[string]string{
			"category": "service",
			"host":     "db02",
		},
	}
	if inh.Decide(other).Suppressed {
		t.Fatalf("derived event on different host must not be suppressed")
	}
}

func TestInhibitor_NoRootCauseLetsAllPass(t *testing.T) {
	rule := buildRule(t, 1,
		[]models.TagFilter{{Key: "severity", Op: "==", Func: "==", Value: "critical"}},
		[]models.TagFilter{{Key: "severity", Op: "==", Func: "==", Value: "warning"}},
		[]string{"service"},
	)
	idx := NewRootCauseIndex(60)
	inh := NewInhibitor(&StaticRuleProvider{Rules: []*CompiledRule{rule}}, idx)

	// Warning arrives without any prior critical → must pass.
	warn := &models.AlertCurEvent{
		Hash: "w",
		TagsMap: map[string]string{
			"severity": "warning",
			"service":  "billing",
		},
	}
	if inh.Decide(warn).Suppressed {
		t.Fatalf("warning must pass when no critical is active")
	}
}

func TestInhibitor_SelfSuppressionPrevented(t *testing.T) {
	// A rule where source and target overlap (matches the same event)
	// should NOT cause an event to suppress itself.
	rule := buildRule(t, 1,
		[]models.TagFilter{{Key: "host", Op: "=~", Func: "=~", Value: ".*"}},
		[]models.TagFilter{{Key: "host", Op: "=~", Func: "=~", Value: ".*"}},
		[]string{"host"},
	)
	idx := NewRootCauseIndex(60)
	inh := NewInhibitor(&StaticRuleProvider{Rules: []*CompiledRule{rule}}, idx)

	event := &models.AlertCurEvent{
		Hash:    "h1",
		TagsMap: map[string]string{"host": "db01"},
	}
	d := inh.Decide(event)
	if d.Suppressed {
		t.Fatalf("event must not suppress itself; got source=%q", d.BySourceHash)
	}
	if len(d.NewRegistrations) != 1 {
		t.Fatalf("expected the event to register itself as a source")
	}
}

func TestInhibitor_RecoveredSourceStopsSuppressing(t *testing.T) {
	rule := buildRule(t, 1,
		[]models.TagFilter{{Key: "alertname", Op: "==", Func: "==", Value: "HostDown"}},
		[]models.TagFilter{{Key: "category", Op: "==", Func: "==", Value: "service"}},
		[]string{"host"},
	)
	idx := NewRootCauseIndex(60)
	inh := NewInhibitor(&StaticRuleProvider{Rules: []*CompiledRule{rule}}, idx)

	src := &models.AlertCurEvent{Hash: "src", TagsMap: map[string]string{"alertname": "HostDown", "host": "db01"}}
	tgt := &models.AlertCurEvent{Hash: "tgt", TagsMap: map[string]string{"category": "service", "host": "db01"}}

	inh.Decide(src) // registers
	if !inh.Decide(tgt).Suppressed {
		t.Fatalf("expected target suppression while source active")
	}

	idx.ForgetEvent("src") // simulate recovery

	if inh.Decide(tgt).Suppressed {
		t.Fatalf("target must pass after source recovered")
	}
}

func TestInhibitor_GroupScopeRespected(t *testing.T) {
	rule := buildRule(t, 1,
		[]models.TagFilter{{Key: "alertname", Op: "==", Func: "==", Value: "HostDown"}},
		[]models.TagFilter{{Key: "category", Op: "==", Func: "==", Value: "service"}},
		[]string{"host"},
	)
	rule.Rule.GroupId = 7 // restrict rule to group 7
	idx := NewRootCauseIndex(60)
	inh := NewInhibitor(&StaticRuleProvider{Rules: []*CompiledRule{rule}}, idx)

	// Event in group 8 — rule must be ignored entirely.
	other := &models.AlertCurEvent{
		Hash:    "x",
		GroupId: 8,
		TagsMap: map[string]string{"alertname": "HostDown", "host": "db01"},
	}
	d := inh.Decide(other)
	if len(d.NewRegistrations) != 0 {
		t.Fatalf("rule with mismatched group must not register")
	}
}

func TestInhibitor_NilEvent(t *testing.T) {
	inh := NewInhibitor(&StaticRuleProvider{}, NewRootCauseIndex(60))
	d := inh.Decide(nil)
	if d.Suppressed || len(d.NewRegistrations) != 0 {
		t.Fatalf("nil event must produce zero decision")
	}
}
