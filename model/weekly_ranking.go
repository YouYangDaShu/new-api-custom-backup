package model

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	weeklyRankingLimit    = 20
	weeklyRewardFirstWeek = "2026-08-10"
)

var shanghaiLocation = time.FixedZone("Asia/Shanghai", 8*60*60)

// WeeklyRankingReward is the idempotency ledger for automatic weekly awards.
// WeekStart is the Asia/Shanghai Monday in YYYY-MM-DD format.
type WeeklyRankingReward struct {
	Id          int    `json:"id" gorm:"primaryKey;autoIncrement"`
	WeekStart   string `json:"week_start" gorm:"type:varchar(10);not null;uniqueIndex:idx_weekly_reward_user"`
	UserID      int    `json:"user_id" gorm:"not null;uniqueIndex:idx_weekly_reward_user"`
	Rank        int    `json:"rank" gorm:"not null"`
	TotalTokens int64  `json:"total_tokens" gorm:"not null"`
	RewardQuota int    `json:"reward_quota" gorm:"not null"`
	CreatedAt   int64  `json:"created_at" gorm:"not null"`
}

func (WeeklyRankingReward) TableName() string { return "weekly_ranking_rewards" }

type WeeklyUserRanking struct {
	Rank           int     `json:"rank"`
	UserID         int     `json:"-"`
	IsCurrentUser  bool    `json:"is_current_user"`
	RewardEligible bool    `json:"reward_eligible"`
	DisplayName    string  `json:"display_name"`
	InputTokens    int64   `json:"input_tokens"`
	OutputTokens   int64   `json:"output_tokens"`
	CacheTokens    int64   `json:"cache_tokens"`
	TotalTokens    int64   `json:"total_tokens"`
	RequestCount   int64   `json:"request_count"`
	RewardQuota    int     `json:"reward_quota"`
	RewardAmount   float64 `json:"reward_amount"`
}

func WeeklyRankingRewardsEnabled() bool {
	common.OptionMapRWMutex.RLock()
	raw, exists := common.OptionMap["TokenRankingEnabled"]
	common.OptionMapRWMutex.RUnlock()
	if !exists || raw == "" {
		return true
	}
	enabled, err := strconv.ParseBool(raw)
	return err == nil && enabled
}

// ConfiguredWeeklyRankingRewardAmounts returns user-visible balance amounts.
// The legacy three-value options remain a fallback for upgrades from the old implementation.
func ConfiguredWeeklyRankingRewardAmounts() []float64 {
	common.OptionMapRWMutex.RLock()
	raw := common.OptionMap["TokenRankingRewards"]
	legacy := []string{
		common.OptionMap["WeeklyRankReward1"],
		common.OptionMap["WeeklyRankReward2"],
		common.OptionMap["WeeklyRankReward3"],
	}
	common.OptionMapRWMutex.RUnlock()

	var amounts []float64
	if raw != "" {
		if err := common.Unmarshal([]byte(raw), &amounts); err != nil {
			amounts = nil
		}
	}
	if len(amounts) == 0 {
		amounts = []float64{common.WeeklyRankReward1, common.WeeklyRankReward2, common.WeeklyRankReward3}
		for i, value := range legacy {
			if value != "" {
				amounts[i], _ = strconv.ParseFloat(value, 64)
			}
		}
	}
	for i, amount := range amounts {
		quota := amount * common.QuotaPerUnit
		if math.IsNaN(amount) || math.IsInf(amount, 0) || amount < 0 || math.IsNaN(quota) || math.IsInf(quota, 0) || quota > float64(common.MaxQuota) {
			amounts[i] = 0
		}
	}
	return amounts
}

func ConfiguredWeeklyRankingRewards() []int {
	amounts := ConfiguredWeeklyRankingRewardAmounts()
	rewards := make([]int, len(amounts))
	for i, amount := range amounts {
		if amount > 0 {
			rewards[i] = common.QuotaFromFloat(amount * common.QuotaPerUnit)
		}
	}
	return rewards
}

