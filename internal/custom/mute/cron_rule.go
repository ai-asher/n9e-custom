package mute

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"

	customModels "github.com/ccfos/nightingale/v6/internal/custom/models"
	"github.com/ccfos/nightingale/v6/models"
)

// CompiledCronRule is a CustomMuteCron with cron expression, tag filters,
// and JSON arrays parsed once and cached. The hot path (Match) reuses the
// parsed forms across all events until the rule set is refreshed.
type CompiledCronRule struct {
	Rule          *customModels.CustomMuteCron
	Cron          *CompiledCron
	TagFilters    []models.TagFilter
	DatasourceIds []int64
	Severities    []int
}

// CompileCronRule transforms a stored CustomMuteCron into its compiled form.
// A rule that fails to compile is rejected outright — the alternative
// (silently treating it as "match nothing") would let typos in cron mute
// every alert without anyone noticing.
func CompileCronRule(raw *customModels.CustomMuteCron) (*CompiledCronRule, error) {
	if raw == nil {
		return nil, fmt.Errorf("nil rule")
	}

	cc, err := CompileCron(raw.CronExpr, raw.Timezone, raw.DurationSec)
	if err != nil {
		return nil, fmt.Errorf("rule %d: %w", raw.Id, err)
	}

	tagFilters, err := decodeTagFilters(raw.Tags)
	if err != nil {
		return nil, fmt.Errorf("rule %d tags: %w", raw.Id, err)
	}

	return &CompiledCronRule{
		Rule:          raw,
		Cron:          cc,
		TagFilters:    tagFilters,
		DatasourceIds: raw.DatasourceIdsJson,
		Severities:    raw.SeveritiesJson,
	}, nil
}

// Match reports whether the event should be muted by this rule at time `now`.
// All four conditions are AND-combined:
//
//  1. cron schedule is currently active
//  2. event's datasource is in scope (or scope is "all")
//  3. event's severity is in scope (or scope is "all")
//  4. all tag filters match
//
// First-mismatch short-circuits — checks are ordered cheapest first so a
// disabled-or-out-of-scope event never pays the cron evaluation cost.
func (c *CompiledCronRule) Match(event *models.AlertCurEvent, now time.Time) bool {
	if c == nil || c.Rule == nil || c.Rule.Disabled == 1 || event == nil {
		return false
	}

	// Group scope (0 = global).
	if c.Rule.GroupId != 0 && c.Rule.GroupId != event.GroupId {
		return false
	}

	// Datasource scope.
	if len(c.DatasourceIds) > 0 && c.DatasourceIds[0] != 0 &&
		event.DatasourceId != 0 &&
		!slices.Contains(c.DatasourceIds, event.DatasourceId) {
		return false
	}

	// Severity scope.
	if len(c.Severities) > 0 && !slices.Contains(c.Severities, event.Severity) {
		return false
	}

	// Tag filter — empty means "match anything".
	if len(c.TagFilters) > 0 && !matchAllTagFilters(event.TagsMap, c.TagFilters) {
		return false
	}

	// Most expensive check last.
	return c.Cron.IsActive(now)
}

// CronRuleProvider returns the active set of compiled cron mute rules.
// Implementations are expected to refresh from DB periodically and keep
// the slice immutable between refreshes (the hot path does not lock).
type CronRuleProvider interface {
	GetActive() []*CompiledCronRule
}

// StaticCronRuleProvider serves a fixed slice; used by tests and as a stub
// before the memsto cache wiring lands.
type StaticCronRuleProvider struct {
	Rules []*CompiledCronRule
}

func (s *StaticCronRuleProvider) GetActive() []*CompiledCronRule { return s.Rules }

// decodeTagFilters mirrors the helper in suppress/. Kept private to mute
// for the same reason: a shared common package would create coupling that
// outweighs the cost of a 15-line duplicate.
func decodeTagFilters(raw []byte) ([]models.TagFilter, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var filters []models.TagFilter
	if err := json.Unmarshal(raw, &filters); err != nil {
		return nil, err
	}
	return models.ParseTagFilter(filters)
}
