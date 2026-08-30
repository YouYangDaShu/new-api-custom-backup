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

type tokenRankingAdminConfig struct {
	Enabled *bool     `json:"enabled"`
	Rewards []float64 `json:"rewards"`
}

type tokenRankingHistoryRow struct {
	WeekStart    string `json:"week_start"`
	DisplayName  string `json:"display_name"`
	Rank         int    `json:"rank"`
	TotalTokens  int64  `json:"total_tokens"`
	RewardQuota  int    `json:"reward_quota"`
	CreatedAt    int64  `json:"created_at"`
	RewardStatus string `json:"reward_status"`
}

func adminRankingHistory(rows []model.WeeklyRankingReward) []tokenRankingHistoryRow {
	ids := make([]int, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.UserID)
	}
	var users []model.User
	_ = model.DB.Select("id, username, display_name").Where("id IN ?", ids).Find(&users).Error
	names := make(map[int]string, len(users))
	for _, user := range users {
		name := strings.TrimSpace(user.DisplayName)
		if name == "" {
			name = user.Username
		}
		if name == "" {
			name = "***"
		}
		runes := []rune(name)
		if len(runes) > 1 {
			name = string(runes[0]) + "***"
		}
		names[user.Id] = name
	}
	history := make([]tokenRankingHistoryRow, 0, len(rows))
	for _, row := range rows {
		history = append(history, tokenRankingHistoryRow{WeekStart: row.WeekStart, DisplayName: names[row.UserID], Rank: row.Rank, TotalTokens: row.TotalTokens, RewardQuota: row.RewardQuota, CreatedAt: row.CreatedAt, RewardStatus: "awarded"})
	}
	return history
}

func GetTokenRankingAdmin(c *gin.Context) {
	common.OptionMapRWMutex.RLock()
	enabled := common.TokenRankingEnabled
	common.OptionMapRWMutex.RUnlock()
	rewards := model.ConfiguredWeeklyRankingRewardAmounts()
	var ledgers []model.WeeklyRankingReward
	if err := model.DB.Order("created_at desc").Limit(200).Find(&ledgers).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	history := adminRankingHistory(ledgers)
	common.ApiSuccess(c, gin.H{"enabled": enabled, "rewards": rewards, "history": history})
}

func UpdateTokenRankingAdmin(c *gin.Context) {
	var req tokenRankingAdminConfig
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorMsg(c, "invalid request")
		return
	}
	if len(req.Rewards) > 100 {
		common.ApiErrorMsg(c, "too many reward ranks")
		return
	}
	for _, reward := range req.Rewards {
		if math.IsNaN(reward) || math.IsInf(reward, 0) || reward < 0 || reward*common.QuotaPerUnit > float64(common.MaxQuota) {
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
		raw, _ := common.Marshal(req.Rewards)
		values["TokenRankingRewards"] = string(raw)
	}
	if err := model.UpdateOptionsBulk(values); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

func AdminGetWeeklyRankingHistory(c *gin.Context) {
	week := strings.TrimSpace(c.Query("week_start"))
	q := model.DB.Order("rank asc")
	if week != "" {
		q = q.Where("week_start = ?", week)
	}
	var rows []model.WeeklyRankingReward
	if err := q.Limit(500).Find(&rows).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	history := adminRankingHistory(rows)
	common.ApiSuccess(c, history)
}
