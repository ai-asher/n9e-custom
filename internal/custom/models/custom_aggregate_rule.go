package models

import (
	"github.com/ccfos/nightingale/v6/pkg/ormx"
)

// CustomAggregateRule defines how alerts are merged into Incidents.
//
// Aggregation logic:
//  1. For an incoming AlertCurEvent, build an "incident key" by joining
//     the values of Dimensions labels (e.g. service+cluster+alertname).
//  2. Within WindowSec seconds, all events sharing the same key are merged
//     into the same Incident.
//  3. Only the FIRST event per incident triggers a notification; subsequent
//     events are appended silently (count++) until the window closes or the
//     incident is resolved.
//
// Storm detection (optional):
//
//	If StormThreshold > 0 and the rule sees more than StormThreshold matching
//	events within StormWindowSec, an extra "alert storm" notification fires.
type CustomAggregateRule struct {
	Id      int64  `json:"id" gorm:"primaryKey"`
	GroupId int64  `json:"group_id" gorm:"index;comment:busi group id, 0 = global"`
	Name    string `json:"name" gorm:"size:255;not null"`
	Note    string `json:"note" gorm:"size:1024"`

	// Label keys used to group events into the same incident.
	// JSON-encoded []string, e.g. ["service", "cluster", "alertname"].
	Dimensions ormx.JSONArr `json:"dimensions" gorm:"type:text;not null"`

	// Aggregation window in seconds. Default 300 (5 min).
	WindowSec int64 `json:"window_sec" gorm:"default:300;comment:aggregation window length"`

	// Optional filter — only events matching these tag filters are aggregated.
	// JSON-encoded []TagFilter (same shape as AlertMute.Tags).
	// Empty = aggregate all events.
	Filters ormx.JSONArr `json:"filters" gorm:"type:text"`

	// Optional datasource scoping. Empty = all datasources.
	DatasourceIds     string  `json:"-" gorm:"column:datasource_ids;size:1024"`
	DatasourceIdsJson []int64 `json:"datasource_ids" gorm:"-"`

	// Storm detection — 0 to disable.
	StormThreshold int   `json:"storm_threshold" gorm:"default:0;comment:0=disable storm detection"`
	StormWindowSec int64 `json:"storm_window_sec" gorm:"default:60"`

	// Severity filter — empty = all severities.
	Severities     string `json:"-" gorm:"column:severities;size:64"`
	SeveritiesJson []int  `json:"severities" gorm:"-"`

	Disabled int    `json:"disabled" gorm:"default:0;comment:0=enabled 1=disabled"`
	Priority int    `json:"priority" gorm:"default:0;comment:higher priority rule wins on conflict"`
	CreateBy string `json:"create_by" gorm:"size:64"`
	UpdateBy string `json:"update_by" gorm:"size:64"`
	CreateAt int64  `json:"create_at"`
	UpdateAt int64  `json:"update_at"`
}

func (m *CustomAggregateRule) TableName() string {
	return "custom_aggregate_rule"
}
