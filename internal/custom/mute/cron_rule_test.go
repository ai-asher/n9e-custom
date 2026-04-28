package mute

import (
	"encoding/json"
	"testing"
	"time"

	customModels "github.com/ccfos/nightingale/v6/internal/custom/models"
	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/ormx"
)

// alwaysActiveRule produces a CompiledCronRule whose Cron schedule fires
// every minute with a 5-minute window — effectively "always active" so
// tests can isolate the filter logic from cron timing.
func alwaysActiveRule(t *testing.T, mods func(*customModels.CustomMuteCron)) *CompiledCronRule {
	t.Helper()
	raw := &customModels.CustomMuteCron{
		Id:          1,
		CronExpr:    "* * * * *",
		DurationSec: 300,
	}
	if mods != nil {
		mods(raw)
	}
	c, err := CompileCronRule(raw)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return c
}

func TestCompileCronRule_RejectsBadCron(t *testing.T) {
	raw := &customModels.CustomMuteCron{Id: 1, CronExpr: "garbage", DurationSec: 60}
	if _, err := CompileCronRule(raw); err == nil {
		t.Fatalf("bad cron must reject")
	}
}

func TestMatch_DisabledRuleIgnored(t *testing.T) {
	rule := alwaysActiveRule(t, func(r *customModels.CustomMuteCron) { r.Disabled = 1 })
	event := &models.AlertCurEvent{}
	if rule.Match(event, time.Now()) {
		t.Fatalf("disabled rule must not match")
	}
}

func TestMatch_GroupScope(t *testing.T) {
	event := &models.AlertCurEvent{GroupId: 7}

	global := alwaysActiveRule(t, func(r *customModels.CustomMuteCron) { r.GroupId = 0 })
	if !global.Match(event, time.Now()) {
		t.Fatalf("global rule must match")
	}

	other := alwaysActiveRule(t, func(r *customModels.CustomMuteCron) { r.GroupId = 8 })
	if other.Match(event, time.Now()) {
		t.Fatalf("rule for group 8 must not match group 7 event")
	}

	same := alwaysActiveRule(t, func(r *customModels.CustomMuteCron) { r.GroupId = 7 })
	if !same.Match(event, time.Now()) {
		t.Fatalf("rule for group 7 must match group 7 event")
	}
}

func TestMatch_DatasourceScope(t *testing.T) {
	event := &models.AlertCurEvent{DatasourceId: 5}

	rule := alwaysActiveRule(t, func(r *customModels.CustomMuteCron) {
		r.DatasourceIdsJson = []int64{1, 2, 3}
	})
	if rule.Match(event, time.Now()) {
		t.Fatalf("event ds=5 must not match rule ds list [1,2,3]")
	}

	rule = alwaysActiveRule(t, func(r *customModels.CustomMuteCron) {
		r.DatasourceIdsJson = []int64{5, 6}
	})
	if !rule.Match(event, time.Now()) {
		t.Fatalf("event ds=5 must match rule ds list [5,6]")
	}
}

func TestMatch_SeverityScope(t *testing.T) {
	event := &models.AlertCurEvent{Severity: 2}

	rule := alwaysActiveRule(t, func(r *customModels.CustomMuteCron) {
		r.SeveritiesJson = []int{1}
	})
	if rule.Match(event, time.Now()) {
		t.Fatalf("severity 2 must not match rule for [1]")
	}

	rule = alwaysActiveRule(t, func(r *customModels.CustomMuteCron) {
		r.SeveritiesJson = []int{1, 2}
	})
	if !rule.Match(event, time.Now()) {
		t.Fatalf("severity 2 must match rule for [1,2]")
	}
}

func TestMatch_TagFilter(t *testing.T) {
	tagBytes, _ := json.Marshal([]models.TagFilter{
		{Key: "env", Op: "==", Func: "==", Value: "prod"},
	})

	rule := alwaysActiveRule(t, func(r *customModels.CustomMuteCron) {
		r.Tags = ormx.JSONArr(tagBytes)
	})

	prodEvent := &models.AlertCurEvent{TagsMap: map[string]string{"env": "prod"}}
	if !rule.Match(prodEvent, time.Now()) {
		t.Fatalf("env=prod event must match rule for env=prod")
	}

	devEvent := &models.AlertCurEvent{TagsMap: map[string]string{"env": "dev"}}
	if rule.Match(devEvent, time.Now()) {
		t.Fatalf("env=dev event must not match rule for env=prod")
	}
}

func TestMatch_OutsideCronWindowReturnsFalse(t *testing.T) {
	// Cron fires at minute 0 every hour; window 60s.
	raw := &customModels.CustomMuteCron{
		Id:          1,
		CronExpr:    "0 * * * *",
		DurationSec: 60,
		Timezone:    "UTC",
	}
	rule, err := CompileCronRule(raw)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	event := &models.AlertCurEvent{}

	// At 14:30 UTC — well outside 14:00's 60s window.
	at1430 := mustParseTime(t, time.RFC3339, "2026-04-28T14:30:00Z")
	// Inject clock by calling Cron.IsActive directly via Match path.
	if rule.Cron.IsActive(at1430) {
		t.Fatalf("cron must not be active at 14:30")
	}
	// Use Match's "now" parameter through a closure over the public surface.
	got := rule.Match(event, at1430)
	if got {
		t.Fatalf("Match must return false outside cron window")
	}
}

func TestStaticCronRuleProvider_ReturnsRulesAsIs(t *testing.T) {
	r := alwaysActiveRule(t, nil)
	p := &StaticCronRuleProvider{Rules: []*CompiledCronRule{r}}
	got := p.GetActive()
	if len(got) != 1 || got[0] != r {
		t.Fatalf("provider must return exact slice")
	}
}
