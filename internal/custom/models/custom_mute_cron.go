package models

import (
	"github.com/ccfos/nightingale/v6/pkg/ormx"
)

// CustomMuteCron extends N9e's native mute with cron-expression based periodic mute.
//
// Native AlertMute supports:
//   - TimeRange (one-shot window via btime/etime)
//   - Periodic (day-of-week + HH:MM ranges)
//
// This model adds a third mute time type: arbitrary cron expressions, e.g.
//   - "0 22 * * 1-5"   weekdays at 22:00
//   - "*/30 9-18 * * *" every 30 min during business hours
//
// Matches a triggering AlertCurEvent against the cron schedule + filters
// (group / datasource / severity / tag).
type CustomMuteCron struct {
	Id      int64  `json:"id" gorm:"primaryKey"`
	GroupId int64  `json:"group_id" gorm:"index;comment:busi group id, 0 = global"`
	Note    string `json:"note" gorm:"size:1024"`

	// Cron expression in standard 5-field format (minute hour dom month dow).
	// Evaluated in server local timezone unless overridden by Timezone field.
	CronExpr string `json:"cron_expr" gorm:"size:128;not null"`

	// Duration in seconds — how long each cron firing keeps mute active.
	// e.g. cron="0 22 * * *" + duration=3600 → mute every day 22:00-23:00.
	DurationSec int64 `json:"duration_sec" gorm:"default:3600;comment:mute window length in seconds after each cron fire"`

	// IANA timezone name, e.g. "Asia/Shanghai". Empty = server local time.
	Timezone string `json:"timezone" gorm:"size:64"`

	// Filter scope (same conventions as AlertMute).
	DatasourceIds     string  `json:"-" gorm:"column:datasource_ids;size:1024"`
	DatasourceIdsJson []int64 `json:"datasource_ids" gorm:"-"`

	Severities     string `json:"-" gorm:"column:severities;size:64"`
	SeveritiesJson []int  `json:"severities" gorm:"-"`

	// Tag filters; same shape as AlertMute.Tags ([]TagFilter JSON-encoded).
	Tags ormx.JSONArr `json:"tags" gorm:"type:text"`

	Disabled int    `json:"disabled" gorm:"default:0;comment:0=enabled 1=disabled"`
	CreateBy string `json:"create_by" gorm:"size:64"`
	UpdateBy string `json:"update_by" gorm:"size:64"`
	CreateAt int64  `json:"create_at"`
	UpdateAt int64  `json:"update_at"`
}

func (m *CustomMuteCron) TableName() string {
	return "custom_mute_cron"
}
