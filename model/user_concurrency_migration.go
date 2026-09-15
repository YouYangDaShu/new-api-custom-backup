package model

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// migrateUserConcurrencyDefaults gives legacy rows an explicit value after
// AutoMigrate adds the concurrency columns. The legacy flag is captured before
// AutoMigrate: every existing row predates the policy and must follow the current
// system default. Once the column exists, zero is a valid unlimited setting and
// must never be rewritten.
func migrateUserConcurrencyDefaults(db *gorm.DB, legacyColumn bool) error {
	if !legacyColumn || db == nil || !db.Migrator().HasTable(&User{}) {
		return nil
	}
	defaultLimit := common.GetUserConcurrencyDefault()
	if defaultLimit < 1 || defaultLimit > common.MaxUserConcurrency {
		return fmt.Errorf("invalid user concurrency default: %d", defaultLimit)
	}
	return db.Model(&User{}).Where("1 = 1").Updates(map[string]any{
		"concurrency":          defaultLimit,
		"concurrency_override": false,
	}).Error
}
