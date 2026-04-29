package suppress

import (
	"github.com/ccfos/nightingale/v6/models"
)

// Inhibitor decides whether an incoming event should be suppressed and, as a
// side effect, registers the event as a root cause if any rule's SourceMatch
// claims it.
//
// Returned by Decide():
//   - suppressed=true  → caller should drop the event (i.e. the configured
//     EventMuteHook returns true)
//   - registered slice → list of (ruleId, sourceEventHash) the event was
//     registered under, for observability/tests
//
// The function is split out from the Hook adapter so it is easy to unit-test
// in isolation (no global state, no DB).
type Inhibitor struct {
	rules RuleProvider
	index *RootCauseIndex
}

func NewInhibitor(rules RuleProvider, idx *RootCauseIndex) *Inhibitor {
	return &Inhibitor{rules: rules, index: idx}
}

// Registration records that an event was added to the root-cause index
// under a particular rule. Returned (rather than logged) so callers can
// emit structured metrics or test assertions on it.
type Registration struct {
	RuleId    int64
	EventHash string
}

// Decision is the suppression verdict for one event.
type Decision struct {
	Suppressed       bool
	BySourceHash     string // populated when Suppressed=true; empty otherwise
	ByRuleId         int64
	ByRuleName       string // snapshot of the inhibit rule's name at decision time
	BySource         SourceSnapshot
	NewRegistrations []Registration
}

// Decide evaluates every active rule against the event and returns the
// combined verdict. The function performs both jobs in one pass so callers
// don't need to walk the rule set twice; root-cause registration happens
// regardless of whether the event ends up suppressed (a single event can
// be both a root cause for tier-2 alerts AND suppressed by a tier-0 cause).
func (s *Inhibitor) Decide(event *models.AlertCurEvent) Decision {
	if event == nil {
		return Decision{}
	}

	rules := s.rules.GetActive()
	out := Decision{}

	for _, r := range rules {
		// Datasource scoping: empty list = all datasources. Non-empty list
		// must contain the event's datasource id; mismatch skips this rule.
		if len(r.Rule.DatasourceIdsJson) > 0 && r.Rule.DatasourceIdsJson[0] != 0 &&
			event.DatasourceId != 0 && !contains(r.Rule.DatasourceIdsJson, event.DatasourceId) {
			continue
		}
		// Group scope (0 = global).
		if r.Rule.GroupId != 0 && r.Rule.GroupId != event.GroupId {
			continue
		}

		// Source side: if the event matches SourceMatch, register it as a
		// live source under this rule. We do this BEFORE the suppression
		// check so the act of "I am a root cause" doesn't depend on the
		// event also being a target.
		if matchAllTagFilters(event.TagsMap, r.SourceMatch) {
			equalValues := extractEqualValues(event.TagsMap, r.EqualLabels)
			// RegisterFull persists a small snapshot used by the
			// suppression audit page; without it the page would have to
			// re-query alert_cur_event for every row, which is far too
			// expensive on a hot table.
			s.index.RegisterFull(
				r.Rule.Id,
				event.Hash,
				equalValues,
				event.RuleName,
				event.Tags, // CSV form, already populated by N9e's pipeline
				event.Severity,
				event.GroupId,
				event.DatasourceId,
			)
			out.NewRegistrations = append(out.NewRegistrations,
				Registration{RuleId: r.Rule.Id, EventHash: event.Hash})
		}

		// Target side: if already-decided as suppressed by another rule,
		// keep checking remaining rules ONLY for source-side registration
		// (above). We don't break here because we want to register under
		// every applicable rule even if suppression is already decided.
		if out.Suppressed {
			continue
		}

		if !matchAllTagFilters(event.TagsMap, r.TargetMatch) {
			continue
		}

		targetValues := extractEqualValues(event.TagsMap, r.EqualLabels)
		if snap, ok := s.index.MatchAnyFull(r.Rule.Id, targetValues); ok {
			// Skip self-suppression: an event must not suppress itself
			// when it matches both Source AND Target sides of the same
			// rule. (Common with severity-based rules like "critical
			// suppresses warning" — a critical alert matches both sides.)
			if snap.EventHash == event.Hash {
				continue
			}
			out.Suppressed = true
			out.BySourceHash = snap.EventHash
			out.ByRuleId = r.Rule.Id
			out.ByRuleName = r.Rule.Name
			out.BySource = snap
		}
	}

	return out
}

// extractEqualValues pulls the values for the given label keys from the
// event's tag map. Missing labels are simply skipped — equalValuesMatch in
// the index treats keys present on the source but absent on the target as a
// non-match, which is the correct semantics: if the source has host=db01
// but the target has no host label at all, the suppression must NOT apply.
func extractEqualValues(tags map[string]string, keys []string) map[string]string {
	if len(keys) == 0 {
		return nil
	}
	out := make(map[string]string, len(keys))
	for _, k := range keys {
		if v, ok := tags[k]; ok {
			out[k] = v
		}
	}
	return out
}

func contains(haystack []int64, needle int64) bool {
	for _, v := range haystack {
		if v == needle {
			return true
		}
	}
	return false
}
