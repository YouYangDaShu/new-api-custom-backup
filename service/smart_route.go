package service

import (
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

// Smart routing modes (汇流同款精简版：智能自动 + 价格优先).
// Stored in token.group as:
//
//	@smart:balanced  — 综合价格、速度、成功率
//	@smart:price     — 优先更便宜的计费分组
//
// Legacy "auto" keeps fixed AutoGroups order (not smart scoring).

const (
	SmartModeBalanced = "balanced"
	SmartModePrice    = "price"
	tokenGroupSmartPrefix = "@smart:"
)

func IsSmartTokenGroup(tokenGroup string) bool {
	return strings.HasPrefix(strings.TrimSpace(tokenGroup), tokenGroupSmartPrefix)
}

func ParseSmartMode(tokenGroup string) string {
	tokenGroup = strings.TrimSpace(tokenGroup)
	if !IsSmartTokenGroup(tokenGroup) {
		return ""
	}
	mode := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(tokenGroup, tokenGroupSmartPrefix)))
	switch mode {
	case SmartModeBalanced, "auto", "smart", "":
		return SmartModeBalanced
	case SmartModePrice, "cheap", "cost":
		return SmartModePrice
	default:
		return SmartModeBalanced
	}
}

func EncodeSmartTokenGroup(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case SmartModePrice, "cheap", "cost":
		return tokenGroupSmartPrefix + SmartModePrice
	default:
		return tokenGroupSmartPrefix + SmartModeBalanced
	}
}

// IsMultiGroupToken: auto / @manual / @smart all need multi-group routing.
func isSmartOrAutoOrManual(tokenGroup string) bool {
	return tokenGroup == TokenGroupAuto || IsManualTokenGroup(tokenGroup) || IsSmartTokenGroup(tokenGroup)
}

// SmartRouteCandidate is a scored group for one model request.
type SmartRouteCandidate struct {
	Group       string
	Price       float64 // lower better (billing group ratio, pre-membership)
	SuccessRate float64 // 0~1, higher better; -1 = unknown
	AvgLatency  float64 // ms, lower better; -1 = unknown
	Score       float64 // higher better for balanced; for price we sort by Price
}

// RankGroupsForModel sorts usable groups that can serve modelName.
// mode: balanced | price
// Only returns groups that currently have at least one enabled channel for the model.
func RankGroupsForModel(userGroup, modelName, mode, requestPath string, candidates []string) []string {
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
	for _, g := range candidates {
		g = strings.TrimSpace(g)
		if g == "" || IsMembershipGroup(g) || g == TokenGroupAuto {
			continue
		}
		// Must have channel for this model (same path filter as normal select)
		ch, err := model.GetRandomSatisfiedChannel(g, modelName, 0, requestPath)
		if err != nil || ch == nil {
			continue
		}
		price := ratio_setting.GetGroupRatio(g)
		if price < 0 {
			price = 1
		}
		sr, lat := lookupGroupModelPerf(modelName, g)
		rows = append(rows, row{
			group: g,
			c: SmartRouteCandidate{
				Group:       g,
				Price:       price,
				SuccessRate: sr,
				AvgLatency:  lat,
			},
		})
	}
	if len(rows) == 0 {
		return nil
	}

	// normalize for balanced scoring
	minP, maxP := rows[0].c.Price, rows[0].c.Price
	minL, maxL := 0.0, 0.0
	hasL := false
	for i := range rows {
		p := rows[i].c.Price
		if p < minP {
			minP = p
		}
		if p > maxP {
			maxP = p
		}
		if rows[i].c.AvgLatency >= 0 {
			if !hasL {
				minL, maxL = rows[i].c.AvgLatency, rows[i].c.AvgLatency
				hasL = true
			} else {
				if rows[i].c.AvgLatency < minL {
					minL = rows[i].c.AvgLatency
				}
				if rows[i].c.AvgLatency > maxL {
					maxL = rows[i].c.AvgLatency
				}
			}
		}
	}

	for i := range rows {
		// priceNorm: 0 = cheapest, 1 = most expensive
		priceNorm := 0.0
		if maxP > minP {
			priceNorm = (rows[i].c.Price - minP) / (maxP - minP)
		}
		// success: unknown → 0.7 neutral
		success := rows[i].c.SuccessRate
		if success < 0 {
			success = 0.7
		}
		// latencyNorm: 0 = fastest, 1 = slowest; unknown → 0.5
		latNorm := 0.5
		if rows[i].c.AvgLatency >= 0 && hasL && maxL > minL {
			latNorm = (rows[i].c.AvgLatency - minL) / (maxL - minL)
		} else if rows[i].c.AvgLatency >= 0 && hasL {
			latNorm = 0
		}
		// higher score better
		// balanced: 40% price + 35% success + 25% speed
		rows[i].c.Score = (1-priceNorm)*0.40 + success*0.35 + (1-latNorm)*0.25
	}

	if mode == SmartModePrice {
		sort.SliceStable(rows, func(i, j int) bool {
			if rows[i].c.Price != rows[j].c.Price {
				return rows[i].c.Price < rows[j].c.Price
			}
			// tie-break: higher success, then lower latency, then name
			si, sj := rows[i].c.SuccessRate, rows[j].c.SuccessRate
			if si < 0 {
				si = 0
			}
			if sj < 0 {
				sj = 0
			}
			if si != sj {
				return si > sj
			}
			li, lj := rows[i].c.AvgLatency, rows[j].c.AvgLatency
			if li < 0 {
				li = math.MaxFloat64
			}
			if lj < 0 {
				lj = math.MaxFloat64
			}
			if li != lj {
				return li < lj
			}
			return rows[i].group < rows[j].group
		})
	} else {
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

	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.group)
	}
	return out
}

