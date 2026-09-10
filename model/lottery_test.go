package model

import (
	"fmt"
	"math"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setLotteryCurrencyForTest(t *testing.T, qpu float64, display string, rate float64) {
	t.Helper()
	oldQPU, oldType, oldRate := common.QuotaPerUnit, operation_setting.GetGeneralSetting().QuotaDisplayType, operation_setting.USDExchangeRate
	common.QuotaPerUnit = qpu
	operation_setting.GetGeneralSetting().QuotaDisplayType = display
	operation_setting.USDExchangeRate = rate
	t.Cleanup(func() {
		common.QuotaPerUnit = oldQPU
		operation_setting.GetGeneralSetting().QuotaDisplayType = oldType
		operation_setting.USDExchangeRate = oldRate
		lotteryConfigCache.Store(nil)
	})
	lotteryConfigCache.Store(nil)
}

func canonicalLotteryConfig(threshold int, prizes []LotteryPrize) LotteryConfig {
	return LotteryConfig{Enabled: true, Threshold: threshold, Prizes: prizes, SchemaVersion: lotteryConfigSchemaVersion, Unit: lotteryConfigUnitRawQuota}
}

func TestGetLotteryProgressHandlesConcurrentCreateConflict(t *testing.T) {
	originalDB := DB
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "lottery-conflict.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&UserLotteryProgress{}))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(2)
	DB = db
	t.Cleanup(func() {
		DB = originalDB
		_ = sqlDB.Close()
	})

	const userID = 992000
	injected := false
	var injectErr error
	require.NoError(t, db.Callback().Query().After("gorm:query").Register("test:lottery-create-conflict", func(tx *gorm.DB) {
		if injected || tx.Statement.Table != "user_lottery_progresses" || tx.RowsAffected != 0 {
			return
		}
		injected = true
		injectErr = db.Create(&UserLotteryProgress{UserID: userID, Accumulated: 7, AvailableDraws: 3}).Error
	}))

	progress, err := GetLotteryProgress(userID)
	require.NoError(t, injectErr)
	require.NoError(t, err)
	require.True(t, injected)
	assert.Equal(t, 7, progress.Accumulated)
	assert.Equal(t, 3, progress.AvailableDraws)
}

func TestLotteryQuotaUnitBoundary(t *testing.T) {
	setLotteryCurrencyForTest(t, 1_000_000, operation_setting.QuotaDisplayTypeCNY, 1)
	require.NoError(t, DB.AutoMigrate(&Option{}, &UserLotteryProgress{}, &LotteryConsumptionEvent{}, &LotteryDraw{}))
	require.Equal(t, 20_000_000, DefaultLotteryConfig().Threshold)
	require.NoError(t, SaveLotteryConfig(canonicalLotteryConfig(20_000_000, []LotteryPrize{{ID: "x", Name: "X", Weight: 1}})))
	userID := 992001
	DB.Where("user_id = ?", userID).Delete(&UserLotteryProgress{})
	DB.Where("event_key LIKE ?", "lottery-unit-test-%").Delete(&LotteryConsumptionEvent{})
	require.NoError(t, AccumulateLotteryConsumption(userID, 19_999_999, "lottery-unit-test-1"))
	p, err := GetLotteryProgress(userID)
	require.NoError(t, err)
	require.Equal(t, 19_999_999, p.Accumulated)
	require.Equal(t, 0, p.AvailableDraws)
	require.NoError(t, AccumulateLotteryConsumption(userID, 1, "lottery-unit-test-2"))
	p, err = GetLotteryProgress(userID)
	require.NoError(t, err)
	require.Equal(t, 0, p.Accumulated)
	require.Equal(t, 1, p.AvailableDraws)
}

func TestLotteryDisplayConversionsAndInvalidInputs(t *testing.T) {
	setLotteryCurrencyForTest(t, 1_000_000, operation_setting.QuotaDisplayTypeCNY, 1)
	quota, err := LotteryDisplayToQuota(0.1)
	require.NoError(t, err)
	require.Equal(t, 100_000, quota)
	operation_setting.USDExchangeRate = 7.3
	quota, err = LotteryDisplayToQuota(7.3)
	require.NoError(t, err)
	require.Equal(t, 1_000_000, quota)
	_, err = LotteryDisplayToQuota(math.Inf(1))
	require.Error(t, err)
	_, err = LotteryDisplayToQuota(math.MaxFloat64)
	require.Error(t, err)
	operation_setting.GetGeneralSetting().QuotaDisplayType = operation_setting.QuotaDisplayTypeTokens
	_, err = LotteryDisplayToQuota(1)
	require.Error(t, err)
}

func TestLotteryLegacyConfigMigratesOnceWithoutProgressMutation(t *testing.T) {
	setLotteryCurrencyForTest(t, 1_000_000, operation_setting.QuotaDisplayTypeCNY, 1)
	require.NoError(t, DB.AutoMigrate(&Option{}, &UserLotteryProgress{}, &LotteryConsumptionEvent{}, &LotteryDraw{}))
	userID := 992002
	DB.Where("user_id = ?", userID).Delete(&UserLotteryProgress{})
	DB.Where("user_id = ?", userID).Delete(&LotteryDraw{})
	require.NoError(t, DB.Create(&UserLotteryProgress{UserID: userID, Accumulated: 123, AvailableDraws: 4}).Error)
	require.NoError(t, DB.Create(&LotteryDraw{UserID: userID, PrizeID: "old", PrizeName: "Old"}).Error)
	legacy := `{"enabled":true,"threshold":20,"prizes":[{"id":"cash","name":"Cash","weight":1,"amount":0.1}]}`
	require.NoError(t, DB.Save(&Option{Key: lotteryConfigKey, Value: legacy}).Error)
	lotteryConfigCache.Store(nil)
	cfg := GetLotteryConfig()
	require.Equal(t, lotteryConfigSchemaVersion, cfg.SchemaVersion)
	require.Equal(t, lotteryConfigUnitRawQuota, cfg.Unit)
	require.Equal(t, 20_000_000, cfg.Threshold)
	require.Equal(t, 100_000, cfg.Prizes[0].AmountQuota)
	require.Zero(t, cfg.Prizes[0].Amount)
	var option Option
	require.NoError(t, DB.First(&option, "key = ?", lotteryConfigKey).Error)
	var persisted LotteryConfig
	require.NoError(t, common.UnmarshalJsonStr(option.Value, &persisted))
	require.Equal(t, lotteryConfigSchemaVersion, persisted.SchemaVersion)
	require.Equal(t, lotteryConfigUnitRawQuota, persisted.Unit)
	require.Equal(t, cfg.Threshold, persisted.Threshold)
	require.Equal(t, cfg.Prizes[0].AmountQuota, persisted.Prizes[0].AmountQuota)
	p, err := GetLotteryProgress(userID)
	require.NoError(t, err)
	require.Equal(t, 123, p.Accumulated)
	require.Equal(t, 4, p.AvailableDraws)
	var drawCount int64
	require.NoError(t, DB.Model(&LotteryDraw{}).Where("user_id = ?", userID).Count(&drawCount).Error)
	require.Equal(t, int64(1), drawCount)
	operation_setting.USDExchangeRate = 7.3
	lotteryConfigCache.Store(nil)
	cfg = GetLotteryConfig()
	require.Equal(t, 20_000_000, cfg.Threshold)
}

func TestSaveLotteryLegacyConfigConvertsThresholdOnceWithoutProgressMutation(t *testing.T) {
	setLotteryCurrencyForTest(t, 1_000_000, operation_setting.QuotaDisplayTypeCNY, 1)
	require.NoError(t, DB.AutoMigrate(&Option{}, &UserLotteryProgress{}, &LotteryDraw{}))
	userID := 992003
	DB.Where("user_id = ?", userID).Delete(&UserLotteryProgress{})
	DB.Where("user_id = ?", userID).Delete(&LotteryDraw{})
	require.NoError(t, DB.Create(&UserLotteryProgress{UserID: userID, Accumulated: 123, AvailableDraws: 4}).Error)
	require.NoError(t, DB.Create(&LotteryDraw{UserID: userID, PrizeID: "old", PrizeName: "Old"}).Error)

	legacy := LotteryConfig{Enabled: true, Threshold: 20, Prizes: []LotteryPrize{{ID: "cash", Name: "Cash", Weight: 1, Amount: 0.1}}}
	require.NoError(t, SaveLotteryConfig(legacy))
	cfg := GetLotteryConfig()
	require.Equal(t, 20_000_000, cfg.Threshold)
	require.Equal(t, 100_000, cfg.Prizes[0].AmountQuota)
	require.Zero(t, cfg.Prizes[0].Amount)

	var option Option
	require.NoError(t, DB.First(&option, "key = ?", lotteryConfigKey).Error)
	var persisted LotteryConfig
	require.NoError(t, common.UnmarshalJsonStr(option.Value, &persisted))
	require.Equal(t, 20_000_000, persisted.Threshold)
	require.Equal(t, 100_000, persisted.Prizes[0].AmountQuota)

	lotteryConfigCache.Store(nil)
	require.NoError(t, SaveLotteryConfig(GetLotteryConfig()))
	cfg = GetLotteryConfig()
	require.Equal(t, 20_000_000, cfg.Threshold)
	require.Equal(t, 100_000, cfg.Prizes[0].AmountQuota)

	p, err := GetLotteryProgress(userID)
	require.NoError(t, err)
	require.Equal(t, 123, p.Accumulated)
	require.Equal(t, 4, p.AvailableDraws)
	var drawCount int64
	require.NoError(t, DB.Model(&LotteryDraw{}).Where("user_id = ?", userID).Count(&drawCount).Error)
	require.Equal(t, int64(1), drawCount)
}

func TestLotteryAdminDisplayRoundTrip(t *testing.T) {
	setLotteryCurrencyForTest(t, 1_000_000, operation_setting.QuotaDisplayTypeCNY, 7.3)
	raw, err := LotteryDisplayToQuota(20)
	require.NoError(t, err)
	display, err := LotteryConfigForDisplay(canonicalLotteryConfig(raw, []LotteryPrize{{ID: "cash", Name: "Cash", Weight: 1, AmountQuota: 1_000_000}}))
	require.NoError(t, err)
	require.InDelta(t, 20, display.ThresholdDisplayAmount, 1e-6)
	require.InDelta(t, 7.3, display.Prizes[0].Amount, 1e-6)
}

func TestLotteryAccumulationRemainderAndIdempotency(t *testing.T) {
	require.NotNil(t, DB)
	require.NoError(t, DB.AutoMigrate(&Option{}, &UserLotteryProgress{}, &LotteryConsumptionEvent{}, &LotteryDraw{}))
	cfg := canonicalLotteryConfig(20, []LotteryPrize{{ID: "x", Name: "X", Weight: 1}})
	require.NoError(t, SaveLotteryConfig(cfg))
	userID := 991001
	DB.Where("user_id = ?", userID).Delete(&UserLotteryProgress{})
	DB.Where("user_id = ?", userID).Delete(&LotteryConsumptionEvent{})
	require.NoError(t, AccumulateLotteryConsumption(userID, 35, "lottery-test-event"))
	require.NoError(t, AccumulateLotteryConsumption(userID, 35, "lottery-test-event"))
	p, err := GetLotteryProgress(userID)
	require.NoError(t, err)
	require.Equal(t, 15, p.Accumulated)
	require.Equal(t, 1, p.AvailableDraws)
}

func TestLotteryConcurrentAccumulationPreservesEveryEvent(t *testing.T) {
	originalDB := DB
	dsn := filepath.Join(t.TempDir(), "lottery-accumulation.db") + "?_busy_timeout=10000&_journal_mode=WAL"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Option{}, &UserLotteryProgress{}, &LotteryConsumptionEvent{}))
	DB = db
	lotteryConfigCache.Store(nil)
	t.Cleanup(func() {
		DB = originalDB
		lotteryConfigCache.Store(nil)
		sqlDB, sqlErr := db.DB()
		if sqlErr == nil {
			_ = sqlDB.Close()
		}
	})

	require.NoError(t, SaveLotteryConfig(canonicalLotteryConfig(10, []LotteryPrize{{ID: "x", Name: "X", Weight: 1}})))
	const eventCount = 20
	errorsByEvent := make(chan error, eventCount)
	var wg sync.WaitGroup
	for i := range eventCount {
		wg.Go(func() {
			errorsByEvent <- AccumulateLotteryConsumption(42, 3, fmt.Sprintf("concurrent:%d", i))
		})
	}
	wg.Wait()
	close(errorsByEvent)
	for eventErr := range errorsByEvent {
		require.NoError(t, eventErr)
	}

	progress, err := GetLotteryProgress(42)
	require.NoError(t, err)
	assert.Equal(t, 0, progress.Accumulated)
	assert.Equal(t, 6, progress.AvailableDraws)
	var eventTotal int64
	require.NoError(t, db.Model(&LotteryConsumptionEvent{}).Count(&eventTotal).Error)
	assert.EqualValues(t, eventCount, eventTotal)
}