func ShanghaiDayRange(now time.Time) (startUnix, endUnix int64) {
	local := now.In(shanghaiLocation)
	start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, shanghaiLocation)
	return start.Unix(), start.AddDate(0, 0, 1).Unix()
}

// ShanghaiWeekRange returns [start,end) for a relative natural week.
func ShanghaiWeekRange(now time.Time, offset int) (startUnix, endUnix int64, key string) {
	local := now.In(shanghaiLocation)
	dayStart := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, shanghaiLocation)
	daysSinceMonday := (int(dayStart.Weekday()) + 6) % 7
	start := dayStart.AddDate(0, 0, -daysSinceMonday+offset*7)
	end := start.AddDate(0, 0, 7)
	return start.Unix(), end.Unix(), start.Format("2006-01-02")
}

func WeeklyRewardWeekDue(now time.Time) (string, bool) {
	local := now.In(shanghaiLocation)
	currentStartUnix, _, _ := ShanghaiWeekRange(local, 0)
	if local.Before(time.Unix(currentStartUnix, 0).In(shanghaiLocation).Add(5 * time.Minute)) {
		return "", false
	}
	_, _, previousKey := ShanghaiWeekRange(local, -1)
	if previousKey < weeklyRewardFirstWeek {
		return "", false
	}
	return previousKey, true
}

func NextWeeklyRewardCheck(now time.Time) time.Time {
	local := now.In(shanghaiLocation)
	currentStartUnix, _, _ := ShanghaiWeekRange(local, 0)
	next := time.Unix(currentStartUnix, 0).In(shanghaiLocation).Add(5 * time.Minute)
	if !local.Before(next) {
		next = next.AddDate(0, 0, 7)
	}
	return next
}

func maskRankingName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "***"
	}
	if at := strings.IndexByte(value, '@'); at > 0 {
		local := []rune(value[:at])
		if len(local) <= 2 {
			return value
		}
		masked := min(3, len(local)-2)
		return string(local[:1]) + strings.Repeat("*", masked) + string(local[len(local)-1:]) + value[at:]
	}
	runes := []rune(value)
	allDigits := true
	for _, r := range runes {
		if r < '0' || r > '9' {
			allDigits = false
			break
		}
	}
	if allDigits {
		if len(runes) <= 6 {
			return value
		}
		masked := min(3, len(runes)-5)
		return string(runes[:3]) + strings.Repeat("*", masked) + string(runes[len(runes)-2:])
	}
	allEnglish := true
	for _, r := range runes {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')) {
			allEnglish = false
			break
		}
	}
	if allEnglish {
		return value
	}
	return string(runes[0]) + "***"
}

func GetWeeklyUserRanking(now time.Time, limit int) ([]WeeklyUserRanking, error) {
	start, end, _ := ShanghaiWeekRange(now, 0)
	rows, err := getUserRankingByRange(start, end, 0, 0)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > weeklyRankingLimit {
		limit = weeklyRankingLimit
	}
	return rows[:min(limit, len(rows))], nil
}

func GetUserRankingWithCurrent(now time.Time, period string, limit, currentUserID int) ([]WeeklyUserRanking, *WeeklyUserRanking, error) {
	var start, end int64
	if period == "day" {
		start, end = ShanghaiDayRange(now)
	} else {
		start, end, _ = ShanghaiWeekRange(now, 0)
	}
	rows, err := getUserRankingByRange(start, end, 0, currentUserID)
	if err != nil {
		return nil, nil, err
	}
	var current *WeeklyUserRanking
	for i := range rows {
		if rows[i].UserID == currentUserID {
			rows[i].IsCurrentUser = true
			copy := rows[i]
			current = &copy
			break
		}
	}
	if limit <= 0 || limit > weeklyRankingLimit {
		limit = weeklyRankingLimit
	}
	return rows[:min(limit, len(rows))], current, nil
}

