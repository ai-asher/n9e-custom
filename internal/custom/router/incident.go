package router

import (
	"strconv"
	"time"

	customModels "github.com/ccfos/nightingale/v6/internal/custom/models"
	"github.com/ccfos/nightingale/v6/models"

	"github.com/gin-gonic/gin"
	"github.com/toolkits/pkg/ginx"
)

// Incident is mostly read-only — the runtime aggregator writes them. The
// only mutation we expose is "manually close", for the rare case an
// operator wants to drop an incident without waiting for natural recovery.

type incidentListResp struct {
	List   []*customModels.CustomIncident `json:"list"`
	Total  int64                          `json:"total"`
	Limit  int                            `json:"limit"`
	Offset int                            `json:"offset"`
}

func (r *Router) incidentList(c *gin.Context) {
	limit := parseQueryIntDefault(c, "limit", 50, 500)
	offset := parseQueryIntDefault(c, "offset", 0, 1<<31)
	statusStr := c.Query("status") // empty / "0" / "1" / "2"
	ruleIDStr := c.Query("rule_id")

	q := models.DB(r.Ctx).Model(&customModels.CustomIncident{})
	if statusStr != "" {
		// The status enum is small (0 open, 1 resolved, 2 closed); reject
		// values outside the expected range to keep the SQL predictable.
		if s, err := strconv.Atoi(statusStr); err == nil && s >= 0 && s <= 2 {
			q = q.Where("status = ?", s)
		} else {
			ginx.NewRender(c).Data(nil, ginxBadRequest("invalid status %q", statusStr))
			return
		}
	}
	if ruleIDStr != "" {
		if rid, err := strconv.ParseInt(ruleIDStr, 10, 64); err == nil && rid > 0 {
			q = q.Where("rule_id = ?", rid)
		}
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		ginx.NewRender(c).Data(nil, err)
		return
	}

	var list []*customModels.CustomIncident
	if err := q.Order("id DESC").Limit(limit).Offset(offset).Find(&list).Error; err != nil {
		ginx.NewRender(c).Data(nil, err)
		return
	}

	ginx.NewRender(c).Data(incidentListResp{
		List: list, Total: total, Limit: limit, Offset: offset,
	}, nil)
}

// incidentDetail extends Incident with the latest 100 linked events for
// the detail page. We cap at 100 to avoid pulling every event when an
// incident has thousands; the front-end can paginate via /incident/:id/events
// later if we add that endpoint.
type incidentDetail struct {
	*customModels.CustomIncident
	Events []*customModels.CustomIncidentEvent `json:"events"`
}

func (r *Router) incidentGet(c *gin.Context) {
	id := urlParamID(c)
	var incident customModels.CustomIncident
	if err := models.DB(r.Ctx).Where("id = ?", id).First(&incident).Error; err != nil {
		ginx.NewRender(c).Data(nil, err)
		return
	}

	var events []*customModels.CustomIncidentEvent
	if err := models.DB(r.Ctx).
		Where("incident_id = ?", id).
		Order("merged_at DESC, id DESC").
		Limit(100).
		Find(&events).Error; err != nil {
		ginx.NewRender(c).Data(nil, err)
		return
	}

	ginx.NewRender(c).Data(incidentDetail{CustomIncident: &incident, Events: events}, nil)
}

// incidentClose flips an incident's status to Closed (manual close). We
// only allow Open → Closed; closing an already-resolved incident is a
// no-op (returns the row as-is).
func (r *Router) incidentClose(c *gin.Context) {
	id := urlParamID(c)
	var existing customModels.CustomIncident
	if err := models.DB(r.Ctx).Where("id = ?", id).First(&existing).Error; err != nil {
		ginx.NewRender(c).Data(nil, err)
		return
	}
	if existing.Status == customModels.IncidentStatusClosed {
		ginx.NewRender(c).Data(&existing, nil)
		return
	}

	now := time.Now().Unix()
	if err := models.DB(r.Ctx).Model(&existing).Updates(map[string]interface{}{
		"status":    customModels.IncidentStatusClosed,
		"closed_at": now,
	}).Error; err != nil {
		ginx.NewRender(c).Data(nil, err)
		return
	}
	r.auditWrite.Write(c, "close", "incident", &existing, &customModels.CustomIncident{
		Id: existing.Id, Status: customModels.IncidentStatusClosed, ClosedAt: now,
	})
	existing.Status = customModels.IncidentStatusClosed
	existing.ClosedAt = now
	ginx.NewRender(c).Data(&existing, nil)
}

// parseQueryIntDefault reads an int query param with a default and an
// upper bound. Used to keep pagination requests sane (limit=10000 isn't
// ever a legitimate UX request).
func parseQueryIntDefault(c *gin.Context, key string, def, max int) int {
	s := c.Query(key)
	if s == "" {
		return def
	}
	v, err := strconv.Atoi(s)
	if err != nil || v < 0 {
		return def
	}
	if v > max {
		return max
	}
	return v
}
