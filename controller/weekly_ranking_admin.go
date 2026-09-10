package controller

import (
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

type weeklyRankingAdminConfig struct {
	Enabled *bool     `json:"enabled"`
	Rewards []float64 `json:"rewards"`
}

type weeklyRankingHistoryRow struct {
	WeekStart    string `json:"week_start"`
	DisplayName  string `json:"display_name"`
	Rank         int    `json:"rank"`
	TotalTokens  int64  `json:"total_tokens"`
	RewardQuota  int    `json:"reward_quota"`
	CreatedAt    int64  `json:"created_at"`
	RewardStatus string `json:"reward_status"`
}

func weeklyRankingAdminHistory(rows []model.WeeklyRankingReward) []weeklyRankingHistoryRow {
	ids := make([]int, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.UserID)
	}
	var users []model.User
	if len(ids) > 0 {
		_ = model.DB.Select("id, username, display_name").Where("id IN ?", ids).Find(&users).Error
	}
	names := make(map[int]string, len(users))
	for _, user := range users {
		name := strings.TrimSpace(user.DisplayName)
		if name == "" {
			name = user.Username
		}
		if name == "" {
			name = "***"
		} else {
			runes := []rune(name)
			if len(runes) > 1 {
				name = string(runes[0]) + "***"
			}
		}
		names[user.Id] = name
	}
	history := make([]weeklyRankingHistoryRow, 0, len(rows))
	for _, row := range rows {
		history = append(history, weeklyRankingHistoryRow{
			WeekStart: row.WeekStart, DisplayName: names[row.UserID], Rank: row.Rank,
			TotalTokens: row.TotalTokens, RewardQuota: row.RewardQuota,
			CreatedAt: row.CreatedAt, RewardStatus: "awarded",
		})
	}
	return history
}

func GetWeeklyRankingAdmin(c *gin.Context) {
	var ledgers []model.WeeklyRankingReward
	if err := model.DB.Order("created_at desc, rank asc").Limit(200).Find(&ledgers).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	var stats struct {
		SettlementBatchCount int64
		TotalQuota           int64
	}
	if err := model.DB.Model(&model.WeeklyRankingReward{}).
		Select("COUNT(DISTINCT week_start) AS settlement_batch_count, COALESCE(SUM(reward_quota), 0) AS total_quota").Scan(&stats).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{
		"enabled": model.WeeklyRankingRewardsEnabled(), "rewards": model.ConfiguredWeeklyRankingRewardAmounts(),
		"history": weeklyRankingAdminHistory(ledgers), "total_reward_count": stats.SettlementBatchCount,
		"total_reward_quota": stats.TotalQuota,
	})
}

func UpdateWeeklyRankingAdmin(c *gin.Context) {
	var req weeklyRankingAdminConfig
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorMsg(c, "invalid request")
		return
	}
	if len(req.Rewards) > 100 || common.QuotaPerUnit <= 0 || math.IsNaN(common.QuotaPerUnit) || math.IsInf(common.QuotaPerUnit, 0) {
		common.ApiErrorMsg(c, "invalid reward amount")
		return
	}
	for _, reward := range req.Rewards {
		quota := reward * common.QuotaPerUnit
		if math.IsNaN(reward) || math.IsInf(reward, 0) || reward < 0 || math.IsNaN(quota) || math.IsInf(quota, 0) || quota > float64(common.MaxQuota) {
			common.ApiErrorMsg(c, "invalid reward amount")
			return
		}
	}
	values := map[string]string{}
	if req.Enabled != nil {
		values["TokenRankingEnabled"] = strconv.FormatBool(*req.Enabled)
		if !*req.Enabled {
			values["TokenRankingDisabledAt"] = strconv.FormatInt(time.Now().Unix(), 10)
		}
	}
	if req.Rewards != nil {
		raw, err := common.Marshal(req.Rewards)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		values["TokenRankingRewards"] = string(raw)
	}
	if err := model.UpdateOptionsBulk(values); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

func GetWeeklyRankingHistory(c *gin.Context) {
	query := model.DB.Order("created_at desc, rank asc")
	if week := strings.TrimSpace(c.Query("week_start")); week != "" {
		query = query.Where("week_start = ?", week)
	}
	var rows []model.WeeklyRankingReward
	if err := query.Limit(500).Find(&rows).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, weeklyRankingAdminHistory(rows))
}
