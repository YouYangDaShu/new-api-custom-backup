package controller

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProbeChannelMonitorModelFollowsRetryPriorities(t *testing.T) {
	retryableError := func() testResult {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		err := types.NewOpenAIError(errors.New("upstream failed"), types.ErrorCodeBadResponse, http.StatusInternalServerError)
		return testResult{context: c, localErr: err, newAPIError: err}
	}
	success := func() testResult {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		return testResult{context: c}
	}

	tests := []struct {
		name         string
		retryTimes   int
		results      map[int]testResult
		wantAttempts []int
		wantStatus   string
	}{
		{
			name:         "highest priority succeeds",
			retryTimes:   1,
			results:      map[int]testResult{101: success()},
			wantAttempts: []int{101},
			wantStatus:   model.ChannelMonitorStatusSuccess,
		},
		{
			name:         "second priority succeeds with one retry",
			retryTimes:   1,
			results:      map[int]testResult{101: retryableError(), 102: success()},
			wantAttempts: []int{101, 102},
			wantStatus:   model.ChannelMonitorStatusSuccess,
		},
		{
			name:         "zero retries stops after first failure",
			retryTimes:   0,
			results:      map[int]testResult{101: retryableError(), 102: success()},
			wantAttempts: []int{101},
			wantStatus:   model.ChannelMonitorStatusFailed,
		},
		{
			name:         "two failures are unavailable",
			retryTimes:   1,
			results:      map[int]testResult{101: retryableError(), 102: retryableError()},
			wantAttempts: []int{101, 102},
			wantStatus:   model.ChannelMonitorStatusFailed,
		},
		{
			name:         "third priority succeeds with two retries",
			retryTimes:   2,
			results:      map[int]testResult{101: retryableError(), 102: retryableError(), 103: success()},
			wantAttempts: []int{101, 102, 103},
			wantStatus:   model.ChannelMonitorStatusSuccess,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			channels := []*model.Channel{{Id: 101}, {Id: 102}, {Id: 103}}
			var selectedRetries []int
			selector := func(group, modelName string, retry int, requestPath string) (*model.Channel, error) {
				assert.Equal(t, "monitor-group", group)
				assert.Equal(t, "model-a", modelName)
				assert.Empty(t, requestPath)
				selectedRetries = append(selectedRetries, retry)
				return channels[retry], nil
			}
			var attempts []int
			probe := func(probeCtx context.Context, channel *model.Channel, _ int, modelName, endpointType string, stream bool) testResult {
				assert.Equal(t, "model-a", modelName)
				assert.Empty(t, endpointType)
				assert.False(t, stream)
				_, monitorProbe := probeCtx.Value(channelMonitorProbeContextKey{}).(bool)
				assert.True(t, monitorProbe)
				attempts = append(attempts, channel.Id)
				return tt.results[channel.Id]
			}

			check := probeChannelMonitorModel(context.Background(), 77, "monitor-group", "model-a", 9, tt.retryTimes, selector, probe)

			assert.Equal(t, tt.wantAttempts, attempts)
			assert.Equal(t, len(tt.wantAttempts), len(selectedRetries))
			for retry := range selectedRetries {
				assert.Equal(t, retry, selectedRetries[retry])
			}
			assert.Equal(t, tt.wantStatus, check.Status)
			assert.Equal(t, int64(77), check.MonitorID)
			assert.Equal(t, "model-a", check.Model)
			assert.NotContains(t, check.ErrorBrief, "upstream failed")
		})
	}
}

func TestProbeChannelMonitorModelDoesNotRetryNonRetryableErrors(t *testing.T) {
	tests := []struct {
		name string
		err  *types.NewAPIError
	}{
		{
			name: "status is not retryable",
			err:  types.NewOpenAIError(errors.New("bad request"), types.ErrorCodeBadResponse, http.StatusBadRequest),
		},
		{
			name: "skip retry",
			err:  types.NewOpenAIError(errors.New("do not retry"), types.ErrorCodeChannelParamOverrideInvalid, http.StatusInternalServerError, types.ErrOptionWithSkipRetry()),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var selectedRetries []int
			selector := func(_, _ string, retry int, _ string) (*model.Channel, error) {
				selectedRetries = append(selectedRetries, retry)
				return &model.Channel{Id: retry + 1}, nil
			}
			probe := func(_ context.Context, _ *model.Channel, _ int, _, _ string, _ bool) testResult {
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				return testResult{context: c, localErr: tt.err, newAPIError: tt.err}
			}

			check := probeChannelMonitorModel(context.Background(), 78, "monitor-group", "model-a", 9, 2, selector, probe)

			assert.Equal(t, []int{0}, selectedRetries)
			assert.Equal(t, model.ChannelMonitorStatusFailed, check.Status)
		})
	}
}

