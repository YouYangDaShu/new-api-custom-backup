package service

import (
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

const (
	SmartModeBalanced    = "balanced"
	SmartModePrice       = "price"
	SmartModeSuccessRate = "success_rate"
)

type SmartRouteCandidate struct {
	Group       string
	Price       float64
	SuccessRate float64
	AvgLatency  float64
	Score       float64
}

func successRateUnavailable(requestCount, successCount int64) bool {
	return requestCount > 0 && successCount == 0
}

// GetRequestSmartRouteGroups returns every currently selectable group for the
// user. Smart routing must not inherit the per-token Auto group limit, because
// that would hide models in later groups from /v1/models and routing.
func GetRequestSmartRouteGroups(c *gin.Context, userGroup string) []string {
	available := GetUserUsableGroups(userGroup)
	groups := make([]string, 0, len(available))
	for group := range available {
		if IsUserSelectableGroup(userGroup, group) {
			groups = append(groups, group)
		}
	}
	return groups
}

func RankGroupsForModel(userGroup, modelName, mode, requestPath string, candidates []string) []string {
	return rankGroupsForModel(userGroup, modelName, mode, requestPath, nil, candidates)
}

func rankGroupsForModel(userGroup, modelName, mode, requestPath string, filters []dto.ChannelFilter, candidates []string) []string {
	if modelName == "" || len(candidates) == 0 {
		return nil
	}
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		mode = SmartModeBalanced
	}

	type row struct {
		group string
		c     SmartRouteCandidate
	}
	rows := make([]row, 0, len(candidates))
	seen := make(map[string]struct{}, len(candidates))
	for _, group := range candidates {
		group = strings.TrimSpace(group)
		if group == "" || group == TokenGroupAuto {
			continue
		}
		if _, ok := seen[group]; ok {
			continue
		}
		seen[group] = struct{}{}
		channel, err := model.GetRandomSatisfiedChannel(group, modelName, 0, filters)
		if err != nil || channel == nil {
			continue
		}
		price := GetUserGroupRatio(userGroup, group)
		if price < 0 || math.IsNaN(price) || math.IsInf(price, 0) {
			price = 1
		}
		successRate, latency, requestCount, successCount := lookupGroupModelPerf(modelName, group)
		if mode == SmartModeSuccessRate && successRateUnavailable(requestCount, successCount) {
			continue
		}
		rows = append(rows, row{group: group, c: SmartRouteCandidate{
			Group: group, Price: price, SuccessRate: successRate, AvgLatency: latency,
		}})
	}
	if len(rows) == 0 {
		return nil
	}

	minPrice, maxPrice := rows[0].c.Price, rows[0].c.Price
	minLatency, maxLatency := 0.0, 0.0
	hasLatency := false
	for i := range rows {
		minPrice = min(minPrice, rows[i].c.Price)
		maxPrice = max(maxPrice, rows[i].c.Price)
		if rows[i].c.AvgLatency >= 0 {
			if !hasLatency {
				minLatency, maxLatency = rows[i].c.AvgLatency, rows[i].c.AvgLatency
				hasLatency = true
			} else {
				minLatency = min(minLatency, rows[i].c.AvgLatency)
				maxLatency = max(maxLatency, rows[i].c.AvgLatency)
			}
		}
	}
	for i := range rows {
		priceNorm := 0.0
		if maxPrice > minPrice {
			priceNorm = (rows[i].c.Price - minPrice) / (maxPrice - minPrice)
		}
		success := rows[i].c.SuccessRate
		if success < 0 {
			success = 0.7
		}
		latencyNorm := 0.5
		if rows[i].c.AvgLatency >= 0 && hasLatency {
			if maxLatency > minLatency {
				latencyNorm = (rows[i].c.AvgLatency - minLatency) / (maxLatency - minLatency)
			} else {
				latencyNorm = 0
			}
		}
		rows[i].c.Score = (1-priceNorm)*0.40 + success*0.35 + (1-latencyNorm)*0.25
	}

	switch mode {
	case SmartModePrice:
		sort.SliceStable(rows, func(i, j int) bool {
			if rows[i].c.Price != rows[j].c.Price {
				return rows[i].c.Price < rows[j].c.Price
			}
			return smartTieBreak(rows[i].c, rows[j].c, rows[i].group, rows[j].group)
		})
	case SmartModeSuccessRate:
		sort.SliceStable(rows, func(i, j int) bool {
			if rows[i].c.SuccessRate != rows[j].c.SuccessRate {
				return rows[i].c.SuccessRate > rows[j].c.SuccessRate
			}
			return smartTieBreak(rows[i].c, rows[j].c, rows[i].group, rows[j].group)
		})
	default:
		sort.SliceStable(rows, func(i, j int) bool {
			if rows[i].c.Score != rows[j].c.Score {
				return rows[i].c.Score > rows[j].c.Score
			}
			if rows[i].c.Price != rows[j].c.Price {
				return rows[i].c.Price < rows[j].c.Price
			}
			return rows[i].group < rows[j].group
		})
	}
	result := make([]string, 0, len(rows))
	for _, row := range rows {
		result = append(result, row.group)
	}
	return result
}

