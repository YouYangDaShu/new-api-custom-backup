package service

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

// Token group encodings (aligned with 汇流 manual multi-group tokens):
//
//	"" / "default" / "grok"  → single billing group
//	"auto"                   → site AutoGroups list
//	"@manual:a,b,c"          → token-level priority list (order = priority)
//
// Billing MUST always use the actually selected real group, never the raw
// "@manual:..." string.

const (
	TokenGroupAuto          = "auto"
	tokenGroupManualPrefix  = "@manual:"
)

// IsMultiGroupToken returns true when the token uses auto / @manual: / @smart: routing.
func IsMultiGroupToken(tokenGroup string) bool {
	return tokenGroup == TokenGroupAuto || IsManualTokenGroup(tokenGroup) || IsSmartTokenGroup(tokenGroup)
}

// IsManualTokenGroup reports whether group is a @manual: multi-group encoding.
func IsManualTokenGroup(tokenGroup string) bool {
	return strings.HasPrefix(tokenGroup, tokenGroupManualPrefix)
}

// ParseManualTokenGroups extracts ordered real groups from "@manual:a,b,c".
// Empty / invalid encoding returns nil.
func ParseManualTokenGroups(tokenGroup string) []string {
	if !IsManualTokenGroup(tokenGroup) {
		return nil
	}
	raw := strings.TrimSpace(strings.TrimPrefix(tokenGroup, tokenGroupManualPrefix))
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, p := range parts {
		g := strings.TrimSpace(p)
		if g == "" || g == TokenGroupAuto || IsManualTokenGroup(g) {
			continue
		}
		if _, ok := seen[g]; ok {
			continue
		}
		seen[g] = struct{}{}
		out = append(out, g)
	}
	return out
}

// EncodeManualTokenGroup builds the storage string.
// One group → plain name (汇流 same). Multiple → @manual:a,b,c.
func EncodeManualTokenGroup(groups []string) string {
	clean := make([]string, 0, len(groups))
	seen := make(map[string]struct{}, len(groups))
	for _, g := range groups {
		g = strings.TrimSpace(g)
		if g == "" || g == TokenGroupAuto || IsManualTokenGroup(g) {
			continue
		}
		if _, ok := seen[g]; ok {
			continue
		}
		seen[g] = struct{}{}
		clean = append(clean, g)
	}
	if len(clean) == 0 {
		return ""
	}
	if len(clean) == 1 {
		return clean[0]
	}
	return tokenGroupManualPrefix + strings.Join(clean, ",")
}

// NormalizeTokenGroup trims and normalizes token group input from API/UI.
// For @manual: re-encodes to drop empties/dupes.
func NormalizeTokenGroup(tokenGroup string) string {
	tokenGroup = strings.TrimSpace(tokenGroup)
	if tokenGroup == "" {
		return ""
	}
	if IsManualTokenGroup(tokenGroup) {
		return EncodeManualTokenGroup(ParseManualTokenGroups(tokenGroup))
	}
	if IsSmartTokenGroup(tokenGroup) {
		return EncodeSmartTokenGroup(ParseSmartMode(tokenGroup))
	}
	return tokenGroup
}

// ResolveTokenGroupCandidates returns the ordered real groups this token may use.
//
//	auto     → GetUserAutoGroup(userGroup)
//	@manual: → parsed list, filtered by user usable groups
//	single   → [that group]
func ResolveTokenGroupCandidates(tokenGroup, userGroup string) []string {
	tokenGroup = strings.TrimSpace(tokenGroup)
	if tokenGroup == TokenGroupAuto {
		return GetUserAutoGroup(userGroup)
	}
	if IsSmartTokenGroup(tokenGroup) {
		// Smart modes re-rank per model at select time; pool = all token-selectable groups.
		return SmartRoutePool(userGroup)
	}
	if IsManualTokenGroup(tokenGroup) {
		usable := GetUserTokenSelectableGroups(userGroup)
		raw := ParseManualTokenGroups(tokenGroup)
		out := make([]string, 0, len(raw))
		for _, g := range raw {
			if IsMembershipGroup(g) {
				continue
			}
			if _, ok := usable[g]; ok {
				out = append(out, g)
			}
		}
		return out
	}
	if tokenGroup == "" {
		if userGroup != "" {
			return []string{userGroup}
		}
		return nil
	}
	return []string{tokenGroup}
}

// ValidateTokenGroupForUser checks create/update token.group for a user.
// Allows: empty, auto, single usable group, or @manual: with ≥1 usable groups.
func ValidateTokenGroupForUser(tokenGroup, userGroup string) error {
	tokenGroup = NormalizeTokenGroup(tokenGroup)
	if tokenGroup == "" {
		return nil
	}
	// Token may only bind real model pools (+ auto), not membership tiers.
	usable := GetUserTokenSelectableGroups(userGroup)
	if tokenGroup == TokenGroupAuto {
		if _, ok := usable[TokenGroupAuto]; !ok {
			// auto is a virtual group; allow if AutoGroups intersects usable
			if len(GetUserAutoGroup(userGroup)) == 0 {
				return fmt.Errorf("无权使用 auto 分组，或自动分组列表为空")
			}
		}
		return nil
	}
	if IsSmartTokenGroup(tokenGroup) {
		if len(SmartRoutePool(userGroup)) == 0 {
			return fmt.Errorf("智能路由可用模型分组为空")
		}
		_ = ParseSmartMode(tokenGroup) // normalize accept
		return nil
	}
	if IsManualTokenGroup(tokenGroup) {
		groups := ParseManualTokenGroups(tokenGroup)
		if len(groups) == 0 {
			return fmt.Errorf("手动模式下请至少选择一个分组")
		}
		for _, g := range groups {
			if IsMembershipGroup(g) {
				return fmt.Errorf("分组 %s 是会员档，不能作为令牌模型分组", g)
			}
			if _, ok := usable[g]; !ok {
				return fmt.Errorf("无权访问 %s 分组", g)
			}
			if !ratio_setting.ContainsGroupRatio(g) {
				return fmt.Errorf("分组 %s 已被弃用", g)
			}
		}
		return nil
	}
	// single
	if IsMembershipGroup(tokenGroup) {
		return fmt.Errorf("分组 %s 是会员档，不能作为令牌模型分组", tokenGroup)
	}
	if _, ok := usable[tokenGroup]; !ok {
		return fmt.Errorf("无权访问 %s 分组", tokenGroup)
	}
	if !ratio_setting.ContainsGroupRatio(tokenGroup) {
		return fmt.Errorf("分组 %s 已被弃用", tokenGroup)
	}
	return nil
}

// DisplayTokenGroup formats group for error messages / logs.
func DisplayTokenGroup(tokenGroup, selected string) string {
	if IsManualTokenGroup(tokenGroup) {
		if selected != "" {
			return fmt.Sprintf("manual(%s)", selected)
		}
		return "manual"
	}
	if IsSmartTokenGroup(tokenGroup) {
		mode := ParseSmartMode(tokenGroup)
		if selected != "" {
			return fmt.Sprintf("smart:%s(%s)", mode, selected)
		}
		return "smart:" + mode
	}
	if tokenGroup == TokenGroupAuto {
		if selected != "" {
			return fmt.Sprintf("auto(%s)", selected)
		}
		return TokenGroupAuto
	}
	if selected != "" && selected != tokenGroup {
		return selected
	}
	return tokenGroup
}
