package ratio_setting

import (
	"encoding/json"
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/types"
)

var defaultGroupRatio = map[string]float64{
	"default": 1,
	"vip":     1,
	"svip":    1,
}

var groupRatioMap = types.NewRWMap[string, float64]()

var defaultGroupGroupRatio = map[string]map[string]float64{
	"vip": {
		"edit_this": 0.9,
	},
}

var groupGroupRatioMap = types.NewRWMap[string, map[string]float64]()

var defaultGroupSpecialUsableGroup = map[string]map[string]string{
	"vip": {
		"append_1":   "vip_special_group_1",
		"-:remove_1": "vip_removed_group_1",
	},
}

// defaultMembershipGroups: user-tier / discount rows (not token model pools).
// Empty admin config falls back to these names. Billing still uses GroupRatio[user].
var defaultMembershipGroups = map[string]bool{
	"default": true,
	"VIP":     true,
	"vip":     true,
	"svip":    true,
	"ssvip":   true,
}

type GroupRatioSetting struct {
	GroupRatio              *types.RWMap[string, float64]            `json:"group_ratio"`
	GroupGroupRatio         *types.RWMap[string, map[string]float64] `json:"group_group_ratio"`
	GroupSpecialUsableGroup *types.RWMap[string, map[string]string]  `json:"group_special_usable_group"`
	// MembershipGroups: group name -> true means membership tier (discount only, not token-selectable).
	MembershipGroups *types.RWMap[string, bool] `json:"membership_groups"`
}

var groupRatioSetting GroupRatioSetting

func init() {
	groupSpecialUsableGroup := types.NewRWMap[string, map[string]string]()
	groupSpecialUsableGroup.AddAll(defaultGroupSpecialUsableGroup)

	groupRatioMap.AddAll(defaultGroupRatio)
	groupGroupRatioMap.AddAll(defaultGroupGroupRatio)

	membershipGroups := types.NewRWMap[string, bool]()
	membershipGroups.AddAll(defaultMembershipGroups)

	groupRatioSetting = GroupRatioSetting{
		GroupSpecialUsableGroup: groupSpecialUsableGroup,
		GroupRatio:              groupRatioMap,
		GroupGroupRatio:         groupGroupRatioMap,
		MembershipGroups:        membershipGroups,
	}

	config.GlobalConfig.Register("group_ratio_setting", &groupRatioSetting)
}

func GetGroupRatioSetting() *GroupRatioSetting {
	if groupRatioSetting.GroupSpecialUsableGroup == nil {
		groupRatioSetting.GroupSpecialUsableGroup = types.NewRWMap[string, map[string]string]()
		groupRatioSetting.GroupSpecialUsableGroup.AddAll(defaultGroupSpecialUsableGroup)
	}
	if groupRatioSetting.MembershipGroups == nil {
		groupRatioSetting.MembershipGroups = types.NewRWMap[string, bool]()
		groupRatioSetting.MembershipGroups.AddAll(defaultMembershipGroups)
	}
	return &groupRatioSetting
}

func GetGroupRatioCopy() map[string]float64 {
	return groupRatioMap.ReadAll()
}

func ContainsGroupRatio(name string) bool {
	_, ok := groupRatioMap.Get(name)
	return ok
}

func GroupRatio2JSONString() string {
	return groupRatioMap.MarshalJSONString()
}

func UpdateGroupRatioByJSONString(jsonStr string) error {
	return types.LoadFromJsonString(groupRatioMap, jsonStr)
}

func GetGroupRatio(name string) float64 {
	ratio, ok := groupRatioMap.Get(name)
	if !ok {
		common.SysLog("group ratio not found: " + name)
		return 1
	}
	return ratio
}

func GetGroupGroupRatio(userGroup, usingGroup string) (float64, bool) {
	gp, ok := groupGroupRatioMap.Get(userGroup)
	if !ok {
		return -1, false
	}
	ratio, ok := gp[usingGroup]
	if !ok {
		return -1, false
	}
	return ratio, true
}

