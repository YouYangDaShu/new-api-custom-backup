package controller

import (
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
			RecentDraw map[string]interface{} `json:"recent_draw"`
			History    struct {
				Items    []map[string]interface{} `json:"items"`
				Total    int64                    `json:"total"`
				PageSize int                      `json:"page_size"`
			} `json:"history"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, map[string]interface{}{
		"id":           float64(2),
		"prize_id":     "cash2",
		"prize_name":   "Cash 2",
		"prize_amount": float64(45),
		"created_at":   float64(457),
	}, response.Data.RecentDraw)
	require.NotContains(t, response.Data.RecentDraw, "user_id")
	require.NotContains(t, response.Data.RecentDraw, "weight")
	require.Equal(t, int64(2), response.Data.History.Total)
	require.Equal(t, 10, response.Data.History.PageSize)
	require.Len(t, response.Data.History.Items, 2)
	require.Equal(t, "Cash 2", response.Data.History.Items[0]["prize_name"])
	require.NotContains(t, response.Data.History.Items[0], "user_id")
	require.NotContains(t, response.Data.History.Items[0], "weight")
}
