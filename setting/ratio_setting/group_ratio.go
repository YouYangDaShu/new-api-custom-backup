package ratio_setting

import (
	"encoding/json"
	"errors"
	"math"

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

var defaultGroupSpecialUsableGroup = map[string]map[string]string{}

var defaultMembershipGroups = map[string]bool{
	"default": true,
	"vip":     true,
	"svip":    true,
}

type GroupRatioSetting struct {
	GroupRatio              *types.RWMap[string, float64]            `json:"group_ratio"`
	GroupGroupRatio         *types.RWMap[string, map[string]float64] `json:"group_group_ratio"`
	GroupSpecialUsableGroup *types.RWMap[string, map[string]string]  `json:"group_special_usable_group"`
	MembershipGroups        *types.RWMap[string, bool]               `json:"membership_groups"`
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

func MembershipGroups2JSONString() string {
	if groupRatioSetting.MembershipGroups == nil {
		return "{}"
	}
	return groupRatioSetting.MembershipGroups.MarshalJSONString()
}

func CheckMembershipGroups(jsonStr string) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(jsonStr), &raw); err != nil {
		return errors.New("membership groups must be a JSON object: " + err.Error())
	}
	if raw == nil {
		return errors.New("membership groups must be a JSON object")
	}
	for name, encoded := range raw {
		if name == "" {
			return errors.New("membership group name cannot be empty")
		}
		var enabled bool
		if err := json.Unmarshal(encoded, &enabled); err != nil {
			return errors.New("membership group " + name + " must be boolean")
		}
	}
	return nil
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

func IsMembershipGroup(name string) bool {
	if name == "" || groupRatioSetting.MembershipGroups == nil {
		return false
	}
	value, ok := groupRatioSetting.MembershipGroups.Get(name)
	return ok && value
}

func GetMembershipGroupsCopy() map[string]bool {
	if groupRatioSetting.MembershipGroups == nil {
		return map[string]bool{}
	}
	groups := groupRatioSetting.MembershipGroups.ReadAll()
	for name, enabled := range groups {
		if !enabled {
			delete(groups, name)
		}
	}
	return groups
}

// ResolveGroupRatio combines the selected model route multiplier with the
// current user's membership discount. An explicit pair override wins.
func ResolveGroupRatio(userGroup, usingGroup string) (finalRatio float64, specialRatio float64, hasSpecial bool) {
	if usingGroup == "" {
		usingGroup = "default"
	}
	if override, ok := GetGroupGroupRatio(userGroup, usingGroup); ok {
		return override, override, true
	}
	baseRatio := GetGroupRatio(usingGroup)
	if userGroup == "" || userGroup == usingGroup || !IsMembershipGroup(userGroup) {
		return baseRatio, -1, false
	}
	membershipRatio, ok := groupRatioMap.Get(userGroup)
	if !ok || membershipRatio == 1 {
		return baseRatio, -1, false
	}
	if membershipRatio < 0 {
		common.SysLog("membership group ratio < 0 ignored (treat as 1): " + userGroup)
		return baseRatio, -1, false
	}
	return baseRatio * membershipRatio, membershipRatio, true
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
	if checkGroupRatio == nil {
		return errors.New("group ratio must be a JSON object")
	}
	for name, ratio := range checkGroupRatio {
		if name == "" || math.IsNaN(ratio) || math.IsInf(ratio, 0) {
			return errors.New("invalid group ratio: " + name)
		}
		if ratio < 0 {
			return errors.New("group ratio must be not less than 0: " + name)
		}
	}
	return nil
}
