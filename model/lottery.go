package model

import (
	"errors"
	"fmt"
	"math"
	"math/rand"
	"strings"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const lotteryConfigKey = "LotteryConfig"
const lotteryConfigSchemaVersion = 2
const lotteryConfigUnitRawQuota = "raw_quota"

type LotteryPrize struct {
	ID     string  `json:"id"`
	Name   string  `json:"name"`
	Weight int     `json:"weight"`
	Amount float64 `json:"amount,omitempty"`
	// AmountQuota is the canonical persisted wallet credit. Amount remains the
	// current display-currency value at API boundaries.
	AmountQuota int `json:"amount_quota"`
}

type LotteryConfig struct {
	Enabled       bool           `json:"enabled"`
	Threshold     int            `json:"threshold"`
	Prizes        []LotteryPrize `json:"prizes"`
	SchemaVersion int            `json:"schema_version,omitempty"`
	Unit          string         `json:"unit,omitempty"`
}

// LotteryDisplayConfig is the admin-facing representation. Stored config
// remains raw quota; this type keeps display amounts out of persistence.
type LotteryDisplayConfig struct {
	Enabled                bool                  `json:"enabled"`
	ThresholdDisplayAmount float64               `json:"threshold_display_amount"`
	Prizes                 []LotteryDisplayPrize `json:"prizes"`
}

type LotteryDisplayPrize struct {
	ID     string  `json:"id"`
	Name   string  `json:"name"`
	Weight int     `json:"weight"`
	Amount float64 `json:"amount"`
}

type UserLotteryProgress struct {
	ID             uint  `gorm:"primaryKey" json:"id"`
	UserID         int   `gorm:"uniqueIndex;not null" json:"user_id"`
	Accumulated    int   `gorm:"not null;default:0" json:"accumulated"`
	AvailableDraws int   `gorm:"not null;default:0" json:"available_draws"`
	UpdatedAt      int64 `gorm:"autoUpdateTime" json:"updated_at"`
}

type LotteryConsumptionEvent struct {
	ID        uint   `gorm:"primaryKey"`
	UserID    int    `gorm:"index;not null"`
	EventKey  string `gorm:"uniqueIndex;not null"`
	Quota     int    `gorm:"not null"`
	CreatedAt int64  `gorm:"autoCreateTime"`
}

type LotteryDraw struct {
	ID          uint    `gorm:"primaryKey" json:"id"`
	UserID      int     `gorm:"index;not null" json:"user_id"`
	PrizeID     string  `gorm:"not null" json:"prize_id"`
	PrizeName   string  `gorm:"not null" json:"prize_name"`
	PrizeAmount float64 `gorm:"not null;default:0" json:"prize_amount"`
	CreatedAt   int64   `gorm:"autoCreateTime" json:"created_at"`
}

// Lottery events are retained long enough to cover all request and task
// settlement retry windows while bounding idempotency-ledger growth.
const lotteryEventRetentionDays = 30

// CleanupLotteryConsumptionEvents deletes expired idempotency events. It is
// safe for multiple instances to run concurrently.
func CleanupLotteryConsumptionEvents(now time.Time) (int64, error) {
	cutoff := now.AddDate(0, 0, -lotteryEventRetentionDays).Unix()
	result := DB.Where("created_at < ?", cutoff).Delete(&LotteryConsumptionEvent{})
	return result.RowsAffected, result.Error
}

func DefaultLotteryConfig() LotteryConfig {
	threshold, ok := lotteryDisplayToQuota(20)
	if !ok {
		threshold = 0
	}
	return LotteryConfig{Enabled: false, Threshold: threshold, Prizes: []LotteryPrize{{ID: "thanks", Name: "谢谢参与", Weight: 1}}, SchemaVersion: lotteryConfigSchemaVersion, Unit: lotteryConfigUnitRawQuota}
}

func lotteryDisplayRate() (float64, bool) {
	if common.QuotaPerUnit <= 0 || math.IsNaN(common.QuotaPerUnit) || math.IsInf(common.QuotaPerUnit, 0) {
		return 0, false
	}
	setting := operation_setting.GetGeneralSetting()
	if setting == nil {
		return 0, false
	}
	if setting.QuotaDisplayType == operation_setting.QuotaDisplayTypeTokens {
		return 0, false
	}
	if setting.QuotaDisplayType != operation_setting.QuotaDisplayTypeUSD && setting.QuotaDisplayType != operation_setting.QuotaDisplayTypeCNY && setting.QuotaDisplayType != operation_setting.QuotaDisplayTypeCustom {
		return 0, false
	}
	if setting.QuotaDisplayType == operation_setting.QuotaDisplayTypeCustom && (setting.CustomCurrencyExchangeRate <= 0 || math.IsNaN(setting.CustomCurrencyExchangeRate) || math.IsInf(setting.CustomCurrencyExchangeRate, 0)) {
		return 0, false
	}
	rate := operation_setting.GetUsdToCurrencyRate(operation_setting.USDExchangeRate)
	if rate <= 0 || math.IsNaN(rate) || math.IsInf(rate, 0) || operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens {
		return 0, false
	}
	return rate, true
}

func lotteryDisplayToQuota(amount float64) (int, bool) {
	rate, ok := lotteryDisplayRate()
	if !ok || math.IsNaN(amount) || math.IsInf(amount, 0) || amount < 0 {
		return 0, false
	}
	product := amount / rate * common.QuotaPerUnit
	if math.IsNaN(product) || math.IsInf(product, 0) {
		return 0, false
	}
	quota, clamp := common.QuotaRoundChecked(product)
	return quota, clamp == nil
}

func lotteryQuotaToDisplay(quota int) (float64, bool) {
	rate, ok := lotteryDisplayRate()
	if !ok {
		return 0, false
	}
	if quota < 0 {
		return 0, false
	}
	value := float64(quota) / common.QuotaPerUnit * rate
	return value, !math.IsNaN(value) && !math.IsInf(value, 0)
}

func roundedLotteryDisplay(amount float64) float64 {
	return math.Round(amount*1_000_000) / 1_000_000
}

// LotteryDisplayToQuota converts admin display amounts into canonical raw
// quota. It is intentionally exported for controller validation and tests.
func LotteryDisplayToQuota(amount float64) (int, error) {
	quota, ok := lotteryDisplayToQuota(amount)
	if !ok || amount <= 0 {
		return 0, errors.New("lottery display amount is invalid")
	}
	return quota, nil
}

func LotteryQuotaToDisplay(quota int) (float64, error) {
	amount, ok := lotteryQuotaToDisplay(quota)
	if !ok {
		return 0, errors.New("lottery display conversion is unavailable")
	}
	return amount, nil
}

func LotteryConfigForDisplay(cfg LotteryConfig) (LotteryDisplayConfig, error) {
	threshold, err := LotteryQuotaToDisplay(cfg.Threshold)
	if err != nil {
		return LotteryDisplayConfig{}, err
	}
	display := LotteryDisplayConfig{Enabled: cfg.Enabled, ThresholdDisplayAmount: threshold, Prizes: make([]LotteryDisplayPrize, len(cfg.Prizes))}
	display.ThresholdDisplayAmount = roundedLotteryDisplay(display.ThresholdDisplayAmount)
	for i, prize := range cfg.Prizes {
		display.Prizes[i] = LotteryDisplayPrize{ID: prize.ID, Name: prize.Name, Weight: prize.Weight}
		if prize.AmountQuota > 0 {
			display.Prizes[i].Amount, err = LotteryQuotaToDisplay(prize.AmountQuota)
			if err != nil {
				return LotteryDisplayConfig{}, err
			}
			display.Prizes[i].Amount = roundedLotteryDisplay(display.Prizes[i].Amount)
		}
	}
	return display, nil
}

func ValidateLotteryConfig(cfg LotteryConfig) error {
	if cfg.Threshold <= 0 || cfg.Threshold > common.MaxQuota {
		return errors.New("lottery threshold must be positive")
	}
	if len(cfg.Prizes) == 0 || len(cfg.Prizes) > 100 {
		return errors.New("lottery prizes must contain 1-100 entries")
	}
	total := 0
	seen := make(map[string]struct{}, len(cfg.Prizes))
	for _, prize := range cfg.Prizes {
		if strings.TrimSpace(prize.ID) == "" || strings.TrimSpace(prize.Name) == "" || prize.Weight <= 0 || math.IsNaN(prize.Amount) || math.IsInf(prize.Amount, 0) || prize.Amount < 0 || prize.AmountQuota < 0 || prize.AmountQuota > common.MaxQuota {
			return errors.New("lottery prize id, name and weight are required")
		}
		if _, ok := seen[prize.ID]; ok {
			return errors.New("lottery prize ids must be unique")
		}
		seen[prize.ID] = struct{}{}
		if prize.Weight > 1000000-total {
			return errors.New("lottery prize weights are too large")
		}
		total += prize.Weight
	}
	return nil
}

// lotteryConfigCache keeps the parsed config off the per-request accrual path.
// It is refreshed by SaveLotteryConfig on the writing instance and by the
// option sync loop (updateOptionMap dispatches lotteryConfigKey on startup and
// every SYNC_FREQUENCY seconds), the same invalidation the other cached
// settings use. The safe default is disabled, so a briefly stale cache can
// delay draws but never unexpectedly enable payouts.
var lotteryConfigCache atomic.Pointer[LotteryConfig]

func refreshLotteryConfigCache(value string) {
	cfg, migrated, err := decodeLotteryConfig(value)
	if err != nil || ValidateLotteryConfig(cfg) != nil {
		common.SysError("stored lottery config is invalid, falling back to the disabled default")
		cfg = DefaultLotteryConfig()
	}
	lotteryConfigCache.Store(&cfg)
	if migrated && DB != nil {
		if b, marshalErr := common.Marshal(cfg); marshalErr == nil {
			_ = DB.Save(&Option{Key: lotteryConfigKey, Value: string(b)}).Error
		}
	}
}

func decodeLotteryConfig(value string) (LotteryConfig, bool, error) {
	var cfg LotteryConfig
	if err := common.UnmarshalJsonStr(value, &cfg); err != nil {
		return cfg, false, err
	}
	if cfg.SchemaVersion == lotteryConfigSchemaVersion && cfg.Unit == lotteryConfigUnitRawQuota {
		return cfg, false, nil
	}
	// Legacy config values are display-currency amounts. Convert exactly once
	// and mark the result so future loads cannot multiply it again.
	threshold, ok := lotteryDisplayToQuota(float64(cfg.Threshold))
	if !ok || threshold <= 0 {
		return cfg, false, errors.New("lottery legacy threshold conversion is unavailable")
	}
	cfg.Threshold = threshold
	for i := range cfg.Prizes {
		quota, valid := lotteryDisplayToQuota(cfg.Prizes[i].Amount)
		if !valid {
			return cfg, false, errors.New("lottery legacy prize conversion is unavailable")
		}
		cfg.Prizes[i].AmountQuota = quota
		cfg.Prizes[i].Amount = 0
	}
	cfg.SchemaVersion = lotteryConfigSchemaVersion
	cfg.Unit = lotteryConfigUnitRawQuota
	return cfg, true, nil
}

func GetLotteryConfig() LotteryConfig {
	if cached := lotteryConfigCache.Load(); cached != nil {
		return *cached
	}
	if DB == nil {
		return DefaultLotteryConfig()
	}
	var option Option
	if err := DB.First(&option, "key = ?", lotteryConfigKey).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// No config stored yet: cache the disabled default until a save or
			// the option sync loop provides one.
			cfg := DefaultLotteryConfig()
			lotteryConfigCache.Store(&cfg)
			return cfg
		}
		// Transient read failure: stay uncached so the next call retries.
		return DefaultLotteryConfig()
	}
	refreshLotteryConfigCache(option.Value)
	return *lotteryConfigCache.Load()
}