func TestLotteryAccumulationSkipsMissingEventKey(t *testing.T) {
	require.NoError(t, SaveLotteryConfig(canonicalLotteryConfig(20, []LotteryPrize{{ID: "x", Name: "X", Weight: 1}})))
	userID := 991005
	DB.Where("user_id = ?", userID).Delete(&UserLotteryProgress{})
	DB.Where("user_id = ?", userID).Delete(&LotteryConsumptionEvent{})
	require.NoError(t, AccumulateLotteryConsumption(userID, 35, ""))
	p, err := GetLotteryProgress(userID)
	require.NoError(t, err)
	require.Equal(t, 0, p.Accumulated)
	require.Equal(t, 0, p.AvailableDraws)
}

func TestLotteryLogRequiresSuccessfulSettlement(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&Option{}, &UserLotteryProgress{}, &LotteryConsumptionEvent{}, &LotteryDraw{}))
	require.NoError(t, SaveLotteryConfig(canonicalLotteryConfig(20, []LotteryPrize{{ID: "x", Name: "X", Weight: 1}})))
	userID := 991006
	DB.Where("user_id = ?", userID).Delete(&UserLotteryProgress{})
	DB.Where("event_key LIKE ?", "lottery-log-test%").Delete(&LotteryConsumptionEvent{})

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	c.Set(common.RequestIdKey, "lottery-log-test-1")
	other := NewLogOther()
	other.SetPublic("billing_source", "wallet")
	other.SetPublic("status_code", 200)
	RecordConsumeLog(c, userID, RecordConsumeLogParams{
		Quota: 25,
		Other: other,
	})
	p, err := GetLotteryProgress(userID)
	require.NoError(t, err)
	require.Equal(t, 0, p.Accumulated)

	c.Set("billing_settled", true)
	RecordConsumeLog(c, userID, RecordConsumeLogParams{
		Quota: 25,
		Other: other,
	})
	p, err = GetLotteryProgress(userID)
	require.NoError(t, err)
	require.Equal(t, 5, p.Accumulated)
}

