// Package models defines GORM models for the custom denoise/suppress/mute tables.
//
// All custom tables share the `custom_` prefix. Models follow N9e conventions:
//   - int64 id primary key
//   - group_id for business-group scoping (consistent with native AlertMute)
//   - disabled (0=enabled, 1=disabled)
//   - create_by / update_by / create_at / update_at audit fields
//   - JSON-serialized fields stored as ormx.JSONArr / ormx.JSONObj
//
// Migration is performed via MigrateCustomTables (see migrate.go).
package models