func SaveLotteryConfig(cfg LotteryConfig) error {
	if cfg.SchemaVersion != lotteryConfigSchemaVersion || cfg.Unit != lotteryConfigUnitRawQuota {
		threshold, ok := lotteryDisplayToQuota(float64(cfg.Threshold))
		if !ok || threshold <= 0 {
			return errors.New("lottery legacy threshold conversion is unavailable")
		}
		cfg.Threshold = threshold
		for i := range cfg.Prizes {
			if cfg.Prizes[i].AmountQuota != 0 {
				continue
			}
			if cfg.Prizes[i].Amount > 0 {
				quota, conversionErr := LotteryDisplayToQuota(cfg.Prizes[i].Amount)
				if conversionErr != nil {
					return conversionErr
				}
				cfg.Prizes[i].AmountQuota = quota
			}
		}
		cfg.SchemaVersion = lotteryConfigSchemaVersion
		cfg.Unit = lotteryConfigUnitRawQuota
	}
	// Display amounts are never persisted in canonical config. They are derived
	// from AmountQuota at the API boundary using the current site settings.
	for i := range cfg.Prizes {
		cfg.Prizes[i].Amount = 0
	}
	if err := ValidateLotteryConfig(cfg); err != nil {
		return err
	}
	b, err := common.Marshal(cfg)
	if err != nil {
		return err
	}
	if err := DB.Save(&Option{Key: lotteryConfigKey, Value: string(b)}).Error; err != nil {
		return err
	}
	refreshLotteryConfigCache(string(b))
	return nil
}