func TestLotteryConcurrentDrawSpendsOnce(t *testing.T) {
	userID := 991002
	DB.Where("user_id = ?", userID).Delete(&UserLotteryProgress{})
	DB.Where("user_id = ?", userID).Delete(&LotteryDraw{})
	require.NoError(t, DB.Create(&UserLotteryProgress{UserID: userID, AvailableDraws: 1}).Error)
	require.NoError(t, SaveLotteryConfig(canonicalLotteryConfig(20, []LotteryPrize{{ID: "x", Name: "X", Weight: 1}})))
	var wg sync.WaitGroup
	var successes int
	var mu sync.Mutex
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := DrawLottery(userID); err == nil {
				mu.Lock()
				successes++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	require.Equal(t, 1, successes)
	var p UserLotteryProgress
	require.NoError(t, DB.Where("user_id = ?", userID).First(&p).Error)
	require.Equal(t, 0, p.AvailableDraws)
}

func TestLotteryWeightSelectionDoesNotIndexByWeight(t *testing.T) {
	userID := 991004
	DB.Where("user_id = ?", userID).Delete(&UserLotteryProgress{})
	require.NoError(t, DB.Create(&UserLotteryProgress{UserID: userID, AvailableDraws: 1}).Error)
	require.NoError(t, SaveLotteryConfig(canonicalLotteryConfig(20, []LotteryPrize{{ID: "heavy", Name: "Heavy", Weight: 100}, {ID: "light", Name: "Light", Weight: 1}})))
	_, err := DrawLottery(userID)
	require.NoError(t, err)
}

func TestLotteryPrizeAmountIsBackwardCompatibleAndReturnedOnDraw(t *testing.T) {
	legacy := `{"enabled":true,"threshold":20,"prizes":[{"id":"legacy","name":"Legacy","weight":1}]}`
	var cfg LotteryConfig
	require.NoError(t, common.UnmarshalJsonStr(legacy, &cfg))
	require.NoError(t, ValidateLotteryConfig(cfg))
	require.Zero(t, cfg.Prizes[0].Amount)

	userID := 991007
	DB.Where("user_id = ?", userID).Delete(&UserLotteryProgress{})
	DB.Where("user_id = ?", userID).Delete(&LotteryDraw{})
	DB.Unscoped().Where("id = ?", userID).Delete(&User{})
	require.NoError(t, DB.Create(&User{Id: userID, Username: "lottery-legacy", AffCode: "lot-legacy", Status: common.UserStatusEnabled, Quota: 0}).Error)
	require.NoError(t, DB.Create(&UserLotteryProgress{UserID: userID, AvailableDraws: 1}).Error)
	require.NoError(t, SaveLotteryConfig(LotteryConfig{Enabled: true, Threshold: 20, Prizes: []LotteryPrize{{ID: "amount", Name: "Amount", Weight: 1, Amount: 12.5}}}))
	draw, err := DrawLottery(userID)
	require.NoError(t, err)
	require.Equal(t, 12.5, draw.PrizeAmount)
	var user User
	require.NoError(t, DB.First(&user, userID).Error)
	require.Equal(t, common.QuotaFromFloat(12.5*common.QuotaPerUnit), user.Quota)
}

func TestLotteryDrawCreditsPrizeExactlyOnce(t *testing.T) {
	userID := 991008
	DB.Where("user_id = ?", userID).Delete(&UserLotteryProgress{})
	DB.Where("user_id = ?", userID).Delete(&LotteryDraw{})
	DB.Unscoped().Where("id = ?", userID).Delete(&User{})
	LOG_DB.Where("user_id = ?", userID).Delete(&Log{})
	require.NoError(t, DB.Create(&User{Id: userID, Username: "lottery-winner", AffCode: "lot-win", Status: common.UserStatusEnabled, Quota: 1000}).Error)
	require.NoError(t, DB.Create(&UserLotteryProgress{UserID: userID, AvailableDraws: 1}).Error)
	require.NoError(t, SaveLotteryConfig(LotteryConfig{Enabled: true, Threshold: 20, Prizes: []LotteryPrize{{ID: "cash", Name: "Cash", Weight: 1, Amount: 2}}}))

	draw, err := DrawLottery(userID)
	require.NoError(t, err)
	require.Equal(t, 2.0, draw.PrizeAmount)

	expectedCredit := common.QuotaFromFloat(2 * common.QuotaPerUnit)
	var user User
	require.NoError(t, DB.First(&user, userID).Error)
	require.Equal(t, 1000+expectedCredit, user.Quota)

	// The credit is auditable through a system log entry.
	var logs []Log
	require.NoError(t, LOG_DB.Where("user_id = ? AND type = ?", userID, LogTypeSystem).Find(&logs).Error)
	require.Len(t, logs, 1)
	require.Contains(t, logs[0].Content, "抽奖奖励")

	// A retry with no draws left must fail and never credit again.
	_, err = DrawLottery(userID)
	require.Error(t, err)
	require.NoError(t, DB.First(&user, userID).Error)
	require.Equal(t, 1000+expectedCredit, user.Quota)
}

func TestLotteryPrizeQuotaLimitRollsBackDraw(t *testing.T) {
	setLotteryCurrencyForTest(t, 1, operation_setting.QuotaDisplayTypeCNY, 1)
	require.NoError(t, DB.AutoMigrate(&Option{}, &UserLotteryProgress{}, &LotteryDraw{}))
	require.NoError(t, SaveLotteryConfig(canonicalLotteryConfig(20, []LotteryPrize{{ID: "cash", Name: "Cash", Weight: 1, AmountQuota: 10}})))

	for _, tc := range []struct {
		name      string
		userID    int
		quota     int
		wantError bool
	}{
		{name: "reject overflow", userID: 991013, quota: common.MaxQuota - 5, wantError: true},
		{name: "allow exact maximum", userID: 991014, quota: common.MaxQuota - 10},
		{name: "allow existing balance over safety bound", userID: 991015, quota: common.MaxQuota + 100},
		{name: "reject underlying integer overflow", userID: 991016, quota: int(^uint(0)>>1) - 5, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			DB.Where("user_id = ?", tc.userID).Delete(&UserLotteryProgress{})
			DB.Where("user_id = ?", tc.userID).Delete(&LotteryDraw{})
			DB.Unscoped().Where("id = ?", tc.userID).Delete(&User{})
			require.NoError(t, DB.Create(&User{Id: tc.userID, Username: tc.name, AffCode: tc.name, Status: common.UserStatusEnabled, Quota: tc.quota}).Error)
			require.NoError(t, DB.Create(&UserLotteryProgress{UserID: tc.userID, AvailableDraws: 1}).Error)

			_, err := DrawLottery(tc.userID)
			if tc.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			var user User
			require.NoError(t, DB.First(&user, tc.userID).Error)
			var progress UserLotteryProgress
			require.NoError(t, DB.Where("user_id = ?", tc.userID).First(&progress).Error)
			var draws int64
			require.NoError(t, DB.Model(&LotteryDraw{}).Where("user_id = ?", tc.userID).Count(&draws).Error)
			if tc.wantError {
				assert.Equal(t, tc.quota, user.Quota)
				assert.Equal(t, 1, progress.AvailableDraws)
				assert.Zero(t, draws)
			} else {
				assert.Equal(t, tc.quota+10, user.Quota)
				assert.Zero(t, progress.AvailableDraws)
				assert.EqualValues(t, 1, draws)
			}
		})
	}
}

