// Package denoise implements alert aggregation: merge duplicate alerts within
// a time window into a single Incident, based on a configurable set of
// dimension labels (e.g. service+cluster+alertname).
//
// Implementation overview:
//
//	┌────────────────────────────────────────────────────────────┐
//	│  AlertCurEvent (incoming, via Pipeline node "alert_aggregate") │
//	└──────────────────────────┬─────────────────────────────────┘
//	                           ▼
//	          ┌────────────────────────────────┐
//	          │ key.go: BuildIncidentKey()      │
//	          │   join sorted dim labels        │
//	          └────────────────┬───────────────┘
//	                           ▼
//	          ┌────────────────────────────────┐
//	          │ index.go: ActiveIncidentIndex   │
//	          │   in-memory map keyed by         │
//	          │   (rule_id, incident_key)        │
//	          └────────────────┬───────────────┘
//	                           ▼
//	      ┌────────────────────┴───────────────────┐
//	      │ HIT  → repo.AppendEvent (count++)       │  drop
//	      │ MISS → repo.CreateIncident              │  pass
//	      └────────────────────────────────────────┘
package denoise
