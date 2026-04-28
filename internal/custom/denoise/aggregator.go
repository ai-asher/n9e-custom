package denoise

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"

	customModels "github.com/ccfos/nightingale/v6/internal/custom/models"
	"github.com/ccfos/nightingale/v6/models"
)

// Decision is the verdict the Aggregator returns for a single event.
type Decision int

const (
	// DecisionPassThrough means no rule matched the event; pipeline must
	// forward it unchanged so the existing notification path still fires.
	DecisionPassThrough Decision = iota

	// DecisionFireFirst means a NEW incident was just created. The pipeline
	// SHOULD let this event through so the operator gets a notification —
	// the incident is what they'll see, but it must be triggered by an
	// actual event.
	DecisionFireFirst

	// DecisionMerged means the event was absorbed into an EXISTING incident.
	// The pipeline MUST drop the event so the operator isn't paged again.
	DecisionMerged

	// DecisionStorm is a special variant of DecisionMerged emitted when
	// the storm threshold is crossed: the event itself is dropped, but
	// the caller may want to fire a separate "alert storm" notification
	// using IncidentId.
	DecisionStorm
)

// Result carries the outcome of processing one event through the aggregator.
// Callers (the pipeline processor) translate this into either "pass event
// through" or "drop event".
type Result struct {
	Decision   Decision
	IncidentId int64  // populated for FireFirst / Merged / Storm
	RuleId     int64  // which rule matched, 0 if PassThrough
	Reason     string // human-readable explanation, included in node msg
}

// Aggregator is the heart of the denoise feature. It is intentionally
// decoupled from N9e's pipeline interface so it can be unit-tested with
// plain function calls — the processor wrapper (processor/aggregate.go)
// is a thin adapter on top.
type Aggregator struct {
	rules     RuleProvider
	repo      *Repo
	index     *ActiveIncidentIndex
	storm     *StormDetector
	upgradeOn bool // whether to upgrade incident severity when a higher-severity event joins
	now       func() int64
}

// AggregatorOption configures non-required behavior. Defaults: severity
// upgrade enabled, real wall clock.
type AggregatorOption func(*Aggregator)

// WithSeverityUpgrade controls whether incoming higher-severity events
// promote the parent incident's severity. Default true.
func WithSeverityUpgrade(on bool) AggregatorOption {
	return func(a *Aggregator) { a.upgradeOn = on }
}

// WithClock injects a clock; used only by tests.
func WithClock(now func() int64) AggregatorOption {
	return func(a *Aggregator) { a.now = now }
}

