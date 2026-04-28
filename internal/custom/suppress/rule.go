package suppress

import (
	"encoding/json"
	"fmt"

	customModels "github.com/ccfos/nightingale/v6/internal/custom/models"
	"github.com/ccfos/nightingale/v6/models"
)

// CompiledRule is an InhibitRule with TagFilters pre-parsed and label slices
// already JSON-decoded. The Aggregator-side equivalent re-parses on each
// hot-path call; here we cache the compiled form because suppression runs
// on EVERY alert event AND iterates over every rule, so we want the
// per-event cost to stay flat regardless of rule count.
type CompiledRule struct {
	Rule        *customModels.CustomInhibitRule
	SourceMatch []models.TagFilter
	TargetMatch []models.TagFilter
	EqualLabels []string
}

// RuleProvider returns the currently-active set of inhibit rules in their
// compiled (parse-once) form. Implementations are expected to refresh
// periodically; the hook calls GetActive on every event so it must be O(1).
type RuleProvider interface {
	GetActive() []*CompiledRule
}

// CompileRule parses the JSON-encoded fields on a raw inhibit rule and
// returns a CompiledRule ready for fast matching. Returns an error rather
// than a partial rule on bad input so a malformed rule cannot silently
// fall back to "match everything".
func CompileRule(raw *customModels.CustomInhibitRule) (*CompiledRule, error) {
	if raw == nil {
		return nil, fmt.Errorf("nil rule")
	}

	src, err := decodeTagFilters(raw.SourceMatch)
	if err != nil {
		return nil, fmt.Errorf("source_match: %w", err)
	}
	if len(src) == 0 {
		// An empty SourceMatch would treat every event as a root cause —
		// almost certainly a config error, refuse it.
		return nil, fmt.Errorf("source_match must contain at least one filter")
	}

	tgt, err := decodeTagFilters(raw.TargetMatch)
	if err != nil {
		return nil, fmt.Errorf("target_match: %w", err)
	}
	if len(tgt) == 0 {
		return nil, fmt.Errorf("target_match must contain at least one filter")
	}

	var equalLabels []string
	if len(raw.EqualLabels) > 0 {
		if err := json.Unmarshal([]byte(raw.EqualLabels), &equalLabels); err != nil {
			return nil, fmt.Errorf("equal_labels: %w", err)
		}
	}

	return &CompiledRule{
		Rule:        raw,
		SourceMatch: src,
		TargetMatch: tgt,
		EqualLabels: equalLabels,
	}, nil
}

// StaticRuleProvider serves a pre-compiled slice — primarily a test fixture
// and a stand-in for the eventual memsto-backed cache.
type StaticRuleProvider struct {
	Rules []*CompiledRule
}

func (s *StaticRuleProvider) GetActive() []*CompiledRule { return s.Rules }

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
