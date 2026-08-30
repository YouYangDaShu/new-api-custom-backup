package controller

import (
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

func GetLottery(c *gin.Context) {
	cfg := model.GetLotteryConfig()
	progress, err := model.GetLotteryProgress(c.GetInt("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	// Expose only user-safe prize fields; weights must stay admin-only.
	prizes := make([]gin.H, 0, len(cfg.Prizes))
	for _, prize := range cfg.Prizes {
		amount, conversionErr := model.LotteryQuotaToDisplay(prize.AmountQuota)
		if conversionErr != nil {
			amount = 0
		}
		prizes = append(prizes, gin.H{"id": prize.ID, "name": prize.Name, "amount": amount})
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"enabled": cfg.Enabled, "threshold": cfg.Threshold, "accumulated": progress.Accumulated, "available_draws": progress.AvailableDraws, "prizes": prizes}})
}

func DrawLottery(c *gin.Context) {
	draw, err := model.DrawLottery(c.GetInt("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": draw})
}

func AdminGetLottery(c *gin.Context) {
	cfg := model.GetLotteryConfig()
	displayCfg, err := model.LotteryConfigForDisplay(cfg)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "lottery currency conversion unavailable"})
		return
	}
	pageInfo := common.GetPageQuery(c)
	var usersTotal int64
	if err := model.DB.Model(&model.UserLotteryProgress{}).Count(&usersTotal).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	var progress []model.UserLotteryProgress
	var draws []model.LotteryDraw
	if err := model.DB.Order("user_id asc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&progress).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	if err := model.DB.Order("id desc").Limit(200).Find(&draws).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{
		"config":      displayCfg,
		"users":       progress,
		"users_total": usersTotal,
		"page":        pageInfo.GetPage(),
		"page_size":   pageInfo.GetPageSize(),
		"draws":       draws,
	}})
}

func AdminUpdateLottery(c *gin.Context) {
	var request struct {
		Enabled                bool                 `json:"enabled"`
		ThresholdDisplayAmount *float64             `json:"threshold_display_amount"`
		Threshold              *int                 `json:"threshold"`
		Prizes                 []model.LotteryPrize `json:"prizes"`
	}
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid lottery config"})
		return
	}
	thresholdDisplay := request.ThresholdDisplayAmount
	if thresholdDisplay == nil && request.Threshold != nil {
		legacy := float64(*request.Threshold)
		thresholdDisplay = &legacy
	}
	if thresholdDisplay == nil || *thresholdDisplay <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "lottery threshold must be positive"})
		return
	}
	threshold, err := model.LotteryDisplayToQuota(*thresholdDisplay)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	cfg := model.LotteryConfig{Enabled: request.Enabled, Threshold: threshold, Prizes: request.Prizes, SchemaVersion: 2, Unit: "raw_quota"}
	for i := range cfg.Prizes {
		quota, conversionErr := model.LotteryDisplayToQuota(cfg.Prizes[i].Amount)
		if cfg.Prizes[i].Amount == 0 {
			quota = 0
		}
		if conversionErr != nil && cfg.Prizes[i].Amount != 0 {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": conversionErr.Error()})
			return
		}
		cfg.Prizes[i].AmountQuota = quota
	}
	if err := model.SaveLotteryConfig(cfg); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	model.RecordLogWithAdminInfo(c.GetInt("id"), model.LogTypeManage, "更新抽奖配置", map[string]interface{}{"action": "lottery_config_update"})
	displayCfg, err := model.LotteryConfigForDisplay(model.GetLotteryConfig())
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "lottery currency conversion unavailable"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": displayCfg})
}

func AdminResetLottery(c *gin.Context) {
	var userID *int
	if raw := c.Param("id"); raw != "" {
		id, err := strconv.Atoi(raw)
		if err != nil || id <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid user id"})
			return
		}
		userID = &id
	}
	if err := model.ResetLotteryProgress(userID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	adminInfo := map[string]interface{}{"action": "lottery_progress_reset", "scope": "all"}
	if userID != nil {
		adminInfo["scope"] = "user"
		adminInfo["user_id"] = *userID
	}
	model.RecordLogWithAdminInfo(c.GetInt("id"), model.LogTypeManage, "重置抽奖累计进度", adminInfo)
	c.JSON(http.StatusOK, gin.H{"success": true})
}
