package mute

import (
	"github.com/ccfos/nightingale/v6/alert/dispatch"
	"github.com/ccfos/nightingale/v6/models"
	"github.com/toolkits/pkg/logger"
)

// HookAdapter wires the mute Evaluator into N9e's dispatch.EventMuteHook
// chain. Mirrors the suppress.HookAdapter pattern so the two adapters can
// be stacked at bootstrap:
//
//	suppressHook := suppress.NewHookAdapter(suppressInh, nil)
//	muteHook     := mute.NewHookAdapter(eval, suppressHook.Hook)
//	muteHook.Install()
//
// With that ordering, an event explicitly muted by an operator's rule
// short-circuits before the suppression machinery runs — saving the
// per-event suppress lookup AND giving operator intent precedence over
// implicit inhibition.
type HookAdapter struct {
	eval     *Evaluator
	previous dispatch.EventMuteHookFunc
}

// NewHookAdapter wraps a previous hook so the chain is preserved. Pass nil
// for `prev` to terminate the chain with a no-op.
func NewHookAdapter(eval *Evaluator, prev dispatch.EventMuteHookFunc) *HookAdapter {
	if prev == nil {
		prev = func(*models.AlertCurEvent) bool { return false }
	}
	return &HookAdapter{eval: eval, previous: prev}
}

// Hook is the function value to assign to dispatch.EventMuteHook. Returns
// true iff the event should be dropped (mute decision).
//
// Order of operations:
//  1. Run the mute Evaluator. If it mutes, log and return immediately —
//     operator-configured mute is the highest-precedence suppression.
//  2. Otherwise defer to the previous hook (typically suppress).
func (h *HookAdapter) Hook(event *models.AlertCurEvent) bool {
	if event == nil {
		return false
	}

	v := h.eval.Evaluate(event)
	if v.Muted {
		// INFO level on purpose: every mute is operator-relevant audit
		// data. If volume becomes a problem, downgrade to DEBUG and
		// emit a counter metric instead.
		logger.Infof("mute: event_hash=%s muted by rule=%d reason=%s",
			event.Hash, v.RuleId, v.Reason)
		return true
	}

	return h.previous(event)
}

// Install replaces dispatch.EventMuteHook with this adapter's Hook.
// Call once during bootstrap, after wiring the chain together.
func (h *HookAdapter) Install() {
	dispatch.EventMuteHook = h.Hook
}
