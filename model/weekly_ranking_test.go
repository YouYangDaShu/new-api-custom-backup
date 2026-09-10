package model

import (
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestMaskRankingNameMasksOtherUsers(t *testing.T) {
	// Keep identifiable edges of email local parts and the domain.
	require.Equal(t, "a***e@example.com", maskRankingName("alice@example.com"))
	require.Equal(t, "3***1@qq.com", maskRankingName("304123051@qq.com"))
	require.Equal(t, "ab@example.com", maskRankingName("ab@example.com"))
	// Only digit strings longer than six characters are masked.
	require.Equal(t, "123**67", maskRankingName("1234567"))
	require.Equal(t, "123456", maskRankingName("123456"))
	require.Equal(t, "197***82", maskRankingName("1975702382"))
	// Plain English usernames remain readable; mixed/Chinese names retain the
	// existing conservative first-rune masking.
	require.Equal(t, "player", maskRankingName("player"))
	require.Equal(t, "普***", maskRankingName("普通用户名"))
	require.Equal(t, "p***", maskRankingName("player29369"))
	require.Equal(t, "***", maskRankingName("   "))
}

func TestConfiguredWeeklyRankingRewardsUsesOverrideAndFallback(t *testing.T) {
	oldReward1, oldReward2, oldReward3 := common.WeeklyRankReward1, common.WeeklyRankReward2, common.WeeklyRankReward3
	common.OptionMapRWMutex.Lock()
	oldOptions := common.OptionMap
	common.OptionMap = map[string]string{"TokenRankingRewards": "[1.25, 0.5]"}
	common.OptionMapRWMutex.Unlock()
	common.WeeklyRankReward1, common.WeeklyRankReward2, common.WeeklyRankReward3 = 9, 8, 7
	t.Cleanup(func() {
		common.WeeklyRankReward1, common.WeeklyRankReward2, common.WeeklyRankReward3 = oldReward1, oldReward2, oldReward3
		common.OptionMapRWMutex.Lock()
		common.OptionMap = oldOptions
		common.OptionMapRWMutex.Unlock()
	})

	require.Equal(t, []float64{1.25, 0.5}, ConfiguredWeeklyRankingRewardAmounts())
	require.Equal(t, []int{common.QuotaFromFloat(1.25 * common.QuotaPerUnit), common.QuotaFromFloat(0.5 * common.QuotaPerUnit)}, ConfiguredWeeklyRankingRewards())

	common.OptionMapRWMutex.Lock()
	common.OptionMap["TokenRankingRewards"] = "[]"
	common.OptionMapRWMutex.Unlock()
	require.Equal(t, []float64{9, 8, 7}, ConfiguredWeeklyRankingRewardAmounts())
	require.Equal(t, []int{common.QuotaFromFloat(9 * common.QuotaPerUnit), common.QuotaFromFloat(8 * common.QuotaPerUnit), common.QuotaFromFloat(7 * common.QuotaPerUnit)}, ConfiguredWeeklyRankingRewards())
}

func TestShanghaiWeekRangeUsesMondayBoundary(t *testing.T) {
	now := time.Date(2026, 8, 16, 23, 30, 0, 0, time.FixedZone("UTC+8", 8*3600))
	start, end, key := ShanghaiWeekRange(now, 0)
	require.Equal(t, "2026-08-10", key)
	require.Equal(t, time.Date(2026, 8, 10, 0, 0, 0, 0, time.FixedZone("UTC+8", 8*3600)).Unix(), start)
	require.Equal(t, time.Date(2026, 8, 17, 0, 0, 0, 0, time.FixedZone("UTC+8", 8*3600)).Unix(), end)
}

func TestDailyAndWeeklyRankingUseShanghaiRanges(t *testing.T) {
	truncateTables(t)
	user := User{Id: 92050, Username: "period-user", AffCode: "rank-period", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, DB.Create(&user).Error)
	loc := time.FixedZone("UTC+8", 8*3600)
	weekStart := time.Date(2026, 8, 10, 0, 0, 0, 0, loc).Unix()
	dayStart := time.Date(2026, 8, 16, 0, 0, 0, 0, loc).Unix()
	logs := []Log{
		{UserId: user.Id, Type: LogTypeConsume, CreatedAt: weekStart + 10, PromptTokens: 100, CompletionTokens: 10},
		{UserId: user.Id, Type: LogTypeConsume, CreatedAt: dayStart + 10, PromptTokens: 20, CompletionTokens: 5},
	}
	for i := range logs {
		require.NoError(t, LOG_DB.Create(&logs[i]).Error)
	}
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, loc)
	daily, _, err := GetUserRankingWithCurrent(now, "day", 20, user.Id)
	require.NoError(t, err)
	require.EqualValues(t, 25, daily[0].TotalTokens)
	weekly, _, err := GetUserRankingWithCurrent(now, "week", 20, user.Id)
	require.NoError(t, err)
	require.EqualValues(t, 135, weekly[0].TotalTokens)
}

