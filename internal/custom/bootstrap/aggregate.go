package bootstrap

import (
	"github.com/ccfos/nightingale/v6/internal/custom/denoise"
	denoiseProcessor "github.com/ccfos/nightingale/v6/internal/custom/denoise/processor"
	customModels "github.com/ccfos/nightingale/v6/internal/custom/models"
	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/ctx"
	"github.com/toolkits/pkg/logger"
)

// loadAggregateRules pulls the active aggregate rules from DB. A failed
// query is logged and treated as "no rules" — the system stays online,
// just without aggregation.
func loadAggregateRules(c *ctx.Context) []*customModels.CustomAggregateRule {
	var rules []*customModels.CustomAggregateRule
	if err := models.DB(c).Where("disabled = 0").Find(&rules).Error; err != nil {
		logger.Errorf("custom/bootstrap: failed to load aggregate rules: %v", err)
		return nil
	}
	logger.Infof("custom/bootstrap: loadAggregateRules raw count=%d", len(rules))
	// Decode JSON columns into their *Json siblings so MatchRule can
	// inspect them without re-parsing on every event.
	for _, r := range rules {
		decodeRuleJSON(r)
		logger.Infof("custom/bootstrap: rule id=%d name=%q disabled=%d window_sec=%d dimensions_len=%d",
			r.Id, r.Name, r.Disabled, r.WindowSec, len(r.Dimensions))
	}
	return rules
}

// wireAggregator constructs the aggregator with the loaded rules and
// registers the accessor used by the alert_aggregate pipeline processor.
//
// We capture `agg` in a closure so the processor sees the same instance
// across the lifetime of the process; future hot-reload could swap the
// closure target atomically.
func wireAggregator(c *ctx.Context, rules []*customModels.CustomAggregateRule) {
	provider := &denoise.StaticRuleProvider{Rules: rules}
	repo := denoise.NewRepo(c)
	idx := denoise.NewActiveIncidentIndex()
	storm := denoise.NewStormDetector()

	agg := denoise.NewAggregator(provider, repo, idx, storm)

	denoiseProcessor.Wire(func() *denoise.Aggregator { return agg })

	logger.Infof("custom/bootstrap: aggregator wired (alert_aggregate processor available)")
}

// decodeRuleJSON populates the *Json convenience fields on the rule from
// the comma-separated DB columns. Mirrors what gorm hooks would do if we
// added them to CustomAggregateRule (we deliberately did not — keeping
// the model dumb makes raw queries simpler).
func decodeRuleJSON(r *customModels.CustomAggregateRule) {
	if r == nil {
		return
	}
	r.DatasourceIdsJson = parseInt64CSV(r.DatasourceIds)
	r.SeveritiesJson = parseIntCSV(r.Severities)
}
