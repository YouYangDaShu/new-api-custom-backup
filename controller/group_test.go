package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetGroupsFiltersByRequestedType(t *testing.T) {
	oldRatios := ratio_setting.GroupRatio2JSONString()
	oldMembershipGroups := ratio_setting.GetGroupRatioSetting().MembershipGroups.MarshalJSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(oldRatios))
		require.NoError(t, types.LoadFromJsonString(ratio_setting.GetGroupRatioSetting().MembershipGroups, oldMembershipGroups))
	})

	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{
		"default":1,"vip":0.9,"svip":0.8,"grok havey":1.2,"private-pool":1.5
	}`))
	require.NoError(t, types.LoadFromJsonString(ratio_setting.GetGroupRatioSetting().MembershipGroups, `{
		"default":true,"vip":true,"svip":true
	}`))

	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name  string
		query string
		want  []string
	}{
		{name: "all", want: []string{"default", "grok havey", "private-pool", "svip", "vip"}},
		{name: "channel", query: "?type=channel", want: []string{"grok havey", "private-pool"}},
		{name: "user", query: "?type=user", want: []string{"default", "svip", "vip"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(recorder)
			context.Request = httptest.NewRequest(http.MethodGet, "/api/group/"+tc.query, nil)
			GetGroups(context)
			assert.Equal(t, http.StatusOK, recorder.Code)
			var payload struct {
				Success bool     `json:"success"`
				Data    []string `json:"data"`
			}
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload))
			assert.True(t, payload.Success)
			assert.Equal(t, tc.want, payload.Data)
		})
	}
}