func TestWeeklyRankingReturnsCurrentUserOutsideTopTwenty(t *testing.T) {
	truncateTables(t)
	loc := time.FixedZone("UTC+8", 8*3600)
	start := time.Date(2026, 8, 10, 0, 0, 0, 0, loc).Unix()

	for i := 0; i < 21; i++ {
		userID := 92001 + i
		user := User{Id: userID, Username: fmt.Sprintf("rank-user-%02d", i+1), AffCode: fmt.Sprintf("rank-outside-%02d", i+1), Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
		require.NoError(t, DB.Create(&user).Error)
		require.NoError(t, LOG_DB.Create(&Log{UserId: userID, Type: LogTypeConsume, CreatedAt: start + int64(i), PromptTokens: 21 - i}).Error)
	}

	rows, current, err := GetUserRankingWithCurrent(time.Date(2026, 8, 16, 12, 0, 0, 0, loc), "week", 20, 92021)
	require.NoError(t, err)
	require.Len(t, rows, 20)
	require.NotNil(t, current)
	require.Equal(t, 21, current.Rank)
	require.True(t, current.IsCurrentUser)
	require.Equal(t, "rank-user-21", current.DisplayName)
	// Other users' names are masked for the signed-in viewer.
	for _, row := range rows {
		require.Equal(t, "r***", row.DisplayName)
	}
}

func TestRankingMasksOtherUsersButNotCurrentUser(t *testing.T) {
	truncateTables(t)
	loc := time.FixedZone("UTC+8", 8*3600)
	start := time.Date(2026, 8, 10, 0, 0, 0, 0, loc).Unix()
	users := []User{
		{Id: 92060, Username: "alice@example.com", AffCode: "rank-mask-a", Role: common.RoleCommonUser, Status: common.UserStatusEnabled},
		{Id: 92061, Username: "bob-viewer", AffCode: "rank-mask-b", Role: common.RoleCommonUser, Status: common.UserStatusEnabled},
	}
	for i := range users {
		require.NoError(t, DB.Create(&users[i]).Error)
		require.NoError(t, LOG_DB.Create(&Log{UserId: users[i].Id, Type: LogTypeConsume, CreatedAt: start + int64(i), PromptTokens: 100 - i}).Error)
	}

	rows, _, err := GetUserRankingWithCurrent(time.Date(2026, 8, 16, 12, 0, 0, 0, loc), "week", 20, 92061)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	byID := map[int]WeeklyUserRanking{}
	for _, row := range rows {
		byID[row.UserID] = row
	}
	require.Equal(t, "a***e@example.com", byID[92060].DisplayName)
	require.Equal(t, "bob-viewer", byID[92061].DisplayName)
	require.True(t, byID[92061].IsCurrentUser)
}

func TestWeeklyUserRankingAndIdempotentRewards(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.AutoMigrate(&WeeklyRankingReward{}))
	require.NoError(t, DB.Exec("DELETE FROM weekly_ranking_rewards").Error)
	t.Cleanup(func() { DB.Exec("DELETE FROM weekly_ranking_rewards") })
	users := []User{
		{Id: 91001, Username: "alice@example.com", AffCode: "rank-a", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Quota: 0},
		{Id: 91002, Username: "bob", AffCode: "rank-b", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Quota: 0},
		{Id: 91003, Username: "root", AffCode: "rank-root", Role: common.RoleRootUser, Status: common.UserStatusEnabled, Quota: 0},
	}
	for i := range users {
		require.NoError(t, DB.Create(&users[i]).Error)
	}
	loc := time.FixedZone("UTC+8", 8*3600)
	start := time.Date(2026, 8, 10, 0, 0, 0, 0, loc).Unix()
	logs := []Log{
		{UserId: 91001, Username: "alice@example.com", Type: LogTypeConsume, CreatedAt: start + 10, PromptTokens: 100, CompletionTokens: 20, Quota: 1},
		{UserId: 91001, Username: "alice@example.com", Type: LogTypeConsume, CreatedAt: start + 20, PromptTokens: 50, CompletionTokens: 30, Quota: 1},
		{UserId: 91002, Username: "bob", Type: LogTypeConsume, CreatedAt: start + 30, PromptTokens: 120, CompletionTokens: 10, Quota: 1},
		{UserId: 91003, Username: "root", Type: LogTypeConsume, CreatedAt: start + 40, PromptTokens: 9999, CompletionTokens: 1, Quota: 1},
		{UserId: 91002, Username: "bob", Type: LogTypeError, CreatedAt: start + 50, PromptTokens: 9999, CompletionTokens: 0},
	}
	for i := range logs {
		require.NoError(t, LOG_DB.Create(&logs[i]).Error)
	}

	rows, err := GetWeeklyUserRanking(time.Date(2026, 8, 16, 12, 0, 0, 0, loc), 10)
	require.NoError(t, err)
	require.Len(t, rows, 3)
	require.Equal(t, 91003, rows[0].UserID)
	require.False(t, rows[0].RewardEligible)
	require.EqualValues(t, 10000, rows[0].TotalTokens)
	require.Equal(t, 91001, rows[1].UserID)
	require.True(t, rows[1].RewardEligible)
	require.EqualValues(t, 200, rows[1].TotalTokens)
	require.Equal(t, "a***e@example.com", rows[1].DisplayName)
	require.Equal(t, 91002, rows[2].UserID)
	require.EqualValues(t, 130, rows[2].TotalTokens)

	awarded, err := SettleWeeklyRankingRewards(time.Date(2026, 8, 17, 0, 5, 0, 0, loc), []int{300, 200, 100})
	require.NoError(t, err)
	require.EqualValues(t, 2, awarded)
	awarded, err = SettleWeeklyRankingRewards(time.Date(2026, 8, 17, 1, 5, 0, 0, loc), []int{300, 200, 100})
	require.NoError(t, err)
	require.EqualValues(t, 0, awarded)
	var alice, bob User
	require.NoError(t, DB.First(&alice, 91001).Error)
	require.NoError(t, DB.First(&bob, 91002).Error)
	require.Equal(t, 300, alice.Quota)
	require.Equal(t, 200, bob.Quota)
	var root User
	require.NoError(t, DB.First(&root, 91003).Error)
	require.Zero(t, root.Quota)
	var count int64
	require.NoError(t, DB.Model(&WeeklyRankingReward{}).Count(&count).Error)
	require.EqualValues(t, 2, count)
}

