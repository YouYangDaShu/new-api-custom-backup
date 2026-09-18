package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGetLotteryIncludesRecentDraw(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Option{}, &model.UserLotteryProgress{}, &model.LotteryDraw{}))
	oldQPU := common.QuotaPerUnit
	oldDisplay := operation_setting.GetGeneralSetting().QuotaDisplayType
	oldRate := operation_setting.USDExchangeRate
	t.Cleanup(func() {
		common.QuotaPerUnit = oldQPU
		operation_setting.GetGeneralSetting().QuotaDisplayType = oldDisplay
		operation_setting.USDExchangeRate = oldRate
	})
	common.QuotaPerUnit = 1_000_000
	operation_setting.GetGeneralSetting().QuotaDisplayType = operation_setting.QuotaDisplayTypeCNY
	operation_setting.USDExchangeRate = 1
	require.NoError(t, model.SaveLotteryConfig(model.LotteryConfig{Enabled: true, Threshold: 20_000_000, Prizes: []model.LotteryPrize{{ID: "cash", Name: "Cash", Weight: 1}}, SchemaVersion: 2, Unit: "raw_quota"}))
	require.NoError(t, db.Create(&model.UserLotteryProgress{UserID: 77}).Error)
	require.NoError(t, db.Create(&[]model.LotteryDraw{
		{UserID: 77, PrizeID: "cash", PrizeName: "Cash", PrizeAmount: 123, CreatedAt: 456},
		{UserID: 77, PrizeID: "cash2", PrizeName: "Cash 2", PrizeAmount: 45, CreatedAt: 457},
	}).Error)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Set("id", 77)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/lottery/", nil)
	GetLottery(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Data struct {
			RecentDraw map[string]any   `json:"recent_draw"`
			Prizes     []map[string]any `json:"prizes"`
			History    struct {
				Items    []map[string]any `json:"items"`
				Total    int64            `json:"total"`
				PageSize int              `json:"page_size"`
			} `json:"history"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, map[string]any{
		"id":           float64(2),
		"prize_id":     "cash2",
		"prize_name":   "Cash 2",
		"prize_amount": float64(45),
		"created_at":   float64(457),
	}, response.Data.RecentDraw)
	require.NotContains(t, response.Data.RecentDraw, "user_id")
	require.NotContains(t, response.Data.RecentDraw, "weight")
	require.Len(t, response.Data.Prizes, 1)
	require.NotContains(t, response.Data.Prizes[0], "weight")
	require.NotContains(t, response.Data.Prizes[0], "user_id")
	require.Equal(t, int64(2), response.Data.History.Total)
	require.Equal(t, 10, response.Data.History.PageSize)
	require.Len(t, response.Data.History.Items, 2)
	require.Equal(t, "Cash 2", response.Data.History.Items[0]["prize_name"])
	require.NotContains(t, response.Data.History.Items[0], "user_id")
	require.NotContains(t, response.Data.History.Items[0], "weight")
}

func TestDrawLotteryRejectsInvalidJSON(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Set("id", 78)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/lottery/draw", bytes.NewBufferString("{"))

	DrawLottery(c)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	var response struct {
		Success bool `json:"success"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.False(t, response.Success)
}

func TestDrawLotteryRejectsExplicitZeroCount(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Set("id", 78)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/lottery/draw", bytes.NewBufferString(`{"count":0}`))

	DrawLottery(c)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	var response struct {
		Success bool `json:"success"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.False(t, response.Success)
}

func TestDrawLotteryRejectsExplicitNullCount(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Set("id", 78)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/lottery/draw", bytes.NewBufferString(`{"count":null}`))

	DrawLottery(c)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	var response struct {
		Success bool `json:"success"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.False(t, response.Success)
}

func TestDrawLotteryRejectsNonObjectRequestBodies(t *testing.T) {
	for _, body := range []string{`[]`, `1`, `"count"`, `true`} {
		t.Run(body, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Set("id", 78)
			c.Request = httptest.NewRequest(http.MethodPost, "/api/lottery/draw", bytes.NewBufferString(body))

			DrawLottery(c)

			require.Equal(t, http.StatusBadRequest, recorder.Code)
		})
	}
}

func TestDrawLotteryRejectsUnknownDuplicateAndTrailingJSON(t *testing.T) {
	for _, body := range []string{
		`{"unexpected":true}`,
		`{"count":1,"unexpected":true}`,
		`{"count":1,"count":10}`,
		`{"count":10}{"count":1}`,
	} {
		t.Run(body, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Set("id", 78)
			c.Request = httptest.NewRequest(http.MethodPost, "/api/lottery/draw", bytes.NewBufferString(body))

			DrawLottery(c)

			require.Equal(t, http.StatusBadRequest, recorder.Code)
		})
	}
}

func TestDrawLotteryTenDrawResponsePreservesOrderAndHidesPityFields(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Option{}, &model.User{}, &model.UserLotteryProgress{}, &model.LotteryDraw{}))
	oldQPU := common.QuotaPerUnit
	oldDisplay := operation_setting.GetGeneralSetting().QuotaDisplayType
	oldRate := operation_setting.USDExchangeRate
	t.Cleanup(func() {
		common.QuotaPerUnit = oldQPU
		operation_setting.GetGeneralSetting().QuotaDisplayType = oldDisplay
		operation_setting.USDExchangeRate = oldRate
	})
	common.QuotaPerUnit = 1
	operation_setting.GetGeneralSetting().QuotaDisplayType = operation_setting.QuotaDisplayTypeCNY
	operation_setting.USDExchangeRate = 1
	userID := 79
	require.NoError(t, db.Create(&model.User{Id: userID, Username: "ten-draw", AffCode: "lottery-ten"}).Error)
	require.NoError(t, db.Create(&model.UserLotteryProgress{UserID: userID, AvailableDraws: 10}).Error)
	require.NoError(t, model.SaveLotteryConfig(model.LotteryConfig{
		Enabled:   true,
		Threshold: 10,
		Prizes: []model.LotteryPrize{
			{ID: "first", Name: "First", PrizeLevel: 1, Weight: 1, AmountQuota: 2, Enabled: true},
			{ID: "second", Name: "Second", PrizeLevel: 2, Weight: 1, Enabled: true},
		},
		PityEnabled:          true,
		PityTargetPrizeLevel: 1,
		PityBaseRate:         0,
		PityBaseEnd:          1,
		PityStage1Start:      2,
		PityStage1End:        2,
		PityStage1Rates:      []float64{0},
		PityStage2Start:      3,
		PityStage2End:        3,
		PityStage2Rates:      []float64{0},
		PityHardThreshold:    4,
		SchemaVersion:        3,
		Unit:                 "raw_quota",
	}))

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Set("id", userID)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/lottery/draw", bytes.NewBufferString(`{"count":10}`))

	DrawLottery(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Data []map[string]any `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.Len(t, response.Data, 10)
	for index, draw := range response.Data {
		require.Equal(t, float64(index+1), draw["id"])
		require.NotContains(t, draw, "pity_step")
		require.NotContains(t, draw, "is_guaranteed")
		require.NotContains(t, draw, "user_id")
	}
}
