package model

import (
	"errors"
	"fmt"
	"math"
	"math/rand"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const lotteryConfigKey = "LotteryConfig"
const lotteryConfigSchemaVersion = 3
const lotteryConfigUnitRawQuota = "raw_quota"

const (
	lotteryDefaultBaseEnd       = 10
	lotteryDefaultStage1Start   = 11
	lotteryDefaultStage1End     = 15
	lotteryDefaultStage2Start   = 16
	lotteryDefaultStage2End     = 19
	lotteryDefaultHardThreshold = 20
	lotteryDefaultBaseRate      = 0.5
)

type LotteryPrize struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	PrizeLevel int     `json:"prize_level"`
	Weight     int     `json:"weight"`
	Amount     float64 `json:"amount,omitempty"`
	Enabled    bool    `json:"enabled"`
	SortOrder  int     `json:"sort_order"`
	// AmountQuota is the canonical persisted wallet credit. Amount remains the
	// current display-currency value at API boundaries.
	AmountQuota int `json:"amount_quota"`
}

type LotteryConfig struct {
	Enabled              bool           `json:"enabled"`
	Threshold            int            `json:"threshold"`
	Prizes               []LotteryPrize `json:"prizes"`
	PityEnabled          bool           `json:"pity_enabled"`
	PityTargetPrizeLevel int            `json:"pity_target_prize_level"`
	PityBaseRate         float64        `json:"pity_base_rate"`
	PityBaseEnd          int            `json:"pity_base_end"`
	PityStage1Start      int            `json:"pity_stage_1_start"`
	PityStage1End        int            `json:"pity_stage_1_end"`
	PityStage1Rates      []float64      `json:"pity_stage_1_rates"`
	PityStage2Start      int            `json:"pity_stage_2_start"`
	PityStage2End        int            `json:"pity_stage_2_end"`
	PityStage2Rates      []float64      `json:"pity_stage_2_rates"`
	PityHardThreshold    int            `json:"pity_hard_threshold"`
	SchemaVersion        int            `json:"schema_version,omitempty"`
	Unit                 string         `json:"unit,omitempty"`
}

// LotteryDisplayConfig is the admin-facing representation. Stored config
// remains raw quota; this type keeps display amounts out of persistence.
type LotteryDisplayConfig struct {
	Enabled                bool                  `json:"enabled"`
	ThresholdDisplayAmount float64               `json:"threshold_display_amount"`
	Prizes                 []LotteryDisplayPrize `json:"prizes"`
	PityEnabled            bool                  `json:"pity_enabled"`
	PityTargetPrizeLevel   int                   `json:"pity_target_prize_level"`
	PityBaseRate           float64               `json:"pity_base_rate"`
	PityBaseEnd            int                   `json:"pity_base_end"`
	PityStage1Start        int                   `json:"pity_stage_1_start"`
	PityStage1End          int                   `json:"pity_stage_1_end"`
	PityStage1Rates        []float64             `json:"pity_stage_1_rates"`
	PityStage2Start        int                   `json:"pity_stage_2_start"`
	PityStage2End          int                   `json:"pity_stage_2_end"`
	PityStage2Rates        []float64             `json:"pity_stage_2_rates"`
	PityHardThreshold      int                   `json:"pity_hard_threshold"`
}

type LotteryDisplayPrize struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	PrizeLevel int     `json:"prize_level"`
	Weight     int     `json:"weight"`
	Amount     float64 `json:"amount"`
	Enabled    bool    `json:"enabled"`
	SortOrder  int     `json:"sort_order"`
}