func TestLotteryZeroAmountPrizeDoesNotCredit(t *testing.T) {
	userID := 991009
	DB.Where("user_id = ?", userID).Delete(&UserLotteryProgress{})
	DB.Where("user_id = ?", userID).Delete(&LotteryDraw{})
	DB.Unscoped().Where("id = ?", userID).Delete(&User{})
	LOG_DB.Where("user_id = ?", userID).Delete(&Log{})
	require.NoError(t, DB.Create(&User{Id: userID, Username: "lottery-thanks", AffCode: "lot-thx", Status: common.UserStatusEnabled, Quota: 777}).Error)
	require.NoError(t, DB.Create(&UserLotteryProgress{UserID: userID, AvailableDraws: 1}).Error)
	require.NoError(t, SaveLotteryConfig(canonicalLotteryConfig(20, []LotteryPrize{{ID: "thanks", Name: "Thanks", Weight: 1, Amount: 0}})))

	_, err := DrawLottery(userID)
	require.NoError(t, err)
	var user User
	require.NoError(t, DB.First(&user, userID).Error)
	require.Equal(t, 777, user.Quota)
	var count int64
	require.NoError(t, LOG_DB.Model(&Log{}).Where("user_id = ? AND type = ?", userID, LogTypeSystem).Count(&count).Error)
	require.Zero(t, count)
}

