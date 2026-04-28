package models

// CustomIncidentEvent links AlertCurEvent / AlertHisEvent records to a
// CustomIncident. We do NOT alter native event tables — instead, we maintain
// a join table that records which events were merged into which incident.
//
// EventHash refers to AlertCurEvent.Hash (string), not the int64 id, because
// the same logical event may transition between cur_event and his_event
// while keeping a stable hash.
type CustomIncidentEvent struct {
	Id         int64  `json:"id" gorm:"primaryKey"`
	IncidentId int64  `json:"incident_id" gorm:"index;not null;comment:fk to custom_incident.id"`
	EventHash  string `json:"event_hash" gorm:"size:64;index;not null;comment:AlertCurEvent.Hash"`

	// Snapshot of the event id at merge time (may be cur_event or his_event id).
	EventId int64 `json:"event_id"`

	// Whether this particular event has recovered. Used to determine
	// whether the parent incident can be auto-resolved.
	IsRecovered int `json:"is_recovered" gorm:"default:0"`

	MergedAt int64 `json:"merged_at" gorm:"index"`
}

func (m *CustomIncidentEvent) TableName() string {
	return "custom_incident_event"
}