// IsMembershipGroup reports whether name is marked as a membership/discount tier.
// - Explicit config entry wins (true/false).
// - Name absent from a partial config → fall back to built-in defaults (half-config safe).
// - Config unset/empty → built-in defaults only.
func IsMembershipGroup(name string) bool {
	if name == "" {
		return false
	}
	if groupRatioSetting.MembershipGroups != nil && groupRatioSetting.MembershipGroups.Len() > 0 {
		if v, ok := groupRatioSetting.MembershipGroups.Get(name); ok {
			return v
		}
		// Half-config: only listed keys were toggled; unlisted keep default membership set.
		return defaultMembershipGroups[name]
	}
	return defaultMembershipGroups[name]
}

// GetMembershipGroupsCopy returns configured membership flags (name -> true).
func GetMembershipGroupsCopy() map[string]bool {
	if groupRatioSetting.MembershipGroups == nil {
		return map[string]bool{}
	}
	out := make(map[string]bool)
	for k, v := range groupRatioSetting.MembershipGroups.ReadAll() {
		if v {
			out[k] = true
		}
	}
	return out
}

// ResolveGroupRatio returns the final group multiplier used for billing.
//
// Priority:
//  1. Explicit special rule GroupGroupRatio[userGroup][usingGroup] (absolute override)
//  2. Else: billing-group ratio × membership discount
//     membership = GroupRatio[userGroup] when userGroup != usingGroup and ratio is configured
//     (e.g. user=svip 0.85, token uses grok 0.05 → final 0.05*0.85)
//     When userGroup == usingGroup, only the billing-group ratio is used (no double multiply).
//
// specialRatio is the membership discount applied (or the absolute override value when hasSpecial).
// hasSpecial is true when either an explicit override exists or a membership discount was applied.
func ResolveGroupRatio(userGroup, usingGroup string) (finalRatio float64, specialRatio float64, hasSpecial bool) {
	if usingGroup == "" {
		usingGroup = "default"
	}
	// 1) Absolute per-pair override still wins (advanced / one-off prices).
	if override, ok := GetGroupGroupRatio(userGroup, usingGroup); ok {
		return override, override, true
	}

	base := GetGroupRatio(usingGroup)

	// Same group: no extra membership layer (avoid svip token × svip discount twice).
	if userGroup == "" || userGroup == usingGroup {
		return base, -1, false
	}

	// Membership discount only when userGroup is a real membership tier.
	// Prevents misconfigured user group names that match a model-pool ratio from
	// accidentally multiplying billing (e.g. user wrongly set to "grok" → ×0.05).
	if !IsMembershipGroup(userGroup) {
		return base, -1, false
	}

	// Membership discount from the user's group row in GroupRatio (e.g. svip=0.85).
	// Only apply when that group is explicitly configured; missing → treat as 1 (no discount).
	// ratio == 1 means "no extra discount" (e.g. VIP=1), still a membership tier but no multiply effect.
	// ratio <= 0 is treated as misconfig for membership tiers: do NOT zero-out billing (would be free).
	if membership, ok := groupRatioMap.Get(userGroup); ok && membership > 0 && membership != 1 {
		return base * membership, membership, true
	}
	if membership, ok := groupRatioMap.Get(userGroup); ok && membership <= 0 {
		common.SysLog("membership group ratio <= 0 ignored (treat as 1): " + userGroup)
	}
	return base, -1, false
}

func GroupGroupRatio2JSONString() string {
	return groupGroupRatioMap.MarshalJSONString()
}

func UpdateGroupGroupRatioByJSONString(jsonStr string) error {
	return types.LoadFromJsonString(groupGroupRatioMap, jsonStr)
}

func CheckGroupRatio(jsonStr string) error {
	checkGroupRatio := make(map[string]float64)
	err := json.Unmarshal([]byte(jsonStr), &checkGroupRatio)
	if err != nil {
		return err
	}
	for name, ratio := range checkGroupRatio {
		if ratio < 0 {
			return errors.New("group ratio must be not less than 0: " + name)
		}
	}
	return nil
}
