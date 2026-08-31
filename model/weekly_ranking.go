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

// ConfiguredWeeklyRankingRewardAmounts returns user-visible balance amounts
// from the persisted admin configuration. Legacy three-option settings remain
// the fallback. Invalid values are zeroed defensively because this function is
// also used to build a response visible to ordinary users.
func ConfiguredWeeklyRankingRewardAmounts() []float64 {
	common.OptionMapRWMutex.RLock()
	raw := common.OptionMap["TokenRankingRewards"]
	legacyAmounts := []float64{common.WeeklyRankReward1, common.WeeklyRankReward2, common.WeeklyRankReward3}
	common.OptionMapRWMutex.RUnlock()
	var amounts []float64
	if raw != "" {
		if err := common.Unmarshal([]byte(raw), &amounts); err != nil {
			amounts = nil
		}
	}
	if len(amounts) == 0 {
		amounts = legacyAmounts
	}
	for i, amount := range amounts {
		if math.IsNaN(amount) || math.IsInf(amount, 0) || amount < 0 || amount*common.QuotaPerUnit > float64(common.MaxQuota) {
			amounts[i] = 0
		}
	}
	return amounts
}

// ConfiguredWeeklyRankingRewards returns quota values using the same amounts
// exposed by ConfiguredWeeklyRankingRewardAmounts. Keeping conversion here
// ensures settlement and the user-facing ranking response share one config.
func ConfiguredWeeklyRankingRewards() []int {
	amounts := ConfiguredWeeklyRankingRewardAmounts()
	rewards := make([]int, len(amounts))
	for i, amount := range amounts {
		if amount <= 0 {
			rewards[i] = 0
			continue
		}
		rewards[i] = common.QuotaFromFloat(amount * common.QuotaPerUnit)
	}
	return rewards
}

// WeeklyRankingReward is the idempotency ledger for automatic weekly awards.
// WeekStart is the Asia/Shanghai Monday (YYYY-MM-DD).
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

var shanghaiLocation = time.FixedZone("Asia/Shanghai", 8*60*60)

const weeklyRewardFirstWeek = "2026-08-10"

// WeeklyRewardWeekDue returns the previous natural week once its Monday 00:05
// settlement time has arrived. Weeks before launch are deliberately ignored.
func WeeklyRewardWeekDue(now time.Time) (string, bool) {
	local := now.In(shanghaiLocation)
	currentStartUnix, _, _ := ShanghaiWeekRange(local, 0)
	currentStart := time.Unix(currentStartUnix, 0).In(shanghaiLocation)
	if local.Before(currentStart.Add(5 * time.Minute)) {
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
	candidate := time.Unix(currentStartUnix, 0).In(shanghaiLocation).Add(5 * time.Minute)
	if !local.Before(candidate) {
		candidate = candidate.AddDate(0, 0, 7)
	}
	return candidate
}

// ShanghaiWeekRange returns [start,end) for the requested relative natural week.
// offset=0 is the current week, -1 is the previous week.
func ShanghaiWeekRange(now time.Time, offset int) (startUnix, endUnix int64, key string) {
	local := now.In(shanghaiLocation)
	dayStart := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, shanghaiLocation)
	daysSinceMonday := (int(dayStart.Weekday()) + 6) % 7
	start := dayStart.AddDate(0, 0, -daysSinceMonday+offset*7)
	end := start.AddDate(0, 0, 7)
	return start.Unix(), end.Unix(), start.Format("2006-01-02")
}

// maskRankingName hides identifying account details from other signed-in
// users while retaining enough of the name to distinguish ranking rows.
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

// GetWeeklyUserRanking aggregates successful consumption logs, then filters out
// administrators, disabled/deleted users, and test accounts before ranking.
func GetWeeklyUserRanking(now time.Time, limit int) ([]WeeklyUserRanking, error) {
	return getWeeklyUserRanking(now, 0, limit)
}