func TestLotteryConcurrentDrawCreditsPrizeOnce(t *testing.T) {
	userID := 991010
	DB.Where("user_id = ?", userID).Delete(&UserLotteryProgress{})
	DB.Where("user_id = ?", userID).Delete(&LotteryDraw{})
	DB.Unscoped().Where("id = ?", userID).Delete(&User{})
	require.NoError(t, DB.Create(&User{Id: userID, Username: "lottery-race", AffCode: "lot-race", Status: common.UserStatusEnabled, Quota: 0}).Error)
	require.NoError(t, DB.Create(&UserLotteryProgress{UserID: userID, AvailableDraws: 1}).Error)
	require.NoError(t, SaveLotteryConfig(LotteryConfig{Enabled: true, Threshold: 20, Prizes: []LotteryPrize{{ID: "cash", Name: "Cash", Weight: 1, Amount: 1}}}))

	var wg sync.WaitGroup
	var successes int
	var mu sync.Mutex
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := DrawLottery(userID); err == nil {
				mu.Lock()
				successes++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	require.Equal(t, 1, successes)
	var user User
	require.NoError(t, DB.First(&user, userID).Error)
	require.Equal(t, common.QuotaFromFloat(common.QuotaPerUnit), user.Quota)
}

func TestLotteryDrawResponseContractIsSnakeCase(t *testing.T) {
	draw := LotteryDraw{ID: 7, UserID: 991011, PrizeID: "cash", PrizeName: "Cash", PrizeAmount: 1.5, CreatedAt: 123}
	b, err := common.Marshal(draw)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, common.Unmarshal(b, &m))
	for _, key := range []string{"id", "user_id", "prize_id", "prize_name", "prize_amount", "created_at"} {
		require.Contains(t, m, key)
	}
	for _, key := range []string{"ID", "UserID", "PrizeID", "PrizeName", "PrizeAmount", "CreatedAt", "weight"} {
		require.NotContains(t, m, key)
	}

	progressJSON, err := common.Marshal(UserLotteryProgress{ID: 3, UserID: 991011})
	require.NoError(t, err)
	var progressMap map[string]any
	require.NoError(t, common.Unmarshal(progressJSON, &progressMap))
	require.Contains(t, progressMap, "id")
	require.NotContains(t, progressMap, "ID")
}

