package controller

import (
	"net/http"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

func GetWeeklyUserRanking(c *gin.Context) {
	if !model.WeeklyRankingRewardsEnabled() {
		common.ApiErrorMsg(c, "weekly user ranking is disabled")
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	period := c.DefaultQuery("period", "week")
	if period != "day" && period != "week" {
		period = "week"
	}
	now := time.Now()
	rows, currentUser, err := model.GetUserRankingWithCurrent(now, period, limit, c.GetInt("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}

	rewardAmounts := model.ConfiguredWeeklyRankingRewardAmounts()
	rewardIndex := 0
	for i := range rows {
		if !rows[i].RewardEligible {
			continue
		}
		if rewardIndex >= len(rewardAmounts) {
			break
		}
		rows[i].RewardAmount = rewardAmounts[rewardIndex]
		rows[i].RewardQuota = common.QuotaFromFloat(rows[i].RewardAmount * common.QuotaPerUnit)
		rewardIndex++
	}
	if currentUser != nil {
		for _, row := range rows {
			if row.IsCurrentUser {
				currentUser.RewardAmount = row.RewardAmount
				currentUser.RewardQuota = row.RewardQuota
				break
			}
		}
	}

	var start, end int64
	if period == "day" {
		start, end = model.ShanghaiDayRange(now)
	} else {
		start, end, _ = model.ShanghaiWeekRange(now, 0)
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{
		"timezone": "Asia/Shanghai", "week_start": start, "week_end": end,
		"rows": rows, "current_user": currentUser, "reward_amounts": rewardAmounts,
	}})
}
