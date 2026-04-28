package router

import (
	"time"

	customModels "github.com/ccfos/nightingale/v6/internal/custom/models"
	"github.com/ccfos/nightingale/v6/models"

	"github.com/gin-gonic/gin"
	"github.com/toolkits/pkg/ginx"
)

// aggregateRuleListResp is the wire format for List. We expose the parsed
// JSON fields (DatasourceIdsJson etc.) rather than the raw strings — same
// convention as N9e's AlertMute. Front-end consumes the *Json fields.
type aggregateRuleListResp struct {
	List  []*customModels.CustomAggregateRule `json:"list"`
	Total int64                               `json:"total"`
}

func (r *Router) aggregateRuleList(c *gin.Context) {
	var rules []*customModels.CustomAggregateRule
	if err := models.DB(r.Ctx).Order("priority DESC, id ASC").Find(&rules).Error; err != nil {
		ginx.NewRender(c).Data(nil, err)
		return
	}
	for _, rule := range rules {
		hydrateAggregateRule(rule)
	}
	ginx.NewRender(c).Data(aggregateRuleListResp{List: rules, Total: int64(len(rules))}, nil)
}

func (r *Router) aggregateRuleGet(c *gin.Context) {
	id := urlParamID(c)
	rule, err := getAggregateRuleByID(r, id)
	if err != nil {
		ginx.NewRender(c).Data(nil, err)
		return
	}
	hydrateAggregateRule(rule)
	ginx.NewRender(c).Data(rule, nil)
}

// aggregateRuleWriteReq is the request body for create / update. Using a
// dedicated DTO (instead of binding the model directly) keeps internal
// fields like create_at out of the public surface and gives us a place
// to validate.
type aggregateRuleWriteReq struct {
	GroupId        int64    `json:"group_id"`
	Name           string   `json:"name"`
	Note           string   `json:"note"`
	Dimensions     []string `json:"dimensions"`     // e.g. ["service","cluster"]
	WindowSec      int64    `json:"window_sec"`     // default 300
	Filters        string   `json:"filters"`        // raw JSON string for []TagFilter; "" → "[]"
	DatasourceIds  []int64  `json:"datasource_ids"` // empty = all
	Severities     []int    `json:"severities"`     // empty = all
	StormThreshold int      `json:"storm_threshold"`
	StormWindowSec int64    `json:"storm_window_sec"`
	Disabled       int      `json:"disabled"`
	Priority       int      `json:"priority"`
}

// validate enforces the invariants Phase 1 cares about. Fancier validation
// (cron expr, JSON shape, equal-labels semantics) lands in Phase 1.2.
func (req *aggregateRuleWriteReq) validate() error {
	if req.Name == "" {
		return ginxBadRequest("name is required")
	}
	if len(req.Dimensions) == 0 {
		return ginxBadRequest("dimensions must contain at least one label key")
	}
	if req.WindowSec <= 0 {
		req.WindowSec = 300
	}
	if req.StormWindowSec <= 0 {
		req.StormWindowSec = 60
	}
	return nil
}

// toModel maps the DTO onto a fresh CustomAggregateRule. JSON-array-typed
// columns are stored as raw text so GORM's stock TEXT column type accepts
// them — the model's *Json sibling fields are populated for any read path.
func (req *aggregateRuleWriteReq) toModel(now int64, username string) *customModels.CustomAggregateRule {
	dimsJSON := encodeJSONList(req.Dimensions)
	filters := req.Filters
	if filters == "" {
		filters = "[]"
	}
	return &customModels.CustomAggregateRule{
		GroupId:           req.GroupId,
		Name:              req.Name,
		Note:              req.Note,
		Dimensions:        []byte(dimsJSON),
		WindowSec:         req.WindowSec,
		Filters:           []byte(filters),
		DatasourceIds:     encodeJSONList(req.DatasourceIds),
		DatasourceIdsJson: req.DatasourceIds,
		Severities:        encodeJSONList(req.Severities),
		SeveritiesJson:    req.Severities,
		StormThreshold:    req.StormThreshold,
		StormWindowSec:    req.StormWindowSec,
		Disabled:          req.Disabled,
		Priority:          req.Priority,
		CreateBy:          username,
		UpdateBy:          username,
		CreateAt:          now,
		UpdateAt:          now,
	}
}

func (r *Router) aggregateRuleCreate(c *gin.Context) {
	var req aggregateRuleWriteReq
	ginx.BindJSON(c, &req)
	if err := req.validate(); err != nil {
		ginx.NewRender(c).Data(nil, err)
		return
	}
	rule := req.toModel(time.Now().Unix(), currentUsername(c))
	if err := models.DB(r.Ctx).Create(rule).Error; err != nil {
		ginx.NewRender(c).Data(nil, err)
		return
	}
	r.auditWrite.Write(c, "create", "aggregate_rule", nil, rule)
	ginx.NewRender(c).Data(rule, nil)
}

func (r *Router) aggregateRuleUpdate(c *gin.Context) {
	id := urlParamID(c)
	existing, err := getAggregateRuleByID(r, id)
	if err != nil {
		ginx.NewRender(c).Data(nil, err)
		return
	}
	var req aggregateRuleWriteReq
	ginx.BindJSON(c, &req)
	if err := req.validate(); err != nil {
		ginx.NewRender(c).Data(nil, err)
		return
	}

	updated := req.toModel(time.Now().Unix(), currentUsername(c))
	updated.Id = id
	updated.CreateBy = existing.CreateBy
	updated.CreateAt = existing.CreateAt

	if err := models.DB(r.Ctx).Save(updated).Error; err != nil {
		ginx.NewRender(c).Data(nil, err)
		return
	}
	r.auditWrite.Write(c, "update", "aggregate_rule", existing, updated)
	hydrateAggregateRule(updated)
	ginx.NewRender(c).Data(updated, nil)
}

func (r *Router) aggregateRuleDelete(c *gin.Context) {
	id := urlParamID(c)
	existing, err := getAggregateRuleByID(r, id)
	if err != nil {
		ginx.NewRender(c).Data(nil, err)
		return
	}
	if err := models.DB(r.Ctx).Delete(&customModels.CustomAggregateRule{}, id).Error; err != nil {
		ginx.NewRender(c).Data(nil, err)
		return
	}
	r.auditWrite.Write(c, "delete", "aggregate_rule", existing, nil)
	ginx.NewRender(c).Data(gin.H{"id": id, "deleted": true}, nil)
}

// getAggregateRuleByID is a shared lookup used by Get / Update / Delete so
// they all surface the same "not found" error shape.
func getAggregateRuleByID(r *Router, id int64) (*customModels.CustomAggregateRule, error) {
	var rule customModels.CustomAggregateRule
	err := models.DB(r.Ctx).Where("id = ?", id).First(&rule).Error
	if err != nil {
		return nil, err
	}
	return &rule, nil
}

// hydrateAggregateRule decodes the comma/JSON-encoded scalar columns onto
// their *Json siblings. The bootstrap loader does the same — we duplicate
// it here rather than import bootstrap to avoid a router→bootstrap edge in
// the dependency graph (bootstrap already imports router would create a
// cycle if we also imported the other way).
func hydrateAggregateRule(rule *customModels.CustomAggregateRule) {
	rule.DatasourceIdsJson = decodeJSONInt64List(rule.DatasourceIds)
	rule.SeveritiesJson = decodeJSONIntList(rule.Severities)
}
