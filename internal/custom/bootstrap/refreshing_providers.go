package bootstrap

import (
	"context"

	customModels "github.com/ccfos/nightingale/v6/internal/custom/models"
	"github.com/ccfos/nightingale/v6/internal/custom/mute"
	"github.com/ccfos/nightingale/v6/internal/custom/refresher"
	"github.com/ccfos/nightingale/v6/internal/custom/suppress"
	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/ctx"
	"github.com/toolkits/pkg/logger"
)

// This file glues the generic refresher.Refresher to the three module-
// specific RuleProvider interfaces (denoise / suppress / mute). Each
// adapter is the smallest amount of code that satisfies the target
// interface and forwards GetActive to refresher.Get.

// ── Aggregate (denoise) ────────────────────────────────────────────

type refreshingAggregateProvider struct {
	r *refresher.Refresher[*customModels.CustomAggregateRule]
}

func (p *refreshingAggregateProvider) GetActiveRules() []*customModels.CustomAggregateRule {
	return p.r.Get()
}

func newRefreshingAggregateProvider(c *ctx.Context) *refreshingAggregateProvider {
	r := refresher.New("aggregate-rules", aggregateRefreshInterval,
		func(_ context.Context) ([]*customModels.CustomAggregateRule, error) {
			var rows []*customModels.CustomAggregateRule
			if err := models.DB(c).Where("disabled = 0").Find(&rows).Error; err != nil {
				return nil, err
			}
			for _, row := range rows {
				row.DatasourceIdsJson = parseInt64CSV(row.DatasourceIds)
				row.SeveritiesJson = parseIntCSV(row.Severities)
			}
			return rows, nil
		})
	r.Start(c.Ctx)
	return &refreshingAggregateProvider{r: r}
}

// ── Suppress ──────────────────────────────────────────────────────

type refreshingSuppressProvider struct {
	r *refresher.Refresher[*suppress.CompiledRule]
}

func (p *refreshingSuppressProvider) GetActive() []*suppress.CompiledRule {
	return p.r.Get()
}

func newRefreshingSuppressProvider(c *ctx.Context) *refreshingSuppressProvider {
	r := refresher.New("inhibit-rules", suppressRefreshInterval,
		func(_ context.Context) ([]*suppress.CompiledRule, error) {
			var rows []*customModels.CustomInhibitRule
			if err := models.DB(c).Where("disabled = 0").Find(&rows).Error; err != nil {
				return nil, err
			}
			out := make([]*suppress.CompiledRule, 0, len(rows))
			for _, row := range rows {
				row.DatasourceIdsJson = parseInt64CSV(row.DatasourceIds)
				compiled, err := suppress.CompileRule(row)
				if err != nil {
					logger.Warningf("custom/refresh: skipping inhibit rule id=%d: %v", row.Id, err)
					continue
				}
				out = append(out, compiled)
			}
			return out, nil
		})
	r.Start(c.Ctx)
	return &refreshingSuppressProvider{r: r}
}

// ── Cron mute ─────────────────────────────────────────────────────

type refreshingCronProvider struct {
	r *refresher.Refresher[*mute.CompiledCronRule]
}

func (p *refreshingCronProvider) GetActive() []*mute.CompiledCronRule {
	return p.r.Get()
}

func newRefreshingCronProvider(c *ctx.Context) *refreshingCronProvider {
	r := refresher.New("mute-cron-rules", cronRefreshInterval,
		func(_ context.Context) ([]*mute.CompiledCronRule, error) {
			var rows []*customModels.CustomMuteCron
			if err := models.DB(c).Where("disabled = 0").Find(&rows).Error; err != nil {
				return nil, err
			}
			out := make([]*mute.CompiledCronRule, 0, len(rows))
			for _, row := range rows {
				row.DatasourceIdsJson = parseInt64CSV(row.DatasourceIds)
				row.SeveritiesJson = parseIntCSV(row.Severities)
				compiled, err := mute.CompileCronRule(row)
				if err != nil {
					logger.Warningf("custom/refresh: skipping cron mute rule id=%d: %v", row.Id, err)
					continue
				}
				out = append(out, compiled)
			}
			return out, nil
		})
	r.Start(c.Ctx)
	return &refreshingCronProvider{r: r}
}

// ── Emergency mute (singleton, fastest cadence) ───────────────────

// Emergency mute uses the *EmergencyHolder pattern (lock-free Set/Match)
// rather than slice-of-rules, so we don't go through refresher.Refresher
// for it. Instead we spawn a tiny goroutine that re-reads the singleton
// row every emergencyRefreshInterval and atomically swaps the holder.
//
// We deliberately keep this separate because EmergencyHolder ALREADY does
// the atomic.Pointer dance internally (see internal/custom/mute/emergency.go).
// Wrapping it in another Refresher would be redundant.
func startEmergencyRefresh(c *ctx.Context, holder *mute.EmergencyHolder) {
	go func() {
		t := newTicker(emergencyRefreshInterval)
		defer t.Stop()
		for {
			select {
			case <-c.Ctx.Done():
				return
			case <-t.C:
				row := loadEmergencyMute(c)
				holder.Set(row) // Set(nil) is treated as 'disabled'
			}
		}
	}()
}
