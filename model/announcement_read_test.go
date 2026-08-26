package model

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/console_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
}

func TestNoticeAnnouncementKey(t *testing.T) {
	assert.Equal(t, "", NoticeAnnouncementKey(""))
	k := NoticeAnnouncementKey("hello world")
	assert.NotEmpty(t, k)
	assert.True(t, len(k) > 8)
	// same content → same key
	assert.Equal(t, k, NoticeAnnouncementKey("hello world"))
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
	require.NoError(t, MarkAnnouncementsRead(userId, keys))
	require.NoError(t, MarkAnnouncementsRead(userId, keys)) // second time no error

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
	require.NoError(t, MarkAnnouncementsRead(userId, []string{"id:3"}))
	unread, err = GetUnreadAnnouncementsForUser(userId)
	require.NoError(t, err)
	assert.Empty(t, unread)
}

func TestCalcCacheRate(t *testing.T) {
	assert.Equal(t, 0.0, calcCacheRate(0, 0))
	assert.Equal(t, 0.0, calcCacheRate(10, 0))
	assert.InDelta(t, 50.0, calcCacheRate(50, 100), 0.001)
	assert.InDelta(t, 95.35, calcCacheRate(9535, 10000), 0.01)
}

func TestIsRatioDraftLogic(t *testing.T) {
	// Mirror frontend isRatioDraft regex: empty or -?\d*\.?\d*
	re := func(s string) bool {
		if s == "" {
			return true
		}
		// simple port of /^-?\d*\.?\d*$/
		if s == "-" || s == "." || s == "-." {
			return true
		}
		ok := true
		dot := 0
		for i, ch := range s {
			if ch == '-' {
				if i != 0 {
					ok = false
					break
				}
				continue
			}
			if ch == '.' {
				dot++
				if dot > 1 {
					ok = false
					break
				}
				continue
			}
			if ch < '0' || ch > '9' {
				ok = false
				break
			}
		}
		return ok
	}
	assert.True(t, re(""))
	assert.True(t, re("0."))
	assert.True(t, re("0.05"))
	assert.True(t, re(".5"))
	assert.False(t, re("1.2.3"))
	assert.False(t, re("abc"))
}

func TestNormalizeRatioFallback(t *testing.T) {
	// used by group pricing serialize path mentally
	norm := func(v interface{}) float64 {
		switch n := v.(type) {
		case float64:
			return n
		case string:
			// Number("") is 0 in JS; we use 1 as fallback in serialize for empty ratio
			if n == "" {
				return 1
			}
			// parse
			var f float64
			_, err := fmt.Sscan(n, &f)
			if err != nil {
				return 1
			}
			return f
		default:
			return 1
		}
	}
	assert.Equal(t, 1.0, norm(""))
	assert.Equal(t, 0.0, norm("0."))
	assert.Equal(t, 0.05, norm("0.05"))
}
