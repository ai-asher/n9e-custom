package denoise

import (
	"encoding/json"
	"testing"

	customModels "github.com/ccfos/nightingale/v6/internal/custom/models"
	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/ormx"
)

func mustJSONArr(t *testing.T, v interface{}) ormx.JSONArr {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return ormx.JSONArr(b)
}

func TestMatchRule_DisabledRuleNeverMatches(t *testing.T) {
	rule := &customModels.CustomAggregateRule{Disabled: 1}
	event := &models.AlertCurEvent{}
	if MatchRule(event, rule) {
		t.Fatalf("disabled rule must not match")
	}
}

func TestMatchRule_GroupIdScope(t *testing.T) {
	event := &models.AlertCurEvent{GroupId: 7, Severity: 1}

	// Global rule (group_id=0) matches any group.
	if !MatchRule(event, &customModels.CustomAggregateRule{GroupId: 0}) {
		t.Fatalf("global rule should match")
	}
	// Same group matches.
	if !MatchRule(event, &customModels.CustomAggregateRule{GroupId: 7}) {
		t.Fatalf("matching group_id should match")
	}
	// Different group does not match.
	if MatchRule(event, &customModels.CustomAggregateRule{GroupId: 8}) {
		t.Fatalf("different group_id must not match")
	}
}

func TestMatchRule_SeverityFilter(t *testing.T) {
	event := &models.AlertCurEvent{Severity: 2}

	rule := &customModels.CustomAggregateRule{SeveritiesJson: []int{1}}
	if MatchRule(event, rule) {
		t.Fatalf("severity 2 should not match rule for severity 1")
	}
	rule.SeveritiesJson = []int{1, 2}
	if !MatchRule(event, rule) {
		t.Fatalf("severity 2 should match rule containing 2")
	}
}

func TestMatchRule_TagFilter(t *testing.T) {
	event := &models.AlertCurEvent{TagsMap: map[string]string{"env": "prod"}}

	rule := &customModels.CustomAggregateRule{
		Filters: mustJSONArr(t, []models.TagFilter{
			{Key: "env", Op: "==", Func: "==", Value: "prod"},
		}),
	}
	if !MatchRule(event, rule) {
		t.Fatalf("env=prod filter should match")
	}

	rule.Filters = mustJSONArr(t, []models.TagFilter{
		{Key: "env", Op: "==", Func: "==", Value: "dev"},
	})
	if MatchRule(event, rule) {
		t.Fatalf("env=dev filter should not match prod event")
	}
}

func TestSelectBestRule_PriorityWins(t *testing.T) {
	event := &models.AlertCurEvent{}

	low := &customModels.CustomAggregateRule{Id: 1, Priority: 0}
	high := &customModels.CustomAggregateRule{Id: 2, Priority: 5}

	got := SelectBestRule(event, []*customModels.CustomAggregateRule{low, high})
	if got != high {
		t.Fatalf("higher priority rule should win")
	}
}

func TestSelectBestRule_TiebreakerByLowerId(t *testing.T) {
	event := &models.AlertCurEvent{}

	a := &customModels.CustomAggregateRule{Id: 5, Priority: 1}
	b := &customModels.CustomAggregateRule{Id: 3, Priority: 1}

	got := SelectBestRule(event, []*customModels.CustomAggregateRule{a, b})
	if got != b {
		t.Fatalf("lower id should win on priority tie; got id=%d", got.Id)
	}
}

func TestSelectBestRule_NoneMatchReturnsNil(t *testing.T) {
	event := &models.AlertCurEvent{GroupId: 1}
	r := &customModels.CustomAggregateRule{GroupId: 99}
	if SelectBestRule(event, []*customModels.CustomAggregateRule{r}) != nil {
		t.Fatalf("no match should return nil")
	}
}
