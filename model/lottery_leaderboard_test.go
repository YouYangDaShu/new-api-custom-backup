package model

import (
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLotteryLeaderboardRangeUsesShanghaiNaturalPeriods(t *testing.T) {
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.FixedZone("UTC", 0))
	start, end, err := LotteryLeaderboardRange(now, "day")
	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, 9, 18, 0, 0, 0, 0, shanghaiLocation).Unix(), start)
	assert.Equal(t, time.Date(2026, 9, 19, 0, 0, 0, 0, shanghaiLocation).Unix(), end)
	start, end, err = LotteryLeaderboardRange(now, "all")
	require.NoError(t, err)
	assert.Zero(t, start)
	assert.Zero(t, end)

	start, end, err = LotteryLeaderboardRange(now, "week")
	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, 9, 14, 0, 0, 0, 0, shanghaiLocation).Unix(), start)
	assert.Equal(t, time.Date(2026, 9, 21, 0, 0, 0, 0, shanghaiLocation).Unix(), end)

	start, end, err = LotteryLeaderboardRange(now, "month")
	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, 9, 1, 0, 0, 0, 0, shanghaiLocation).Unix(), start)
	assert.Equal(t, time.Date(2026, 10, 1, 0, 0, 0, 0, shanghaiLocation).Unix(), end)

	_, _, err = LotteryLeaderboardRange(now, "invalid")
	assert.Error(t, err)
}

func TestLotteryLeaderboardUsesTokenRankingMasking(t *testing.T) {
	assert.Equal(t, "y***g@qq.com", maskRankingName("youyang@qq.com"))
	assert.Equal(t, "197***82", maskRankingName("197123382"))
	assert.Equal(t, "alice", maskRankingName("alice"))
}

