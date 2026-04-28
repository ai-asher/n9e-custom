package denoise

import (
	"sync"
	"time"
)

// activeIncidentEntry caches a tiny projection of an open incident — just
// enough to decide "merge or create" without hitting the DB on every event.
// We deliberately do NOT cache event_count or severity — those are bumped
// transactionally in the DB and the cache value would race against truth.
type activeIncidentEntry struct {
	incidentId  int64
	lastEventAt int64
}

// indexKey identifies a unique incident slot. We key on (rule, key) — the
// same incident_key under different rules legitimately produces separate
// incidents, so the rule_id must participate.
type indexKey struct {
	ruleId      int64
	incidentKey string
}

// ActiveIncidentIndex is an in-memory write-through cache that maps
// (rule, incident_key) → currently-open incident id.
//
// Lifecycle:
//   - Process() consults this index FIRST. On hit, it skips the DB lookup.
//   - On miss, it falls back to Repo.FindActiveIncident. If the DB also
//     returns nothing, a new incident is created and added to the index.
//   - Entries are evicted lazily by Sweep() once they are older than the
//     longest aggregation window seen — so the cache cannot grow unbounded
//     even if no one ever calls Resolve.
//
// Concurrency: every method takes mu.RLock or mu.Lock around the map; a
// dedicated sync.Map would be slightly faster but loses Sweep's cheap
// O(N) iteration. Hot path is Get + Put, both O(1).
type ActiveIncidentIndex struct {
	mu  sync.RWMutex
	m   map[indexKey]activeIncidentEntry
	now func() int64 // injectable for tests
}

func NewActiveIncidentIndex() *ActiveIncidentIndex {
	return &ActiveIncidentIndex{
		m:   make(map[indexKey]activeIncidentEntry),
		now: func() int64 { return time.Now().Unix() },
	}
}

// Get returns the cached incident id for (ruleId, key). The boolean return
// is `false` either when no entry exists OR when the cached entry is older
// than `windowSec` — callers MUST treat both cases identically and re-check
// the DB.
//
// We piggyback the staleness check on Get to avoid putting expired hits
// back into the merge path; an entry older than the window cannot logically
// belong to the active incident anyway.
func (idx *ActiveIncidentIndex) Get(ruleId int64, key string, windowSec int64) (int64, bool) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	e, ok := idx.m[indexKey{ruleId, key}]
	if !ok {
		return 0, false
	}
	if idx.now()-e.lastEventAt > windowSec {
		return 0, false
	}
	return e.incidentId, true
}

// Put inserts or refreshes a (ruleId, key) → incidentId entry. Always
// updates lastEventAt to the current time, since any caller invoking Put
// has just observed an event for this incident.
func (idx *ActiveIncidentIndex) Put(ruleId int64, key string, incidentId int64) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.m[indexKey{ruleId, key}] = activeIncidentEntry{
		incidentId:  incidentId,
		lastEventAt: idx.now(),
	}
}

// Forget evicts the entry for (ruleId, key). Called when an incident is
// resolved or manually closed, so the next event opens a fresh incident
// instead of being merged into the closed one.
func (idx *ActiveIncidentIndex) Forget(ruleId int64, key string) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	delete(idx.m, indexKey{ruleId, key})
}

// Sweep evicts every entry older than `maxAgeSec`. Intended to be called
// periodically (e.g. every minute) by a background goroutine; callers
// should pass the largest aggregation-window value across all active rules
// to ensure no still-relevant entry is evicted.
//
// Returns the number of entries evicted, primarily for observability.
func (idx *ActiveIncidentIndex) Sweep(maxAgeSec int64) int {
	cutoff := idx.now() - maxAgeSec
	idx.mu.Lock()
	defer idx.mu.Unlock()

	evicted := 0
	for k, e := range idx.m {
		if e.lastEventAt < cutoff {
			delete(idx.m, k)
			evicted++
		}
	}
	return evicted
}

// Len reports the current cache size; useful for metrics and tests.
func (idx *ActiveIncidentIndex) Len() int {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return len(idx.m)
}