func TestLotteryDisabledAccruesExactlyOnceAndPreservesRemainder(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&Option{}, &UserLotteryProgress{}, &LotteryConsumptionEvent{}, &LotteryDraw{}))
	userID := 991012
	DB.Where("user_id = ?", userID).Delete(&UserLotteryProgress{})
	DB.Where("user_id = ?", userID).Delete(&LotteryDraw{})
	DB.Where("event_key LIKE ?", "lottery-disabled-test%").Delete(&LotteryConsumptionEvent{})
	require.NoError(t, DB.Create(&UserLotteryProgress{UserID: userID, Accumulated: 5}).Error)

	require.NoError(t, SaveLotteryConfig(LotteryConfig{Enabled: false, Threshold: 20, Prizes: []LotteryPrize{{ID: "x", Name: "X", Weight: 1}}, SchemaVersion: lotteryConfigSchemaVersion, Unit: lotteryConfigUnitRawQuota}))
	require.NoError(t, AccumulateLotteryConsumption(userID, 46, "lottery-disabled-test-1"))
	require.NoError(t, AccumulateLotteryConsumption(userID, 46, "lottery-disabled-test-1"))
	var events int64
	require.NoError(t, DB.Model(&LotteryConsumptionEvent{}).Where("event_key = ?", "lottery-disabled-test-1").Count(&events).Error)
	assert.Equal(t, int64(1), events)
	p, err := GetLotteryProgress(userID)
	require.NoError(t, err)
	assert.Equal(t, 11, p.Accumulated)
	assert.Equal(t, 2, p.AvailableDraws)

	_, err = DrawLottery(userID)
	assert.ErrorContains(t, err, "lottery is disabled")

	require.NoError(t, SaveLotteryConfig(canonicalLotteryConfig(20, []LotteryPrize{{ID: "x", Name: "X", Weight: 1}})))
	_, err = DrawLottery(userID)
	require.NoError(t, err)
	p, err = GetLotteryProgress(userID)
	require.NoError(t, err)
	assert.Equal(t, 11, p.Accumulated)
	assert.Equal(t, 1, p.AvailableDraws)
}

