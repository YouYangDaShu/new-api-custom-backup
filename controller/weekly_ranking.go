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
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	period := c.DefaultQuery("period", "week")
	if period != "day" && period != "week" {
		period = "week"
	}
	currentUserID := c.GetInt("id")
	now := time.Now()
	rows, currentUser, err := model.GetUserRankingWithCurrent(now, period, limit, currentUserID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	var start, end int64
	if period == "day" {
		local := now.In(time.FixedZone("Asia/Shanghai", 8*60*60))
		dayStart := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, local.Location())
		start, end = dayStart.Unix(), dayStart.AddDate(0, 0, 1).Unix()
	} else {
		start, end, _ = model.ShanghaiWeekRange(now, 0)
	}
	rewardAmounts := []float64{common.WeeklyRankReward1, common.WeeklyRankReward2, common.WeeklyRankReward3}
	rewardIndex := 0
	for i := range rows {
		if rewardIndex >= len(rewardAmounts) {
			break
		}
		if rows[i].RewardEligible {
			rows[i].RewardAmount = rewardAmounts[rewardIndex]
			rows[i].RewardQuota = common.QuotaFromFloat(rows[i].RewardAmount * common.QuotaPerUnit)
			rewardIndex++
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"timezone":       "Asia/Shanghai",
			"week_start":     start,
			"week_end":       end,
			"rows":           rows,
			"current_user":   currentUser,
			"reward_amounts": rewardAmounts,
		},
	})
}
