package ratio_setting

import (
	"testing"

	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveGroupRatioCombinesRouteAndMembership(t *testing.T) {
	oldRatios := GroupRatio2JSONString()
	oldOverrides := GroupGroupRatio2JSONString()
	oldMembershipGroups := GetGroupRatioSetting().MembershipGroups.MarshalJSONString()
	t.Cleanup(func() {
		require.NoError(t, UpdateGroupRatioByJSONString(oldRatios))
		require.NoError(t, UpdateGroupGroupRatioByJSONString(oldOverrides))
		require.NoError(t, types.LoadFromJsonString(GetGroupRatioSetting().MembershipGroups, oldMembershipGroups))
	})

	require.NoError(t, UpdateGroupRatioByJSONString(`{"default":1,"vip":0.9,"svip":0.8,"grok":1.2}`))
	require.NoError(t, UpdateGroupGroupRatioByJSONString(`{}`))
	require.NoError(t, types.LoadFromJsonString(GetGroupRatioSetting().MembershipGroups, `{"default":true,"vip":true,"svip":true}`))

	final, special, hasSpecial := ResolveGroupRatio("vip", "grok")
	assert.InDelta(t, 1.08, final, 0.000001)
	assert.Equal(t, 0.9, special)
	assert.True(t, hasSpecial)

	final, special, hasSpecial = ResolveGroupRatio("default", "grok")
	assert.Equal(t, 1.2, final)
	assert.Equal(t, -1.0, special)
	assert.False(t, hasSpecial)

	final, special, hasSpecial = ResolveGroupRatio("grok", "default")
	assert.Equal(t, 1.0, final)
	assert.Equal(t, -1.0, special)
	assert.False(t, hasSpecial)

	require.NoError(t, UpdateGroupGroupRatioByJSONString(`{"vip":{"grok":0.7}}`))
	final, special, hasSpecial = ResolveGroupRatio("vip", "grok")
	assert.InDelta(t, 0.7, final, 0.000001)
	assert.Equal(t, 0.7, special)
	assert.True(t, hasSpecial)

	require.NoError(t, UpdateGroupGroupRatioByJSONString(`{}`))
	require.NoError(t, UpdateGroupRatioByJSONString(`{"vip":0,"grok":1.25}`))
	final, special, hasSpecial = ResolveGroupRatio("vip", "grok")
	assert.Zero(t, final)
	assert.Zero(t, special)
	assert.True(t, hasSpecial)
}

func TestMembershipGroupConfiguration(t *testing.T) {
	oldMembershipGroups := GetGroupRatioSetting().MembershipGroups.MarshalJSONString()
	t.Cleanup(func() {
		require.NoError(t, types.LoadFromJsonString(GetGroupRatioSetting().MembershipGroups, oldMembershipGroups))
	})

	require.NoError(t, types.LoadFromJsonString(GetGroupRatioSetting().MembershipGroups, `{"default":true,"vip":true,"svip":false,"gold":true}`))
	assert.True(t, IsMembershipGroup("default"))
	assert.True(t, IsMembershipGroup("vip"))
	assert.False(t, IsMembershipGroup("svip"))
	assert.True(t, IsMembershipGroup("gold"))
	assert.False(t, IsMembershipGroup("grok"))
}

func TestGetMembershipGroupsCopyDoesNotReintroduceRemovedDefaults(t *testing.T) {
	settings := GetGroupRatioSetting()
	oldMembershipGroups := settings.MembershipGroups.MarshalJSONString()
	t.Cleanup(func() {
		require.NoError(t, types.LoadFromJsonString(settings.MembershipGroups, oldMembershipGroups))
	})

	require.NoError(t, types.LoadFromJsonString(settings.MembershipGroups, `{"vip":true}`))

	assert.Equal(t, map[string]bool{"vip": true}, GetMembershipGroupsCopy())
	assert.False(t, IsMembershipGroup("default"))
	assert.False(t, IsMembershipGroup("svip"))
}
