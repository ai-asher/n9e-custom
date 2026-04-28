package models

// CustomAuditLog records every write operation on the custom_* tables so
// operators can answer "who muted everything at 03:00, and why" months
// later without scraping container logs.
//
// We never delete audit rows automatically — log retention is an ops
// decision (a cron task in the future may prune past N days). Until then
// the table grows monotonically; one row per write op is cheap (typical
// install: <100 rows/day).
type CustomAuditLog struct {
	Id int64 `json:"id" gorm:"primaryKey"`

	// Username from the JWT — always populated; unauthenticated requests
	// are rejected upstream by the auth middleware.
	Username string `json:"username" gorm:"size:64;index;not null"`

	// Action is the verb: create / update / delete / put / close.
	// Kept as an enum-ish string rather than int because the readability
	// payoff dwarfs the few extra bytes.
	Action string `json:"action" gorm:"size:32;index;not null"`

	// Target is the kind of object: aggregate_rule / inhibit_rule /
	// mute_cron / emergency_mute / incident.
	Target string `json:"target" gorm:"size:64;index;not null"`

	// TargetId is the row id of the affected object. For emergency_mute
	// (a singleton) this is always 1; for incidents it is the incident id.
	TargetId int64 `json:"target_id" gorm:"index"`

	// Before / After are JSON snapshots. We store both so the audit table
	// is self-contained — you can reconstruct what changed without joining
	// against the live config (which by then may have been re-edited
	// several more times). On create, Before is empty; on delete, After
	// is empty.
	Before string `json:"before" gorm:"type:text"`
	After  string `json:"after" gorm:"type:text"`

	// ClientIp is best-effort: extracted from X-Real-IP / X-Forwarded-For
	// when present, otherwise gin's c.ClientIP() result.
	ClientIp string `json:"client_ip" gorm:"size:64"`

	// CreatedAt is unix seconds — same convention as every other time
	// column in the schema for easy join + sort.
	CreatedAt int64 `json:"created_at" gorm:"index"`
}

func (m *CustomAuditLog) TableName() string {
	return "custom_audit_log"
}
