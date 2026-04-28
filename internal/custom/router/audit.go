package router

import (
	"encoding/json"
	"time"

	customModels "github.com/ccfos/nightingale/v6/internal/custom/models"
	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/ctx"

	"github.com/gin-gonic/gin"
	"github.com/toolkits/pkg/logger"
)

// dbAuditWriter persists each write operation to custom_audit_log.
//
// Failure mode: a failure to insert the audit row LOGS THE ERROR but does
// NOT propagate. The alternative — failing the user's API call because
// the audit table is unhappy — would be worse: an operator trying to
// disable a runaway mute would be locked out by their own audit
// infrastructure. Audit completeness is preferred but availability wins.
type dbAuditWriter struct {
	ctx *ctx.Context
}

// NewDBAuditWriter constructs the writer. Call once during bootstrap and
// pass the result to Router.SetAuditWriter.
func NewDBAuditWriter(c *ctx.Context) AuditWriter {
	return &dbAuditWriter{ctx: c}
}

// Write implements AuditWriter. The signature accepts interface{} for
// before/after so handlers can hand over the live model struct without
// caring about its concrete type.
func (w *dbAuditWriter) Write(c *gin.Context, action, target string, before, after interface{}) {
	row := &customModels.CustomAuditLog{
		Username:  currentUsername(c),
		Action:    action,
		Target:    target,
		TargetId:  extractTargetID(after, before),
		Before:    encodeSnapshot(before),
		After:     encodeSnapshot(after),
		ClientIp:  clientIP(c),
		CreatedAt: time.Now().Unix(),
	}
	if err := models.DB(w.ctx).Create(row).Error; err != nil {
		logger.Errorf("custom/audit: failed to record %s on %s: %v", action, target, err)
	}
}

// extractTargetID pulls an Id field off either snapshot. We prefer After
// (post-mutation state, which is what readers usually care about) and
// fall back to Before for delete operations.
//
// The function works by JSON round-tripping; this is slow-ish but happens
// only on writes (typical: <10/min). The alternative — a type switch on
// every model struct — would create the very coupling we worked to avoid
// when designing the AuditWriter interface.
func extractTargetID(primary, fallback interface{}) int64 {
	if id := idFrom(primary); id != 0 {
		return id
	}
	return idFrom(fallback)
}

func idFrom(v interface{}) int64 {
	if v == nil {
		return 0
	}
	b, err := json.Marshal(v)
	if err != nil {
		return 0
	}
	var probe struct {
		Id int64 `json:"id"`
	}
	if err := json.Unmarshal(b, &probe); err != nil {
		return 0
	}
	return probe.Id
}

// encodeSnapshot JSON-marshals a value for the before/after columns.
// Returns "" for nil so the column shows up empty rather than "null" —
// matches operator expectation when reading the table by hand.
func encodeSnapshot(v interface{}) string {
	if v == nil {
		return ""
	}
	b, err := json.Marshal(v)
	if err != nil {
		// Don't drop the audit row over a marshal error — record a
		// placeholder so the audit timeline still has an entry.
		return `{"_marshal_error":"` + err.Error() + `"}`
	}
	return string(b)
}

// clientIP picks the most useful client identifier for the audit row.
// gin's c.ClientIP() respects X-Forwarded-For only when the request comes
// from a trusted proxy; we trust whatever it returns rather than parsing
// headers ourselves.
func clientIP(c *gin.Context) string {
	if c == nil {
		return ""
	}
	return c.ClientIP()
}
