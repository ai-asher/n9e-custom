package suppress

import (
	"sort"
	"sync"
	"time"
)

// rootCauseEntry holds the minimum information needed to decide whether
// a target event should be suppressed: the equal-label values of the root
// alert, a TTL, plus a tiny snapshot used by the suppressed-event audit
// page (rule name + tags + severity at registration time).
//
// We intentionally do NOT cache the full AlertCurEvent — only the few
// fields the audit page needs — to keep the index footprint small even
// when thousands of alerts are firing simultaneously.
type rootCauseEntry struct {
	equalLabelValues map[string]string
	expireAt         int64 // unix seconds; 0 = no expiry (rare, see Register)

	// Snapshot fields (for the suppressed-event audit page; not used in
	// match logic). Populated by Register; nil-safe for older callers.
	ruleName     string
	tagsCSV      string
	severity     int
	groupId      int64
	datasourceId int64
}

// SourceSnapshot is the public shape MatchAny returns alongside the hash.
// It mirrors the snapshot fields above so the suppress.Decision can carry
// enough context to populate a CustomSuppressedEvent row without another
// DB lookup.
type SourceSnapshot struct {
	EventHash    string
	RuleName     string
	TagsCSV      string
	Severity     int
	GroupId      int64
	DatasourceId int64
}

// matchKey identifies a (rule, host/service signature) entry inside the
// index. We bucket by ruleId because the same physical event can register
// under multiple inhibit rules (one event may be the source for several
// distinct suppressions).
type matchKey struct {
	ruleId    int64
	eventHash string
}

// RootCauseIndex tracks alerts currently acting as suppression sources.
//
// It is in-memory and process-local. Multi-center deployments should swap
// in an external implementation of RootCauseProvider; this default works
// fine for single-center setups (which covers the bulk of N9e installs).
//
// Entries auto-expire on TTL so an alert that recovered without us being
// told (e.g. process restart, message loss) eventually stops suppressing
// downstream events.
type RootCauseIndex struct {
	mu         sync.RWMutex
	entries    map[matchKey]rootCauseEntry
	defaultTTL int64 // seconds — how long a registered root cause stays active
	now        func() int64
}

// NewRootCauseIndex constructs an index with the given TTL fallback for
// entries that don't carry an explicit expiry. A 10-minute default keeps
// stale entries from haunting forever while still accommodating short
// recovery delays.
func NewRootCauseIndex(defaultTTLSec int64) *RootCauseIndex {
	if defaultTTLSec <= 0 {
		defaultTTLSec = 600
	}
	return &RootCauseIndex{
		entries:    make(map[matchKey]rootCauseEntry),
		defaultTTL: defaultTTLSec,
		now:        func() int64 { return time.Now().Unix() },
	}
}

// Register marks an event as a live source for the given rule. equalValues
// is the set of (label → value) pairs the event carries for keys listed in
// the rule's EqualLabels — the caller is expected to extract them before
// calling Register so this hot-path method is just a map insert.
//
// Kept for backwards compat (existing tests). Production code uses
// RegisterFull below to attach the snapshot needed by the audit page.
func (idx *RootCauseIndex) Register(ruleId int64, eventHash string, equalValues map[string]string) {
	idx.RegisterFull(ruleId, eventHash, equalValues, "", "", 0, 0, 0)
}

// RegisterFull is Register + a snapshot of the source event's metadata.
// The snapshot fields are read back by MatchAnyFull so the suppression
// audit row can be populated without re-querying alert_cur_event.
func (idx *RootCauseIndex) RegisterFull(
	ruleId int64,
	eventHash string,
	equalValues map[string]string,
	ruleName, tagsCSV string,
	severity int,
	groupId, datasourceId int64,
) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.entries[matchKey{ruleId, eventHash}] = rootCauseEntry{
		equalLabelValues: equalValues,
		expireAt:         idx.now() + idx.defaultTTL,
		ruleName:         ruleName,
		tagsCSV:          tagsCSV,
		severity:         severity,
		groupId:          groupId,
		datasourceId:     datasourceId,
	}
}

