package suppress

import (
	"encoding/json"
	"testing"

	customModels "github.com/ccfos/nightingale/v6/internal/custom/models"
	"github.com/ccfos/nightingale/v6/models"
)

func TestCompileRule_RejectsEmptySourceMatch(t *testing.T) {
	tgtJSON, _ := json.Marshal([]models.TagFilter{{Key: "x", Op: "==", Func: "==", Value: "y"}})
	r := &customModels.CustomInhibitRule{
		SourceMatch: nil,
		TargetMatch: tgtJSON,
	}
	if _, err := CompileRule(r); err == nil {
		t.Fatalf("empty source_match must be rejected")
	}
}

func TestCompileRule_RejectsEmptyTargetMatch(t *testing.T) {
	srcJSON, _ := json.Marshal([]models.TagFilter{{Key: "x", Op: "==", Func: "==", Value: "y"}})
	r := &customModels.CustomInhibitRule{
		SourceMatch: srcJSON,
		TargetMatch: nil,
	}
	if _, err := CompileRule(r); err == nil {
		t.Fatalf("empty target_match must be rejected")
	}
}

func TestCompileRule_ParsesEqualLabels(t *testing.T) {
	srcJSON, _ := json.Marshal([]models.TagFilter{{Key: "alertname", Op: "==", Func: "==", Value: "HostDown"}})
	tgtJSON, _ := json.Marshal([]models.TagFilter{{Key: "category", Op: "==", Func: "==", Value: "svc"}})
	eqJSON, _ := json.Marshal([]string{"host", "cluster"})

	r := &customModels.CustomInhibitRule{
		SourceMatch: srcJSON,
		TargetMatch: tgtJSON,
		EqualLabels: string(eqJSON),
	}
	c, err := CompileRule(r)
	if err != nil {
		t.Fatalf("CompileRule: %v", err)
	}
	if len(c.EqualLabels) != 2 || c.EqualLabels[0] != "host" || c.EqualLabels[1] != "cluster" {
		t.Fatalf("equal labels not parsed: %v", c.EqualLabels)
	}
}

func TestCompileRule_RejectsMalformedJSON(t *testing.T) {
	r := &customModels.CustomInhibitRule{
		SourceMatch: []byte("not-json"),
		TargetMatch: []byte("not-json"),
	}
	if _, err := CompileRule(r); err == nil {
		t.Fatalf("malformed JSON must be rejected")
	}
}

func TestCompileRule_NilInputRejected(t *testing.T) {
	if _, err := CompileRule(nil); err == nil {
		t.Fatalf("nil rule must be rejected")
	}
}
