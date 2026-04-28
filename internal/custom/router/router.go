package router

import (
	"net/http"

	centerRouter "github.com/ccfos/nightingale/v6/center/router"
	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/ctx"

	"github.com/gin-gonic/gin"
	"github.com/toolkits/pkg/ginx"
)

// Router holds the dependencies needed by every handler. We deliberately
// keep this struct narrow — we only need the application context (for DB
// access) and the parent N9e Router (so we can reuse its Auth/User
// middleware without re-implementing JWT parsing).
type Router struct {
	Ctx        *ctx.Context
	parent     *centerRouter.Router
	auditWrite AuditWriter
}

// AuditWriter is invoked after every successful write to record the change.
// Phase 1.2 will provide a real implementation backed by a custom_audit_log
// table; for Phase 1.1 we accept a no-op default so handlers stay testable
// without dragging in audit infrastructure.
type AuditWriter interface {
	Write(c *gin.Context, action string, target string, before, after interface{})
}

type noopAudit struct{}

func (noopAudit) Write(*gin.Context, string, string, interface{}, interface{}) {}

// New constructs the custom router. Pass the same *centerRouter.Router you
// already use to register N9e's native routes; we attach our handlers to
// the same gin.Engine, just under a different prefix.
func New(c *ctx.Context, parent *centerRouter.Router) *Router {
	return &Router{Ctx: c, parent: parent, auditWrite: noopAudit{}}
}

// SetAuditWriter swaps the audit sink. Useful for wiring the real DB-backed
// auditor in Phase 1.2 without re-constructing the router.
func (r *Router) SetAuditWriter(w AuditWriter) {
	if w != nil {
		r.auditWrite = w
	}
}

// Config attaches every custom route to the given gin.Engine. Idempotent
// only across distinct *gin.Engine instances; calling twice on the same
// engine will panic via gin's duplicate-route check (which is what we
// want — a duplicate registration is always a bug).
//
// Route grouping:
//
//	/api/n9e/custom/aggregate-rule(s)/*       Aggregate rules CRUD
//	/api/n9e/custom/inhibit-rule(s)/*         Inhibit rules CRUD
//	/api/n9e/custom/mute-cron(s)/*            Cron mute CRUD
//	/api/n9e/custom/emergency-mute            Singleton: GET / PUT
//	/api/n9e/custom/incidents/*               Incidents (read + close)
func (r *Router) Config(engine *gin.Engine) {
	g := engine.Group("/api/n9e/custom")
	g.Use(r.parent.Auth(), r.parent.User())

	// Reads — any authenticated user.
	g.GET("/aggregate-rules", r.aggregateRuleList)
	g.GET("/aggregate-rule/:id", r.aggregateRuleGet)
	g.GET("/inhibit-rules", r.inhibitRuleList)
	g.GET("/inhibit-rule/:id", r.inhibitRuleGet)
	g.GET("/mute-crons", r.muteCronList)
	g.GET("/mute-cron/:id", r.muteCronGet)
	g.GET("/emergency-mute", r.emergencyMuteGet)
	g.GET("/incidents", r.incidentList)
	g.GET("/incident/:id", r.incidentGet)
	g.GET("/audit-logs", r.auditLogList)

	// Writes — admin only.
	w := g.Group("")
	w.Use(r.requireAdmin())
	w.POST("/aggregate-rule", r.aggregateRuleCreate)
	w.PUT("/aggregate-rule/:id", r.aggregateRuleUpdate)
	w.DELETE("/aggregate-rule/:id", r.aggregateRuleDelete)
	w.POST("/inhibit-rule", r.inhibitRuleCreate)
	w.PUT("/inhibit-rule/:id", r.inhibitRuleUpdate)
	w.DELETE("/inhibit-rule/:id", r.inhibitRuleDelete)
	w.POST("/mute-cron", r.muteCronCreate)
	w.PUT("/mute-cron/:id", r.muteCronUpdate)
	w.DELETE("/mute-cron/:id", r.muteCronDelete)
	w.PUT("/emergency-mute", r.emergencyMutePut)
	w.PUT("/incident/:id/close", r.incidentClose)
}

// requireAdmin gates write operations. We piggyback on the "isadmin" flag
// the user() middleware already populates rather than re-querying the
// user — saves an extra DB hit on every write.
func (r *Router) requireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		isAdmin, ok := c.Get("isadmin")
		if !ok {
			ginx.Bomb(http.StatusUnauthorized, "unauthorized")
		}
		if b, _ := isAdmin.(bool); !b {
			ginx.Bomb(http.StatusForbidden, "admin required")
		}
		c.Next()
	}
}

// currentUsername returns the username set by N9e's auth middleware. Helper
// because every write handler stamps create_by/update_by.
func currentUsername(c *gin.Context) string {
	if v, ok := c.Get("username"); ok {
		if s, _ := v.(string); s != "" {
			return s
		}
	}
	return "unknown"
}

// currentUser returns the *models.User attached by user() middleware. We
// don't strictly need it in Phase 1.1 (the username string is enough to
// stamp audit fields) but include it so future per-business-group filters
// can use User.IsAdmin / User.Id without re-fetching.
func currentUser(c *gin.Context) *models.User {
	if v, ok := c.Get("user"); ok {
		if u, _ := v.(*models.User); u != nil {
			return u
		}
	}
	return nil
}
