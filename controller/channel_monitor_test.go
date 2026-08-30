package controller

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateMonitorFieldsAcceptsUnicodeNameAndValidModels(t *testing.T) {
	extra, err := validateMonitorFields(strings.Repeat("中", 128), "default", "model-a", []string{"model-b", "model-b", "model-a"}, 60, []string{"model-a", "model-b"})
	require.NoError(t, err)
	assert.Equal(t, []string{"model-b"}, extra)
}

func TestValidateMonitorFieldsRejectsNameOver128Runes(t *testing.T) {
	_, err := validateMonitorFields(strings.Repeat("中", 129), "default", "model-a", nil, 60, []string{"model-a"})
	require.EqualError(t, err, "invalid monitor name")
}

func TestValidateMonitorFieldsRejectsControlCharactersInName(t *testing.T) {
	tests := []string{"line\nbreak", "hidden\u0000control", "tab\tname", "\ntrimmed name"}
	for _, name := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := validateMonitorFields(name, "default", "model-a", nil, 60, []string{"model-a"})
			require.EqualError(t, err, "invalid monitor name")
		})
	}
}

func TestAdminGetChannelMonitorModelsRejectsInvalidGroup(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/channel-monitor/admin/models?group=%0Adefault", nil)
	AdminGetChannelMonitorModels(c)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "invalid monitor group")
}

func TestValidateMonitorFieldsRejectsUnavailableModels(t *testing.T) {
	t.Run("primary", func(t *testing.T) {
		_, err := validateMonitorFields("monitor", "default", "missing", nil, 60, []string{"model-a"})
		require.EqualError(t, err, "primary model is not available in selected group")
	})
	t.Run("extra", func(t *testing.T) {
		_, err := validateMonitorFields("monitor", "default", "model-a", []string{"missing"}, 60, []string{"model-a"})
		require.EqualError(t, err, "one or more extra models are not available in selected group")
	})
}

