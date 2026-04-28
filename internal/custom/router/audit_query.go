package router

import (
	"strconv"

	customModels "github.com/ccfos/nightingale/v6/internal/custom/models"
	"github.com/ccfos/nightingale/v6/models"

	"github.com/gin-gonic/gin"
	"github.com/toolkits/pkg/ginx"
)

type auditLogListResp struct {
	List   []*customModels.CustomAuditLog `json:"list"`
	Total  int64                          `json:"total"`
	Limit  int                            `json:"limit"`
	Offset int                            `json:"offset"`
}

// auditLogList serves /api/n9e/custom/audit-logs.
//
// Filters (all optional, all combined with AND):
//
//	target=aggregate_rule|inhibit_rule|mute_cron|emergency_mute|incident
//	action=create|update|delete|put|close
//	username=alice
//	target_id=42
//	since=<unix-seconds>   only rows created at-or-after this time
//
// Pagination:
//
//	limit=50 (default), max 500
//	offset=0
//
// Sorted newest-first because the typical "what just happened" query
// reads top-of-list.
func (r *Router) auditLogList(c *gin.Context) {
	limit := parseQueryIntDefault(c, "limit", 50, 500)
	offset := parseQueryIntDefault(c, "offset", 0, 1<<31)

	q := models.DB(r.Ctx).Model(&customModels.CustomAuditLog{})

	if v := c.Query("target"); v != "" {
		q = q.Where("target = ?", v)
	}
	if v := c.Query("action"); v != "" {
		q = q.Where("action = ?", v)
	}
	if v := c.Query("username"); v != "" {
		q = q.Where("username = ?", v)
	}
	if v := c.Query("target_id"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			q = q.Where("target_id = ?", id)
		}
	}
	if v := c.Query("since"); v != "" {
		if ts, err := strconv.ParseInt(v, 10, 64); err == nil && ts > 0 {
			q = q.Where("created_at >= ?", ts)
		}
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		ginx.NewRender(c).Data(nil, err)
		return
	}

	var list []*customModels.CustomAuditLog
	if err := q.Order("id DESC").Limit(limit).Offset(offset).Find(&list).Error; err != nil {
		ginx.NewRender(c).Data(nil, err)
		return
	}

	ginx.NewRender(c).Data(auditLogListResp{
		List: list, Total: total, Limit: limit, Offset: offset,
	}, nil)
}
