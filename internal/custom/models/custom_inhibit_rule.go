package models

import (
	"github.com/ccfos/nightingale/v6/pkg/ormx"
)

// CustomInhibitRule implements Alertmanager-style cross-rule alert suppression.
//
// Semantics:
//
//	When a "source" alert is currently firing AND a new "target" alert arrives
//	with matching equal-labels, the target alert is suppressed (muted).
//
// Example — "host down suppresses all service alerts on that host":
//
//	SourceMatch: {alertname: "HostDown", severity: 1}
//	TargetMatch: {category: "service"}
//	EqualLabels: ["host"]
//	⇒ when HostDown fires for host=db01, all service alerts on db01 are muted.
//
// Example — "critical suppresses warning on the same object":
//
//	SourceMatch: {severity: 1}
//	TargetMatch: {severity: 2}
//	EqualLabels: ["service", "instance"]
type CustomInhibitRule struct {
	Id      int64  `json:"id" gorm:"primaryKey"`
	GroupId int64  `json:"group_id" gorm:"index;comment:busi group id, 0 = global"`
	Name    string `json:"name" gorm:"size:255;not null"`
	Note    string `json:"note" gorm:"size:1024"`

	// Source-side match: which alert(s), when firing, count as the root cause.
	// JSON-encoded []TagFilter, identical shape to AlertMute.Tags.
	SourceMatch ormx.JSONArr `json:"source_match" gorm:"type:text;not null"`

	// Target-side match: which incoming alerts are candidates for suppression.
	TargetMatch ormx.JSONArr `json:"target_match" gorm:"type:text;not null"`

	// Equal labels: source and target must share identical values for ALL
	// listed label keys for the inhibition to apply.
	// JSON-encoded []string, e.g. ["host"], ["service","instance"].
	EqualLabels     string   `json:"-" gorm:"column:equal_labels;size:1024"`
	EqualLabelsJson []string `json:"equal_labels" gorm:"-"`

	// Optional datasource scoping. Empty = all datasources.
	DatasourceIds     string  `json:"-" gorm:"column:datasource_ids;size:1024"`
	DatasourceIdsJson []int64 `json:"datasource_ids" gorm:"-"`

	Disabled int    `json:"disabled" gorm:"default:0;comment:0=enabled 1=disabled"`
	CreateBy string `json:"create_by" gorm:"size:64"`
	UpdateBy string `json:"update_by" gorm:"size:64"`
	CreateAt int64  `json:"create_at"`
	UpdateAt int64  `json:"update_at"`
}

func (m *CustomInhibitRule) TableName() string {
	return "custom_inhibit_rule"
}
