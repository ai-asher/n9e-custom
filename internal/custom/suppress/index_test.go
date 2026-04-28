package suppress

import (
	"testing"
)

func TestRootCauseIndex_RegisterAndMatch(t *testing.T) {
	idx := NewRootCauseIndex(60)

	idx.Register(1, "src-hash", map[string]string{"host": "db01"})

	// Same host → suppress.
	if hash, ok := idx.MatchAny(1, map[string]string{"host": "db01"}); !ok || hash != "src-hash" {
		t.Fatalf("expected match for host=db01, got hash=%q ok=%v", hash, ok)
	}
	// Different host → no suppress.
	if _, ok := idx.MatchAny(1, map[string]string{"host": "db02"}); ok {
		t.Fatalf("expected no match for host=db02")
	}
	// Different rule_id → no suppress.
	if _, ok := idx.MatchAny(2, map[string]string{"host": "db01"}); ok {
		t.Fatalf("expected no match under different rule")
	}
}

func TestRootCauseIndex_TargetMissingEqualLabel(t *testing.T) {
	idx := NewRootCauseIndex(60)
	idx.Register(1, "src", map[string]string{"host": "db01"})

	// Target has no `host` label at all → must NOT match.
	// Otherwise we would suppress unrelated alerts that happen to lack
	// the equal-label entirely.
	if _, ok := idx.MatchAny(1, map[string]string{"service": "billing"}); ok {
		t.Fatalf("target without host label must not match")
	}
}

func TestRootCauseIndex_EmptyEqualLabelsMatchesAll(t *testing.T) {
	idx := NewRootCauseIndex(60)
	idx.Register(1, "src", nil)

	if _, ok := idx.MatchAny(1, map[string]string{"anything": "goes"}); !ok {
		t.Fatalf("empty equal-labels source should match any target")
	}
}

func TestRootCauseIndex_ForgetRemovesEntry(t *testing.T) {
	idx := NewRootCauseIndex(60)
	idx.Register(1, "src", map[string]string{"host": "db01"})
	idx.Forget(1, "src")

	if _, ok := idx.MatchAny(1, map[string]string{"host": "db01"}); ok {
		t.Fatalf("Forget did not evict")
	}
}

func TestRootCauseIndex_ForgetEventClearsAcrossRules(t *testing.T) {
	idx := NewRootCauseIndex(60)
	idx.Register(1, "shared", map[string]string{"host": "db01"})
	idx.Register(2, "shared", map[string]string{"host": "db01"})

	idx.ForgetEvent("shared")

	if _, ok := idx.MatchAny(1, map[string]string{"host": "db01"}); ok {
		t.Fatalf("ForgetEvent did not evict from rule 1")
	}
	if _, ok := idx.MatchAny(2, map[string]string{"host": "db01"}); ok {
		t.Fatalf("ForgetEvent did not evict from rule 2")
	}
}

func TestRootCauseIndex_ExpiredEntryNotMatched(t *testing.T) {
	clock := int64(1000)
	idx := NewRootCauseIndex(60)
	idx.now = func() int64 { return clock }

	idx.Register(1, "src", map[string]string{"host": "db01"}) // expireAt = 1060

	clock = 2000 // way past expiry

	if _, ok := idx.MatchAny(1, map[string]string{"host": "db01"}); ok {
		t.Fatalf("expired entry must not match")
	}
}

func TestRootCauseIndex_SweepEvictsExpired(t *testing.T) {
	clock := int64(1000)
	idx := NewRootCauseIndex(60)
	idx.now = func() int64 { return clock }

	idx.Register(1, "old", map[string]string{"host": "db01"}) // expire 1060
	clock = 2000
	idx.Register(2, "fresh", map[string]string{"host": "db02"}) // expire 2060

	clock = 2030

	if got := idx.Sweep(); got != 1 {
		t.Fatalf("expected 1 eviction, got %d", got)
	}
	if idx.Len() != 1 {
		t.Fatalf("expected 1 surviving entry, got %d", idx.Len())
	}
}

func TestRootCauseIndex_KeysOfHelper(t *testing.T) {
	got := keysOf(map[string]string{"b": "2", "a": "1"})
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("expected sorted [a b], got %v", got)
	}
}