type UserLotteryProgress struct {
	ID             uint  `gorm:"primaryKey" json:"id"`
	UserID         int   `gorm:"uniqueIndex;not null" json:"user_id"`
	Accumulated    int   `gorm:"not null;default:0" json:"accumulated"`
	AvailableDraws int   `gorm:"not null;default:0" json:"available_draws"`
	PityCount      int   `gorm:"not null;default:0" json:"-"`
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
	ID               uint    `gorm:"primaryKey" json:"id"`
	UserID           int     `gorm:"index;not null" json:"user_id"`
	PrizeID          string  `gorm:"not null" json:"prize_id"`
	PrizeName        string  `gorm:"not null" json:"prize_name"`
	PrizeLevel       int     `gorm:"not null;default:0" json:"prize_level"`
	PrizeAmount      float64 `gorm:"not null;default:0" json:"prize_amount"`
	PrizeAmountQuota int     `gorm:"not null;default:0" json:"-"`
	PityStep         int     `gorm:"not null;default:0" json:"pity_step"`
	IsGuaranteed     bool    `gorm:"not null;default:false" json:"is_guaranteed"`
	CreatedAt        int64   `gorm:"autoCreateTime" json:"created_at"`
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
	return LotteryConfig{
		Enabled: false, Threshold: threshold,
		// Keep a fresh installation empty. The first admin-created tier must be
		// prize_level=1; pre-seeding "thanks" would permanently turn it into the
		// first-prize identity used by pity and leaderboards.
		Prizes:      []LotteryPrize{},
		PityEnabled: true, PityTargetPrizeLevel: 1, PityBaseRate: lotteryDefaultBaseRate,
		PityBaseEnd: lotteryDefaultBaseEnd, PityStage1Start: lotteryDefaultStage1Start,
		PityStage1End: lotteryDefaultStage1End, PityStage1Rates: []float64{2, 4, 6, 8, 10},
		PityStage2Start: lotteryDefaultStage2Start, PityStage2End: lotteryDefaultStage2End,
		PityStage2Rates: []float64{30, 50, 70, 90}, PityHardThreshold: lotteryDefaultHardThreshold,
		SchemaVersion: lotteryConfigSchemaVersion, Unit: lotteryConfigUnitRawQuota,
	}
}