func TestGetLotteryLeaderboardsUsesDrawFactsAndExcludesDisabledUsers(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&User{}, &LotteryDraw{}))
	ids := []int{993101, 993102, 993103, 993104, 993105}
	DB.Unscoped().Where("id IN ?", ids).Delete(&User{})
	DB.Where("user_id IN ?", ids).Delete(&LotteryDraw{})
	t.Cleanup(func() {
		DB.Where("user_id IN ?", ids).Delete(&LotteryDraw{})
		DB.Unscoped().Where("id IN ?", ids).Delete(&User{})
	})

	users := []User{
		{Id: ids[0], Username: "197123382", Email: "unused@example.com", AffCode: "lb-993101", Status: common.UserStatusEnabled},
		{Id: ids[1], Username: "youyang", Email: "youyang@qq.com", AffCode: "lb-993102", Status: common.UserStatusEnabled},
		{Id: ids[2], Username: "hard-user", Email: "hard@example.com", AffCode: "lb-993103", Status: common.UserStatusEnabled},
		{Id: ids[3], Username: "disabled", Email: "disabled@example.com", AffCode: "lb-993104", Status: common.UserStatusDisabled},
		{Id: ids[4], Username: "deleted", Email: "deleted@example.com", AffCode: "lb-993105", Status: common.UserStatusEnabled},
	}
	require.NoError(t, DB.Create(&users).Error)
	require.NoError(t, DB.Delete(&User{}, ids[4]).Error)

	now := time.Date(2026, 9, 18, 12, 0, 0, 0, shanghaiLocation)
	draws := []LotteryDraw{
		{UserID: ids[0], PrizeLevel: 1, PityStep: 3, IsGuaranteed: false, PityPhase: lotteryPityPhaseBase, CreatedAt: now.Add(-time.Hour).Unix()},
		{UserID: ids[0], PrizeLevel: 1, PityStep: 14, IsGuaranteed: false, PityPhase: lotteryPityPhaseSoft, CreatedAt: now.Unix()},
		{UserID: ids[0], PrizeLevel: 1, PityStep: 20, IsGuaranteed: true, PityPhase: lotteryPityPhaseHard, CreatedAt: now.Unix()},
		{UserID: ids[0], PrizeLevel: 1, PityStep: 4, IsGuaranteed: true, PityPhase: lotteryPityPhaseSoft, CreatedAt: now.Unix()},
		{UserID: ids[1], PrizeLevel: 1, PityStep: 1, IsGuaranteed: false, CreatedAt: now.Add(time.Minute).Unix()},
		{UserID: ids[1], PrizeLevel: 2, PityStep: 2, IsGuaranteed: false, CreatedAt: now.Add(time.Minute).Unix()},
		{UserID: ids[2], PrizeLevel: 1, PityStep: 20, IsGuaranteed: true, CreatedAt: now.Add(time.Minute).Unix()},
		{UserID: ids[2], PrizeLevel: 1, PityStep: 20, IsGuaranteed: true, CreatedAt: now.Add(2 * time.Minute).Unix()},
		{UserID: ids[3], PrizeLevel: 1, PityStep: 1, IsGuaranteed: false, CreatedAt: now.Add(3 * time.Minute).Unix()},
		{UserID: ids[4], PrizeLevel: 1, PityStep: 1, IsGuaranteed: false, CreatedAt: now.Add(4 * time.Minute).Unix()},
		{UserID: ids[0], PrizeLevel: 2, PityStep: 2, IsGuaranteed: false, CreatedAt: now.Add(-8 * 24 * time.Hour).Unix()},
	}
	require.NoError(t, DB.Create(&draws).Error)

	data, err := GetLotteryLeaderboards(ids[0], now, "all")
	require.NoError(t, err)
	require.Len(t, data.Lucky.Rows, 2)
	assert.Equal(t, 1, data.Lucky.Rows[0].PityStep)
	assert.Equal(t, "youyang", data.Lucky.Rows[0].DisplayName)
	assert.Equal(t, 3, data.Lucky.Rows[1].PityStep)
	assert.Equal(t, int64(5), data.DrawCount.CurrentUser.Value)
	assert.Equal(t, "youyang", data.DrawCount.Rows[1].DisplayName)
	assert.Equal(t, "h***", data.DrawCount.Rows[2].DisplayName)
	assert.Equal(t, int64(2), data.HardPity.Rows[0].Value)
	require.NotNil(t, data.HardPity.CurrentUser)
	// Explicit phases win over numeric steps: the guaranteed soft row at step 4
	// enters neither the lucky nor the hard-pity board.
	assert.Equal(t, int64(1), data.HardPity.CurrentUser.Value)

	data, err = GetLotteryLeaderboards(ids[0], now, "day")
	require.NoError(t, err)
	assert.Equal(t, int64(4), data.DrawCount.CurrentUser.Value)
	assert.NotZero(t, data.CountStart)
	assert.NotZero(t, data.CountEnd)

	data, err = GetLotteryLeaderboards(ids[0], now, "week")
	require.NoError(t, err)
	assert.Equal(t, int64(4), data.DrawCount.CurrentUser.Value)

	data, err = GetLotteryLeaderboards(ids[0], now, "month")
	require.NoError(t, err)
	assert.Equal(t, int64(5), data.DrawCount.CurrentUser.Value)
}