// NewAggregator constructs a fully-wired Aggregator. The repo, rules
// provider, index, and storm detector are required; the variadic options
// tweak behavior.
func NewAggregator(rules RuleProvider, repo *Repo, idx *ActiveIncidentIndex, storm *StormDetector, opts ...AggregatorOption) *Aggregator {
	a := &Aggregator{
		rules:     rules,
		repo:      repo,
		index:     idx,
		storm:     storm,
		upgradeOn: true,
		now:       func() int64 { return time.Now().Unix() },
	}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

// Handle is the single entry point the pipeline processor calls. It runs
// the full match → key → lookup → merge-or-create → storm sequence, and
// returns a Result describing what happened.
//
// The function NEVER returns an error for "expected" cases (no matching
// rule, malformed dimensions on a single event, etc.); those flow through
// as DecisionPassThrough with a Reason. The error return is reserved for
// genuine infrastructure failures (DB unreachable, JSON encode failure)
// where the caller should log + still pass-through to fail open.
func (a *Aggregator) Handle(event *models.AlertCurEvent) (Result, error) {
	if event == nil {
		return Result{Decision: DecisionPassThrough, Reason: "nil event"}, nil
	}

	rule := SelectBestRule(event, a.rules.GetActiveRules())
	if rule == nil {
		return Result{Decision: DecisionPassThrough, Reason: "no aggregate rule matched"}, nil
	}

	dims, err := decodeStringSlice(rule.Dimensions)
	if err != nil || len(dims) == 0 {
		// Malformed rule — bail out with pass-through. Logging is the
		// caller's responsibility.
		return Result{
			Decision: DecisionPassThrough,
			RuleId:   rule.Id,
			Reason:   fmt.Sprintf("rule %d has invalid dimensions: %v", rule.Id, err),
		}, nil
	}

	key := BuildIncidentKey(event, dims)
	if key == "" {
		return Result{
			Decision: DecisionPassThrough,
			RuleId:   rule.Id,
			Reason:   "empty incident key",
		}, nil
	}

	// Cache fast-path: if we have a fresh entry, merge directly.
	if incidentId, hit := a.index.Get(rule.Id, key, rule.WindowSec); hit {
		return a.merge(event, rule, incidentId)
	}

	// Cache miss — consult the DB. Another worker on this center might
	// already have created the incident, in which case we adopt it.
	existing, err := a.repo.FindActiveIncident(rule.Id, key, rule.WindowSec, a.now())
	if err != nil {
		return Result{}, fmt.Errorf("find active incident: %w", err)
	}
	if existing != nil {
		a.index.Put(rule.Id, key, existing.Id)
		return a.merge(event, rule, existing.Id)
	}

	// Genuinely new incident.
	return a.createNew(event, rule, dims, key)
}

// merge appends the event to an existing incident and returns either
// DecisionMerged or DecisionStorm if the storm threshold was crossed.
func (a *Aggregator) merge(event *models.AlertCurEvent, rule *customModels.CustomAggregateRule, incidentId int64) (Result, error) {
	if err := a.repo.AppendEvent(incidentId, event, a.upgradeOn); err != nil {
		return Result{}, fmt.Errorf("append event to incident %d: %w", incidentId, err)
	}
	a.index.Put(rule.Id, "", incidentId) // refresh lastEventAt; key not needed

	if a.storm.Observe(incidentId, rule.StormThreshold, rule.StormWindowSec) {
		// Atomically claim the storm slot — only the worker that flips
		// 0→1 fires the storm notification, even under sharded centers.
		fired, err := a.repo.MarkStormFired(incidentId)
		if err == nil && fired {
			return Result{
				Decision:   DecisionStorm,
				IncidentId: incidentId,
				RuleId:     rule.Id,
				Reason:     fmt.Sprintf("storm threshold crossed on incident %d", incidentId),
			}, nil
		}
	}

	return Result{
		Decision:   DecisionMerged,
		IncidentId: incidentId,
		RuleId:     rule.Id,
		Reason:     fmt.Sprintf("merged into incident %d", incidentId),
	}, nil
}

// createNew persists a fresh incident with the first event's metadata, then
// records the event in the join table.
func (a *Aggregator) createNew(event *models.AlertCurEvent, rule *customModels.CustomAggregateRule, dims []string, key string) (Result, error) {
	dimVals := DimensionValuesMap(event, dims)
	dimJSON, err := EncodeDimensionValues(dimVals)
	if err != nil {
		return Result{}, fmt.Errorf("encode dimension values: %w", err)
	}

	now := a.now()
	inc := &customModels.CustomIncident{
		RuleId:          rule.Id,
		IncidentKey:     key,
		GroupId:         event.GroupId,
		GroupName:       event.GroupName,
		DatasourceId:    event.DatasourceId,
		Severity:        event.Severity,
		Title:           buildTitle(event, dimVals),
		Summary:         event.RuleNote,
		DimensionValues: dimJSON,
		FirstEventAt:    now,
		LastEventAt:     now,
		EventCount:      1,
	}

	if err := a.repo.CreateIncident(inc); err != nil {
		return Result{}, fmt.Errorf("create incident: %w", err)
	}

	// Link the seed event so the incident → events join is complete from
	// the start (avoids "incident with 0 events" anomalies in the UI).
	link := &customModels.CustomIncidentEvent{
		IncidentId: inc.Id,
		EventHash:  event.Hash,
		EventId:    event.Id,
		MergedAt:   now,
	}
	if err := models.DB(a.repo.ctx).Create(link).Error; err != nil {
		// Non-fatal: the incident exists, the seed event will eventually
		// surface via subsequent merges. Log and move on.
		return Result{
			Decision:   DecisionFireFirst,
			IncidentId: inc.Id,
			RuleId:     rule.Id,
			Reason:     fmt.Sprintf("created incident %d (seed link failed: %v)", inc.Id, err),
		}, nil
	}

	a.index.Put(rule.Id, key, inc.Id)

	return Result{
		Decision:   DecisionFireFirst,
		IncidentId: inc.Id,
		RuleId:     rule.Id,
		Reason:     fmt.Sprintf("created incident %d", inc.Id),
	}, nil
}

// buildTitle composes a human-readable incident title combining the rule
// name (if available) with the dimension values, in stable sorted order.
func buildTitle(event *models.AlertCurEvent, dimVals map[string]string) string {
	keys := make([]string, 0, len(dimVals))
	for k := range dimVals {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	pairs := make([]byte, 0, 64)
	for i, k := range keys {
		if i > 0 {
			pairs = append(pairs, ',', ' ')
		}
		pairs = append(pairs, k...)
		pairs = append(pairs, '=')
		pairs = append(pairs, dimVals[k]...)
	}

	if event.RuleName != "" {
		return fmt.Sprintf("[%s] %s", event.RuleName, string(pairs))
	}
	return string(pairs)
}

// decodeStringSlice JSON-decodes a stored []string field. Returns an empty
// slice (and no error) for nil/empty input — the caller decides how to
// treat that case.
func decodeStringSlice(raw []byte) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var out []string
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}