func normalizeLotteryConfig(cfg LotteryConfig, legacy bool) LotteryConfig {
	// GetLotteryConfig returns a value copy whose slices may still point at the
	// atomic cache. Clone them before applying compatibility defaults so
	// concurrent draw requests never mutate shared cached configuration.
	cfg.Prizes = append([]LotteryPrize(nil), cfg.Prizes...)
	cfg.PityStage1Rates = append([]float64(nil), cfg.PityStage1Rates...)
	cfg.PityStage2Rates = append([]float64(nil), cfg.PityStage2Rates...)

	defaults := DefaultLotteryConfig()
	if cfg.PityTargetPrizeLevel == 0 {
		cfg.PityTargetPrizeLevel = defaults.PityTargetPrizeLevel
	}
	if legacy && cfg.PityBaseRate == 0 {
		cfg.PityBaseRate = defaults.PityBaseRate
	}
	if legacy && cfg.PityBaseEnd == 0 {
		cfg.PityBaseEnd = defaults.PityBaseEnd
	}
	if legacy && cfg.PityStage1Start == 0 {
		cfg.PityStage1Start = defaults.PityStage1Start
	}
	if legacy && cfg.PityStage1End == 0 {
		cfg.PityStage1End = defaults.PityStage1End
	}
	if legacy && len(cfg.PityStage1Rates) == 0 {
		cfg.PityStage1Rates = append([]float64(nil), defaults.PityStage1Rates...)
	}
	if legacy && cfg.PityStage2Start == 0 {
		cfg.PityStage2Start = defaults.PityStage2Start
	}
	if legacy && cfg.PityStage2End == 0 {
		cfg.PityStage2End = defaults.PityStage2End
	}
	if legacy && len(cfg.PityStage2Rates) == 0 {
		cfg.PityStage2Rates = append([]float64(nil), defaults.PityStage2Rates...)
	}
	if legacy && cfg.PityHardThreshold == 0 {
		cfg.PityHardThreshold = defaults.PityHardThreshold
	}
	for i := range cfg.Prizes {
		if cfg.Prizes[i].PrizeLevel <= 0 {
			if legacy {
				// The legacy admin UI numbered tiers from low to high in array
				// order (the live config is thanks, Tier 2, Tier 3, Tier 4).
				// New prize levels use 1 for the highest/first prize, so migrate
				// that ordering in reverse instead of making "thanks" the pity
				// target.
				cfg.Prizes[i].PrizeLevel = len(cfg.Prizes) - i
			} else {
				cfg.Prizes[i].PrizeLevel = i + 1
			}
		}
		// Legacy configs had no enabled field, so their zero value means enabled.
		// Canonical configs preserve an explicit false value.
		if legacy && !cfg.Prizes[i].Enabled {
			cfg.Prizes[i].Enabled = true
		}
		if legacy && cfg.Prizes[i].SortOrder == 0 {
			cfg.Prizes[i].SortOrder = i
		}
	}
	if legacy {
		cfg.PityEnabled = true
	}
	sort.SliceStable(cfg.Prizes, func(i, j int) bool {
		if cfg.Prizes[i].SortOrder != cfg.Prizes[j].SortOrder {
			return cfg.Prizes[i].SortOrder < cfg.Prizes[j].SortOrder
		}
		return cfg.Prizes[i].PrizeLevel < cfg.Prizes[j].PrizeLevel
	})
	cfg.SchemaVersion = lotteryConfigSchemaVersion
	cfg.Unit = lotteryConfigUnitRawQuota
	return cfg
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
	display := LotteryDisplayConfig{
		Enabled: cfg.Enabled, ThresholdDisplayAmount: threshold, Prizes: make([]LotteryDisplayPrize, len(cfg.Prizes)),
		PityEnabled: cfg.PityEnabled, PityTargetPrizeLevel: cfg.PityTargetPrizeLevel,
		PityBaseRate: cfg.PityBaseRate, PityBaseEnd: cfg.PityBaseEnd,
		PityStage1Start: cfg.PityStage1Start, PityStage1End: cfg.PityStage1End,
		PityStage1Rates: append([]float64(nil), cfg.PityStage1Rates...),
		PityStage2Start: cfg.PityStage2Start, PityStage2End: cfg.PityStage2End,
		PityStage2Rates:   append([]float64(nil), cfg.PityStage2Rates...),
		PityHardThreshold: cfg.PityHardThreshold,
	}
	display.ThresholdDisplayAmount = roundedLotteryDisplay(display.ThresholdDisplayAmount)
	for i, prize := range cfg.Prizes {
		display.Prizes[i] = LotteryDisplayPrize{ID: prize.ID, Name: prize.Name, PrizeLevel: prize.PrizeLevel, Weight: prize.Weight, Enabled: prize.Enabled, SortOrder: prize.SortOrder}
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
	if cfg.PityTargetPrizeLevel != 0 && cfg.PityTargetPrizeLevel != 1 {
		return errors.New("lottery pity target must be prize level 1")
	}
	if cfg.PityEnabled {
		if cfg.PityBaseRate < 0 || cfg.PityBaseRate > 100 || math.IsNaN(cfg.PityBaseRate) || math.IsInf(cfg.PityBaseRate, 0) {
			return errors.New("lottery base pity rate must be between 0 and 100")
		}
		if cfg.PityBaseEnd <= 0 || cfg.PityStage1Start != cfg.PityBaseEnd+1 || cfg.PityStage1End < cfg.PityStage1Start || cfg.PityStage2Start != cfg.PityStage1End+1 || cfg.PityStage2End < cfg.PityStage2Start || cfg.PityHardThreshold != cfg.PityStage2End+1 {
			return errors.New("lottery pity ranges must be continuous")
		}
		if len(cfg.PityStage1Rates) != cfg.PityStage1End-cfg.PityStage1Start+1 || len(cfg.PityStage2Rates) != cfg.PityStage2End-cfg.PityStage2Start+1 {
			return errors.New("lottery pity rates must match their ranges")
		}
		if cfg.PityHardThreshold <= 0 || cfg.PityHardThreshold > 1000 {
			return errors.New("lottery hard pity threshold is invalid")
		}
		previous := cfg.PityBaseRate
		for _, rate := range append(append([]float64{}, cfg.PityStage1Rates...), cfg.PityStage2Rates...) {
			if rate < 0 || rate > 100 || math.IsNaN(rate) || math.IsInf(rate, 0) || rate < previous {
				return errors.New("lottery pity rates must be non-decreasing percentages")
			}
			previous = rate
		}
	}
	total := 0
	active := 0
	hasFirstPrize := false
	levels := make(map[int]struct{}, len(cfg.Prizes))
	seen := make(map[string]struct{}, len(cfg.Prizes))
	legacySchema := cfg.SchemaVersion < lotteryConfigSchemaVersion
	for _, prize := range cfg.Prizes {
		if strings.TrimSpace(prize.ID) == "" || strings.TrimSpace(prize.Name) == "" || math.IsNaN(prize.Amount) || math.IsInf(prize.Amount, 0) || prize.Amount < 0 || prize.AmountQuota < 0 || prize.AmountQuota > common.MaxQuota {
			return errors.New("lottery prize id, name and weight are required")
		}
		enabled := prize.Enabled
		if !enabled && cfg.SchemaVersion < lotteryConfigSchemaVersion {
			enabled = true
		}
		if enabled && prize.Weight <= 0 {
			return errors.New("lottery prize id, name and weight are required")
		}
		if _, ok := seen[prize.ID]; ok {
			return errors.New("lottery prize ids must be unique")
		}
		seen[prize.ID] = struct{}{}
		level := prize.PrizeLevel
		if level <= 0 && legacySchema {
			level = len(levels) + 1
		}
		if level <= 0 {
			return errors.New("lottery prize levels are required")
		}
		if _, ok := levels[level]; ok {
			return errors.New("lottery prize levels must be unique")
		}
		levels[level] = struct{}{}
		if level == 1 {
			hasFirstPrize = true
		}
		if enabled {
			active++
			if prize.Weight > 1000000-total {
				return errors.New("lottery prize weights are too large")
			}
			total += prize.Weight
		}
	}
	if active == 0 {
		return errors.New("lottery must have an enabled prize")
	}
	if total <= 0 {
		return errors.New("lottery enabled prize weights must be positive")
	}
	if !hasFirstPrize {
		return errors.New("lottery must retain the first-prize anchor")
	}
	if cfg.PityEnabled {
		for _, prize := range cfg.Prizes {
			enabled := prize.Enabled
			if !enabled && cfg.SchemaVersion < lotteryConfigSchemaVersion {
				enabled = true
			}
			if prize.PrizeLevel == 1 && enabled {
				return nil
			}
		}
		return errors.New("first prize must stay enabled while pity is enabled")
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
		return normalizeLotteryConfig(cfg, false), false, nil
	}
	if cfg.Unit == lotteryConfigUnitRawQuota {
		return normalizeLotteryConfig(cfg, true), true, nil
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
	return normalizeLotteryConfig(cfg, true), true, nil
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
	legacy := cfg.SchemaVersion != lotteryConfigSchemaVersion || cfg.Unit != lotteryConfigUnitRawQuota
	if cfg.Unit != lotteryConfigUnitRawQuota {
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
	}
	cfg = normalizeLotteryConfig(cfg, legacy)
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
	if err := DB.Transaction(func(tx *gorm.DB) error {
		// Re-check against the row being written. The controller performs an
		// early validation for a useful error response, but this check closes
		// the race between that read and the actual option update.
		if err := validateLotteryConfigAnchorDB(tx, cfg); err != nil {
			return err
		}
		return tx.Save(&Option{Key: lotteryConfigKey, Value: string(b)}).Error
	}); err != nil {
		return err
	}
	refreshLotteryConfigCache(string(b))
	return nil
}

// ValidateLotteryConfigAnchor keeps persisted prize identities stable. The
// initial save is unrestricted. Later updates may edit or reorder a prize,
// but may not change its ID-to-level pairing. A non-first prize can be removed
// only while it has no draw history; historical prizes must be disabled.
func ValidateLotteryConfigAnchor(next LotteryConfig) error {
	if DB == nil {
		return nil
	}
	return validateLotteryConfigAnchorDB(DB, next)
}

func validateLotteryConfigAnchorDB(db *gorm.DB, next LotteryConfig) error {
	var option Option
	if err := lockForUpdate(db).Where("key = ?", lotteryConfigKey).First(&option).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	previous, _, err := decodeLotteryConfig(option.Value)
	if err != nil {
		return err
	}
	nextByID := make(map[string]LotteryPrize, len(next.Prizes))
	nextByLevel := make(map[int]LotteryPrize, len(next.Prizes))
	for _, prize := range next.Prizes {
		nextByID[prize.ID] = prize
		nextByLevel[prize.PrizeLevel] = prize
	}
	for _, prize := range previous.Prizes {
		if updated, ok := nextByID[prize.ID]; ok {
			if updated.PrizeLevel != prize.PrizeLevel {
				return errors.New("lottery persisted prize level cannot be changed")
			}
			continue
		}
		if replacement, ok := nextByLevel[prize.PrizeLevel]; ok && replacement.ID != prize.ID {
			return errors.New("lottery persisted prize level cannot be replaced")
		}
		if prize.PrizeLevel == 1 {
			return errors.New("lottery first-prize anchor cannot be removed")
		}
		var drawCount int64
		if err := db.Model(&LotteryDraw{}).Where("prize_id = ?", prize.ID).Count(&drawCount).Error; err != nil {
			return err
		}
		if drawCount > 0 {
			return errors.New("lottery prize with draw history cannot be removed; disable it instead")
		}
	}
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

func lotteryPityRate(cfg LotteryConfig, step int) (float64, bool) {
	if !cfg.PityEnabled || step <= 0 {
		return 0, false
	}
	if step >= cfg.PityHardThreshold {
		return 100, true
	}
	if step >= cfg.PityStage2Start && step <= cfg.PityStage2End {
		return cfg.PityStage2Rates[step-cfg.PityStage2Start], true
	}
	if step >= cfg.PityStage1Start && step <= cfg.PityStage1End {
		return cfg.PityStage1Rates[step-cfg.PityStage1Start], true
	}
	if step <= cfg.PityBaseEnd {
		return cfg.PityBaseRate, false
	}
	return 0, false
}

func lotterySelectPrize(prizes []LotteryPrize, target *LotteryPrize, topRate float64) (LotteryPrize, bool) {
	if target != nil && topRate > 0 {
		hasAlternative := false
		for _, prize := range prizes {
			if prize.Enabled && prize.PrizeLevel != target.PrizeLevel {
				hasAlternative = true
				break
			}
		}
		if !hasAlternative {
			return *target, false
		}
	}
	if target != nil && topRate > 0 && (topRate >= 100 || rand.Float64()*100 < topRate) {
		return *target, true
	}
	active := make([]LotteryPrize, 0, len(prizes))
	for _, prize := range prizes {
		if !prize.Enabled {
			continue
		}
		// A zero pity rate means the target remains in the ordinary weight
		// pool. Once a positive rate is applied, the target's probability is
		// reserved first and the remaining prizes share the rest.
		if target != nil && target.PrizeLevel == prize.PrizeLevel && topRate > 0 {
			continue
		}
		active = append(active, prize)
	}
	if len(active) == 0 {
		if target != nil {
			// With no alternative prize, the target is still a natural result;
			// there is no pity decision to record.
			return *target, false
		}
		return LotteryPrize{}, false
	}
	total := 0
	for _, prize := range active {
		total += prize.Weight
	}
	if total <= 0 {
		return LotteryPrize{}, false
	}
	remaining := rand.Intn(total)
	for _, prize := range active {
		remaining -= prize.Weight
		if remaining < 0 {
			return prize, false
		}
	}
	return active[len(active)-1], false
}

func DrawLottery(userID int) (*LotteryDraw, error) {
	draws, err := DrawLotteryCount(userID, 1)
	if len(draws) == 0 {
		return &LotteryDraw{}, err
	}
	return &draws[0], err
}

// DrawLotteryCount settles one or ten draws in sequence inside one transaction.
// Pity is updated after every result, so an early first-prize hit resets the
// counter before the remaining results in the same ten-draw request.
func DrawLotteryCount(userID int, count int) ([]LotteryDraw, error) {
	if count != 1 && count != 10 {
		return nil, errors.New("lottery draw count must be 1 or 10")
	}
	cfg := normalizeLotteryConfig(GetLotteryConfig(), false)
	if !cfg.Enabled {
		return nil, errors.New("lottery is disabled")
	}
	if err := ValidateLotteryConfig(cfg); err != nil {
		return nil, err
	}
	prizes := append([]LotteryPrize(nil), cfg.Prizes...)
	sort.SliceStable(prizes, func(i, j int) bool {
		if prizes[i].SortOrder != prizes[j].SortOrder {
			return prizes[i].SortOrder < prizes[j].SortOrder
		}
		return prizes[i].PrizeLevel < prizes[j].PrizeLevel
	})
	var draws []LotteryDraw
	var totalQuota int
	err := DB.Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&UserLotteryProgress{}).Where("user_id = ? AND available_draws >= ?", userID, count).UpdateColumn("available_draws", gorm.Expr("available_draws - ?", count))
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return errors.New("not enough lottery draws available")
		}
		var progress UserLotteryProgress
		if err := lockForUpdate(tx).Where("user_id = ?", userID).First(&progress).Error; err != nil {
			return err
		}
		var target *LotteryPrize
		for i := range prizes {
			if cfg.PityEnabled && prizes[i].Enabled && prizes[i].PrizeLevel == cfg.PityTargetPrizeLevel {
				target = &prizes[i]
				break
			}
		}
		draws = make([]LotteryDraw, 0, count)
		for i := 0; i < count; i++ {
			step := 0
			if cfg.PityEnabled {
				step = progress.PityCount + 1
			}
			rate, guaranteed := lotteryPityRate(cfg, step)
			selected, top := lotterySelectPrize(prizes, target, rate)
			if selected.ID == "" {
				return errors.New("lottery has no valid prizes")
			}
			if cfg.PityEnabled {
				if selected.PrizeLevel == cfg.PityTargetPrizeLevel {
					progress.PityCount = 0
				} else {
					progress.PityCount = step
				}
			}
			prizeQuota := selected.AmountQuota
			displayAmount := float64(0)
			if prizeQuota > 0 {
				var conversionErr error
				displayAmount, conversionErr = LotteryQuotaToDisplay(prizeQuota)
				if conversionErr != nil {
					return conversionErr
				}
				if prizeQuota > common.MaxQuota-totalQuota {
					return errors.New("lottery prize quota overflow")
				}
				totalQuota += prizeQuota
			}
			draws = append(draws, LotteryDraw{UserID: userID, PrizeID: selected.ID, PrizeName: selected.Name, PrizeLevel: selected.PrizeLevel, PrizeAmount: displayAmount, PrizeAmountQuota: prizeQuota, PityStep: step, IsGuaranteed: top && guaranteed, CreatedAt: time.Now().Unix()})
		}
		if err := tx.Model(&progress).Updates(map[string]any{"pity_count": progress.PityCount}).Error; err != nil {
			return err
		}
		if err := tx.Create(&draws).Error; err != nil {
			return err
		}
		if totalQuota > 0 {
			maxInt := int(^uint(0) >> 1)
			maxAllowed := common.MaxQuota - totalQuota
			maxRepresentable := maxInt - totalQuota
			credit := tx.Model(&User{}).Where("id = ? AND (quota > ? OR quota <= ?) AND quota <= ?", userID, common.MaxQuota, maxAllowed, maxRepresentable).Update("quota", gorm.Expr("quota + ?", totalQuota))
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
		return draws, err
	}
	if totalQuota > 0 {
		if cacheErr := cacheIncrUserQuota(userID, int64(totalQuota)); cacheErr != nil {
			common.SysLog(fmt.Sprintf("failed to update lottery prize quota cache for user %d: %v", userID, cacheErr))
		}
		for _, draw := range draws {
			if draw.PrizeAmount > 0 {
				RecordLog(userID, LogTypeSystem, fmt.Sprintf("抽奖奖励：余额 +%.2f（奖品：%s）", draw.PrizeAmount, draw.PrizeName))
			}
		}
	}
	return draws, nil
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