// GetWeeklyUserRankingWithCurrent returns the top rows plus the signed-in user's
// actual rank when they are outside the visible top list.
func GetUserRankingWithCurrent(now time.Time, period string, limit, currentUserID int) ([]WeeklyUserRanking, *WeeklyUserRanking, error) {
	local := now.In(shanghaiLocation)
	var start, end int64
	if period == "day" {
		dayStart := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, shanghaiLocation)
		start, end = dayStart.Unix(), dayStart.AddDate(0, 0, 1).Unix()
	} else {
		start, end, _ = ShanghaiWeekRange(local, 0)
	}
	rows, err := getUserRankingByRange(start, end, 100, currentUserID)
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
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if len(rows) > limit {
		rows = rows[:limit]
	}
	return rows, current, nil
}

func GetWeeklyUserRankingWithCurrent(now time.Time, limit, currentUserID int) ([]WeeklyUserRanking, *WeeklyUserRanking, error) {
	return GetUserRankingWithCurrent(now, "week", limit, currentUserID)
}

func getWeeklyUserRanking(now time.Time, offset, limit int) ([]WeeklyUserRanking, error) {
	start, end, _ := ShanghaiWeekRange(now, offset)
	return getUserRankingByRange(start, end, limit, 0)
}

// getUserRankingByRange masks every display name except the requesting user's
// own row (currentUserID 0 masks all rows).
func getUserRankingByRange(start, end int64, limit, currentUserID int) ([]WeeklyUserRanking, error) {
	if LOG_DB == nil || DB == nil {
		return []WeeklyUserRanking{}, nil
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	type aggregate struct {
		UserID       int
		PromptTokens int64
		OutputTokens int64
		CacheTokens  int64
		TotalTokens  int64
		RequestCount int64
	}
	var totals []aggregate
	cacheReadExpr, _ := cacheTokenSelectExpr()
	selectSQL := fmt.Sprintf("user_id, COALESCE(SUM(prompt_tokens), 0) AS prompt_tokens, COALESCE(SUM(completion_tokens), 0) AS output_tokens, COALESCE(SUM(%s), 0) AS cache_tokens, COALESCE(SUM(prompt_tokens + completion_tokens), 0) AS total_tokens, COUNT(*) AS request_count", cacheReadExpr)
	err := LOG_DB.Model(&Log{}).
		Select(selectSQL).
		Where("type = ? AND created_at >= ? AND created_at < ? AND user_id > 0", LogTypeConsume, start, end).
		Group("user_id").Scan(&totals).Error
	if err != nil {
		// A malformed `other` JSON value can abort the cache-token extract on
		// MySQL/PostgreSQL. Fall back without it (cache_tokens = 0) so the
		// ranking API and weekly reward settlement keep working, mirroring the
		// SumUsedQuota fallback.
		common.SysError("failed to query weekly ranking with cache extract, fallback: " + err.Error())
		fallbackSQL := "user_id, COALESCE(SUM(prompt_tokens), 0) AS prompt_tokens, COALESCE(SUM(completion_tokens), 0) AS output_tokens, 0 AS cache_tokens, COALESCE(SUM(prompt_tokens + completion_tokens), 0) AS total_tokens, COUNT(*) AS request_count"
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
		user, ok := userByID[total.UserID]
		if !ok || total.TotalTokens <= 0 {
			continue
		}
		name := user.DisplayName
		if name == "" {
			name = user.Username
		}
		if currentUserID == 0 || user.Id != currentUserID {
			name = maskRankingName(name)
		}
		inputTokens := total.PromptTokens - total.CacheTokens
		if inputTokens < 0 {
			inputTokens = 0
		}
		rows = append(rows, WeeklyUserRanking{UserID: user.Id, RewardEligible: user.Role == common.RoleCommonUser, DisplayName: name, InputTokens: inputTokens, OutputTokens: total.OutputTokens, CacheTokens: total.CacheTokens, TotalTokens: total.TotalTokens, RequestCount: total.RequestCount})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].TotalTokens == rows[j].TotalTokens {
			return rows[i].UserID < rows[j].UserID
		}
		return rows[i].TotalTokens > rows[j].TotalTokens
	})
	if len(rows) > limit {
		rows = rows[:limit]
	}
	for i := range rows {
		rows[i].Rank = i + 1
	}
	return rows, nil
}

