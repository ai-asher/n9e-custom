package mute

import (
	"testing"
	"time"

	customModels "github.com/ccfos/nightingale/v6/internal/custom/models"
	"github.com/ccfos/nightingale/v6/models"
)

func TestEvaluator_EmergencyTakesPrecedence(t *testing.T) {
	emergency := NewEmergencyHolder()
	emergency.Set(&EmergencyState{Enabled: true, Reason: "outage"})

	// A cron rule that would also match — to prove emergency wins early.
	cron := alwaysActiveRule(t, nil)
	provider := &StaticCronRuleProvider{Rules: []*CompiledCronRule{cron}}

	eval := NewEvaluator(provider, emergency)

	v := eval.Evaluate(&models.AlertCurEvent{})
	if !v.Muted {
		t.Fatalf("expected mute")
	}
	if v.RuleId != 0 {
		t.Fatalf("emergency mute must report RuleId=0; got %d", v.RuleId)
	}
}

func TestEvaluator_FallsThroughToCronWhenEmergencyOff(t *testing.T) {
	cron := alwaysActiveRule(t, func(r *customModels.CustomMuteCron) {
		r.Id = 42
		r.Note = "weekday-night"
	})
	provider := &StaticCronRuleProvider{Rules: []*CompiledCronRule{cron}}
	eval := NewEvaluator(provider, NewEmergencyHolder())

	v := eval.Evaluate(&models.AlertCurEvent{})
	if !v.Muted || v.RuleId != 42 {
		t.Fatalf("cron rule must fire when emergency is off; got %+v", v)
	}
}

func TestEvaluator_NoMatchReturnsZeroVerdict(t *testing.T) {
	cron := alwaysActiveRule(t, func(r *customModels.CustomMuteCron) {
		r.GroupId = 99 // event group is 0; this rule won't match
	})
	provider := &StaticCronRuleProvider{Rules: []*CompiledCronRule{cron}}
	eval := NewEvaluator(provider, NewEmergencyHolder())

	v := eval.Evaluate(&models.AlertCurEvent{GroupId: 1})
	if v.Muted {
		t.Fatalf("no rule should match; got %+v", v)
	}
}

func TestEvaluator_NilEventReturnsZeroVerdict(t *testing.T) {
	eval := NewEvaluator(&StaticCronRuleProvider{}, NewEmergencyHolder())
	if v := eval.Evaluate(nil); v.Muted {
		t.Fatalf("nil event must not mute")
	}
}

func TestEvaluator_FirstMatchingCronRuleWins(t *testing.T) {
	a := alwaysActiveRule(t, func(r *customModels.CustomMuteCron) { r.Id = 10 })
	b := alwaysActiveRule(t, func(r *customModels.CustomMuteCron) { r.Id = 20 })
	provider := &StaticCronRuleProvider{Rules: []*CompiledCronRule{a, b}}
	eval := NewEvaluator(provider, NewEmergencyHolder())

	v := eval.Evaluate(&models.AlertCurEvent{})
	if v.RuleId != 10 {
		t.Fatalf("first rule in slice should win; got rule id %d", v.RuleId)
	}
}

// Sanity test: clock injection works (used implicitly by other tests but
// verified here to be defensive against future refactors of NewEvaluator).
func TestEvaluator_ClockInjectionRespected(t *testing.T) {
	eval := NewEvaluator(&StaticCronRuleProvider{}, NewEmergencyHolder())
	called := false
	eval.now = func() time.Time {
		called = true
		return time.Now()
	}
	eval.Evaluate(&models.AlertCurEvent{})
	if !called {
		t.Fatalf("evaluator.now should be called on Evaluate")
	}
}
