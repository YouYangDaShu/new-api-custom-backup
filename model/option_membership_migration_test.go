package model

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupOptionMigrationDB(t *testing.T, models ...any) {
	t.Helper()
	previousDB := DB
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	DB = db
	require.NoError(t, db.AutoMigrate(models...))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() {
		DB = previousDB
		_ = sqlDB.Close()
	})
}

func TestMigrateMembershipGroupsOptionPreservesLegacyRouteNames(t *testing.T) {
	oldMembership := ratio_setting.MembershipGroups2JSONString()
	t.Cleanup(func() {
		require.NoError(t, types.LoadFromJsonString(ratio_setting.GetGroupRatioSetting().MembershipGroups, oldMembership))
	})
	setupOptionMigrationDB(t, &Option{}, &User{}, &Channel{}, &Ability{}, &Token{})
	require.NoError(t, DB.Create(&User{Username: "member", Password: "password123", Group: "vip"}).Error)
	require.NoError(t, DB.Create(&Channel{Name: "default-route", Key: "channel-key", Group: "default"}).Error)
	require.NoError(t, DB.Create(&Token{UserId: 1, Name: "route-token", Key: "route-key", Group: "grok"}).Error)

	require.NoError(t, MigrateMembershipGroupsOption())

	var option Option
	require.NoError(t, DB.Where("key = ?", "group_ratio_setting.membership_groups").First(&option).Error)
	var groups map[string]bool
	require.NoError(t, common.Unmarshal([]byte(option.Value), &groups))
	assert.True(t, groups["vip"])
	assert.NotContains(t, groups, "default")
	assert.NotContains(t, groups, "grok")

	firstValue := option.Value
	require.NoError(t, MigrateMembershipGroupsOption())
	require.NoError(t, DB.Where("key = ?", "group_ratio_setting.membership_groups").First(&option).Error)
	assert.Equal(t, firstValue, option.Value)
}

func TestMigrateMembershipGroupsOptionDoesNotOverwriteExistingValue(t *testing.T) {
	setupOptionMigrationDB(t, &Option{}, &User{}, &Channel{}, &Ability{}, &Token{})
	const existing = `{"custom":true}`
	require.NoError(t, DB.Create(&Option{Key: "group_ratio_setting.membership_groups", Value: existing}).Error)
	require.NoError(t, DB.Create(&User{Username: "member", Password: "password123", Group: "vip"}).Error)

	require.NoError(t, MigrateMembershipGroupsOption())

	var option Option
	require.NoError(t, DB.Where("key = ?", "group_ratio_setting.membership_groups").First(&option).Error)
	assert.Equal(t, existing, option.Value)
}

func TestMigrateMembershipGroupsOptionConcurrentCallsAreIdempotent(t *testing.T) {
	setupOptionMigrationDB(t, &Option{}, &User{}, &Channel{}, &Ability{}, &Token{})
	require.NoError(t, DB.Create(&User{Username: "member", Password: "password123", Group: "vip"}).Error)

	const callers = 8
	start := make(chan struct{})
	errors := make(chan error, callers)
	var workers sync.WaitGroup
	for range callers {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			errors <- MigrateMembershipGroupsOption()
		}()
	}
	close(start)
	workers.Wait()
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}

	var count int64
	require.NoError(t, DB.Model(&Option{}).Where("key = ?", "group_ratio_setting.membership_groups").Count(&count).Error)
	assert.Equal(t, int64(1), count)
}

func TestUpdateOptionsBulkRejectsDuplicateNestedJSONKeys(t *testing.T) {
	setupOptionMigrationDB(t, &Option{})
	require.NoError(t, DB.Create(&Option{Key: "GroupRatio", Value: `{"default":1}`}).Error)

	err := UpdateOptionsBulk(map[string]string{
		"GroupRatio": `{"vip":0.8,"vip":1.2}`,
	})
	require.Error(t, err)

	var groupRatio Option
	require.NoError(t, DB.Where("key = ?", "GroupRatio").First(&groupRatio).Error)
	assert.Equal(t, `{"default":1}`, groupRatio.Value)
}

func TestUpdateOptionsBulkRejectsInvalidSetWithoutPartialWrite(t *testing.T) {
	setupOptionMigrationDB(t, &Option{})
	require.NoError(t, DB.Create(&Option{Key: "GroupRatio", Value: `{"default":1}`}).Error)
	require.NoError(t, DB.Create(&Option{Key: "AutoGroups", Value: `["default"]`}).Error)

	err := UpdateOptionsBulk(map[string]string{
		"GroupRatio": `{"default":0.9}`,
		"AutoGroups": `{not-an-array}`,
	})
	require.Error(t, err)

	var groupRatio Option
	var autoGroups Option
	require.NoError(t, DB.Where("key = ?", "GroupRatio").First(&groupRatio).Error)
	require.NoError(t, DB.Where("key = ?", "AutoGroups").First(&autoGroups).Error)
	assert.Equal(t, `{"default":1}`, groupRatio.Value)
	assert.Equal(t, `["default"]`, autoGroups.Value)
}
