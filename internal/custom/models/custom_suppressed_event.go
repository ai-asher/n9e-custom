package models

// CustomSuppressedEvent records every (target event, source event, rule)
// triple that the suppress.HookAdapter rejected.
//
// Why a separate table rather than annotating alert_his_event:
//
//	N9e's native alert_his_event holds events that DID fire — adding a
//	"suppressed_by" column would muddy the semantics ("history of fired
//	alerts" vs. "history of all alert decisions"). A side table also
//	gives ops a clean "who was muted by whom recently" page that doesn't
//	pollute the main event view.
//
// Concurrency:
//
//	The hot path (suppress hook) only PUSHES into a channel; a single
//	background worker batch-INSERTs from there. So this table can never
//	block the alert pipeline even under storm.
type CustomSuppressedEvent struct {
	Id int64 `json:"id" gorm:"primaryKey"`

	// RuleId references custom_inhibit_rule.id but we do NOT use a real FK
	// constraint — we want suppressed-event rows to outlive their inhibit
	// rule (an operator deleting a rule shouldn't lose audit trail).
	RuleId   int64  `json:"rule_id" gorm:"index"`
	RuleName string `json:"rule_name" gorm:"size:255"` // snapshot at decision time

	// Both event hashes are the AlertCurEvent.Hash (the rule_id+vector_key
	// composite N9e generates). Stable across cur->his migration so we can
	// later join either way without worrying about id churn.
	SourceEventHash string `json:"source_event_hash" gorm:"size:64;index"`
	TargetEventHash string `json:"target_event_hash" gorm:"size:64;index"`

	// Snapshots so the page is readable even if the underlying events are
	// long gone (alert_his_event has its own retention window).
	SourceRuleName string `json:"source_rule_name" gorm:"size:255"`
	TargetRuleName string `json:"target_rule_name" gorm:"size:255"`
	SourceTags     string `json:"source_tags" gorm:"size:1024"`
	TargetTags     string `json:"target_tags" gorm:"size:1024"`
	SourceSeverity int    `json:"source_severity"`
	TargetSeverity int    `json:"target_severity"`

	GroupId      int64 `json:"group_id" gorm:"index"`
	DatasourceId int64 `json:"datasource_id" gorm:"index"`

	SuppressedAt int64 `json:"suppressed_at" gorm:"index"`
}

func (m *CustomSuppressedEvent) TableName() string {
	return "custom_suppressed_event"
}
