// Package models provides custom GORM models and an isolated migration entry.
//
// MigrateCustomTables registers all custom_* tables independently from N9e's
// native models/migrate package, so this code does not touch upstream files.
//
// Wire it into the application bootstrap (e.g. center/center.go) with a single
// extra call:
//
//	import customModels "github.com/ccfos/nightingale/v6/internal/custom/models"
//	...
//	migrate.Migrate(db)
//	customModels.MigrateCustomTables(db)
package models

import (
	"github.com/toolkits/pkg/logger"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// MigrateCustomTables auto-migrates every custom_* table.
//
// Errors are logged per-table but do not abort startup — same convention as
// the upstream models/migrate.MigrateTables, so a single bad column does not
// block the whole service.
func MigrateCustomTables(db *gorm.DB) {
	if _, ok := db.Dialector.(*mysql.Dialector); ok {
		db = db.Set("gorm:table_options", "ENGINE=InnoDB DEFAULT CHARSET=utf8mb4")
	}

	tables := []interface{}{
		&CustomMuteCron{},
		&CustomEmergencyMute{},
		&CustomInhibitRule{},
		&CustomAggregateRule{},
		&CustomIncident{},
		&CustomIncidentEvent{},
		&CustomAuditLog{},
		&CustomSuppressedEvent{},
	}

	for _, t := range tables {
		if err := db.AutoMigrate(t); err != nil {
			logger.Errorf("custom: failed to migrate table %T: %v", t, err)
			continue
		}
		logger.Infof("custom: migrated table %T", t)
	}
}
