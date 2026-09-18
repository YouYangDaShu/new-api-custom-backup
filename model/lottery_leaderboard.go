package model

import (
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

const lotteryLeaderboardLimit = 10

// LotteryLeaderboardRow is the public, privacy-safe representation of one
// leaderboard result. User IDs and account fields are intentionally absent.
type LotteryLeaderboardRow struct {
	Rank        int    `json:"rank"`
	DisplayName string `json:"display_name"`
	Value       int64  `json:"value"`
	PityStep    int    `json:"pity_step,omitempty"`
	CreatedAt   int64  `json:"created_at,omitempty"`
}

type LotteryLeaderboardBoard struct {
	Rows        []LotteryLeaderboardRow `json:"rows"`
	CurrentUser *LotteryLeaderboardRow  `json:"current_user,omitempty"`
}

type LotteryLeaderboardData struct {
	Period     string                  `json:"period"`
	Timezone   string                  `json:"timezone"`
	CountStart int64                   `json:"count_start"`
	CountEnd   int64                   `json:"count_end"`
	Lucky      LotteryLeaderboardBoard `json:"lucky"`
	DrawCount  LotteryLeaderboardBoard `json:"draw_count"`
	HardPity   LotteryLeaderboardBoard `json:"hard_pity"`
}

type lotteryLeaderboardUserRow struct {
	ID          uint
	UserID      int
	PityStep    int
	CreatedAt   int64
	Username    string
	DisplayName string
	Email       string
}

type lotteryLeaderboardCountRow struct {
	UserID      int
	Value       int64
	Username    string
	DisplayName string
	Email       string
}

// LotteryLeaderboardRange returns a Shanghai-local natural period. The lucky
// and hard-pity boards remain all-time; only the draw-count board uses it.
func LotteryLeaderboardRange(now time.Time, period string) (int64, int64, error) {
	local := now.In(shanghaiLocation)
	switch period {
	case "all":
		return 0, 0, nil
	case "day":
		start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, shanghaiLocation)
		return start.Unix(), start.AddDate(0, 0, 1).Unix(), nil
	case "week":
		start, end, _ := ShanghaiWeekRange(local, 0)
		return start, end, nil
	case "month":
		start := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, shanghaiLocation)
		return start.Unix(), start.AddDate(0, 1, 0).Unix(), nil
	default:
		return 0, 0, fmt.Errorf("unsupported lottery leaderboard period: %s", period)
	}
}

func lotteryLeaderboardBaseQuery() *gorm.DB {
	return DB.Table("lottery_draws AS lottery_draws").
		Joins("JOIN users ON users.id = lottery_draws.user_id").
		Where("users.status = ? AND users.deleted_at IS NULL", common.UserStatusEnabled)
}

func applyLotteryLeaderboardRange(query *gorm.DB, start, end int64) *gorm.DB {
	if start > 0 && end > start {
		return query.Where("lottery_draws.created_at >= ? AND lottery_draws.created_at < ?", start, end)
	}
	return query
}

func lotteryDisplayName(displayName, username, email string) string {
	for _, value := range []string{displayName, username, email} {
		if strings.TrimSpace(value) != "" {
			return maskLotteryIdentity(value)
		}
	}
	return "***"
}

func maskLotteryIdentity(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "***"
	}
	if at := strings.LastIndexByte(value, '@'); at > 0 {
		local, domain := []rune(value[:at]), value[at:]
		prefix := 3
		if len(local) < prefix {
			prefix = 1
		}
		return string(local[:prefix]) + "***" + domain
	}
	runes := []rune(value)
	allDigits := true
	for _, r := range runes {
		if r < '0' || r > '9' {
			allDigits = false
			break
		}
	}
	if allDigits && len(runes) > 6 {
		return string(runes[:3]) + "***" + string(runes[len(runes)-3:])
	}
	return string(runes[:1]) + "***"
}

func lotteryLeaderboardRow(rank int, name string, value int64, pityStep int, createdAt int64) LotteryLeaderboardRow {
	return LotteryLeaderboardRow{
		Rank: rank, DisplayName: name, Value: value,
		PityStep: pityStep, CreatedAt: createdAt,
	}
}

func lotteryLeaderboardUserName(row lotteryLeaderboardUserRow) string {
	return lotteryDisplayName(row.DisplayName, row.Username, row.Email)
}

