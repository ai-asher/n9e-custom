// Package processor exposes denoise functionality as N9e Pipeline processors,
// registered into models.processorRegister via init() side-effects.
//
// Importing this package — typically via a blank-import in the bootstrap
// path — is the ONLY thing required to wire aggregation into the alert
// pipeline. The processor type "alert_aggregate" then becomes available
// for users to add as a Workflow node from the N9e UI.
package processor

import (
	"fmt"

	"github.com/ccfos/nightingale/v6/alert/pipeline/processor/common"
	"github.com/ccfos/nightingale/v6/internal/custom/denoise"
	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/ctx"
	"github.com/toolkits/pkg/logger"
)

// AggregateConfig is the per-node configuration the user sets in the UI.
//
// We keep the config near-empty on purpose: the actual aggregation rules
// (window, dimensions, filters, storm settings) live in the
// `custom_aggregate_rule` table and are matched per-event. The pipeline
// node simply marks "events passing through this node should be subject
// to denoise" — operators get a clear toggle without duplicating rule
// configuration in two places.
type AggregateConfig struct {
	// Mode selects the dispatch policy for matched events:
	//   "drop_merged" (default): merged events are dropped, FireFirst passes.
	//   "annotate":              all events pass through, only annotated
	//                            with incident_id in TagsMap so a downstream
	//                            sender can decide what to do.
	Mode string `json:"mode"`
}

// aggregatorAccessor is filled in at runtime by Init from the package-level
// singleton. Wiring is deferred to a setter (see Wire below) so the model
// doesn't bake in a global at import time — that lets test code construct
// processors against a fake aggregator.
type aggregatorAccessor func() *denoise.Aggregator

var getAggregator aggregatorAccessor = func() *denoise.Aggregator { return nil }

// Wire registers the live Aggregator with the processor package. Call once
// during bootstrap, after the rule cache and DB connection are ready.
//
// Calling Wire multiple times is allowed (last call wins), making
// hot-reload scenarios painless.
func Wire(get aggregatorAccessor) {
	if get != nil {
		getAggregator = get
	}
}

func init() {
	models.RegisterProcessor("alert_aggregate", &AggregateConfig{})
}

func (c *AggregateConfig) Init(settings interface{}) (models.Processor, error) {
	return common.InitProcessor[*AggregateConfig](settings)
}

// Process consults the aggregator and translates its Decision into the
// pipeline's drop/pass-through convention:
//
//	PassThrough → unchanged context, no message.
//	FireFirst   → unchanged context (event continues to senders).
//	Merged      → wfCtx.Event = nil  (engine treats this as "drop").
//	Storm       → wfCtx.Event = nil  + log; the dedicated storm channel
//	              is fired by the aggregator's caller via its own path,
//	              not by this processor.
//
// Failure mode is "fail open": any infrastructure error is logged and the
// event passes through. The cost of an extra notification is far smaller
// than the cost of silently dropping a critical alert because of a DB hiccup.
func (c *AggregateConfig) Process(_ *ctx.Context, wfCtx *models.WorkflowContext) (*models.WorkflowContext, string, error) {
	agg := getAggregator()
	if agg == nil {
		// Aggregator not wired yet (e.g. during early startup) — fail open.
		return wfCtx, "aggregator not initialized; pass-through", nil
	}

	event := wfCtx.Event
	if event == nil {
		return wfCtx, "no event to aggregate", nil
	}

	res, err := agg.Handle(event)
	if err != nil {
		logger.Errorf("denoise: aggregation failure (failing open): %v", err)
		return wfCtx, fmt.Sprintf("aggregation error: %v (passed through)", err), nil
	}

	switch res.Decision {
	case denoise.DecisionPassThrough, denoise.DecisionFireFirst:
		return wfCtx, res.Reason, nil

	case denoise.DecisionMerged, denoise.DecisionStorm:
		if c.Mode == "annotate" {
			// Annotate-only mode: keep event, attach incident id so a
			// downstream node / sender can branch on it.
			if event.TagsMap == nil {
				event.TagsMap = map[string]string{}
			}
			event.TagsMap["incident_id"] = fmt.Sprintf("%d", res.IncidentId)
			return wfCtx, res.Reason, nil
		}
		// Default: drop the event. The engine interprets nil Event as terminate.
		wfCtx.Event = nil
		return wfCtx, res.Reason, nil

	default:
		return wfCtx, fmt.Sprintf("unknown decision %d; pass-through", res.Decision), nil
	}
}
