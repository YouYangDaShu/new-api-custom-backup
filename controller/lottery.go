package controller

import (
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

type lotteryAdminUserProgress struct {
	UserID         int     `json:"user_id"`
	Username       string  `json:"username"`
	Accumulated    float64 `json:"accumulated"`
	AvailableDraws int     `json:"available_draws"`
	Threshold      float64 `json:"threshold"`
	DrawCount      int64   `json:"draw_count"`
}

type lotteryAdminDraw struct {
	ID          uint    `json:"id"`
	UserID      int     `json:"user_id"`
	Username    string  `json:"username"`
	PrizeName   string  `json:"prize_name"`
	PrizeAmount float64 `json:"prize_amount"`
	CreatedAt   int64   `json:"created_at"`
}

func GetLottery(c *gin.Context) {
	cfg := model.GetLotteryConfig()
	progress, err := model.GetLotteryProgress(c.GetInt("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	threshold, thresholdErr := model.LotteryQuotaToDisplay(cfg.Threshold)
	accumulated, accumulatedErr := model.LotteryQuotaToDisplay(progress.Accumulated)
	if thresholdErr != nil || accumulatedErr != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "lottery currency conversion unavailable"})
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
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"enabled": cfg.Enabled, "threshold": threshold, "accumulated": accumulated, "available_draws": progress.AvailableDraws, "prizes": prizes}})
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
	if c.Query("p") == "" {
		if page, conversionErr := strconv.Atoi(c.Query("page")); conversionErr == nil && page > 0 {
			pageInfo.Page = page
		}
	}
	pageInfo.PageSize = 10
	if pageSize, conversionErr := strconv.Atoi(c.Query("page_size")); conversionErr == nil {
		switch pageSize {
		case 10, 20, 50, 100:
			pageInfo.PageSize = pageSize
		}
	}
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
	userIDs := make([]int, 0, len(progress))
	for _, item := range progress {
		userIDs = append(userIDs, item.UserID)
	}
	var users []model.User
	if len(userIDs) > 0 {
		if err := model.DB.Select("id, username").Where("id IN ?", userIDs).Find(&users).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
			return
		}
	}
	usernames := make(map[int]string, len(users))
	for _, user := range users {
		usernames[user.Id] = user.Username
	}
	drawCounts := make(map[int]int64, len(userIDs))
	if len(userIDs) > 0 {
		type drawCountRow struct {
			UserID int
			Count  int64
		}
		var rows []drawCountRow
		if err := model.DB.Model(&model.LotteryDraw{}).Select("user_id, COUNT(*) AS count").Where("user_id IN ?", userIDs).Group("user_id").Scan(&rows).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
			return
		}
		for _, row := range rows {
			drawCounts[row.UserID] = row.Count
		}
	}
	adminProgress := make([]lotteryAdminUserProgress, 0, len(progress))
	for _, item := range progress {
		amount, conversionErr := model.LotteryQuotaToDisplay(item.Accumulated)
		if conversionErr != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "lottery currency conversion unavailable"})
			return
		}
		adminProgress = append(adminProgress, lotteryAdminUserProgress{
			UserID: item.UserID, Username: usernames[item.UserID], Accumulated: amount,
			AvailableDraws: item.AvailableDraws, Threshold: displayCfg.ThresholdDisplayAmount, DrawCount: drawCounts[item.UserID],
		})
	}
	if err := model.DB.Order("id desc").Limit(200).Find(&draws).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	drawUserIDs := make([]int, 0, len(draws))
	for _, draw := range draws {
		drawUserIDs = append(drawUserIDs, draw.UserID)
	}
	var drawUsers []model.User
	if len(drawUserIDs) > 0 {
		if err := model.DB.Select("id, username").Where("id IN ?", drawUserIDs).Find(&drawUsers).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
			return
		}
	}
	drawUsernames := make(map[int]string, len(drawUsers))
	for _, user := range drawUsers {
		drawUsernames[user.Id] = user.Username
	}
	drawLog := make([]lotteryAdminDraw, 0, len(draws))
	for _, draw := range draws {
		drawLog = append(drawLog, lotteryAdminDraw{ID: draw.ID, UserID: draw.UserID, Username: drawUsernames[draw.UserID], PrizeName: draw.PrizeName, PrizeAmount: draw.PrizeAmount, CreatedAt: draw.CreatedAt})
	}
	type drawStats struct {
		TotalCount  int64
		TotalAmount float64
	}
	var stats drawStats
	if err := model.DB.Model(&model.LotteryDraw{}).Select("COUNT(*) AS total_count, COALESCE(SUM(prize_amount), 0) AS total_amount").Scan(&stats).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{
		"config":       displayCfg,
		"users":        adminProgress,
		"users_total":  usersTotal,
		"page":         pageInfo.GetPage(),
		"page_size":    pageInfo.GetPageSize(),
		"draws":        drawLog,
		"total_draws":  stats.TotalCount,
		"total_amount": stats.TotalAmount,
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
