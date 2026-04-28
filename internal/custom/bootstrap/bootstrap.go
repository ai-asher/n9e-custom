// Package bootstrap wires the custom denoise/suppress/mute modules into
// N9e's runtime. The whole point of this package is to keep the
// per-call-site footprint in upstream files at the absolute minimum:
//
//	center/center.go only needs:
//	  bootstrap.MigrateCustomTables(db)        // after migrate.Migrate(db)
//	  bootstrap.InstallHooks(ctx)              // before alert.Start(...)
//
// Everything else — repo construction, in-memory caches, hook chaining,
// processor registration — happens here.
//
// Today this package serves stub "static" rule providers seeded from the
// DB on startup, with NO periodic refresh. That is intentionally simple:
// the cache layer (a memsto-style auto-refresher) is a follow-up task.
// In the meantime, edits to custom_aggregate_rule / custom_inhibit_rule /
// custom_mute_cron tables require a service restart to take effect.
//
// The InstallHooks contract:
//   - safe to call exactly once during bootstrap
//   - replaces dispatch.EventMuteHook with a chained adapter
//   - registers an alert_aggregate Pipeline processor accessor
//
// Failure during InstallHooks logs and proceeds — a misconfigured custom
// rule must never block N9e startup.
package bootstrap

import (
	customModels "github.com/ccfos/nightingale/v6/internal/custom/models"
	"github.com/ccfos/nightingale/v6/pkg/ctx"
	"github.com/toolkits/pkg/logger"
	"gorm.io/gorm"

	// Side-effect import: registers the alert_aggregate Pipeline processor
	// type into models.processorRegister via init(). Without this blank
	// import, users could not add an "alert_aggregate" node from the UI.
	_ "github.com/ccfos/nightingale/v6/internal/custom/denoise/processor"
)

// MigrateCustomTables creates / updates all custom_* tables. Thin wrapper
// over the models package — exposed here so center.go imports a single
// bootstrap package rather than reaching deep into models.
func MigrateCustomTables(db *gorm.DB) {
	customModels.MigrateCustomTables(db)
}

// InstallHooks builds the runtime objects (rule providers, aggregator,
// inhibitor, evaluator), wires them onto N9e's hook seams, and registers
// the alert_aggregate pipeline processor accessor.
//
// The returned cleanup function is currently a no-op but kept in the
// signature so the cache layer can plug in its goroutine-stop logic later
// without changing center.go.
func InstallHooks(c *ctx.Context) func() {
	if c == nil {
		logger.Errorf("custom/bootstrap: nil context, hooks not installed")
		return func() {}
	}

	// Load rules once at startup. A nil DB or empty result is fine — the
	// providers simply hold an empty slice and every event passes through.
	denoiseRules := loadAggregateRules(c)
	suppressRules := loadInhibitRules(c)
	cronRules := loadMuteCronRules(c)
	emergency := loadEmergencyMute(c)

	logger.Infof("custom/bootstrap: loaded denoise=%d suppress=%d cron_mute=%d emergency_enabled=%v",
		len(denoiseRules), len(suppressRules), len(cronRules), emergency != nil && emergency.Enabled)

	// Build the chain: aggregator runs at pipeline level (separate seam),
	// while suppress + mute share dispatch.EventMuteHook.
	wireAggregator(c, denoiseRules)
	wireMuteAndSuppress(cronRules, emergency, suppressRules)

	return func() {}
}
