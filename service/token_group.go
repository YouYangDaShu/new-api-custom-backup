package service

import (
	"fmt"
	"strings"
)

const (
	TokenGroupAuto         = "auto"
	tokenGroupManualPrefix = "@manual:"
	tokenGroupSmartPrefix  = "@smart:"
)

func IsManualTokenGroup(tokenGroup string) bool {
	return strings.HasPrefix(strings.TrimSpace(tokenGroup), tokenGroupManualPrefix)
}

func ParseManualTokenGroups(tokenGroup string) []string {
	tokenGroup = strings.TrimSpace(tokenGroup)
	if !IsManualTokenGroup(tokenGroup) {
		return nil
	}
	groups := make([]string, 0)
	seen := make(map[string]struct{})
	for _, value := range strings.Split(strings.TrimPrefix(tokenGroup, tokenGroupManualPrefix), ",") {
		group := strings.TrimSpace(value)
		if group == "" || group == TokenGroupAuto || IsManualTokenGroup(group) || IsSmartTokenGroup(group) {
			continue
		}
		if _, ok := seen[group]; ok {
			continue
		}
		seen[group] = struct{}{}
		groups = append(groups, group)
	}
	return groups
}

func EncodeManualTokenGroup(groups []string) string {
	clean := make([]string, 0, len(groups))
	seen := make(map[string]struct{}, len(groups))
	for _, value := range groups {
		group := strings.TrimSpace(value)
		if group == "" || group == TokenGroupAuto || IsManualTokenGroup(group) || IsSmartTokenGroup(group) {
			continue
		}
		if _, ok := seen[group]; ok {
			continue
		}
		seen[group] = struct{}{}
		clean = append(clean, group)
	}
	if len(clean) == 0 {
		return ""
	}
	if len(clean) == 1 {
		return clean[0]
	}
	return tokenGroupManualPrefix + strings.Join(clean, ",")
}

func IsSmartTokenGroup(tokenGroup string) bool {
	return strings.HasPrefix(strings.TrimSpace(tokenGroup), tokenGroupSmartPrefix)
}

func ParseSmartMode(tokenGroup string) string {
	if !IsSmartTokenGroup(tokenGroup) {
		return ""
	}
	mode := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(tokenGroup), tokenGroupSmartPrefix)))
	switch mode {
	case SmartModePrice, "cheap", "cost":
		return SmartModePrice
	case SmartModeSuccessRate, "success", "reliability":
		return SmartModeSuccessRate
	default:
		return SmartModeBalanced
	}
}

func EncodeSmartTokenGroup(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case SmartModePrice, "cheap", "cost":
		return tokenGroupSmartPrefix + SmartModePrice
	case SmartModeSuccessRate, "success", "reliability":
		return tokenGroupSmartPrefix + SmartModeSuccessRate
	default:
		return tokenGroupSmartPrefix + SmartModeBalanced
	}
}

func IsMultiGroupToken(tokenGroup string) bool {
	tokenGroup = strings.TrimSpace(tokenGroup)
	return tokenGroup == TokenGroupAuto || IsManualTokenGroup(tokenGroup) || IsSmartTokenGroup(tokenGroup)
}

func NormalizeTokenGroup(tokenGroup string) string {
	tokenGroup = strings.TrimSpace(tokenGroup)
	if tokenGroup == "" {
		return TokenGroupAuto
	}
	if IsManualTokenGroup(tokenGroup) {
		if normalized := EncodeManualTokenGroup(ParseManualTokenGroups(tokenGroup)); normalized != "" {
			return normalized
		}
		return TokenGroupAuto
	}
	if IsSmartTokenGroup(tokenGroup) {
		return EncodeSmartTokenGroup(ParseSmartMode(tokenGroup))
	}
	return tokenGroup
}

func ResolveManualTokenGroupCandidatesForUser(userID int, tokenGroup, userGroup string) []string {
	groups := ParseManualTokenGroups(tokenGroup)
	filtered := make([]string, 0, len(groups))
	for _, group := range groups {
		if IsUserSelectableGroupForUser(userID, userGroup, group) {
			filtered = append(filtered, group)
		}
	}
	return filtered
}

func ResolveManualTokenGroupCandidates(tokenGroup, userGroup string) []string {
	groups := ParseManualTokenGroups(tokenGroup)
	filtered := make([]string, 0, len(groups))
	for _, group := range groups {
		if IsUserSelectableGroup(userGroup, group) {
			filtered = append(filtered, group)
		}
	}
	return filtered
}

func ValidateManualTokenGroupForUserID(userID int, tokenGroup, userGroup string) error {
	if !IsManualTokenGroup(tokenGroup) {
		return nil
	}
	groups := ParseManualTokenGroups(tokenGroup)
	if len(groups) == 0 {
		return fmt.Errorf("手动模式下请至少选择一个分组")
	}
	for _, group := range groups {
		if !IsUserSelectableGroupForUser(userID, userGroup, group) {
			return fmt.Errorf("无权访问 %s 分组", group)
		}
	}
	return nil
}

func ValidateManualTokenGroupForUser(tokenGroup, userGroup string) error {
	if !IsManualTokenGroup(tokenGroup) {
		return nil
	}
	groups := ParseManualTokenGroups(tokenGroup)
	if len(groups) == 0 {
		return fmt.Errorf("手动模式下请至少选择一个分组")
	}
	for _, group := range groups {
		if !IsUserSelectableGroup(userGroup, group) {
			return fmt.Errorf("无权访问 %s 分组", group)
		}
	}
	return nil
}

func DisplayTokenGroup(tokenGroup, selected string) string {
	if IsManualTokenGroup(tokenGroup) {
		if selected != "" {
			return fmt.Sprintf("manual(%s)", selected)
		}
		return "manual"
	}
	if tokenGroup == TokenGroupAuto && selected != "" {
		return fmt.Sprintf("auto(%s)", selected)
	}
	if IsSmartTokenGroup(tokenGroup) && selected != "" {
		return fmt.Sprintf("smart:%s(%s)", ParseSmartMode(tokenGroup), selected)
	}
	return tokenGroup
}