func TestAdminGetChannelMonitorModelsReturnsOnlySortedModelNames(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.Create(&[]model.Ability{
		{Group: "monitor-group", Model: "model-z", ChannelId: 1, Enabled: true},
		{Group: "monitor-group", Model: "model-a", ChannelId: 2, Enabled: true},
		{Group: "monitor-group", Model: "model-z", ChannelId: 3, Enabled: true},
		{Group: "monitor-group", Model: "disabled-model", ChannelId: 4, Enabled: false},
		{Group: "other-group", Model: "other-model", ChannelId: 5, Enabled: true},
	}).Error)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/channel-monitor/admin/models?group=monitor-group", nil)
	AdminGetChannelMonitorModels(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool     `json:"success"`
		Data    []string `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.True(t, response.Success)
	assert.Equal(t, []string{"model-a", "model-z"}, response.Data)
	assert.NotContains(t, recorder.Body.String(), "channel_id")
	assert.NotContains(t, recorder.Body.String(), `"key"`)
	assert.NotContains(t, recorder.Body.String(), "base_url")
	assert.NotContains(t, recorder.Body.String(), "monitor-group")
}

func TestMonitorPublicViewOmitsManagementFields(t *testing.T) {
	row := model.ChannelMonitor{ID: 42, Name: "Public", Group: "default", PrimaryModel: "model-a", IntervalSeconds: 60, Enabled: false, LastCheckedAt: 123}
	view := monitorPublicView(row, []model.ChannelMonitorCheck{{MonitorID: 42, Model: "model-a", Status: model.ChannelMonitorStatusFailed, ErrorBrief: "secret", CheckedAt: 123}})
	require.Equal(t, "Public", view.Name)
	require.NotContains(t, view.Models["model-a"].Status, "secret")
	require.NotContains(t, string(mustMarshal(view)), "id")
	require.NotContains(t, string(mustMarshal(view)), "interval_seconds")
	require.NotContains(t, string(mustMarshal(view)), "enabled")
	require.NotContains(t, string(mustMarshal(view)), "error_summary")
}

func TestMonitorViewUsesFullWindowButReturnsSixtyHistoryPoints(t *testing.T) {
	now := time.Now().Unix()
	checks := make([]model.ChannelMonitorCheck, 0, 100)
	for i := 0; i < 100; i++ {
		status := model.ChannelMonitorStatusFailed
		if i < 60 {
			status = model.ChannelMonitorStatusSuccess
		}
		checks = append(checks, model.ChannelMonitorCheck{MonitorID: 43, Model: "model-a", Status: status, CheckedAt: now - int64(i)})
	}
	view := monitorAdminView(model.ChannelMonitor{ID: 43, PrimaryModel: "model-a"}, checks)
	stat := view.Models["model-a"]
	require.Len(t, stat.History, channelMonitorHistorySlots)
	assert.InDelta(t, 60, stat.Availability["720h"], 0.001)
}

func TestParseMonitorIDRejectsInvalidValues(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Params = gin.Params{{Key: "id", Value: "abc"}}
	_, err := parseMonitorID(c)
	require.Error(t, err)
}

func TestTokenRankingHistoryDTOOmitsIdentityFields(t *testing.T) {
	b := mustMarshal(tokenRankingHistoryRow{WeekStart: "2026-08-24", Rank: 1, TotalTokens: 10, RewardQuota: 2, CreatedAt: 1, RewardStatus: "awarded"})
	s := string(b)
	require.NotContains(t, s, "user_id")
	require.NotContains(t, s, "email")
	require.Contains(t, s, "reward_status")
}

func TestGetChannelMonitorsBlocksWhenDisabled(t *testing.T) {
	previous := common.ChannelMonitorEnabled
	common.ChannelMonitorEnabled = false
	t.Cleanup(func() { common.ChannelMonitorEnabled = previous })
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	GetChannelMonitors(c)
	require.Equal(t, http.StatusForbidden, recorder.Code)
	require.NotContains(t, recorder.Body.String(), "interval_seconds")
}

func TestAdminRunChannelMonitorUsesCompleteThirtyDaySeries(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.ChannelMonitor{}, &model.ChannelMonitorCheck{}))
	require.NoError(t, db.Create(&model.User{
		Id:       1099,
		Username: "monitor-root",
		Password: "monitor-password",
		Role:     common.RoleRootUser,
		Status:   common.UserStatusEnabled,
	}).Error)

	extraModels, err := common.Marshal([]string{"model-b"})
	require.NoError(t, err)
	monitor := model.ChannelMonitor{
		Name:            "manual complete history",
		Group:           "monitor-group",
		PrimaryModel:    "model-a",
		ExtraModelsJSON: string(extraModels),
		IntervalSeconds: 30,
		Enabled:         true,
	}
	require.NoError(t, db.Create(&monitor).Error)

	now := time.Now().Unix()
	checks := make([]model.ChannelMonitorCheck, 0, 1200)
	for _, modelName := range []string{"model-a", "model-b"} {
		for i := 0; i < 600; i++ {
			status := model.ChannelMonitorStatusSuccess
			if i >= 500 {
				status = model.ChannelMonitorStatusFailed
			}
			checks = append(checks, model.ChannelMonitorCheck{
				MonitorID: monitor.ID,
				Model:     modelName,
				Status:    status,
				CheckedAt: now - int64(i+1),
			})
		}
	}
	require.Greater(t, len(checks), 500)
	require.NoError(t, db.CreateInBatches(&checks, 500).Error)

	previous := common.ChannelMonitorEnabled
	common.ChannelMonitorEnabled = false
	t.Cleanup(func() { common.ChannelMonitorEnabled = previous })

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/channel-monitor/admin/"+strconv.FormatInt(monitor.ID, 10)+"/run", nil)
	c.Params = gin.Params{{Key: "id", Value: strconv.FormatInt(monitor.ID, 10)}}
	AdminRunChannelMonitor(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool         `json:"success"`
		Data    monitorAdmin `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	assert.False(t, response.Data.ModuleEnabled)
	for _, modelName := range []string{"model-a", "model-b"} {
		stat, ok := response.Data.Models[modelName]
		require.True(t, ok)
		assert.InDelta(t, float64(500)/601*100, stat.Availability["720h"], 0.001)
	}
}

func mustMarshal(v any) []byte {
	b, err := common.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