// --- lightweight perf cache for smart routing (model+group, ~1 min) ---

type smartPerfSnap struct {
	successRate float64 // -1 unknown
	avgLatency  float64 // -1 unknown
	expireAt    time.Time
}

var (
	smartPerfMu    sync.RWMutex
	smartPerfCache = map[string]smartPerfSnap{}
)

func smartPerfKey(modelName, group string) string {
	return modelName + "\x00" + group
}

func lookupGroupModelPerf(modelName, group string) (successRate, avgLatency float64) {
	successRate, avgLatency = -1, -1
	key := smartPerfKey(modelName, group)
	now := time.Now()
	smartPerfMu.RLock()
	if snap, ok := smartPerfCache[key]; ok && now.Before(snap.expireAt) {
		smartPerfMu.RUnlock()
		return snap.successRate, snap.avgLatency
	}
	smartPerfMu.RUnlock()

	// last 6 hours from DB (cheap aggregate)
	endTs := now.Unix()
	startTs := endTs - 6*3600
	rows, err := model.GetPerfMetrics(modelName, group, startTs, endTs)
	if err == nil && len(rows) > 0 {
		var req, okCnt, latSum int64
		for _, r := range rows {
			req += r.RequestCount
			okCnt += r.SuccessCount
			latSum += r.TotalLatencyMs
		}
		if req > 0 {
			successRate = float64(okCnt) / float64(req)
			if latSum > 0 {
				avgLatency = float64(latSum) / float64(req)
			}
		}
	}

	smartPerfMu.Lock()
	smartPerfCache[key] = smartPerfSnap{
		successRate: successRate,
		avgLatency:  avgLatency,
		expireAt:    now.Add(60 * time.Second),
	}
	// crude size cap
	if len(smartPerfCache) > 5000 {
		for k, v := range smartPerfCache {
			if now.After(v.expireAt) {
				delete(smartPerfCache, k)
			}
		}
	}
	smartPerfMu.Unlock()
	return successRate, avgLatency
}

// SmartRoutePool returns model-pool groups the user may use for smart routing.
func SmartRoutePool(userGroup string) []string {
	usable := GetUserTokenSelectableGroups(userGroup)
	out := make([]string, 0, len(usable))
	for name := range usable {
		if name == "" || name == TokenGroupAuto || IsMembershipGroup(name) {
			continue
		}
		if !ratio_setting.ContainsGroupRatio(name) {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
