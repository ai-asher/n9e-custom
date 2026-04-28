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
	"github.com/ccfos/nightingale/v6/internal/custom/mute"
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

	// Build refreshing providers. Each one runs a synchronous warm-up
	// load (Start does this under the hood), so by the time the function
	// returns, GetActive() reflects the current DB state. After that the
	// providers refresh on their own cadence in background goroutines.
	aggProvider := newRefreshingAggregateProvider(c)
	suppressProvider := newRefreshingSuppressProvider(c)
	cronProvider := newRefreshingCronProvider(c)

	logger.Infof("custom/bootstrap: refreshing providers started — denoise=%d suppress=%d cron_mute=%d (refresh=5s)",
		len(aggProvider.GetActiveRules()),
		len(suppressProvider.GetActive()),
		len(cronProvider.GetActive()))

	// Wire aggregator — pipeline processor seam.
	wireAggregatorWithProvider(c, aggProvider)

	// Wire mute + suppress hook chain. Emergency mute uses its own
	// EmergencyHolder (atomic.Pointer); we seed it with the current row
	// then start a 1-second refresh so the kill-switch is responsive.
	emergencyHolder := mute.NewEmergencyHolder()
	if initial := loadEmergencyMute(c); initial != nil {
		emergencyHolder.Set(initial)
	}
	startEmergencyRefresh(c, emergencyHolder)

	wireMuteAndSuppressWithProviders(cronProvider, emergencyHolder, suppressProvider)

	logger.Infof("custom/bootstrap: hook chain installed (mute->suppress->noop), emergency refresh=1s")

	return func() {
		// No-op for now. The refresher goroutines respect ctx.Ctx and
		// exit when the application context is cancelled, so the only
		// reason to call this would be selective teardown in tests.
	}
}
