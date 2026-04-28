package denoise

import (
	customModels "github.com/ccfos/nightingale/v6/internal/custom/models"
)

// RuleProvider returns the currently-active aggregate rules. The Pipeline
// processor calls this on every Process() invocation, so the implementation
// MUST be cheap — typically backed by a periodically-refreshed in-memory
// cache (analogous to N9e's memsto.AlertMuteCache).
//
// We keep this as an interface — rather than passing a concrete cache
// struct — so the Process() function is trivially testable with a static
// rule slice and so the cache implementation can evolve (or be mocked
// out) without touching the processor code.
type RuleProvider interface {
	GetActiveRules() []*customModels.CustomAggregateRule
}

// StaticRuleProvider serves a fixed slice — used by tests and as a stub
// during early bring-up before the memsto cache is wired in.
type StaticRuleProvider struct {
	Rules []*customModels.CustomAggregateRule
}

func (s *StaticRuleProvider) GetActiveRules() []*customModels.CustomAggregateRule {
	return s.Rules
}
