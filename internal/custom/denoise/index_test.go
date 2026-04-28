package denoise

import (
	"testing"
)

func TestActiveIncidentIndex_HitMiss(t *testing.T) {
	idx := NewActiveIncidentIndex()
	idx.Put(1, "k", 100)

	if id, ok := idx.Get(1, "k", 60); !ok || id != 100 {
		t.Fatalf("expected hit (100,true), got (%d,%v)", id, ok)
	}
	if _, ok := idx.Get(2, "k", 60); ok {
		t.Fatalf("different rule_id must miss")
	}
	if _, ok := idx.Get(1, "other", 60); ok {
		t.Fatalf("different key must miss")
	}
}

func TestActiveIncidentIndex_StaleEntryEvictedByGet(t *testing.T) {
	clock := int64(1000)
	idx := NewActiveIncidentIndex()
	idx.now = func() int64 { return clock }

	idx.Put(1, "k", 100) // lastEventAt=1000
	clock = 1100         // 100s later

	// Window is 60s — entry is stale, Get must miss.
	if _, ok := idx.Get(1, "k", 60); ok {
		t.Fatalf("expected stale entry to miss")
	}
	// But still within 200s window, so it hits.
	if _, ok := idx.Get(1, "k", 200); !ok {
		t.Fatalf("expected entry to hit when window covers age")
	}
}

func TestActiveIncidentIndex_ForgetRemovesEntry(t *testing.T) {
	idx := NewActiveIncidentIndex()
	idx.Put(1, "k", 100)
	idx.Forget(1, "k")
	if _, ok := idx.Get(1, "k", 60); ok {
		t.Fatalf("Forget did not evict")
	}
}

func TestActiveIncidentIndex_SweepEvictsOldEntries(t *testing.T) {
	clock := int64(1000)
	idx := NewActiveIncidentIndex()
	idx.now = func() int64 { return clock }

	idx.Put(1, "old", 100) // lastEventAt=1000
	clock = 1800
	idx.Put(2, "fresh", 200) // lastEventAt=1800
	clock = 2000

	// Sweep with maxAge=300: cutoff = 2000-300 = 1700.
	// "old" (1000) is below cutoff → evicted.
	// "fresh" (1800) is above cutoff → kept.
	evicted := idx.Sweep(300)
	if evicted != 1 {
		t.Fatalf("expected 1 eviction (the old@1000 entry), got %d", evicted)
	}
	if idx.Len() != 1 {
		t.Fatalf("expected 1 surviving entry, got %d", idx.Len())
	}
	if _, ok := idx.Get(2, "fresh", 600); !ok {
		t.Fatalf("the fresh entry should still be present")
	}
}
