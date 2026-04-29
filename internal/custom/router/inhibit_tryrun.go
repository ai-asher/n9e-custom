package router

import (
	"github.com/ccfos/nightingale/v6/alert/dispatch"
	"github.com/ccfos/nightingale/v6/models"

	"github.com/gin-gonic/gin"
	"github.com/toolkits/pkg/ginx"
)

// inhibitTryRun lets operators feed a synthetic AlertCurEvent through the
// installed EventMuteHook (mute -> suppress -> noop) and see what the
// chain decides. Useful for:
//
//  1. Smoke-testing a freshly-saved inhibit rule without waiting for a
//     real outage.
//  2. End-to-end verifying the suppression-audit pipeline (the call goes
//     through suppress.HookAdapter, which records to the async sink).
//
// The endpoint replays into the SAME global EventMuteHook the alert
// pipeline uses, so a successful suppression here also writes a row to
// custom_suppressed_event.
//
// Request body — minimal AlertCurEvent shape:
//
//	{
//	  "hash": "test-source-1",
//	  "rule_name": "HostDown",
//	  "severity": 1,
//	  "tags": "alertname=HostDown,,host=db01",
//	  "tags_map": {"alertname": "HostDown", "host": "db01"}
//	}
//
// Response:
//
//	{ "muted": true|false }
type inhibitTryRunReq struct {
	Hash     string            `json:"hash"`
	RuleName string            `json:"rule_name"`
	Severity int               `json:"severity"`
	Tags     string            `json:"tags"`
	TagsMap  map[string]string `json:"tags_map"`
	GroupId  int64             `json:"group_id"`
	Datasrc  int64             `json:"datasource_id"`
}

type inhibitTryRunResp struct {
	Muted bool `json:"muted"`
}

func (r *Router) inhibitTryRun(c *gin.Context) {
	var req inhibitTryRunReq
	ginx.BindJSON(c, &req)

	event := &models.AlertCurEvent{
		Hash:         req.Hash,
		RuleName:     req.RuleName,
		Severity:     req.Severity,
		Tags:         req.Tags,
		TagsMap:      req.TagsMap,
		GroupId:      req.GroupId,
		DatasourceId: req.Datasrc,
	}

	// Call the live hook chain — same code path real alerts take. This
	// triggers source-side registration AND writes an audit row if the
	// event ends up suppressed.
	muted := dispatch.EventMuteHook(event)

	ginx.NewRender(c).Data(inhibitTryRunResp{Muted: muted}, nil)
}