func lotteryLeaderboardCountName(row lotteryLeaderboardCountRow) string {
	return lotteryDisplayName(row.DisplayName, row.Username, row.Email)
}

func buildLuckyBoard(rows []lotteryLeaderboardUserRow, currentUserID int) LotteryLeaderboardBoard {
	board := LotteryLeaderboardBoard{Rows: make([]LotteryLeaderboardRow, 0, min(len(rows), lotteryLeaderboardLimit))}
	seenUsers := make(map[int]struct{}, len(rows))
	rank := 0
	for _, row := range rows {
		if _, exists := seenUsers[row.UserID]; exists {
			continue
		}
		seenUsers[row.UserID] = struct{}{}
		rank++
		result := lotteryLeaderboardRow(rank, lotteryLeaderboardUserName(row), 1, row.PityStep, row.CreatedAt)
		if rank <= lotteryLeaderboardLimit {
			board.Rows = append(board.Rows, result)
		}
		if currentUserID != 0 && row.UserID == currentUserID {
			copy := result
			board.CurrentUser = &copy
		}
	}
	return board
}

func buildCountBoard(rows []lotteryLeaderboardCountRow, currentUserID int) LotteryLeaderboardBoard {
	board := LotteryLeaderboardBoard{Rows: make([]LotteryLeaderboardRow, 0, min(len(rows), lotteryLeaderboardLimit))}
	for index, row := range rows {
		result := lotteryLeaderboardRow(index+1, lotteryLeaderboardCountName(row), row.Value, 0, 0)
		if index < lotteryLeaderboardLimit {
			board.Rows = append(board.Rows, result)
		}
		if currentUserID != 0 && row.UserID == currentUserID {
			copy := result
			board.CurrentUser = &copy
		}
	}
	return board
}

// GetLotteryLeaderboards derives all public leaderboard values from immutable
// draw rows. The current-user slot is calculated from the same complete
// ordering as the public Top 10 and never exposes the user ID.
func GetLotteryLeaderboards(currentUserID int, now time.Time, period string) (*LotteryLeaderboardData, error) {
	if DB == nil {
		return nil, gorm.ErrInvalidDB
	}
	start, end, err := LotteryLeaderboardRange(now, period)
	if err != nil {
		return nil, err
	}

	var luckyRows []lotteryLeaderboardUserRow
	luckyQuery := lotteryLeaderboardBaseQuery().
		Select("lottery_draws.id, lottery_draws.user_id, lottery_draws.pity_step, lottery_draws.created_at, users.username, users.display_name, users.email").
		Where("lottery_draws.prize_level = ? AND lottery_draws.pity_step BETWEEN ? AND ? AND lottery_draws.is_guaranteed = ?", 1, 1, 10, false).
		Order("lottery_draws.pity_step ASC, lottery_draws.created_at DESC, lottery_draws.id DESC")
	if err := luckyQuery.Scan(&luckyRows).Error; err != nil {
		return nil, err
	}

	var countRows []lotteryLeaderboardCountRow
	countQuery := applyLotteryLeaderboardRange(lotteryLeaderboardBaseQuery(), start, end).
		Select("lottery_draws.user_id, COUNT(*) AS value, users.username, users.display_name, users.email").
		Group("lottery_draws.user_id, users.username, users.display_name, users.email").
		Order("value DESC, lottery_draws.user_id ASC")
	if err := countQuery.Scan(&countRows).Error; err != nil {
		return nil, err
	}

	var hardPityRows []lotteryLeaderboardCountRow
	hardPityQuery := lotteryLeaderboardBaseQuery().
		Select("lottery_draws.user_id, COUNT(*) AS value, users.username, users.display_name, users.email").
		Where("lottery_draws.prize_level = ? AND lottery_draws.is_guaranteed = ? AND lottery_draws.pity_step >= ?", 1, true, 20).
		Group("lottery_draws.user_id, users.username, users.display_name, users.email").
		Order("value DESC, lottery_draws.user_id ASC")
	if err := hardPityQuery.Scan(&hardPityRows).Error; err != nil {
		return nil, err
	}

	return &LotteryLeaderboardData{
		Period: period, Timezone: "Asia/Shanghai", CountStart: start, CountEnd: end,
		Lucky:     buildLuckyBoard(luckyRows, currentUserID),
		DrawCount: buildCountBoard(countRows, currentUserID),
		HardPity:  buildCountBoard(hardPityRows, currentUserID),
	}, nil
}
