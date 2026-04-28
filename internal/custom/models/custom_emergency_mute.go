package models

// CustomEmergencyMute is a global one-click mute switch for maintenance windows
// and large-scale incidents.
//
// When Enabled=1, ALL alert events are silenced regardless of any other rule
// (subject to the optional scope filters below).
//
// Designed as a singleton table — typically only id=1 row exists; the row may
// be toggled rapidly by an on-call engineer.
type CustomEmergencyMute struct {
	Id int64 `json:"id" gorm:"primaryKey"`

	// 1 = mute is active; 0 = mute is off.
	Enabled int `json:"enabled" gorm:"default:0;comment:0=off 1=on"`

	// Human-readable reason shown in audit log and UI banner.
	Reason string `json:"reason" gorm:"size:512"`

	// Optional auto-expiry. 0 = never expire; otherwise unix timestamp at which
	// the mute auto-disables. Prevents accidental forever-mute.
	ExpireAt int64 `json:"expire_at" gorm:"default:0;comment:0=never; unix ts otherwise"`

	// Optional scope: if non-empty, only datasources in this list are muted.
	// Empty = all datasources.
	DatasourceIds     string  `json:"-" gorm:"column:datasource_ids;size:1024"`
	DatasourceIdsJson []int64 `json:"datasource_ids" gorm:"-"`

	// Optional scope: business group ids. Empty = all groups.
	GroupIds     string  `json:"-" gorm:"column:group_ids;size:1024"`
	GroupIdsJson []int64 `json:"group_ids" gorm:"-"`

	CreateBy string `json:"create_by" gorm:"size:64"`
	UpdateBy string `json:"update_by" gorm:"size:64"`
	CreateAt int64  `json:"create_at"`
	UpdateAt int64  `json:"update_at"`
}

func (m *CustomEmergencyMute) TableName() string {
	return "custom_emergency_mute"
}