func GetLotteryProgress(userID int) (*UserLotteryProgress, error) {
	var progress UserLotteryProgress
	err := DB.Where("user_id = ?", userID).First(&progress).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// Another request may create the row after the read above. Ignore that
		// unique-key race, then return the canonical row created by either caller.
		if err = DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&UserLotteryProgress{UserID: userID}).Error; err == nil {
			err = DB.Where("user_id = ?", userID).First(&progress).Error
		}
	}
	return &progress, err
}

// AccumulateLotteryConsumption records a finalized wallet charge exactly once.
func AccumulateLotteryConsumption(userID int, quota int, eventKey string) error {
	if userID <= 0 || quota <= 0 {
		return nil
	}
	eventKey = strings.TrimSpace(eventKey)
	if eventKey == "" {
		// Without a stable request/task identifier there is no safe way to make
		// retries idempotent. Do not turn an unknown event into a new charge.
		return nil
	}
	cfg := GetLotteryConfig()
	if cfg.Threshold <= 0 {
		return nil
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		event := &LotteryConsumptionEvent{UserID: userID, EventKey: eventKey, Quota: quota}
		result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(event)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return nil
		}
		var progress UserLotteryProgress
		if err := lockForUpdate(tx).Where("user_id = ?", userID).First(&progress).Error; err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			progress = UserLotteryProgress{UserID: userID}
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&progress).Error; err != nil {
				return err
			}
			if err := lockForUpdate(tx).Where("user_id = ?", userID).First(&progress).Error; err != nil {
				return err
			}
		}
		if quota > common.MaxQuota-progress.Accumulated {
			return errors.New("lottery quota accumulation overflow")
		}
		newAccum := progress.Accumulated + quota
		draws := newAccum / cfg.Threshold
		newAccum %= cfg.Threshold
		return tx.Model(&progress).Updates(map[string]any{"accumulated": newAccum, "available_draws": gorm.Expr("available_draws + ?", draws)}).Error
	})
}

