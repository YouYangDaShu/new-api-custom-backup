package controller

import (
	"encoding/json"
	"errors"
	"io"
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

type lotteryRecentDraw struct {
	ID           uint    `json:"id"`
	PrizeID      string  `json:"prize_id"`
	PrizeName    string  `json:"prize_name"`
	PrizeAmount  float64 `json:"prize_amount"`
	CreatedAt    int64   `json:"created_at"`
	PityStep     int     `json:"pity_step,omitempty"`
	IsGuaranteed bool    `json:"is_guaranteed,omitempty"`
}

type lotteryHistoryResponse struct {
	Items      []lotteryRecentDraw `json:"items"`
	Page       int                 `json:"page"`
	PageSize   int                 `json:"page_size"`
	Total      int64               `json:"total"`
	TotalPages int                 `json:"total_pages"`
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
		if !prize.Enabled {
			continue
		}
		amount, conversionErr := model.LotteryQuotaToDisplay(prize.AmountQuota)
		if conversionErr != nil {
			amount = 0
		}
		prizes = append(prizes, gin.H{"id": prize.ID, "name": prize.Name, "amount": amount})
	}
	page := 1
	pageSize := 10
	if value, conversionErr := strconv.Atoi(c.Query("page")); conversionErr == nil && value > 0 {
		page = value
	}
	if value, conversionErr := strconv.Atoi(c.Query("page_size")); conversionErr == nil && (value == 10 || value == 20 || value == 50) {
		pageSize = value
	}
	query := model.DB.Model(&model.LotteryDraw{}).Where("user_id = ?", c.GetInt("id"))
	var total int64
	if err := query.Count(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	var history []lotteryRecentDraw
	if err := query.Select("id, prize_id, prize_name, prize_amount, created_at").Order("id desc").Limit(pageSize).Offset((page - 1) * pageSize).Find(&history).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	var recentDraw *lotteryRecentDraw
	if len(history) > 0 {
		recentDraw = &history[0]
	}
	totalPages := int((total + int64(pageSize) - 1) / int64(pageSize))
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"enabled": cfg.Enabled, "threshold": threshold, "accumulated": accumulated, "available_draws": progress.AvailableDraws, "prizes": prizes, "recent_draw": recentDraw, "history": lotteryHistoryResponse{Items: history, Page: page, PageSize: pageSize, Total: total, TotalPages: totalPages}}})
}

func decodeLotteryDrawCount(reader io.Reader) (int, error) {
	if reader == nil {
		return 1, nil
	}
	decoder := json.NewDecoder(reader)
	token, err := decoder.Token()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return 1, nil
		}
		return 0, errors.New("invalid lottery draw request")
	}
	opening, ok := token.(json.Delim)
	if !ok || opening != '{' {
		return 0, errors.New("invalid lottery draw request")
	}
	count := 1
	seenCount := false
	for decoder.More() {
		keyToken, err := decoder.Token()
		key, ok := keyToken.(string)
		if err != nil || !ok || key != "count" || seenCount {
			return 0, errors.New("invalid lottery draw request")
		}
		seenCount = true
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil || string(value) == "null" {
			if string(value) == "null" {
				return 0, errors.New("lottery draw count must be 1 or 10")
			}
			return 0, errors.New("invalid lottery draw request")
		}
		if err := json.Unmarshal(value, &count); err != nil {
			return 0, errors.New("invalid lottery draw request")
		}
	}
	closing, err := decoder.Token()
	if err != nil {
		return 0, errors.New("invalid lottery draw request")
	}
	if closing != json.Delim('}') {
		return 0, errors.New("invalid lottery draw request")
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return 0, errors.New("invalid lottery draw request")
	}
	return count, nil
}

func DrawLottery(c *gin.Context) {
	count, err := decodeLotteryDrawCount(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	draws, err := model.DrawLotteryCount(c.GetInt("id"), count)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	result := make([]lotteryRecentDraw, 0, len(draws))
	for _, draw := range draws {
		result = append(result, lotteryRecentDraw{ID: draw.ID, PrizeID: draw.PrizeID, PrizeName: draw.PrizeName, PrizeAmount: draw.PrizeAmount, CreatedAt: draw.CreatedAt})
	}
	if count == 1 {
		c.JSON(http.StatusOK, gin.H{"success": true, "data": result[0]})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
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
		PityEnabled            bool                 `json:"pity_enabled"`
		PityTargetPrizeLevel   int                  `json:"pity_target_prize_level"`
		PityBaseRate           float64              `json:"pity_base_rate"`
		PityBaseEnd            int                  `json:"pity_base_end"`
		PityStage1Start        int                  `json:"pity_stage_1_start"`
		PityStage1End          int                  `json:"pity_stage_1_end"`
		PityStage1Rates        []float64            `json:"pity_stage_1_rates"`
		PityStage2Start        int                  `json:"pity_stage_2_start"`
		PityStage2End          int                  `json:"pity_stage_2_end"`
		PityStage2Rates        []float64            `json:"pity_stage_2_rates"`
		PityHardThreshold      int                  `json:"pity_hard_threshold"`
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
	cfg := model.LotteryConfig{Enabled: request.Enabled, Threshold: threshold, Prizes: request.Prizes,
		PityEnabled: request.PityEnabled, PityTargetPrizeLevel: request.PityTargetPrizeLevel,
		PityBaseRate: request.PityBaseRate, PityBaseEnd: request.PityBaseEnd,
		PityStage1Start: request.PityStage1Start, PityStage1End: request.PityStage1End,
		PityStage1Rates: request.PityStage1Rates, PityStage2Start: request.PityStage2Start,
		PityStage2End: request.PityStage2End, PityStage2Rates: request.PityStage2Rates,
		PityHardThreshold: request.PityHardThreshold, SchemaVersion: 3, Unit: "raw_quota"}
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
	if err := model.ValidateLotteryConfigAnchor(cfg); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	if err := model.SaveLotteryConfig(cfg); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	model.RecordLogWithAdminInfo(c.GetInt("id"), model.LogTypeManage, "更新抽奖配置", auditOperatorInfo(c), &model.AuditOperation{Action: "lottery_config_update"}, c)
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
	params := model.AuditFields{"scope": "all"}
	if userID != nil {
		params["scope"] = "user"
		params["user_id"] = *userID
	}
	model.RecordLogWithAdminInfo(c.GetInt("id"), model.LogTypeManage, "重置抽奖累计进度", auditOperatorInfo(c), &model.AuditOperation{Action: "lottery_progress_reset", Params: params}, c)
	c.JSON(http.StatusOK, gin.H{"success": true})
}
