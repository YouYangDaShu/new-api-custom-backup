package service

import (
	"testing"

	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManualTokenGroupEncoding(t *testing.T) {
	assert.Equal(t, []string{"a", "b"}, ParseManualTokenGroups(" @manual: a,b,a "))
	assert.Equal(t, "@manual:a,b", NormalizeTokenGroup(" @manual: a,b,a "))
	assert.Equal(t, "a", EncodeManualTokenGroup([]string{"a"}))
	assert.True(t, IsMultiGroupToken("@manual:a,b"))
	assert.Equal(t, "manual(b)", DisplayTokenGroup("@manual:a,b", "b"))
}

func TestManualTokenGroupValidationAndCandidateOrder(t *testing.T) {
	originalUsable := setting.UserUsableGroups2JSONString()
	originalRatios := ratio_setting.GroupRatio2JSONString()
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default","vip":"VIP"}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"vip":2}`))
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(originalUsable))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalRatios))
	})

	require.NoError(t, ValidateManualTokenGroupForUser("@manual:vip,default", "default"))
	assert.Equal(t, []string{"vip", "default"}, ResolveManualTokenGroupCandidates("@manual:vip,missing,default", "default"))
	assert.Error(t, ValidateManualTokenGroupForUser("@manual:vip,missing", "default"))
	assert.Error(t, ValidateManualTokenGroupForUser("@manual:", "default"))
}