func TestWeeklyRewardExactBalanceForTopThree(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.AutoMigrate(&WeeklyRankingReward{}))
	require.NoError(t, DB.Exec("DELETE FROM weekly_ranking_rewards").Error)
	t.Cleanup(func() { DB.Exec("DELETE FROM weekly_ranking_rewards") })

	initialQuota := []int{111, 222, 333}
	for i := range initialQuota {
		user := User{Id: 91101 + i, Username: fmt.Sprintf("winner-%d", i+1), AffCode: "rank-exact-" + string(rune('a'+i)), Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Quota: initialQuota[i]}
		require.NoError(t, DB.Create(&user).Error)
	}
	loc := time.FixedZone("UTC+8", 8*3600)
	start := time.Date(2026, 8, 10, 0, 0, 0, 0, loc).Unix()
	for i, tokens := range []int{3000, 2000, 1000} {
		require.NoError(t, LOG_DB.Create(&Log{UserId: 91101 + i, Type: LogTypeConsume, CreatedAt: start + int64(i), PromptTokens: tokens}).Error)
	}

	rewards := []int{5_000_000, 2_500_000, 1_000_000}
	awarded, err := SettleWeeklyRankingRewards(time.Date(2026, 8, 17, 0, 5, 0, 0, loc), rewards)
	require.NoError(t, err)
	require.EqualValues(t, 3, awarded)

	for i := range initialQuota {
		var user User
		require.NoError(t, DB.First(&user, 91101+i).Error)
		require.Equal(t, initialQuota[i]+rewards[i], user.Quota)
		var ledger WeeklyRankingReward
		require.NoError(t, DB.Where("week_start = ? AND user_id = ?", "2026-08-10", 91101+i).First(&ledger).Error)
		require.Equal(t, i+1, ledger.Rank)
		require.Equal(t, rewards[i], ledger.RewardQuota)
	}
}

