package router

import (
	"strconv"

	customModels "github.com/ccfos/nightingale/v6/internal/custom/models"
	"github.com/ccfos/nightingale/v6/models"

	"github.com/gin-gonic/gin"
	"github.com/toolkits/pkg/ginx"
)

// Suppressed events are produced asynchronously by the suppress hook;
// this endpoint just slices the table for the audit page.

type suppressedEventListResp struct {
	List   []*customModels.CustomSuppressedEvent `json:"list"`
	Total  int64                                 `json:"total"`
	Limit  int                                   `json:"limit"`
	Offset int                                   `json:"offset"`
}

// suppressedEventList serves /api/n9e/custom/suppressed-events.
//
// Filters (all optional, AND-combined):
//
//	rule_id       only rows for this inhibit rule
//	source_hash   only rows where source_event_hash matches (find what a
//	              given root cause has muted)
//	target_hash   only rows where target_event_hash matches (find why a
//	              given alert was muted)
//	since         unix-seconds floor on suppressed_at
//
// Pagination: limit default 50, max 500. Sorted newest-first.
func (r *Router) suppressedEventList(c *gin.Context) {
	limit := parseQueryIntDefault(c, "limit", 50, 500)
	offset := parseQueryIntDefault(c, "offset", 0, 1<<31)

	q := models.DB(r.Ctx).Model(&customModels.CustomSuppressedEvent{})

	if v := c.Query("rule_id"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil && id > 0 {
			q = q.Where("rule_id = ?", id)
		}
	}
	if v := c.Query("source_hash"); v != "" {
		q = q.Where("source_event_hash = ?", v)
	}
	if v := c.Query("target_hash"); v != "" {
		q = q.Where("target_event_hash = ?", v)
	}
	if v := c.Query("since"); v != "" {
		if ts, err := strconv.ParseInt(v, 10, 64); err == nil && ts > 0 {
			q = q.Where("suppressed_at >= ?", ts)
		}
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		ginx.NewRender(c).Data(nil, err)
		return
	}

	var list []*customModels.CustomSuppressedEvent
	if err := q.Order("id DESC").Limit(limit).Offset(offset).Find(&list).Error; err != nil {
		ginx.NewRender(c).Data(nil, err)
		return
	}

	ginx.NewRender(c).Data(suppressedEventListResp{
		List: list, Total: total, Limit: limit, Offset: offset,
	}, nil)
}