// SettleWeeklyRankingRewards awards the previous Shanghai natural week.
// The unique (week_start,user_id) ledger and quota update share one transaction,
// so retries and multi-instance races cannot issue a reward twice.
func SettleWeeklyRankingRewards(now time.Time, rewards []int) (int64, error) {
	if DB == nil || LOG_DB == nil || len(rewards) == 0 {
		return 0, nil
	}
	if !common.TokenRankingEnabled {
		return 0, nil
	}
	if _, due := WeeklyRewardWeekDue(now); !due {
		return 0, nil
	}
	weekStart, _, _ := ShanghaiWeekRange(now, -1)
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
	rows, err := getWeeklyUserRanking(now, -1, 100)
	if err != nil {
		return 0, err
	}
	_, _, weekKey := ShanghaiWeekRange(now, -1)
	var awarded int64
	rewardIndex := 0
	for _, row := range rows {
		if !row.RewardEligible {
			continue
		}
		if rewardIndex >= len(rewards) {
			break
		}
		rewardRank := rewardIndex + 1
		rewardQuota := rewards[rewardIndex]
		rewardIndex++
		if rewardQuota <= 0 {
			continue
		}
		issued, err := awardWeeklyRankingUser(now, weekKey, row, rewardRank, rewardQuota)
		if err != nil {
			return awarded, fmt.Errorf("settle weekly reward user %d: %w", row.UserID, err)
		}
		if issued {
			if err := recordWeeklyRewardUserLog(now, weekKey, row, rewardRank, rewardQuota); err != nil {
				common.SysLog(fmt.Sprintf("failed to record weekly reward log for user %d: %v", row.UserID, err))
			}
			if err := cacheIncrUserQuota(row.UserID, int64(rewardQuota)); err != nil {
				common.SysLog(fmt.Sprintf("failed to update weekly reward quota cache for user %d: %v", row.UserID, err))
			}
			awarded++
		}
	}
	return awarded, nil
}

func recordWeeklyRewardUserLog(now time.Time, weekKey string, row WeeklyUserRanking, rank, rewardQuota int) error {
	weekStart, err := time.ParseInLocation("2006-01-02", weekKey, shanghaiLocation)
	if err != nil {
		return err
	}
	content := fmt.Sprintf("周榜第%d名奖励：余额 +%.2f；统计周期：%s 至 %s", rank, float64(rewardQuota)/common.QuotaPerUnit, weekKey, weekStart.AddDate(0, 0, 7).Format("2006-01-02"))
	return createLog(&Log{
		UserId:    row.UserID,
		Username:  usernameForRewardLog(row.UserID),
		CreatedAt: now.Unix(),
		Type:      LogTypeSystem,
		Content:   content,
	})
}

func usernameForRewardLog(userID int) string {
	username, _ := GetUsernameById(userID, false)
	return username
}

func awardWeeklyRankingUser(now time.Time, weekKey string, row WeeklyUserRanking, rank int, rewardQuota int) (bool, error) {
	reward := WeeklyRankingReward{WeekStart: weekKey, UserID: row.UserID, Rank: rank, TotalTokens: row.TotalTokens, RewardQuota: rewardQuota, CreatedAt: now.Unix()}
	issued := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&reward)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return nil
		}
		quotaUpdate := tx.Model(&User{}).
			Where("id = ? AND status = ? AND role = ?", row.UserID, common.UserStatusEnabled, common.RoleCommonUser).
			Update("quota", gorm.Expr("quota + ?", rewardQuota))
		if quotaUpdate.Error != nil {
			return quotaUpdate.Error
		}
		if quotaUpdate.RowsAffected != 1 {
			return fmt.Errorf("eligible reward user %d no longer exists", row.UserID)
		}
		issued = true
		return nil
	})
	return issued, err
}
