package model

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/console_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestAnnouncementKeyFromItem_StableIdAndHash(t *testing.T) {
	withId := map[string]interface{}{
		"id":          float64(12),
		"content":     "hello",
		"publishDate": "2026-07-18T00:00:00Z",
	}
	assert.Equal(t, "id:12", AnnouncementKeyFromItem(withId))

	a := map[string]interface{}{
		"content":     "same",
		"publishDate": "2026-07-18T00:00:00Z",
		"type":        "warning",
	}
	b := map[string]interface{}{
		"content":     "same",
		"publishDate": "2026-07-18T00:00:00Z",
		"type":        "warning",
	}
	assert.Equal(t, AnnouncementKeyFromItem(a), AnnouncementKeyFromItem(b))

	c := map[string]interface{}{
		"content":     "changed",
		"publishDate": "2026-07-18T00:00:00Z",
		"type":        "warning",
	}
	assert.NotEqual(t, AnnouncementKeyFromItem(a), AnnouncementKeyFromItem(c))
	// Preserve the key format already written by the legacy customized line.
	assert.Equal(t, "hash:4e9b28fa74484710", AnnouncementKeyFromItem(a))
}

func TestNoticeAnnouncementKey(t *testing.T) {
	assert.Equal(t, "", NoticeAnnouncementKey(""))
	k := NoticeAnnouncementKey("hello world")
	assert.NotEmpty(t, k)
	assert.True(t, len(k) > 8)
	// same content → same key
	assert.Equal(t, k, NoticeAnnouncementKey("hello world"))
	assert.Equal(t, "notice:2aae6c35c94fcfb4", k)
}

func TestListAllAnnouncementDTOs_DoesNotDropConfiguredItems(t *testing.T) {
	old := console_setting.GetConsoleSetting().Announcements
	oldEnabled := console_setting.GetConsoleSetting().AnnouncementsEnabled
	defer func() {
		console_setting.GetConsoleSetting().Announcements = old
		console_setting.GetConsoleSetting().AnnouncementsEnabled = oldEnabled
	}()
	console_setting.GetConsoleSetting().AnnouncementsEnabled = true
	announcements := make([]map[string]interface{}, 25)
	for i := range announcements {
		announcements[i] = map[string]interface{}{
			"id":          i + 1,
			"content":     fmt.Sprintf("a%d", i+1),
			"publishDate": fmt.Sprintf("2026-07-%02dT01:00:00Z", i+1),
		}
	}
	encoded, err := json.Marshal(announcements)
	require.NoError(t, err)
	console_setting.GetConsoleSetting().Announcements = string(encoded)

	common.OptionMapRWMutex.Lock()
	if common.OptionMap == nil {
		common.OptionMap = map[string]string{}
	}
	oldNotice := common.OptionMap["Notice"]
	common.OptionMap["Notice"] = ""
	common.OptionMapRWMutex.Unlock()
	defer func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap["Notice"] = oldNotice
		common.OptionMapRWMutex.Unlock()
	}()

	items := ListAllAnnouncementDTOs()
	require.Len(t, items, 25)
	assert.Equal(t, "id:25", items[0].Key)
}

func TestMarkAnnouncementsRead_ConcurrentIdempotent(t *testing.T) {
	if DB == nil {
		t.Skip("DB not initialized")
	}
	require.NoError(t, DB.AutoMigrate(&UserAnnouncementRead{}))

	userId := 900002
	DB.Where("user_id = ?", userId).Delete(&UserAnnouncementRead{})

	const workers = 12
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := MarkAnnouncementsRead(userId, []string{"id:concurrent"})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	var count int64
	require.NoError(t, DB.Model(&UserAnnouncementRead{}).
		Where("user_id = ? AND announcement_key = ?", userId, "id:concurrent").
		Count(&count).Error)
	assert.EqualValues(t, 1, count)
}

