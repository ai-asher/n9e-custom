package suppress

import (
	"github.com/ccfos/nightingale/v6/alert/dispatch"
	"github.com/ccfos/nightingale/v6/models"
	"github.com/toolkits/pkg/logger"
)

// HookAdapter bridges the suppress.Inhibitor into N9e's dispatch package
// via the existing dispatch.EventMuteHook seam. Wiring is split into a
// dedicated type so we can stack additional hooks (e.g. mute) in front of
// or behind suppression without touching this file.
type HookAdapter struct {
	inhibitor *Inhibitor
	previous  dispatch.EventMuteHookFunc
}

// NewHookAdapter wraps a (possibly already-installed) previous hook so the
// resulting function honors both the older mute decision AND suppression.
// Pass dispatch.EventMuteHook (the global) as `prev` to chain on top of
// whatever is already installed.
func NewHookAdapter(inh *Inhibitor, prev dispatch.EventMuteHookFunc) *HookAdapter {
	if prev == nil {
		// Default no-op so the call site can stay unconditional.
		prev = func(*models.AlertCurEvent) bool { return false }
	}
	return &HookAdapter{inhibitor: inh, previous: prev}
}

// Hook is the function value that should be assigned to dispatch.EventMuteHook
// during bootstrap. Returns true iff the event should be muted (dropped).
//
// Order of operations:
//  1. Run the inhibitor — it always registers source-side matches, even when
//     `previous` is going to suppress the event. Skipping this would mean a
//     muted-by-other-rule event silently fails to register as a root cause.
//  2. If the inhibitor decided to suppress, return true.
//  3. Otherwise, defer to the previously-installed hook (typically the mute
//     module). This preserves operator intuition: explicit mute rules take
//     precedence over implicit inhibition.
//
// Concretely, an event muted by a CRON window will not be paged regardless;
// an event suppressed by a root-cause inhibition will not be paged but the
// root cause itself still fires.
func (h *HookAdapter) Hook(event *models.AlertCurEvent) bool {
	if event == nil {
		return false
	}

	decision := h.inhibitor.Decide(event)

	if decision.Suppressed {
		logger.Infof("suppress: event_hash=%s suppressed by source=%s rule=%d",
			event.Hash, decision.BySourceHash, decision.ByRuleId)
		return true
	}

	if len(decision.NewRegistrations) > 0 {
		logger.Debugf("suppress: event_hash=%s registered as source under %d rule(s)",
			event.Hash, len(decision.NewRegistrations))
	}

	return h.previous(event)
}

// Install replaces dispatch.EventMuteHook with the adapter's Hook, capturing
// whatever was installed before. Idempotent under repeated calls only when
// `prev` was already this adapter — usually you should call Install exactly
// once during bootstrap.
func (h *HookAdapter) Install() {
	dispatch.EventMuteHook = h.Hook
}
