package suppress

import (
	"encoding/json"
	"testing"

	"github.com/ccfos/nightingale/v6/alert/dispatch"
	customModels "github.com/ccfos/nightingale/v6/internal/custom/models"
	"github.com/ccfos/nightingale/v6/models"
)

func makeHostDownRule(t *testing.T) *CompiledRule {
	t.Helper()
	srcJSON, _ := json.Marshal([]models.TagFilter{{Key: "alertname", Op: "==", Func: "==", Value: "HostDown"}})
	tgtJSON, _ := json.Marshal([]models.TagFilter{{Key: "category", Op: "==", Func: "==", Value: "service"}})
	eqJSON, _ := json.Marshal([]string{"host"})
	c, err := CompileRule(&customModels.CustomInhibitRule{
		Id:          1,
		SourceMatch: srcJSON,
		TargetMatch: tgtJSON,
		EqualLabels: string(eqJSON),
	})
	if err != nil {
		t.Fatalf("CompileRule: %v", err)
	}
	return c
}

func TestHookAdapter_ChainsPreviousHook(t *testing.T) {
	rule := makeHostDownRule(t)
	idx := NewRootCauseIndex(60)
	inh := NewInhibitor(&StaticRuleProvider{Rules: []*CompiledRule{rule}}, idx)

	prevCalls := 0
	prev := dispatch.EventMuteHookFunc(func(*models.AlertCurEvent) bool {
		prevCalls++
		return false // pass through
	})

	h := NewHookAdapter(inh, prev)

	// Unrelated event: not source, not target → must call previous hook.
	event := &models.AlertCurEvent{Hash: "h", TagsMap: map[string]string{"x": "y"}}
	if got := h.Hook(event); got {
		t.Fatalf("unrelated event must not be muted")
	}
	if prevCalls != 1 {
		t.Fatalf("expected previous hook to be called once; got %d", prevCalls)
	}
}

func TestHookAdapter_PreviousHookWinsWhenInhibitorPasses(t *testing.T) {
	idx := NewRootCauseIndex(60)
	inh := NewInhibitor(&StaticRuleProvider{Rules: nil}, idx)

	prev := dispatch.EventMuteHookFunc(func(*models.AlertCurEvent) bool { return true })
	h := NewHookAdapter(inh, prev)

	event := &models.AlertCurEvent{Hash: "h"}
	if !h.Hook(event) {
		t.Fatalf("event muted by previous hook should still be muted")
	}
}

func TestHookAdapter_InhibitorWinsWhenSuppressing(t *testing.T) {
	rule := makeHostDownRule(t)
	idx := NewRootCauseIndex(60)
	inh := NewInhibitor(&StaticRuleProvider{Rules: []*CompiledRule{rule}}, idx)

	// Previous hook always passes (returns false).
	prev := dispatch.EventMuteHookFunc(func(*models.AlertCurEvent) bool { return false })
	h := NewHookAdapter(inh, prev)

	// Register a root cause first via the normal hook path.
	src := &models.AlertCurEvent{Hash: "src", TagsMap: map[string]string{"alertname": "HostDown", "host": "db01"}}
	h.Hook(src)

	// Now a derived event arrives — must be suppressed.
	tgt := &models.AlertCurEvent{Hash: "tgt", TagsMap: map[string]string{"category": "service", "host": "db01"}}
	if !h.Hook(tgt) {
		t.Fatalf("derived event must be suppressed")
	}
}

func TestHookAdapter_NilEventReturnsFalse(t *testing.T) {
	h := NewHookAdapter(NewInhibitor(&StaticRuleProvider{}, NewRootCauseIndex(60)), nil)
	if h.Hook(nil) {
		t.Fatalf("nil event must not mute")
	}
}

func TestHookAdapter_InstallReplacesGlobalHook(t *testing.T) {
	// Save and restore the global so other tests aren't affected.
	saved := dispatch.EventMuteHook
	t.Cleanup(func() { dispatch.EventMuteHook = saved })

	rule := makeHostDownRule(t)
	idx := NewRootCauseIndex(60)
	inh := NewInhibitor(&StaticRuleProvider{Rules: []*CompiledRule{rule}}, idx)
	h := NewHookAdapter(inh, nil)
	h.Install()

	// Register source through the global.
	dispatch.EventMuteHook(&models.AlertCurEvent{
		Hash:    "src",
		TagsMap: map[string]string{"alertname": "HostDown", "host": "db01"},
	})

	suppressed := dispatch.EventMuteHook(&models.AlertCurEvent{
		Hash:    "tgt",
		TagsMap: map[string]string{"category": "service", "host": "db01"},
	})
	if !suppressed {
		t.Fatalf("Install did not wire the adapter into dispatch.EventMuteHook")
	}
}