func TestWeeklyRewardSkipsZeroRewardRank(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.AutoMigrate(&WeeklyRankingReward{}))
	require.NoError(t, DB.Exec("DELETE FROM weekly_ranking_rewards").Error)
	t.Cleanup(func() { DB.Exec("DELETE FROM weekly_ranking_rewards") })

	for i := 0; i < 3; i++ {
		user := User{Id: 91201 + i, Username: fmt.Sprintf("zero-rank-%d", i+1), AffCode: "rank-zero-" + string(rune('a'+i)), Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Quota: 100}
		require.NoError(t, DB.Create(&user).Error)
	}
	loc := time.FixedZone("UTC+8", 8*3600)
	start := time.Date(2026, 8, 10, 0, 0, 0, 0, loc).Unix()
	for i, tokens := range []int{3000, 2000, 1000} {
		require.NoError(t, LOG_DB.Create(&Log{UserId: 91201 + i, Type: LogTypeConsume, CreatedAt: start + int64(i), PromptTokens: tokens}).Error)
	}

	awarded, err := SettleWeeklyRankingRewards(time.Date(2026, 8, 17, 0, 5, 0, 0, loc), []int{500, 0, 100})
	require.NoError(t, err)
	require.EqualValues(t, 2, awarded)
	for i, expected := range []int{600, 100, 200} {
		var user User
		require.NoError(t, DB.First(&user, 91201+i).Error)
		require.Equal(t, expected, user.Quota)
	}
	var count int64
	require.NoError(t, DB.Model(&WeeklyRankingReward{}).Count(&count).Error)
	require.EqualValues(t, 2, count)
}

func TestWeeklyRewardScheduleStartsAtConfiguredWeekAndRunsAt0005(t *testing.T) {
	cases := []struct {
		name    string
		now     time.Time
		wantKey string
		wantRun bool
	}{
		{"deployment Sunday does not backfill older week", time.Date(2026, 8, 16, 20, 0, 0, 0, shanghaiLocation), "", false},
		{"Monday before 0005 waits", time.Date(2026, 8, 17, 0, 4, 59, 0, shanghaiLocation), "", false},
		{"Monday at 0005 settles current launch week", time.Date(2026, 8, 17, 0, 5, 0, 0, shanghaiLocation), "2026-08-10", true},
		{"later startup catches up same previous week", time.Date(2026, 8, 19, 9, 30, 0, 0, shanghaiLocation), "2026-08-10", true},
		{"next Monday settles next week", time.Date(2026, 8, 24, 0, 5, 0, 0, shanghaiLocation), "2026-08-17", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			key, ok := WeeklyRewardWeekDue(tc.now)
			require.Equal(t, tc.wantRun, ok)
			require.Equal(t, tc.wantKey, key)
		})
	}
}

func TestNextWeeklyRewardCheckIsMonday0005Shanghai(t *testing.T) {
	cases := []struct {
		now  time.Time
		want time.Time
	}{
		{time.Date(2026, 8, 16, 20, 0, 0, 0, shanghaiLocation), time.Date(2026, 8, 17, 0, 5, 0, 0, shanghaiLocation)},
		{time.Date(2026, 8, 17, 0, 4, 59, 0, shanghaiLocation), time.Date(2026, 8, 17, 0, 5, 0, 0, shanghaiLocation)},
		{time.Date(2026, 8, 17, 0, 5, 0, 0, shanghaiLocation), time.Date(2026, 8, 24, 0, 5, 0, 0, shanghaiLocation)},
		{time.Date(2026, 8, 19, 9, 30, 0, 0, shanghaiLocation), time.Date(2026, 8, 24, 0, 5, 0, 0, shanghaiLocation)},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, NextWeeklyRewardCheck(tc.now))
	}
}