func getUserRankingByRange(start, end int64, limit, currentUserID int) ([]WeeklyUserRanking, error) {
	if LOG_DB == nil || DB == nil {
		return []WeeklyUserRanking{}, nil
	}
	type aggregate struct {
		UserID       int
		PromptTokens int64
		OutputTokens int64
		CacheTokens  int64
		TotalTokens  int64
		RequestCount int64
	}
	cacheReadExpr, _ := logCacheTokenSelectExpr()
	selectSQL := fmt.Sprintf("user_id, COALESCE(SUM(prompt_tokens), 0) AS prompt_tokens, COALESCE(SUM(completion_tokens), 0) AS output_tokens, COALESCE(SUM(%s), 0) AS cache_tokens, COALESCE(SUM(prompt_tokens), 0) + COALESCE(SUM(completion_tokens), 0) AS total_tokens, COUNT(*) AS request_count", cacheReadExpr)
	var totals []aggregate
	err := LOG_DB.Model(&Log{}).
		Select(selectSQL).
		Where("type = ? AND created_at >= ? AND created_at < ? AND user_id > 0", LogTypeConsume, start, end).
		Group("user_id").Scan(&totals).Error
	if err != nil {
		common.SysError("failed to query weekly ranking with cache extract, fallback: " + err.Error())
		fallbackSQL := "user_id, COALESCE(SUM(prompt_tokens), 0) AS prompt_tokens, COALESCE(SUM(completion_tokens), 0) AS output_tokens, 0 AS cache_tokens, COALESCE(SUM(prompt_tokens), 0) + COALESCE(SUM(completion_tokens), 0) AS total_tokens, COUNT(*) AS request_count"
		err = LOG_DB.Model(&Log{}).
			Select(fallbackSQL).
			Where("type = ? AND created_at >= ? AND created_at < ? AND user_id > 0", LogTypeConsume, start, end).
			Group("user_id").Scan(&totals).Error
		if err != nil {
			return nil, err
		}
	}
	if len(totals) == 0 {
		return []WeeklyUserRanking{}, nil
	}

	ids := make([]int, 0, len(totals))
	for _, total := range totals {
		ids = append(ids, total.UserID)
	}
	var users []User
	if err := DB.Unscoped().Where("id IN ? AND status = ? AND deleted_at IS NULL", ids, common.UserStatusEnabled).Find(&users).Error; err != nil {
		return nil, err
	}
	userByID := make(map[int]User, len(users))
	for _, user := range users {
		userByID[user.Id] = user
	}
	rows := make([]WeeklyUserRanking, 0, len(users))
	for _, total := range totals {
		user, exists := userByID[total.UserID]
		if !exists || total.TotalTokens <= 0 {
			continue
		}
		name := user.DisplayName
		if name == "" {
			name = user.Username
		}
		if currentUserID == 0 || user.Id != currentUserID {
			name = maskRankingName(name)
		}
		cacheTokens := max(int64(0), min(total.CacheTokens, total.PromptTokens))
		rows = append(rows, WeeklyUserRanking{
			UserID:         user.Id,
			RewardEligible: user.Role == common.RoleCommonUser,
			DisplayName:    name,
			InputTokens:    total.PromptTokens - cacheTokens,
			OutputTokens:   total.OutputTokens,
			CacheTokens:    cacheTokens,
			TotalTokens:    total.TotalTokens,
			RequestCount:   total.RequestCount,
		})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].TotalTokens == rows[j].TotalTokens {
			return rows[i].UserID < rows[j].UserID
		}
		return rows[i].TotalTokens > rows[j].TotalTokens
	})
	if limit > 0 && len(rows) > limit {
		rows = rows[:limit]
	}
	for i := range rows {
		rows[i].Rank = i + 1
	}
	return rows, nil
}

