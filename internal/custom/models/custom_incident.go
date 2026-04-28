package models

const (
	IncidentStatusOpen     = 0 // active, accepting new events
	IncidentStatusResolved = 1 // all underlying alerts recovered
	IncidentStatusClosed   = 2 // manually closed by operator
)

// CustomIncident represents a group of merged alert events produced by an
// CustomAggregateRule. One incident = one notification (instead of N).
//
// Lifecycle:
//   - Created on first matching event within an aggregation window.
//   - Updated (event_count++, last_event_at) on each subsequent matching event.
//   - Marked Resolved when all linked events recover, or after window closes
//     with no recovery — depending on configuration.
//   - Closed manually by operator from UI.
//
// IncidentKey is a stable identifier built by joining dimension label values,
// used for fast lookup of the active incident to merge into.
type CustomIncident struct {
	Id     int64 `json:"id" gorm:"primaryKey"`
	RuleId int64 `json:"rule_id" gorm:"index;not null;comment:fk to custom_aggregate_rule.id"`

	// Stable key built from dimension label values, e.g. "service=order|cluster=prod|alertname=HighCPU".
	IncidentKey string `json:"incident_key" gorm:"size:512;index;not null"`

	GroupId      int64  `json:"group_id" gorm:"index"`
	DatasourceId int64  `json:"datasource_id" gorm:"index"`
	GroupName    string `json:"group_name" gorm:"size:128"`

	// Severity inherited from first event; may be upgraded if a higher-severity
	// event joins the incident (configurable behavior).
	Severity int `json:"severity"`

	// Title and summary derived from first event + dimension values.
	Title   string `json:"title" gorm:"size:512"`
	Summary string `json:"summary" gorm:"type:text"`

	// Dimension values that produced the IncidentKey, JSON-encoded
	// for display in UI without re-parsing the key.
	DimensionValues string `json:"dimension_values" gorm:"type:text"`

	Status int `json:"status" gorm:"default:0;index;comment:0=open 1=resolved 2=closed"`

	// Counters and timestamps.
	EventCount   int64 `json:"event_count" gorm:"default:1"`
	FirstEventAt int64 `json:"first_event_at" gorm:"index"`
	LastEventAt  int64 `json:"last_event_at" gorm:"index"`
	ResolvedAt   int64 `json:"resolved_at"`
	ClosedAt     int64 `json:"closed_at"`

	// True when this incident produced an "alert storm" notification.
	StormFired int `json:"storm_fired" gorm:"default:0"`
}

func (m *CustomIncident) TableName() string {
	return "custom_incident"
}