func TestMonitorChannelSelectionExcludesDisabledAndModelMismatch(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	previousMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { common.MemoryCacheEnabled = previousMemoryCacheEnabled })

	priorityHigh := int64(30)
	priorityMedium := int64(20)
	priorityLow := int64(10)
	channels := []*model.Channel{
		{Name: "disabled-high", Status: common.ChannelStatusManuallyDisabled, Group: "monitor-group", Models: "model-a", Priority: &priorityHigh},
		{Name: "wrong-model", Status: common.ChannelStatusEnabled, Group: "monitor-group", Models: "model-b", Priority: &priorityMedium},
		{Name: "eligible", Status: common.ChannelStatusEnabled, Group: "monitor-group", Models: "model-a", Priority: &priorityLow},
	}
	for _, channel := range channels {
		require.NoError(t, channel.Insert())
	}
	t.Cleanup(func() {
		require.NoError(t, db.Where("name IN ?", []string{"disabled-high", "wrong-model", "eligible"}).Delete(&model.Channel{}).Error)
	})

	selected, err := model.GetRandomSatisfiedChannel("monitor-group", "model-a", 0, "")
	require.NoError(t, err)
	require.NotNil(t, selected)
	assert.Equal(t, channels[2].Id, selected.Id)
}

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

func TestChannelMonitorSchedulerUsesShortestEnabledInterval(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.ChannelMonitor{}))
	monitors := []model.ChannelMonitor{
		{Name: "slow", Group: "default", PrimaryModel: "model-a", IntervalSeconds: 300, Enabled: true},
		{Name: "fast", Group: "default", PrimaryModel: "model-a", IntervalSeconds: 60, Enabled: true},
		{Name: "disabled", Group: "default", PrimaryModel: "model-a", IntervalSeconds: 30, Enabled: false},
	}
	require.NoError(t, db.Create(&monitors).Error)
	t.Cleanup(func() {
		db.Where("id IN ?", []int64{monitors[0].ID, monitors[1].ID, monitors[2].ID}).Delete(&model.ChannelMonitor{})
	})

	assert.Equal(t, 60*time.Second, (channelMonitorHandler{}).Interval())
}

func TestMonitorAdminViewHistoryIsOldestToNewest(t *testing.T) {
	view := monitorAdminView(model.ChannelMonitor{ID: 44, PrimaryModel: "model-a"}, []model.ChannelMonitorCheck{
		{MonitorID: 44, Model: "model-a", Status: model.ChannelMonitorStatusSuccess, CheckedAt: 300},
		{MonitorID: 44, Model: "model-a", Status: model.ChannelMonitorStatusFailed, CheckedAt: 100},
		{MonitorID: 44, Model: "model-a", Status: model.ChannelMonitorStatusTimeout, CheckedAt: 200},
	})
	history := view.Models["model-a"].History
	require.Len(t, history, 3)
	assert.Equal(t, int64(100), history[0].CheckedAt)
	assert.Equal(t, int64(300), history[2].CheckedAt)
}

func TestLotteryAdminProgressDTOExposesDisplayFieldsOnly(t *testing.T) {
	payload := lotteryAdminUserProgress{UserID: 7, Username: "alice", Accumulated: 12.5, AvailableDraws: 2, Threshold: 20}
	body := string(mustMarshal(payload))
	assert.Contains(t, body, `"user_id":7`)
	assert.Contains(t, body, `"username":"alice"`)
	assert.Contains(t, body, `"accumulated":12.5`)
	assert.Contains(t, body, `"threshold":20`)
	assert.NotContains(t, body, "password")
	assert.NotContains(t, body, "token")
	assert.NotContains(t, body, "quota")
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
	require.Equal(t, 60, view.IntervalSeconds)
	require.NotContains(t, view.Models["model-a"].Status, "secret")
	require.NotContains(t, string(mustMarshal(view)), "id")
	require.NotContains(t, string(mustMarshal(view)), "enabled")
	require.NotContains(t, string(mustMarshal(view)), "module_enabled")
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

func TestGetChannelMonitorsReturnsEmptyWhenDisabled(t *testing.T) {
	previous := common.ChannelMonitorEnabled
	common.ChannelMonitorEnabled = false
	t.Cleanup(func() { common.ChannelMonitorEnabled = previous })
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	GetChannelMonitors(c)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.JSONEq(t, `{"success":true,"message":"","data":[]}`, recorder.Body.String())
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
