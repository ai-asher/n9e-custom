package mute

import (
	"time"

	"github.com/ccfos/nightingale/v6/models"
)

// Evaluator combines all mute sources owned by this package: cron rules
// plus the global emergency switch. Evaluation order is "cheapest first":
// emergency (a single atomic load) before cron (a per-rule loop).
//
// This split — keeping the rule provider and emergency holder separate —
// lets the bootstrap layer wire each source independently. Tests can
// substitute either piece without owning the whole stack.
type Evaluator struct {
	cron      CronRuleProvider
	emergency *EmergencyHolder
	now       func() time.Time
}

func NewEvaluator(cron CronRuleProvider, emergency *EmergencyHolder) *Evaluator {
	return &Evaluator{
		cron:      cron,
		emergency: emergency,
		now:       time.Now,
	}
}

// MuteVerdict carries the result of evaluating an event. Reason is the
// operator-facing string included in the suppression log (e.g. shown in
// the audit trail under custom_mute_event_log if/when we add it).
type MuteVerdict struct {
	Muted  bool
	Reason string
	RuleId int64 // 0 for emergency mute; otherwise the cron rule id
}

// Evaluate checks the event against every mute source. First match wins —
// later sources are skipped. Reason and RuleId on the verdict identify
// which source fired.
func (e *Evaluator) Evaluate(event *models.AlertCurEvent) MuteVerdict {
	if event == nil {
		return MuteVerdict{}
	}

	now := e.now()

	if e.emergency != nil {
		if matched, reason := e.emergency.Match(event, now); matched {
			return MuteVerdict{
				Muted:  true,
				Reason: "emergency mute: " + reason,
			}
		}
	}

	if e.cron != nil {
		for _, rule := range e.cron.GetActive() {
			if rule.Match(event, now) {
				return MuteVerdict{
					Muted:  true,
					Reason: "cron mute rule " + rule.Rule.Note,
					RuleId: rule.Rule.Id,
				}
			}
		}
	}

	return MuteVerdict{}
}
