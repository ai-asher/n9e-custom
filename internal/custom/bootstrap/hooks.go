package bootstrap

import (
	"github.com/ccfos/nightingale/v6/alert/dispatch"
	customModels "github.com/ccfos/nightingale/v6/internal/custom/models"
	"github.com/ccfos/nightingale/v6/internal/custom/mute"
	"github.com/ccfos/nightingale/v6/internal/custom/suppress"
	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/ctx"
	"github.com/toolkits/pkg/logger"
)

// loadInhibitRules pulls active inhibit rules and pre-compiles them.
// Compilation errors per-rule are logged and the rule is skipped, so a
// single bad rule cannot poison the whole feature.
func loadInhibitRules(c *ctx.Context) []*suppress.CompiledRule {
	var raw []*customModels.CustomInhibitRule
	if err := models.DB(c).Where("disabled = 0").Find(&raw).Error; err != nil {
		logger.Errorf("custom/bootstrap: failed to load inhibit rules: %v", err)
		return nil
	}
	out := make([]*suppress.CompiledRule, 0, len(raw))
	for _, r := range raw {
		// Hydrate JSON columns the model needs for compilation.
		r.DatasourceIdsJson = parseInt64CSV(r.DatasourceIds)
		c, err := suppress.CompileRule(r)
		if err != nil {
			logger.Errorf("custom/bootstrap: skip inhibit rule %d: %v", r.Id, err)
			continue
		}
		out = append(out, c)
	}
	return out
}

// loadMuteCronRules pulls active cron mute rules and pre-compiles them.
func loadMuteCronRules(c *ctx.Context) []*mute.CompiledCronRule {
	var raw []*customModels.CustomMuteCron
	if err := models.DB(c).Where("disabled = 0").Find(&raw).Error; err != nil {
		logger.Errorf("custom/bootstrap: failed to load cron mute rules: %v", err)
		return nil
	}
	out := make([]*mute.CompiledCronRule, 0, len(raw))
	for _, r := range raw {
		r.DatasourceIdsJson = parseInt64CSV(r.DatasourceIds)
		r.SeveritiesJson = parseIntCSV(r.Severities)
		// r.Tags is already []byte (ormx.JSONArr); CompileCronRule decodes it.
		cc, err := mute.CompileCronRule(r)
		if err != nil {
			logger.Errorf("custom/bootstrap: skip cron mute rule %d: %v", r.Id, err)
			continue
		}
		out = append(out, cc)
	}
	return out
}

// loadEmergencyMute fetches the singleton emergency-mute row (id=1 by
// convention). Missing row → returns nil → emergency is treated as off.
func loadEmergencyMute(c *ctx.Context) *mute.EmergencyState {
	var row customModels.CustomEmergencyMute
	if err := models.DB(c).Where("id = 1").First(&row).Error; err != nil {
		// Not found is the common case on a fresh install — log at debug.
		logger.Debugf("custom/bootstrap: no emergency mute row, defaulting to disabled (%v)", err)
		return nil
	}
	row.DatasourceIdsJson = parseInt64CSV(row.DatasourceIds)
	row.GroupIdsJson = parseInt64CSV(row.GroupIds)
	return mute.FromModel(&row)
}

// wireMuteAndSuppress builds the dispatch.EventMuteHook chain in the
// recommended order:
//
//	mute.HookAdapter -> suppress.HookAdapter -> noop
//
// An event explicitly muted by an operator's rule short-circuits before
// the suppress lookup runs.
func wireMuteAndSuppress(
	cronRules []*mute.CompiledCronRule,
	emergency *mute.EmergencyState,
	suppressRules []*suppress.CompiledRule,
) {
	// Inner: suppress.
	suppressIdx := suppress.NewRootCauseIndex(600)
	suppressInh := suppress.NewInhibitor(
		&suppress.StaticRuleProvider{Rules: suppressRules},
		suppressIdx,
	)
	suppressHook := suppress.NewHookAdapter(suppressInh, nil)

	// Outer: mute.
	emergencyHolder := mute.NewEmergencyHolder()
	if emergency != nil {
		emergencyHolder.Set(emergency)
	}
	muteEval := mute.NewEvaluator(
		&mute.StaticCronRuleProvider{Rules: cronRules},
		emergencyHolder,
	)
	muteHook := mute.NewHookAdapter(muteEval, suppressHook.Hook)

	muteHook.Install()

	logger.Infof("custom/bootstrap: dispatch.EventMuteHook installed (mute->suppress->noop)")
	_ = dispatch.EventMuteHook // avoid unused-import if compiler complains
}
