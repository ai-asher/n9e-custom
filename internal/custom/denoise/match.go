package denoise

import (
	"slices"

	customModels "github.com/ccfos/nightingale/v6/internal/custom/models"
	"github.com/ccfos/nightingale/v6/models"
)

// MatchRule reports whether the given event falls under the scope of the
// given aggregate rule. Returns false on the FIRST mismatch, in roughly
// cheapest-first order so disabled and group-mismatched rules are rejected
// without running tag regex.
//
// A returned `false` is silent: the event simply skips this rule and may
// match a different one (rule selection is the caller's job).
func MatchRule(event *models.AlertCurEvent, rule *customModels.CustomAggregateRule) bool {
	if rule == nil || event == nil || rule.Disabled == 1 {
		return false
	}

	// group_id == 0 means the rule is global; otherwise must match exactly.
	if rule.GroupId != 0 && rule.GroupId != event.GroupId {
		return false
	}

	if len(rule.DatasourceIdsJson) > 0 && rule.DatasourceIdsJson[0] != 0 &&
		event.DatasourceId != 0 &&
		!slices.Contains(rule.DatasourceIdsJson, event.DatasourceId) {
		return false
	}

	if len(rule.SeveritiesJson) > 0 && !slices.Contains(rule.SeveritiesJson, event.Severity) {
		return false
	}

	// Tag filter: an empty filter list (literally [] in JSON, or no rows
	// at all) means "match everything" — same convention as native
	// AlertMute.ITags. A non-empty list means ALL filters must match
	// (logical AND).
	if len(rule.Filters) > 0 {
		filters, err := parseTagFilters(rule.Filters)
		if err != nil {
			// Malformed JSON in the rule should NOT silently match every
			// event. Reject the rule outright.
			return false
		}
		// len(filters)==0 happens when the column literally holds "[]" —
		// that's a valid "no filter" config, fall through to the success
		// branch.
		if len(filters) > 0 && !matchAllTagFilters(event.TagsMap, filters) {
			return false
		}
	}

	return true
}

// SelectBestRule chooses one rule when an event matches more than one.
// The tie-breaker is deterministic:
//
//  1. Higher Priority wins (so operators can pin a specialized rule above
//     a catch-all).
//  2. Smaller id wins (older rule = more reviewed = safer fallback).
//
// Returns nil if no rule in the slice matches the event.
func SelectBestRule(event *models.AlertCurEvent, rules []*customModels.CustomAggregateRule) *customModels.CustomAggregateRule {
	var best *customModels.CustomAggregateRule
	for _, r := range rules {
		if !MatchRule(event, r) {
			continue
		}
		if best == nil {
			best = r
			continue
		}
		if r.Priority > best.Priority ||
			(r.Priority == best.Priority && r.Id < best.Id) {
			best = r
		}
	}
	return best
}
