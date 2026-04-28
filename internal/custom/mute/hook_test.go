package mute

import (
	"testing"

	"github.com/ccfos/nightingale/v6/alert/dispatch"
	customModels "github.com/ccfos/nightingale/v6/internal/custom/models"
	"github.com/ccfos/nightingale/v6/models"
)

func TestHookAdapter_MutedEventReturnsTrue(t *testing.T) {
	emergency := NewEmergencyHolder()
	emergency.Set(&EmergencyState{Enabled: true, Reason: "test"})
	eval := NewEvaluator(&StaticCronRuleProvider{}, emergency)

	prevCalls := 0
	prev := dispatch.EventMuteHookFunc(func(*models.AlertCurEvent) bool {
		prevCalls++
		return false
	})

	h := NewHookAdapter(eval, prev)

	if !h.Hook(&models.AlertCurEvent{Hash: "h"}) {
		t.Fatalf("emergency-muted event must return true")
	}
	if prevCalls != 0 {
		t.Fatalf("previous hook must not be called when mute fires; got %d", prevCalls)
	}
}

func TestHookAdapter_FallsThroughOnPass(t *testing.T) {
	eval := NewEvaluator(&StaticCronRuleProvider{}, NewEmergencyHolder())

	prevCalls := 0
	prev := dispatch.EventMuteHookFunc(func(*models.AlertCurEvent) bool {
		prevCalls++
		return true // bubble up "muted by deeper layer"
	})

	h := NewHookAdapter(eval, prev)
	if !h.Hook(&models.AlertCurEvent{}) {
		t.Fatalf("verdict from previous hook must be returned")
	}
	if prevCalls != 1 {
		t.Fatalf("previous hook must run exactly once; got %d", prevCalls)
	}
}

func TestHookAdapter_NilPreviousIsSafe(t *testing.T) {
	eval := NewEvaluator(&StaticCronRuleProvider{}, NewEmergencyHolder())
	h := NewHookAdapter(eval, nil)

	if h.Hook(&models.AlertCurEvent{}) {
		t.Fatalf("event must pass when no rule fires and prev is nil")
	}
}

func TestHookAdapter_NilEvent(t *testing.T) {
	h := NewHookAdapter(NewEvaluator(&StaticCronRuleProvider{}, NewEmergencyHolder()), nil)
	if h.Hook(nil) {
		t.Fatalf("nil event must not be muted")
	}
}

func TestHookAdapter_InstallReplacesGlobalHook(t *testing.T) {
	saved := dispatch.EventMuteHook
	t.Cleanup(func() { dispatch.EventMuteHook = saved })

	emergency := NewEmergencyHolder()
	emergency.Set(&EmergencyState{Enabled: true})
	eval := NewEvaluator(&StaticCronRuleProvider{}, emergency)
	h := NewHookAdapter(eval, nil)
	h.Install()

	if !dispatch.EventMuteHook(&models.AlertCurEvent{}) {
		t.Fatalf("Install did not wire adapter into dispatch.EventMuteHook")
	}
}

// Verify that the recommended chain (mute → suppress) really yields the
// "operator mute precedes suppress" semantics described in HookAdapter docs.
func TestHookAdapter_StackedChainOrder(t *testing.T) {
	mutedByEmergency := NewEmergencyHolder()
	mutedByEmergency.Set(&EmergencyState{Enabled: true})

	innerCalls := 0
	inner := dispatch.EventMuteHookFunc(func(*models.AlertCurEvent) bool {
		innerCalls++
		return false
	})

	muteHook := NewHookAdapter(
		NewEvaluator(&StaticCronRuleProvider{}, mutedByEmergency),
		inner,
	)

	if !muteHook.Hook(&models.AlertCurEvent{}) {
		t.Fatalf("mute layer must short-circuit")
	}
	if innerCalls != 0 {
		t.Fatalf("inner suppress layer must NOT be invoked when mute fires")
	}
}

// Defensive: a CompiledCronRule with a real cron and a real event flows
// through the adapter without panic. (Ensures no nil-pointer regressions
// when the cron path runs end-to-end.)
func TestHookAdapter_CronRulePath(t *testing.T) {
	rule, err := CompileCronRule(&customModels.CustomMuteCron{
		Id:          1,
		CronExpr:    "* * * * *", // every minute
		DurationSec: 300,
	})
	if err != nil {
		t.Fatalf("compile rule: %v", err)
	}
	eval := NewEvaluator(
		&StaticCronRuleProvider{Rules: []*CompiledCronRule{rule}},
		NewEmergencyHolder(),
	)
	h := NewHookAdapter(eval, nil)

	if !h.Hook(&models.AlertCurEvent{Hash: "x"}) {
		t.Fatalf("always-active cron rule must mute event via the hook path")
	}
}