// Forget evicts a (rule, event) pair — call this when the source alert
// recovers, so its suppression effect ends immediately rather than waiting
// for TTL.
func (idx *RootCauseIndex) Forget(ruleId int64, eventHash string) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	delete(idx.entries, matchKey{ruleId, eventHash})
}

// ForgetEvent removes the event from every rule it was registered under.
// Used by recovery handlers that don't track which rules an event hit.
func (idx *RootCauseIndex) ForgetEvent(eventHash string) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	for k := range idx.entries {
		if k.eventHash == eventHash {
			delete(idx.entries, k)
		}
	}
}

// MatchAny reports whether any registered source under `ruleId` shares the
// equal-label values described by `targetValues`. Returns the matching
// source's eventHash (for logging) on the first hit; empty string + false
// if nothing matches.
//
// Hot-path method: O(active sources for this rule). Typical deployments
// have <100 simultaneous root-cause alerts per rule, so a linear scan
// stays well under 1µs.
func (idx *RootCauseIndex) MatchAny(ruleId int64, targetValues map[string]string) (string, bool) {
	snap, ok := idx.MatchAnyFull(ruleId, targetValues)
	if !ok {
		return "", false
	}
	return snap.EventHash, true
}

// MatchAnyFull is MatchAny + the snapshot the suppression audit row needs.
// Returns the full source snapshot on hit. Hot-path callers use this to
// avoid a follow-up DB lookup for the source event's tags / severity.
func (idx *RootCauseIndex) MatchAnyFull(ruleId int64, targetValues map[string]string) (SourceSnapshot, bool) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	now := idx.now()
	for k, e := range idx.entries {
		if k.ruleId != ruleId {
			continue
		}
		if e.expireAt > 0 && e.expireAt < now {
			continue
		}
		if equalValuesMatch(e.equalLabelValues, targetValues) {
			return SourceSnapshot{
				EventHash:    k.eventHash,
				RuleName:     e.ruleName,
				TagsCSV:      e.tagsCSV,
				Severity:     e.severity,
				GroupId:      e.groupId,
				DatasourceId: e.datasourceId,
			}, true
		}
	}
	return SourceSnapshot{}, false
}

// Sweep removes expired entries. Should be called periodically (e.g. once
// per minute) by a background goroutine. Returns count of evictions for
// observability.
func (idx *RootCauseIndex) Sweep() int {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	now := idx.now()
	evicted := 0
	for k, e := range idx.entries {
		if e.expireAt > 0 && e.expireAt < now {
			delete(idx.entries, k)
			evicted++
		}
	}
	return evicted
}

// Len returns the number of registered entries. Useful for metrics + tests.
func (idx *RootCauseIndex) Len() int {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return len(idx.entries)
}

// equalValuesMatch reports whether every key in `source` is present in
// `target` with the same value. Crucially, target may carry MORE labels
// than source — we only care about the equal-label intersection.
//
// An empty source map is matched only by an empty target map, which is
// the correct behavior for an inhibit rule with no equal_labels: it
// implies "any source suppresses any target", but the caller should not
// be in this branch (we require non-empty source to register).
func equalValuesMatch(source, target map[string]string) bool {
	if len(source) == 0 {
		// No equal-label constraints — interpreted as "always match".
		// This is the configuration where EqualLabels is empty in the
		// rule, meaning a single root-cause suppresses every target
		// event, regardless of host/service. Powerful + dangerous;
		// operators must opt in by leaving the field blank.
		return true
	}
	for k, sv := range source {
		if tv, ok := target[k]; !ok || tv != sv {
			return false
		}
	}
	return true
}

// keysOf is a tiny helper used by tests to assert on equal-label
// extraction; exported only via test files.
func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
