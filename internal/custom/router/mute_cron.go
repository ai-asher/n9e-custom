package router

import (
	"time"

	customModels "github.com/ccfos/nightingale/v6/internal/custom/models"
	custMute "github.com/ccfos/nightingale/v6/internal/custom/mute"
	"github.com/ccfos/nightingale/v6/models"

	"github.com/gin-gonic/gin"
	"github.com/toolkits/pkg/ginx"
)

type muteCronListResp struct {
	List  []*customModels.CustomMuteCron `json:"list"`
	Total int64                          `json:"total"`
}

func (r *Router) muteCronList(c *gin.Context) {
	var rules []*customModels.CustomMuteCron
	if err := models.DB(r.Ctx).Order("id ASC").Find(&rules).Error; err != nil {
		ginx.NewRender(c).Data(nil, err)
		return
	}
	for _, rule := range rules {
		hydrateMuteCron(rule)
	}
	ginx.NewRender(c).Data(muteCronListResp{List: rules, Total: int64(len(rules))}, nil)
}

func (r *Router) muteCronGet(c *gin.Context) {
	id := urlParamID(c)
	rule, err := getMuteCronByID(r, id)
	if err != nil {
		ginx.NewRender(c).Data(nil, err)
		return
	}
	hydrateMuteCron(rule)
	ginx.NewRender(c).Data(rule, nil)
}

type muteCronWriteReq struct {
	GroupId       int64   `json:"group_id"`
	Note          string  `json:"note"`
	CronExpr      string  `json:"cron_expr"`
	DurationSec   int64   `json:"duration_sec"`
	Timezone      string  `json:"timezone"`
	DatasourceIds []int64 `json:"datasource_ids"`
	Severities    []int   `json:"severities"`
	Tags          string  `json:"tags"` // raw JSON string for []TagFilter
	Disabled      int     `json:"disabled"`
}

func (req *muteCronWriteReq) validate() error {
	if req.CronExpr == "" {
		return ginxBadRequest("cron_expr is required")
	}
	if req.DurationSec <= 0 {
		return ginxBadRequest("duration_sec must be > 0")
	}
	// Eagerly compile the cron expression so a typo fails the API call,
	// not the next service restart. CompileCron also validates the
	// timezone name.
	if _, err := custMute.CompileCron(req.CronExpr, req.Timezone, req.DurationSec); err != nil {
		return ginxBadRequest("invalid cron config: %v", err)
	}
	if req.Tags != "" && !isValidJSONArray(req.Tags) {
		return ginxBadRequest("tags must be a JSON array, got %q", req.Tags)
	}
	return nil
}

func (req *muteCronWriteReq) toModel(now int64, username string) *customModels.CustomMuteCron {
	tags := req.Tags
	if tags == "" {
		tags = "[]"
	}
	return &customModels.CustomMuteCron{
		GroupId:           req.GroupId,
		Note:              req.Note,
		CronExpr:          req.CronExpr,
		DurationSec:       req.DurationSec,
		Timezone:          req.Timezone,
		DatasourceIds:     encodeJSONList(req.DatasourceIds),
		DatasourceIdsJson: req.DatasourceIds,
		Severities:        encodeJSONList(req.Severities),
		SeveritiesJson:    req.Severities,
		Tags:              []byte(tags),
		Disabled:          req.Disabled,
		CreateBy:          username,
		UpdateBy:          username,
		CreateAt:          now,
		UpdateAt:          now,
	}
}

func (r *Router) muteCronCreate(c *gin.Context) {
	var req muteCronWriteReq
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
	r.auditWrite.Write(c, "create", "mute_cron", nil, rule)
	ginx.NewRender(c).Data(rule, nil)
}

func (r *Router) muteCronUpdate(c *gin.Context) {
	id := urlParamID(c)
	existing, err := getMuteCronByID(r, id)
	if err != nil {
		ginx.NewRender(c).Data(nil, err)
		return
	}
	var req muteCronWriteReq
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
	r.auditWrite.Write(c, "update", "mute_cron", existing, updated)
	hydrateMuteCron(updated)
	ginx.NewRender(c).Data(updated, nil)
}

func (r *Router) muteCronDelete(c *gin.Context) {
	id := urlParamID(c)
	existing, err := getMuteCronByID(r, id)
	if err != nil {
		ginx.NewRender(c).Data(nil, err)
		return
	}
	if err := models.DB(r.Ctx).Delete(&customModels.CustomMuteCron{}, id).Error; err != nil {
		ginx.NewRender(c).Data(nil, err)
		return
	}
	r.auditWrite.Write(c, "delete", "mute_cron", existing, nil)
	ginx.NewRender(c).Data(gin.H{"id": id, "deleted": true}, nil)
}

func getMuteCronByID(r *Router, id int64) (*customModels.CustomMuteCron, error) {
	var rule customModels.CustomMuteCron
	err := models.DB(r.Ctx).Where("id = ?", id).First(&rule).Error
	if err != nil {
		return nil, err
	}
	return &rule, nil
}

func hydrateMuteCron(rule *customModels.CustomMuteCron) {
	rule.DatasourceIdsJson = decodeJSONInt64List(rule.DatasourceIds)
	rule.SeveritiesJson = decodeJSONIntList(rule.Severities)
}