func TestWeeklyRewardWritesUserVisibleSystemLogOnce(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.AutoMigrate(&WeeklyRankingReward{}))
	require.NoError(t, LOG_DB.AutoMigrate(&Log{}))
	require.NoError(t, DB.Exec("DELETE FROM weekly_ranking_rewards").Error)
	t.Cleanup(func() { DB.Exec("DELETE FROM weekly_ranking_rewards") })

	loc := time.FixedZone("UTC+8", 8*3600)
	now := time.Date(2026, 8, 17, 0, 5, 0, 0, loc)
	user := User{Id: 91401, Username: "reward-log-user", AffCode: "reward-log-aff", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Quota: 100}
	require.NoError(t, DB.Create(&user).Error)
	require.NoError(t, LOG_DB.Create(&Log{UserId: user.Id, Username: user.Username, Type: LogTypeConsume, CreatedAt: time.Date(2026, 8, 10, 0, 0, 1, 0, loc).Unix(), PromptTokens: 1000}).Error)

	awarded, err := SettleWeeklyRankingRewards(now, []int{500_000})
	require.NoError(t, err)
	require.EqualValues(t, 1, awarded)
	awarded, err = SettleWeeklyRankingRewards(now.Add(time.Hour), []int{500})
	require.NoError(t, err)
	require.Zero(t, awarded)

	var logs []Log
	require.NoError(t, LOG_DB.Where("user_id = ? AND type = ? AND content LIKE ?", user.Id, LogTypeSystem, "%周榜第1名奖励%").Find(&logs).Error)
	require.Len(t, logs, 1)
	require.Contains(t, logs[0].Content, "2026-08-10")
	require.Contains(t, logs[0].Content, "+1.00")

	userLogs, total, err := GetUserLogs(user.Id, LogTypeUnknown, 0, 0, "", "", 0, 10, "", "", "")
	require.NoError(t, err)
	require.EqualValues(t, 2, total)
	visible := false
	for _, log := range userLogs {
		if log.Type == LogTypeSystem && strings.Contains(log.Content, "周榜第1名奖励") {
			visible = true
		}
	}
	require.True(t, visible)
}

func TestWeeklyRewardRollsBackLedgerWhenWinnerDisappears(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.AutoMigrate(&WeeklyRankingReward{}))
	require.NoError(t, DB.Exec("DELETE FROM weekly_ranking_rewards").Error)
	t.Cleanup(func() { DB.Exec("DELETE FROM weekly_ranking_rewards") })

	now := time.Date(2026, 8, 12, 12, 0, 0, 0, shanghaiLocation)
	row := WeeklyUserRanking{UserID: 91301, DisplayName: "vanishing-winner", TotalTokens: 1100}
	user := User{Id: row.UserID, Username: "vanishing-winner", AffCode: "vanishing-winner-aff", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Quota: 100}
	require.NoError(t, DB.Create(&user).Error)
	require.NoError(t, DB.Delete(&User{}, user.Id).Error)

	_, _, weekKey := ShanghaiWeekRange(now, -1)
	issued, err := awardWeeklyRankingUser(now, weekKey, row, 1, 500)
	require.Error(t, err)
	require.False(t, issued)

	var count int64
	require.NoError(t, DB.Model(&WeeklyRankingReward{}).Where("user_id = ?", user.Id).Count(&count).Error)
	require.Zero(t, count)
}

