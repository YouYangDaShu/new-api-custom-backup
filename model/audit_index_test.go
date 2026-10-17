package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestAuditTimeEventIndexMigration(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		name := "fresh"
		if legacy {
			name = "existing_rows"
		}
		t.Run(name, func(t *testing.T) {
			db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(1)
			t.Cleanup(func() { _ = sqlDB.Close() })
			oldDB, oldType := LOG_DB, common.LogDatabaseType()
			LOG_DB = db
			common.SetLogDatabaseType(common.DatabaseTypeSQLite)
			t.Cleanup(func() { LOG_DB = oldDB; common.SetLogDatabaseType(oldType) })
			if legacy {
				require.NoError(t, db.AutoMigrate(&AuditLog{}))
				if db.Migrator().HasIndex(&AuditLog{}, "idx_audit_time_event") {
					require.NoError(t, db.Migrator().DropIndex(&AuditLog{}, "idx_audit_time_event"))
				}
				entries := []AuditLog{
					{EventId: "audit-index-a", CreatedAt: 100, UserId: 10, ActorRole: common.RoleCommonUser, Category: AuditCategoryLogin, Content: "keep-a"},
					{EventId: "audit-index-c", CreatedAt: 100, UserId: 10, ActorRole: common.RoleRootUser, Category: AuditCategorySecurity, Content: "keep-c"},
					{EventId: "audit-index-b", CreatedAt: 100, UserId: 20, ActorRole: common.RoleAdminUser, Category: AuditCategoryOperation, Content: "keep-b"},
				}
				require.NoError(t, db.Create(&entries).Error)
			}
			var before []AuditLog
			if legacy {
				require.NoError(t, db.Order("id").Find(&before).Error)
			}
			require.NoError(t, MigrateAuditLogs())
			require.True(t, db.Migrator().HasIndex(&AuditLog{}, "idx_audit_time_event"), "audit ordering needs its matching compound index")
			var columns []struct {
				Seqno int
				Name  string
			}
			require.NoError(t, db.Raw("PRAGMA index_info('idx_audit_time_event')").Scan(&columns).Error)
			require.Len(t, columns, 2)
			require.Equal(t, "created_at", columns[0].Name)
			require.Equal(t, "event_id", columns[1].Name)
			require.NoError(t, MigrateAuditLogs(), "repeated startup migration must be idempotent")
			require.True(t, db.Migrator().HasIndex(&AuditLog{}, "idx_audit_logs_event_id"), "retain unique event identity index")
			if !legacy {
				return
			}
			var after []AuditLog
			require.NoError(t, db.Order("id").Find(&after).Error)
			require.Equal(t, before, after, "migration must not change audit data")
			rootRows, total, err := GetAuditLogs(AuditLogFilter{}, 0, 20, common.RoleRootUser)
			require.NoError(t, err)
			require.EqualValues(t, 3, total)
			require.Equal(t, []string{"audit-index-c", "audit-index-b", "audit-index-a"}, []string{rootRows[0].EventId, rootRows[1].EventId, rootRows[2].EventId})
			adminRows, total, err := GetAuditLogs(AuditLogFilter{}, 0, 20, common.RoleAdminUser)
			require.NoError(t, err)
			require.EqualValues(t, 2, total)
			for _, row := range adminRows {
				require.NotEqual(t, common.RoleRootUser, row.ActorRole)
			}
			ownRows, total, err := GetAuditLogs(AuditLogFilter{UserId: 10, SelfView: true}, 0, 20, common.RoleCommonUser)
			require.NoError(t, err)
			require.EqualValues(t, 1, total)
			require.Equal(t, "audit-index-a", ownRows[0].EventId)
		})
	}
}
