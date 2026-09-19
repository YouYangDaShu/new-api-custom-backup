package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenericOptionEndpointRejectsLotteryConfig(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(
		http.MethodPut,
		"/api/option/",
		strings.NewReader(`{"key":"LotteryConfig","value":"{}"}`),
	)

	UpdateOption(c)

	var response struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.False(t, response.Success)
	assert.Contains(t, response.Message, "dedicated lottery settings endpoint")
}

func TestAdminGetLotteryAggregatesAllDrawsAndPerUserCounts(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Option{}, &model.UserLotteryProgress{}, &model.LotteryDraw{}))

	originalQuotaPerUnit := common.QuotaPerUnit
	originalDisplayType := operation_setting.GetGeneralSetting().QuotaDisplayType
	originalExchangeRate := operation_setting.USDExchangeRate
	t.Cleanup(func() {
		common.QuotaPerUnit = originalQuotaPerUnit
		operation_setting.GetGeneralSetting().QuotaDisplayType = originalDisplayType
		operation_setting.USDExchangeRate = originalExchangeRate
	})
	common.QuotaPerUnit = 1_000_000
	operation_setting.GetGeneralSetting().QuotaDisplayType = operation_setting.QuotaDisplayTypeCNY
	operation_setting.USDExchangeRate = 1
	require.NoError(t, model.SaveLotteryConfig(model.LotteryConfig{
		Enabled:       true,
		Threshold:     common.QuotaFromFloat(20 * common.QuotaPerUnit),
		Prizes:        []model.LotteryPrize{{ID: "thanks", Name: "Thanks", Weight: 1}},
		SchemaVersion: 2,
		Unit:          "raw_quota",
	}))
	require.NoError(t, db.Create(&[]model.User{
		{Id: 1, Username: "alice", AffCode: "lottery-alice"},
		{Id: 2, Username: "bob", AffCode: "lottery-bob"},
	}).Error)
	require.NoError(t, db.Create(&[]model.UserLotteryProgress{
		{UserID: 1, Accumulated: common.QuotaFromFloat(2 * common.QuotaPerUnit), AvailableDraws: 1},
		{UserID: 2, Accumulated: common.QuotaFromFloat(3 * common.QuotaPerUnit), AvailableDraws: 0},
	}).Error)
	require.NoError(t, db.Create(&[]model.LotteryDraw{
		{UserID: 1, PrizeID: "a", PrizeName: "A", PrizeAmount: 1.5},
		{UserID: 1, PrizeID: "b", PrizeName: "B", PrizeAmount: 2},
		{UserID: 2, PrizeID: "c", PrizeName: "C", PrizeAmount: 3},
	}).Error)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/lottery/admin/?p=1&page_size=10", nil)
	AdminGetLottery(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			Users []struct {
				UserID    int    `json:"user_id"`
				Username  string `json:"username"`
				DrawCount int64  `json:"draw_count"`
			} `json:"users"`
			Draws []struct {
				ID          uint    `json:"id"`
				UserID      int     `json:"user_id"`
				Username    string  `json:"username"`
				PrizeName   string  `json:"prize_name"`
				PrizeAmount float64 `json:"prize_amount"`
				CreatedAt   int64   `json:"created_at"`
			} `json:"draws"`
			UsersTotal  int64   `json:"users_total"`
			TotalDraws  int64   `json:"total_draws"`
			TotalAmount float64 `json:"total_amount"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.True(t, response.Success)
	assert.Len(t, response.Data.Users, 2)
	assert.Equal(t, 1, response.Data.Users[0].UserID)
	assert.Equal(t, "alice", response.Data.Users[0].Username)
	assert.Equal(t, int64(2), response.Data.Users[0].DrawCount)
	assert.Equal(t, int64(2), response.Data.UsersTotal)
	assert.Equal(t, int64(3), response.Data.TotalDraws)
	assert.InDelta(t, 6.5, response.Data.TotalAmount, 0.000001)
	require.Len(t, response.Data.Draws, 3)
	assert.Equal(t, 2, response.Data.Draws[0].UserID)
	assert.Equal(t, "bob", response.Data.Draws[0].Username)
	assert.Equal(t, "C", response.Data.Draws[0].PrizeName)
	assert.InDelta(t, 3, response.Data.Draws[0].PrizeAmount, 0.000001)
	encodedBytes, err := common.Marshal(response.Data.Draws)
	require.NoError(t, err)
	encoded := string(encodedBytes)
	assert.NotContains(t, encoded, "password")
	assert.NotContains(t, encoded, "token")
	assert.NotContains(t, encoded, "key")
}

func TestAdminGetLotteryNormalizesInvalidPageSizeAndSupportsPageAlias(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Option{}, &model.UserLotteryProgress{}, &model.LotteryDraw{}))

	originalQuotaPerUnit := common.QuotaPerUnit
	originalDisplayType := operation_setting.GetGeneralSetting().QuotaDisplayType
	originalExchangeRate := operation_setting.USDExchangeRate
	t.Cleanup(func() {
		common.QuotaPerUnit = originalQuotaPerUnit
		operation_setting.GetGeneralSetting().QuotaDisplayType = originalDisplayType
		operation_setting.USDExchangeRate = originalExchangeRate
	})
	common.QuotaPerUnit = 1_000_000
	operation_setting.GetGeneralSetting().QuotaDisplayType = operation_setting.QuotaDisplayTypeCNY
	operation_setting.USDExchangeRate = 1
	require.NoError(t, model.SaveLotteryConfig(model.LotteryConfig{
		Enabled:       true,
		Threshold:     common.QuotaFromFloat(20 * common.QuotaPerUnit),
		Prizes:        []model.LotteryPrize{{ID: "thanks", Name: "Thanks", Weight: 1}},
		SchemaVersion: 2,
		Unit:          "raw_quota",
	}))

	progress := make([]model.UserLotteryProgress, 11)
	for index := range progress {
		progress[index] = model.UserLotteryProgress{UserID: index + 1}
	}
	require.NoError(t, db.Create(&progress).Error)

	for _, pageSize := range []string{"1", "9", "11", "200", "-1", "invalid"} {
		t.Run(pageSize, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodGet, "/api/lottery/admin/?page=2&page_size="+pageSize, nil)
			AdminGetLottery(c)

			require.Equal(t, http.StatusOK, recorder.Code)
			var response struct {
				Data struct {
					Users    []lotteryAdminUserProgress `json:"users"`
					Page     int                        `json:"page"`
					PageSize int                        `json:"page_size"`
				} `json:"data"`
			}
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			assert.Equal(t, 2, response.Data.Page)
			assert.Equal(t, 10, response.Data.PageSize)
			require.Len(t, response.Data.Users, 1)
			assert.Equal(t, 11, response.Data.Users[0].UserID)
		})
	}
}