func TestGetLotteryLeaderboardsUsesDatabaseTieOrderAndCurrentRanks(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&User{}, &LotteryDraw{}))
	ids := make([]int, 12)
	for i := range ids {
		ids[i] = 994201 + i
	}
	DB.Unscoped().Where("id IN ?", ids).Delete(&User{})
	DB.Where("user_id IN ?", ids).Delete(&LotteryDraw{})
	t.Cleanup(func() {
		DB.Where("user_id IN ?", ids).Delete(&LotteryDraw{})
		DB.Unscoped().Where("id IN ?", ids).Delete(&User{})
	})

	users := make([]User, 0, len(ids))
	draws := make([]LotteryDraw, 0, len(ids)*5+1)
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, shanghaiLocation)
	for i, id := range ids {
		username := fmt.Sprintf("%07d", 1_000_001+i)
		users = append(users, User{
			Id: id, Username: username, Email: fmt.Sprintf("tie-%d@example.com", i),
			AffCode: fmt.Sprintf("lb-tie-%d", i), Status: common.UserStatusEnabled,
		})

		luckyTime := now.Add(time.Duration(i) * time.Minute).Unix()
		luckyStep := i + 1
		if i >= 10 {
			luckyStep = 10
		}
		if i == len(ids)-1 {
			// Keep the current user outside the lucky Top 10 while still
			// exercising the SQL tie-break on the two step-10 rows.
			luckyTime = now.Add(-time.Minute).Unix()
		}
		draws = append(draws, LotteryDraw{
			UserID: id, PrizeLevel: 1, PityStep: luckyStep,
			IsGuaranteed: false, CreatedAt: luckyTime,
		})
		if i == 0 {
			// A worse result for the same user must not create a second seat.
			draws = append(draws, LotteryDraw{
				UserID: id, PrizeLevel: 1, PityStep: 10,
				IsGuaranteed: false, CreatedAt: now.Add(11 * time.Minute).Unix(),
			})
		}
		regularDraws := 4
		if i == 0 {
			regularDraws = 3
		}
		for j := 0; j < regularDraws; j++ {
			draws = append(draws, LotteryDraw{
				UserID: id, PrizeLevel: 2, PityStep: 0,
				IsGuaranteed: false, CreatedAt: now.Add(time.Duration(j) * time.Second).Unix(),
			})
		}
	}
	require.NoError(t, DB.Create(&users).Error)
	require.NoError(t, DB.Create(&draws).Error)

	data, err := GetLotteryLeaderboards(ids[len(ids)-1], now, "all")
	require.NoError(t, err)
	assert.Zero(t, data.CountStart)
	assert.Zero(t, data.CountEnd)

	// Every user has five draws, so COUNT(*) ties must follow user_id ASC.
	require.Len(t, data.DrawCount.Rows, lotteryLeaderboardLimit)
	assert.Equal(t, "100**01", data.DrawCount.Rows[0].DisplayName)
	assert.Equal(t, "100**10", data.DrawCount.Rows[9].DisplayName)
	require.NotNil(t, data.DrawCount.CurrentUser)
	assert.Equal(t, 12, data.DrawCount.CurrentUser.Rank)
	assert.Equal(t, "100**12", data.DrawCount.CurrentUser.DisplayName)

	// The duplicate lucky result for the first user must not consume a rank.
	require.Len(t, data.Lucky.Rows, lotteryLeaderboardLimit)
	require.NotNil(t, data.Lucky.CurrentUser)
	assert.Equal(t, 12, data.Lucky.CurrentUser.Rank)
	assert.Equal(t, 10, data.Lucky.CurrentUser.PityStep)
	assert.Equal(t, "100**12", data.Lucky.CurrentUser.DisplayName)
}

func TestLotteryLeaderboardExplicitPhaseOverridesGuaranteedFlag(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&User{}, &LotteryDraw{}))
	ids := []int{994301, 994302, 994303}
	DB.Unscoped().Where("id IN ?", ids).Delete(&User{})
	DB.Where("user_id IN ?", ids).Delete(&LotteryDraw{})
	t.Cleanup(func() {
		DB.Where("user_id IN ?", ids).Delete(&LotteryDraw{})
		DB.Unscoped().Where("id IN ?", ids).Delete(&User{})
	})
	require.NoError(t, DB.Create(&[]User{
		{Id: ids[0], Username: "base-phase", AffCode: "lb-phase-base", Status: common.UserStatusEnabled},
		{Id: ids[1], Username: "hard-phase", AffCode: "lb-phase-hard", Status: common.UserStatusEnabled},
		{Id: ids[2], Username: "soft-phase", AffCode: "lb-phase-soft", Status: common.UserStatusEnabled},
	}).Error)
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, shanghaiLocation)
	require.NoError(t, DB.Create(&[]LotteryDraw{
		{UserID: ids[0], PrizeLevel: 1, PityStep: 50, IsGuaranteed: true, PityPhase: lotteryPityPhaseBase, CreatedAt: now.Unix()},
		{UserID: ids[1], PrizeLevel: 1, PityStep: 1, IsGuaranteed: false, PityPhase: lotteryPityPhaseHard, CreatedAt: now.Unix()},
		{UserID: ids[2], PrizeLevel: 1, PityStep: 1, IsGuaranteed: false, PityPhase: lotteryPityPhaseSoft, CreatedAt: now.Unix()},
	}).Error)

	data, err := GetLotteryLeaderboards(0, now, "all")
	require.NoError(t, err)
	require.Len(t, data.Lucky.Rows, 1)
	require.Len(t, data.HardPity.Rows, 1)
	assert.Equal(t, "b***", data.Lucky.Rows[0].DisplayName)
	assert.Equal(t, "h***", data.HardPity.Rows[0].DisplayName)
}