func smartTieBreak(left, right SmartRouteCandidate, leftGroup, rightGroup string) bool {
	leftSuccess, rightSuccess := left.SuccessRate, right.SuccessRate
	if leftSuccess < 0 {
		leftSuccess = 0
	}
	if rightSuccess < 0 {
		rightSuccess = 0
	}
	if leftSuccess != rightSuccess {
		return leftSuccess > rightSuccess
	}
	leftLatency, rightLatency := left.AvgLatency, right.AvgLatency
	if leftLatency < 0 {
		leftLatency = math.MaxFloat64
	}
	if rightLatency < 0 {
		rightLatency = math.MaxFloat64
	}
	if leftLatency != rightLatency {
		return leftLatency < rightLatency
	}
	return leftGroup < rightGroup
}

type smartPerfSnap struct {
	successRate  float64
	avgLatency   float64
	requestCount int64
	successCount int64
	expireAt     time.Time
}

var (
	smartPerfMu    sync.RWMutex
	smartPerfCache = map[string]smartPerfSnap{}
)

func smartPerfKey(modelName, group string) string {
	return modelName + "\x00" + group
}

func lookupGroupModelPerf(modelName, group string) (successRate, avgLatency float64, requestCount, successCount int64) {
	successRate, avgLatency = -1, -1
	now := time.Now()
	key := smartPerfKey(modelName, group)
	smartPerfMu.RLock()
	if snap, ok := smartPerfCache[key]; ok && now.Before(snap.expireAt) {
		smartPerfMu.RUnlock()
		return snap.successRate, snap.avgLatency, snap.requestCount, snap.successCount
	}
	smartPerfMu.RUnlock()

	rows, err := model.GetPerfMetrics(modelName, group, now.Unix()-5*60, now.Unix())
	if err == nil {
		var requestTotal, successTotal, latencyTotal int64
		for _, row := range rows {
			requestTotal += row.RequestCount
			successTotal += row.SuccessCount
			latencyTotal += row.TotalLatencyMs
		}
		if requestTotal > 0 {
			requestCount, successCount = requestTotal, successTotal
			successRate = float64(successTotal) / float64(requestTotal)
			if latencyTotal > 0 {
				avgLatency = float64(latencyTotal) / float64(requestTotal)
			}
		}
	}

	smartPerfMu.Lock()
	smartPerfCache[key] = smartPerfSnap{successRate: successRate, avgLatency: avgLatency, requestCount: requestCount, successCount: successCount, expireAt: now.Add(time.Minute)}
	for cachedKey, snap := range smartPerfCache {
		if now.After(snap.expireAt) {
			delete(smartPerfCache, cachedKey)
		}
	}
	smartPerfMu.Unlock()
	return successRate, avgLatency, requestCount, successCount
}
