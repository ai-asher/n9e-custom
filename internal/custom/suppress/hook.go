package suppress

import (
	"github.com/ccfos/nightingale/v6/alert/dispatch"
	"github.com/ccfos/nightingale/v6/models"
	"github.com/toolkits/pkg/logger"
)

// SuppressionRecord is the audit-row shape the HookAdapter produces every
// time it suppresses an event. We define it locally (rather than importing
// the customModels.CustomSuppressedEvent struct) so the suppress package
// stays free of DB dependencies — it would create an import cycle with
// the bootstrap package and forces every test to drag in GORM.
//
// The bootstrap layer maps SuppressionRecord -> customModels and forwards
// it to the suppressrec async sink.
type SuppressionRecord struct {
	RuleId          int64
	RuleName        string
	SourceEventHash string
	TargetEventHash string
	SourceRuleName  string
	TargetRuleName  string
	SourceTags      string
	TargetTags      string
	SourceSeverity  int
	TargetSeverity  int
	GroupId         int64
	DatasourceId    int64
	SuppressedAt    int64
}

// RecordSink receives suppression records. The hot-path call is required
// to be non-blocking; the production implementation pushes onto a buffered
// channel and returns immediately.
type RecordSink interface {
	RecordSuppression(rec *SuppressionRecord)
}

// noopRecordSink is the default — no audit, no panic. Tests that don't
// care about the audit trail can rely on this being installed by default.
type noopRecordSink struct{}

func (noopRecordSink) RecordSuppression(*SuppressionRecord) {}

// HookAdapter bridges the suppress.Inhibitor into N9e's dispatch package
// via the existing dispatch.EventMuteHook seam. Wiring is split into a
// dedicated type so we can stack additional hooks (e.g. mute) in front of
// or behind suppression without touching this file.
type HookAdapter struct {
	inhibitor *Inhibitor
	previous  dispatch.EventMuteHookFunc
	sink      RecordSink
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
	return &HookAdapter{inhibitor: inh, previous: prev, sink: noopRecordSink{}}
}

// SetRecordSink swaps the audit sink. Bootstrap calls this once with the
// async sink; tests can leave it on noop or substitute a capturing fake.
func (h *HookAdapter) SetRecordSink(s RecordSink) {
	if s != nil {
		h.sink = s
	}
}

// Hook is the function value that should be assigned to dispatch.EventMuteHook
// during bootstrap. Returns true iff the event should be muted (dropped).
//
// Order of operations:
//  1. Run the inhibitor — it always registers source-side matches, even when
//     `previous` is going to suppress the event. Skipping this would mean a
//     muted-by-other-rule event silently fails to register as a root cause.
//  2. If the inhibitor decided to suppress, push an audit record onto the
//     sink (non-blocking) and return true.
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
		// Audit row push. Non-blocking by contract.
		h.sink.RecordSuppression(&SuppressionRecord{
			RuleId:          decision.ByRuleId,
			RuleName:        decision.ByRuleName,
			SourceEventHash: decision.BySourceHash,
			TargetEventHash: event.Hash,
			SourceRuleName:  decision.BySource.RuleName,
			TargetRuleName:  event.RuleName,
			SourceTags:      decision.BySource.TagsCSV,
			TargetTags:      event.Tags,
			SourceSeverity:  decision.BySource.Severity,
			TargetSeverity:  event.Severity,
			GroupId:         event.GroupId,
			DatasourceId:    event.DatasourceId,
		})
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