func TestWeeklyRewardRejectsInvalidRewardQuota(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.AutoMigrate(&WeeklyRankingReward{}))
	require.NoError(t, DB.Exec("DELETE FROM weekly_ranking_rewards").Error)
	t.Cleanup(func() { DB.Exec("DELETE FROM weekly_ranking_rewards") })

	user := User{Id: 91301, Username: "invalid-reward", AffCode: "rank-invalid", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Quota: 100}
	require.NoError(t, DB.Create(&user).Error)
	loc := time.FixedZone("UTC+8", 8*3600)
	start := time.Date(2026, 8, 10, 0, 0, 0, 0, loc).Unix()
	require.NoError(t, LOG_DB.Create(&Log{UserId: user.Id, Type: LogTypeConsume, CreatedAt: start + 1, PromptTokens: 100}).Error)

	for _, reward := range []int{-1, math.MaxInt32 + 1} {
		awarded, err := SettleWeeklyRankingRewards(time.Date(2026, 8, 17, 0, 5, 0, 0, loc), []int{reward})
		require.Error(t, err)
		require.Zero(t, awarded)
	}

	require.NoError(t, DB.First(&user, user.Id).Error)
	require.Equal(t, 100, user.Quota)
	var count int64
	require.NoError(t, DB.Model(&WeeklyRankingReward{}).Count(&count).Error)
	require.Zero(t, count)
}

func TestWeeklyRankingFallsBackWhenCacheExtractFails(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.AutoMigrate(&WeeklyRankingReward{}))
	require.NoError(t, DB.Exec("DELETE FROM weekly_ranking_rewards").Error)
	t.Cleanup(func() { DB.Exec("DELETE FROM weekly_ranking_rewards") })

	// Force the MySQL JSON-extract expression against the SQLite test DB so the
	// first aggregation query fails, proving one bad `other` value cannot 500
	// the ranking API or block weekly reward settlement.
	common.SetLogDatabaseType(common.DatabaseTypeMySQL)
	t.Cleanup(func() { common.SetLogDatabaseType(common.DatabaseTypeSQLite) })

	user := User{Id: 92070, Username: "fallback-user", AffCode: "rank-fallback", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Quota: 0}
	require.NoError(t, DB.Create(&user).Error)
	loc := time.FixedZone("UTC+8", 8*3600)
	start := time.Date(2026, 8, 10, 0, 0, 0, 0, loc).Unix()
	require.NoError(t, LOG_DB.Create(&Log{UserId: user.Id, Type: LogTypeConsume, CreatedAt: start + 10, PromptTokens: 100, CompletionTokens: 20, Other: "not-json"}).Error)

	rows, err := GetWeeklyUserRanking(time.Date(2026, 8, 16, 12, 0, 0, 0, loc), 10)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.EqualValues(t, 120, rows[0].TotalTokens)
	require.Zero(t, rows[0].CacheTokens)

	awarded, err := SettleWeeklyRankingRewards(time.Date(2026, 8, 17, 0, 5, 0, 0, loc), []int{500})
	require.NoError(t, err)
	require.EqualValues(t, 1, awarded)
}

func TestWeeklyRewardConcurrentSettlementHasSingleWinner(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.AutoMigrate(&WeeklyRankingReward{}))
	require.NoError(t, DB.Exec("DELETE FROM weekly_ranking_rewards").Error)
	t.Cleanup(func() { DB.Exec("DELETE FROM weekly_ranking_rewards") })
	user := User{Id: 91010, Username: "race", AffCode: "rank-race", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, DB.Create(&user).Error)
	loc := time.FixedZone("UTC+8", 8*3600)
	start := time.Date(2026, 8, 10, 0, 0, 0, 0, loc).Unix()
	require.NoError(t, LOG_DB.Create(&Log{UserId: user.Id, Type: LogTypeConsume, CreatedAt: start + 1, PromptTokens: 100}).Error)

	var wg sync.WaitGroup
	results := make([]int64, 5)
	errs := make([]error, 5)
	for i := range results {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			results[index], errs[index] = SettleWeeklyRankingRewards(time.Date(2026, 8, 17, 1, 0, 0, 0, loc), []int{500})
		}(i)
	}
	wg.Wait()
	var winners int64
	for i := range results {
		require.NoError(t, errs[i])
		winners += results[i]
	}
	require.EqualValues(t, 1, winners)
	require.NoError(t, DB.First(&user, user.Id).Error)
	require.Equal(t, 500, user.Quota)
}