func DrawLottery(userID int) (*LotteryDraw, error) {
	cfg := GetLotteryConfig()
	if !cfg.Enabled {
		return nil, errors.New("lottery is disabled")
	}
	total := 0
	for _, p := range cfg.Prizes {
		if p.Weight <= 0 || p.Weight > 1000000-total {
			return nil, errors.New("lottery has no valid prizes")
		}
		total += p.Weight
	}
	if total <= 0 {
		return nil, errors.New("lottery has no valid prizes")
	}
	remaining := rand.Intn(total)
	selected := cfg.Prizes[0]
	for _, p := range cfg.Prizes {
		remaining -= p.Weight
		if remaining < 0 {
			selected = p
			break
		}
	}
	// Prize amounts are configured in site display currency; the canonical raw
	// quota is persisted and used for crediting so settings changes do not drift.
	prizeQuota := selected.AmountQuota
	var draw LotteryDraw
	err := DB.Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&UserLotteryProgress{}).Where("user_id = ? AND available_draws > 0", userID).UpdateColumn("available_draws", gorm.Expr("available_draws - ?", 1))
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return errors.New("no lottery draws available")
		}
		displayAmount := float64(0)
		if prizeQuota > 0 {
			var conversionErr error
			displayAmount, conversionErr = LotteryQuotaToDisplay(prizeQuota)
			if conversionErr != nil {
				return conversionErr
			}
		}
		draw = LotteryDraw{UserID: userID, PrizeID: selected.ID, PrizeName: selected.Name, PrizeAmount: displayAmount, CreatedAt: time.Now().Unix()}
		if err := tx.Create(&draw).Error; err != nil {
			return err
		}
		// Credit the prize in the same transaction as the draw-count decrement
		// and the draw record, so a draw is fulfilled exactly once: retries and
		// concurrent draws each consume their own available_draw or fail.
		if prizeQuota > 0 {
			// MaxQuota is the billing conversion safety bound, not necessarily the
			// database column's current upper bound. Existing accounts can be above
			// it (for example after historical credits), and should still receive a
			// valid small prize. Preserve the legacy guard for balances below the
			// safety bound while also preventing overflow of the Go/SQL integer.
			maxInt := int(^uint(0) >> 1)
			maxAllowed := common.MaxQuota - prizeQuota
			maxRepresentable := maxInt - prizeQuota
			credit := tx.Model(&User{}).
				Where("id = ? AND (quota > ? OR quota <= ?) AND quota <= ?", userID, common.MaxQuota, maxAllowed, maxRepresentable).
				Update("quota", gorm.Expr("quota + ?", prizeQuota))
			if credit.Error != nil {
				return credit.Error
			}
			if credit.RowsAffected != 1 {
				return errors.New("lottery prize would exceed quota limit or winner no longer exists")
			}
		}
		return nil
	})
	if err != nil {
		return &draw, err
	}
	if prizeQuota > 0 {
		if cacheErr := cacheIncrUserQuota(userID, int64(prizeQuota)); cacheErr != nil {
			common.SysLog(fmt.Sprintf("failed to update lottery prize quota cache for user %d: %v", userID, cacheErr))
		}
		displayAmount, _ := LotteryQuotaToDisplay(prizeQuota)
		RecordLog(userID, LogTypeSystem, fmt.Sprintf("抽奖奖励：余额 +%.2f（奖品：%s）", displayAmount, selected.Name))
	}
	return &draw, nil
}

func ResetLotteryProgress(userID *int) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		q := tx.Model(&UserLotteryProgress{})
		if userID != nil {
			q = q.Where("user_id = ?", *userID)
		}
		// Reset only the unconverted remainder. Already-issued draws belong to the
		// user and must survive both per-user and global resets.
		return q.Updates(map[string]any{"accumulated": 0}).Error
	})
}
