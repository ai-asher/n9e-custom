package router

import (
	"encoding/json"
	"time"

	customModels "github.com/ccfos/nightingale/v6/internal/custom/models"
	"github.com/ccfos/nightingale/v6/models"

	"github.com/gin-gonic/gin"
	"github.com/toolkits/pkg/ginx"
)

type inhibitRuleListResp struct {
	List  []*customModels.CustomInhibitRule `json:"list"`
	Total int64                             `json:"total"`
}

func (r *Router) inhibitRuleList(c *gin.Context) {
	var rules []*customModels.CustomInhibitRule
	if err := models.DB(r.Ctx).Order("id ASC").Find(&rules).Error; err != nil {
		ginx.NewRender(c).Data(nil, err)
		return
	}
	for _, rule := range rules {
		hydrateInhibitRule(rule)
	}
	ginx.NewRender(c).Data(inhibitRuleListResp{List: rules, Total: int64(len(rules))}, nil)
}

func (r *Router) inhibitRuleGet(c *gin.Context) {
	id := urlParamID(c)
	rule, err := getInhibitRuleByID(r, id)
	if err != nil {
		ginx.NewRender(c).Data(nil, err)
		return
	}
	hydrateInhibitRule(rule)
	ginx.NewRender(c).Data(rule, nil)
}

// inhibitRuleWriteReq mirrors CustomInhibitRule but accepts source/target as
// raw JSON strings the front-end will already have built. EqualLabels is
// expressed as a plain []string for ergonomics.
type inhibitRuleWriteReq struct {
	GroupId       int64    `json:"group_id"`
	Name          string   `json:"name"`
	Note          string   `json:"note"`
	SourceMatch   string   `json:"source_match"` // raw JSON of []TagFilter
	TargetMatch   string   `json:"target_match"` // raw JSON of []TagFilter
	EqualLabels   []string `json:"equal_labels"`
	DatasourceIds []int64  `json:"datasource_ids"`
	Disabled      int      `json:"disabled"`
}

func (req *inhibitRuleWriteReq) validate() error {
	if req.Name == "" {
		return ginxBadRequest("name is required")
	}
	// SourceMatch / TargetMatch must be valid JSON arrays. We do not require
	// them to be non-empty here — the suppress/CompileRule pass already
	// rejects empty matches at load time, and we want the API to surface
	// THAT error (with rule id) rather than a vague 400 here.
	if !isValidJSONArray(req.SourceMatch) {
		return ginxBadRequest("source_match must be a JSON array, got %q", req.SourceMatch)
	}
	if !isValidJSONArray(req.TargetMatch) {
		return ginxBadRequest("target_match must be a JSON array, got %q", req.TargetMatch)
	}
	return nil
}

func (req *inhibitRuleWriteReq) toModel(now int64, username string) *customModels.CustomInhibitRule {
	return &customModels.CustomInhibitRule{
		GroupId:           req.GroupId,
		Name:              req.Name,
		Note:              req.Note,
		SourceMatch:       []byte(req.SourceMatch),
		TargetMatch:       []byte(req.TargetMatch),
		EqualLabels:       encodeJSONList(req.EqualLabels),
		EqualLabelsJson:   req.EqualLabels,
		DatasourceIds:     encodeJSONList(req.DatasourceIds),
		DatasourceIdsJson: req.DatasourceIds,
		Disabled:          req.Disabled,
		CreateBy:          username,
		UpdateBy:          username,
		CreateAt:          now,
		UpdateAt:          now,
	}
}

func (r *Router) inhibitRuleCreate(c *gin.Context) {
	var req inhibitRuleWriteReq
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
	r.auditWrite.Write(c, "create", "inhibit_rule", nil, rule)
	ginx.NewRender(c).Data(rule, nil)
}

func (r *Router) inhibitRuleUpdate(c *gin.Context) {
	id := urlParamID(c)
	existing, err := getInhibitRuleByID(r, id)
	if err != nil {
		ginx.NewRender(c).Data(nil, err)
		return
	}
	var req inhibitRuleWriteReq
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
	r.auditWrite.Write(c, "update", "inhibit_rule", existing, updated)
	hydrateInhibitRule(updated)
	ginx.NewRender(c).Data(updated, nil)
}

func (r *Router) inhibitRuleDelete(c *gin.Context) {
	id := urlParamID(c)
	existing, err := getInhibitRuleByID(r, id)
	if err != nil {
		ginx.NewRender(c).Data(nil, err)
		return
	}
	if err := models.DB(r.Ctx).Delete(&customModels.CustomInhibitRule{}, id).Error; err != nil {
		ginx.NewRender(c).Data(nil, err)
		return
	}
	r.auditWrite.Write(c, "delete", "inhibit_rule", existing, nil)
	ginx.NewRender(c).Data(gin.H{"id": id, "deleted": true}, nil)
}

func getInhibitRuleByID(r *Router, id int64) (*customModels.CustomInhibitRule, error) {
	var rule customModels.CustomInhibitRule
	err := models.DB(r.Ctx).Where("id = ?", id).First(&rule).Error
	if err != nil {
		return nil, err
	}
	return &rule, nil
}

func hydrateInhibitRule(rule *customModels.CustomInhibitRule) {
	rule.EqualLabelsJson = decodeJSONStringList(rule.EqualLabels)
	rule.DatasourceIdsJson = decodeJSONInt64List(rule.DatasourceIds)
}

// isValidJSONArray returns true iff s is a syntactically valid JSON array
// (including the empty array "[]"). Used to gate inhibit-rule writes so a
// typo in the front-end can't silently store malformed source_match that
// later poisons the rule loader.
func isValidJSONArray(s string) bool {
	if s == "" {
		return false
	}
	var probe []json.RawMessage
	return json.Unmarshal([]byte(s), &probe) == nil
}

// decodeJSONStringList: ["a","b"] → ["a","b"]; "" / bad input → nil.
func decodeJSONStringList(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil
	}
	return out
}