func TestLotteryConfigCacheRefreshesOnSaveAndOptionSync(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&Option{}))
	common.OptionMapRWMutex.Lock()
	if common.OptionMap == nil {
		common.OptionMap = make(map[string]string)
	}
	common.OptionMapRWMutex.Unlock()

	require.NoError(t, SaveLotteryConfig(canonicalLotteryConfig(20, []LotteryPrize{{ID: "x", Name: "X", Weight: 1}})))
	require.True(t, GetLotteryConfig().Enabled)

	// A change arriving through the option sync loop (another instance saved)
	// must refresh the cache without a per-request DB read.
	disabled, err := common.Marshal(LotteryConfig{Enabled: false, Threshold: 30, Prizes: []LotteryPrize{{ID: "x", Name: "X", Weight: 1}}})
	require.NoError(t, err)
	require.NoError(t, updateOptionMap(lotteryConfigKey, string(disabled)))
	cfg := GetLotteryConfig()
	require.False(t, cfg.Enabled)
	require.Equal(t, 15_000_000, cfg.Threshold)

	// An invalid synced value falls back to the safe disabled default.
	require.NoError(t, updateOptionMap(lotteryConfigKey, "{not json"))
	cfg = GetLotteryConfig()
	require.False(t, cfg.Enabled)
	require.Equal(t, DefaultLotteryConfig().Threshold, cfg.Threshold)
}

func TestLotteryResetPreservesDraws(t *testing.T) {
	originalDB := DB
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "lottery-reset.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&UserLotteryProgress{}))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	DB = db
	t.Cleanup(func() {
		DB = originalDB
		_ = sqlDB.Close()
	})

	userID := 991003
	DB.Where("user_id = ?", userID).Delete(&UserLotteryProgress{})
	require.NoError(t, DB.Create(&UserLotteryProgress{UserID: userID, Accumulated: 9, AvailableDraws: 3}).Error)
	require.NoError(t, ResetLotteryProgress(&userID))
	p, err := GetLotteryProgress(userID)
	require.NoError(t, err)
	require.Equal(t, 0, p.Accumulated)
	require.Equal(t, 3, p.AvailableDraws)
}