func TestAnnouncementRead_FormalMigrationPreservesPopulatedDatabase(t *testing.T) {
	previousDB := DB
	previousType := common.MainDatabaseType()
	dbPath := filepath.Join(t.TempDir(), "populated.db")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Option{}))
	require.NoError(t, db.Create(&Option{Key: "Notice", Value: "existing data"}).Error)

	DB = db
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		DB = previousDB
		common.SetMainDatabaseType(previousType)
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	require.NoError(t, migrateDB())
	assert.True(t, db.Migrator().HasTable(&UserAnnouncementRead{}))
	var existing Option
	require.NoError(t, db.Where("key = ?", "Notice").First(&existing).Error)
	assert.Equal(t, "existing data", existing.Value)

	rows := []UserAnnouncementRead{
		{UserId: 42, AnnouncementKey: "id:migrated"},
		{UserId: 42, AnnouncementKey: "id:migrated"},
	}
	require.Error(t, db.Create(&rows).Error, "formal migration must install the composite unique index")
}

func TestMarkAnnouncementsRead_IdempotentAndUnreadFilter(t *testing.T) {
	if DB == nil {
		t.Skip("DB not initialized")
	}
	// Ensure table exists for this test process.
	require.NoError(t, DB.AutoMigrate(&UserAnnouncementRead{}))

	userId := 900001
	// clean previous
	DB.Where("user_id = ?", userId).Delete(&UserAnnouncementRead{})

	keys := []string{"id:1", "id:2", "id:1", ""}
	marked, err := MarkAnnouncementsRead(userId, keys)
	require.NoError(t, err)
	assert.EqualValues(t, 2, marked)
	marked, err = MarkAnnouncementsRead(userId, keys)
	require.NoError(t, err)
	assert.Zero(t, marked)

	read, err := GetReadAnnouncementKeys(userId)
	require.NoError(t, err)
	assert.Contains(t, read, "id:1")
	assert.Contains(t, read, "id:2")
	assert.Len(t, read, 2)

	// Inject announcements into console setting for unread filter.
	old := console_setting.GetConsoleSetting().Announcements
	oldEnabled := console_setting.GetConsoleSetting().AnnouncementsEnabled
	defer func() {
		console_setting.GetConsoleSetting().Announcements = old
		console_setting.GetConsoleSetting().AnnouncementsEnabled = oldEnabled
	}()
	console_setting.GetConsoleSetting().AnnouncementsEnabled = true
	console_setting.GetConsoleSetting().Announcements = `[
	  {"id":1,"content":"a1","publishDate":"2026-07-18T10:00:00Z","type":"default"},
	  {"id":2,"content":"a2","publishDate":"2026-07-18T11:00:00Z","type":"warning"},
	  {"id":3,"content":"a3","publishDate":"2026-07-18T12:00:00Z","type":"success"}
	]`

	// Clear notice so it doesn't pollute.
	common.OptionMapRWMutex.Lock()
	if common.OptionMap == nil {
		common.OptionMap = map[string]string{}
	}
	oldNotice := common.OptionMap["Notice"]
	common.OptionMap["Notice"] = ""
	common.OptionMapRWMutex.Unlock()
	defer func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap["Notice"] = oldNotice
		common.OptionMapRWMutex.Unlock()
	}()

	unread, err := GetUnreadAnnouncementsForUser(userId)
	require.NoError(t, err)
	// id 1 and 2 read → only id 3 unread
	require.Len(t, unread, 1)
	assert.Equal(t, "id:3", unread[0].Key)
	assert.Equal(t, "a3", unread[0].Content)

	// mark all remaining
	marked, err = MarkAnnouncementsRead(userId, []string{"id:3"})
	require.NoError(t, err)
	assert.EqualValues(t, 1, marked)
	unread, err = GetUnreadAnnouncementsForUser(userId)
	require.NoError(t, err)
	assert.Empty(t, unread)
}