// SettleWeeklyRankingRewards awards the previous Shanghai natural week.
func SettleWeeklyRankingRewards(now time.Time, rewards []int) (int64, error) {
	if DB == nil || LOG_DB == nil || len(rewards) == 0 || !WeeklyRankingRewardsEnabled() {
		return 0, nil
	}
	if _, due := WeeklyRewardWeekDue(now); !due {
		return 0, nil
	}
	weekStart, _, weekKey := ShanghaiWeekRange(now, -1)
	common.OptionMapRWMutex.RLock()
	disabledAt, _ := strconv.ParseInt(common.OptionMap["TokenRankingDisabledAt"], 10, 64)
	common.OptionMapRWMutex.RUnlock()
	if disabledAt >= weekStart {
		return 0, nil
	}
	for i, reward := range rewards {
		if reward < 0 || reward > common.MaxQuota {
			return 0, fmt.Errorf("weekly reward rank %d quota out of range: %d", i+1, reward)
		}
	}
	start, end, _ := ShanghaiWeekRange(now, -1)
	rows, err := getUserRankingByRange(start, end, 0, 0)
	if err != nil {
		return 0, err
	}

	var awarded int64
	rewardIndex := 0
	for _, row := range rows {
		if !row.RewardEligible {
			continue
		}
		if rewardIndex >= len(rewards) {
			break
		}
		rank := rewardIndex + 1
		rewardQuota := rewards[rewardIndex]
		rewardIndex++
		if rewardQuota == 0 {
			continue
		}
		issued, err := awardWeeklyRankingUser(now, weekKey, row, rank, rewardQuota)
		if err != nil {
			return awarded, fmt.Errorf("settle weekly reward user %d: %w", row.UserID, err)
		}
		if !issued {
			continue
		}
		awarded++
		if err := cacheIncrUserQuota(row.UserID, int64(rewardQuota)); err != nil {
			common.SysError(fmt.Sprintf("failed to update weekly reward quota cache for user %d: %v", row.UserID, err))
		}
		if err := recordWeeklyRewardUserLog(now, weekKey, row, rank, rewardQuota); err != nil {
			common.SysError(fmt.Sprintf("failed to record weekly reward log for user %d: %v", row.UserID, err))
		}
	}
	return awarded, nil
}

func awardWeeklyRankingUser(now time.Time, weekKey string, row WeeklyUserRanking, rank, rewardQuota int) (bool, error) {
	reward := WeeklyRankingReward{WeekStart: weekKey, UserID: row.UserID, Rank: rank, TotalTokens: row.TotalTokens, RewardQuota: rewardQuota, CreatedAt: now.Unix()}
	issued := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&reward)
		if result.Error != nil || result.RowsAffected == 0 {
			return result.Error
		}
		quotaUpdate := tx.Model(&User{}).
			Where("id = ? AND status = ? AND role = ? AND quota <= ?", row.UserID, common.UserStatusEnabled, common.RoleCommonUser, common.MaxWalletQuota-rewardQuota).
			Update("quota", gorm.Expr("quota + ?", rewardQuota))
		if quotaUpdate.Error != nil {
			return quotaUpdate.Error
		}
		if quotaUpdate.RowsAffected != 1 {
			return fmt.Errorf("reward user %d is ineligible or quota would overflow", row.UserID)
		}
		issued = true
		return nil
	})
	return issued, err
}

func recordWeeklyRewardUserLog(now time.Time, weekKey string, row WeeklyUserRanking, rank, rewardQuota int) error {
	weekStart, err := time.ParseInLocation("2006-01-02", weekKey, shanghaiLocation)
	if err != nil {
		return err
	}
	username, _ := GetUsernameById(row.UserID, false)
	content := fmt.Sprintf("周榜第%d名奖励：余额 +%.2f；统计周期：%s 至 %s", rank, float64(rewardQuota)/common.QuotaPerUnit, weekKey, weekStart.AddDate(0, 0, 7).Format("2006-01-02"))
	return createLog(&Log{UserId: row.UserID, Username: username, CreatedAt: now.Unix(), Type: LogTypeSystem, Content: content})
}
