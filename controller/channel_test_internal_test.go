package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestValidateChannelProxy(t *testing.T) {
	tests := []struct {
		name    string
		proxy   string
		wantErr bool
	}{
		{name: "empty"},
		{name: "http", proxy: "http://proxy.example:8080"},
		{name: "https", proxy: "https://proxy.example:8443"},
		{name: "socks5", proxy: "socks5://proxy.example"},
		{name: "socks5h", proxy: "socks5h://proxy.example:1080/"},
		{name: "unsupported", proxy: "ftp://proxy.example", wantErr: true},
		{name: "path", proxy: "socks5://proxy.example:1080/path", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setting, err := common.Marshal(dto.ChannelSettings{Proxy: test.proxy})
			require.NoError(t, err)
			channel := &model.Channel{
				Type:    constant.ChannelTypeOpenAI,
				Setting: common.GetPointer(string(setting)),
			}

			err = validateChannel(channel, false)

			if test.wantErr {
				require.ErrorContains(t, err, "invalid channel proxy")
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestValidateChannelRequiresNewAPIBaseURL(t *testing.T) {
	tests := []struct {
		name    string
		baseURL *string
		wantErr bool
	}{
		{name: "missing", wantErr: true},
		{name: "blank", baseURL: common.GetPointer("  "), wantErr: true},
		{name: "configured", baseURL: common.GetPointer("https://new-api.example")},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			channel := &model.Channel{
				Type:    constant.ChannelTypeNewAPI,
				BaseURL: test.baseURL,
			}

			err := validateChannel(channel, false)

			if test.wantErr {
				require.ErrorContains(t, err, "New API channel base URL cannot be empty")
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestNewAPIChannelRegistration(t *testing.T) {
	apiType, ok := common.ChannelType2APIType(constant.ChannelTypeNewAPI)

	require.True(t, ok)
	assert.Equal(t, constant.APITypeNewAPI, apiType)
	assert.Equal(t, "New API", constant.GetChannelTypeName(constant.ChannelTypeNewAPI))
	require.Greater(t, len(constant.ChannelBaseURLs), constant.ChannelTypeNewAPI)
	assert.Empty(t, constant.ChannelBaseURLs[constant.ChannelTypeNewAPI])
}

func TestResponsesCompactChannelSupport(t *testing.T) {
	tests := []struct {
		name        string
		channelType int
		apiType     int
		want        bool
	}{
		{name: "OpenAI", channelType: constant.ChannelTypeOpenAI, apiType: constant.APITypeOpenAI, want: true},
		{name: "Azure", channelType: constant.ChannelTypeAzure, apiType: constant.APITypeOpenAI, want: true},
		{name: "Codex", channelType: constant.ChannelTypeCodex, apiType: constant.APITypeCodex, want: true},
		{name: "Advanced Custom", channelType: constant.ChannelTypeAdvancedCustom, apiType: constant.APITypeAdvancedCustom, want: true},
		{name: "Sub2API", channelType: constant.ChannelTypeSub2API, apiType: constant.APITypeSub2API, want: true},
		{name: "New API", channelType: constant.ChannelTypeNewAPI, apiType: constant.APITypeNewAPI, want: true},
		{name: "Anthropic", channelType: constant.ChannelTypeAnthropic, apiType: constant.APITypeAnthropic, want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, common.SupportsResponsesCompact(test.channelType, test.apiType))
		})
	}
}

func TestMultiprotocolGatewayEndpointTypes(t *testing.T) {
	want := []constant.EndpointType{
		constant.EndpointTypeOpenAI,
		constant.EndpointTypeOpenAIResponse,
		constant.EndpointTypeOpenAIResponseCompact,
		constant.EndpointTypeAnthropic,
		constant.EndpointTypeGemini,
		constant.EndpointTypeOpenAIAlphaSearch,
	}

	assert.Equal(t, want, common.GetEndpointTypesByChannelType(constant.ChannelTypeNewAPI, "gpt-5"))
	assert.Equal(t, want, common.GetEndpointTypesByChannelType(constant.ChannelTypeSub2API, "gpt-5"))
}

func TestCopyChannelRejectsInvalidLegacyProxySettings(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	settingBytes, err := common.Marshal(dto.ChannelSettings{
		Proxy: "socks5://proxy.example/legacy-path",
	})
	require.NoError(t, err)
	setting := string(settingBytes)
	origin := &model.Channel{
		Type:    constant.ChannelTypeOpenAI,
		Name:    "legacy proxy channel",
		Key:     "test-key",
		Models:  "gpt-test",
		Group:   "default",
		Setting: &setting,
	}
	require.NoError(t, db.Create(origin).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "id", Value: fmt.Sprintf("%d", origin.Id)}}
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/channel/copy", nil)

	CopyChannel(ctx)

	assert.Contains(t, recorder.Body.String(), "invalid channel settings")
	var channelCount int64
	require.NoError(t, db.Model(&model.Channel{}).Count(&channelCount).Error)
	assert.Equal(t, int64(1), channelCount)
}

func TestDeleteChannelResetsProxyCacheWhenPreReadFails(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Log{}, &model.AuditLog{}))
	service.ResetProxyClientCache()
	t.Cleanup(service.ResetProxyClientCache)

	proxyURL := "http://proxy.example:8080"
	beforeDelete, err := service.GetHttpClientWithProxy(proxyURL)
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "id", Value: "999999"}}
	ctx.Request = httptest.NewRequest(http.MethodDelete, "/api/channel/999999", nil)

	DeleteChannel(ctx)

	assert.Contains(t, recorder.Body.String(), `"success":true`)
	afterDelete, err := service.GetHttpClientWithProxy(proxyURL)
	require.NoError(t, err)
	assert.NotSame(t, beforeDelete, afterDelete)
}

func TestDeleteChannelBatchReportsAndAuditsActualDeletedCount(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Log{}, &model.AuditLog{}))
	channel := &model.Channel{Name: "existing", Key: "test-key"}
	require.NoError(t, db.Create(channel).Error)

	requestBody, err := common.Marshal(ChannelBatch{Ids: []int{channel.Id, 999999}})
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodDelete, "/api/channel/batch", bytes.NewReader(requestBody))
	ctx.Request.Header.Set("Content-Type", "application/json")

	DeleteChannelBatch(ctx)

	var response struct {
		Success bool  `json:"success"`
		Data    int64 `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.True(t, response.Success)
	assert.Equal(t, int64(1), response.Data)

	var auditLog model.AuditLog
	require.NoError(t, db.Order("id desc").First(&auditLog).Error)
	var auditData struct {
		Operation struct {
			Params map[string]any `json:"params"`
		} `json:"op"`
	}
	encodedAudit, err := common.Marshal(auditLog.Other)
	require.NoError(t, err)
	require.NoError(t, common.Unmarshal(encodedAudit, &auditData))
	assert.Equal(t, float64(1), auditData.Operation.Params["count"])
}

func TestSettleTestQuotaUsesTieredBilling(t *testing.T) {
	info := &relaycommon.RelayInfo{
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode:   "tiered_expr",
			ExprString:    `param("stream") == true ? tier("stream", p * 3) : tier("base", p * 2)`,
			ExprHash:      billingexpr.ExprHashString(`param("stream") == true ? tier("stream", p * 3) : tier("base", p * 2)`),
			GroupRatio:    1,
			EstimatedTier: "stream",
			QuotaPerUnit:  common.QuotaPerUnit,
			ExprVersion:   1,
		},
		BillingRequestInput: &billingexpr.RequestInput{
			Body: []byte(`{"stream":true}`),
		},
	}

	quota, result := settleTestQuota(info, types.PriceData{
		ModelRatio:      1,
		CompletionRatio: 2,
	}, &dto.Usage{
		PromptTokens: 1000,
	})

	require.Equal(t, 1500, quota)
	require.NotNil(t, result)
	require.Equal(t, "stream", result.MatchedTier)
}

func TestBuildTestLogOtherInjectsTieredInfo(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	info := &relaycommon.RelayInfo{
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode: "tiered_expr",
			ExprString:  `tier("base", p * 2)`,
		},
		ChannelMeta: &relaycommon.ChannelMeta{},
	}
	priceData := types.PriceData{
		GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1},
	}
	usage := &dto.Usage{
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 12,
		},
	}

	requestRules := []billingexpr.RequestRuleTrace{{
		Cond:       `param("service_tier") == "fast"`,
		Multiplier: 2,
		Matched:    true,
	}}
	other := buildTestLogOther(ctx, info, priceData, usage, &billingexpr.TieredResult{
		MatchedTier:  "base",
		RequestRules: requestRules,
	})

	fields := other.Snapshot()
	require.Equal(t, "tiered_expr", fields["billing_mode"])
	require.Equal(t, "base", fields["matched_tier"])
	require.Equal(t, requestRules, fields["request_rules"])
	require.NotEmpty(t, fields["expr_b64"])
}

func TestRecordChannelTestErrorPersistsModelTestFailure(t *testing.T) {
	other := model.NewLogOther()
	require.True(t, other.SetPublic("status_code", http.StatusInternalServerError))
	require.True(t, other.SetPublic("request_status", "failed"))
	require.True(t, other.SetPublic("duration_ms", (1500*time.Millisecond).Milliseconds()))
	require.Equal(t, http.StatusInternalServerError, other.Snapshot()["status_code"])
	require.Equal(t, "failed", other.Snapshot()["request_status"])
	require.Equal(t, int64(1500), other.Snapshot()["duration_ms"])
}

func TestChannelTestSuccessStatusIsRecordedForStatistics(t *testing.T) {
	other := model.NewLogOther()
	require.True(t, other.SetPublic("status_code", http.StatusOK))
	require.Equal(t, http.StatusOK, other.Snapshot()["status_code"])
}

func TestTestChannelFailureWritesModelTestLogAndCountsFailure(t *testing.T) {
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousMainDBType, previousLogDBType := common.MainDatabaseType(), common.LogDatabaseType()
	previousRedisEnabled := common.RedisEnabled
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Log{}))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	model.DB, model.LOG_DB = db, db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousMainDBType, previousLogDBType)
		common.RedisEnabled = previousRedisEnabled
		_ = sqlDB.Close()
	})

	result := testChannel(context.Background(), &model.Channel{Id: 42, Type: constant.ChannelTypeMidjourney}, 7, "test-model", "", false)
	require.Error(t, result.localErr)

	var log model.Log
	require.NoError(t, db.Where("token_name = ?", "模型测试").First(&log).Error)
	assert.Equal(t, model.LogTypeError, log.Type)
	assert.Equal(t, 42, log.ChannelId)
	assert.Equal(t, "test-model", log.ModelName)
	assert.Contains(t, log.Content, "status_code=500")

	var other map[string]any
	require.NoError(t, json.Unmarshal([]byte(log.Other), &other))
	assert.EqualValues(t, http.StatusInternalServerError, other["status_code"])
	assert.Equal(t, "failed", other["request_status"])

	stat, err := model.GetModelTestTodayStat(time.Now())
	require.NoError(t, err)
	assert.EqualValues(t, 1, stat.RequestCount)
	assert.Zero(t, stat.SuccessRate)
}

func TestChannelTestResultFailureIncludesLocalError(t *testing.T) {
	localFailure := channelTestResultFailed(testResult{localErr: errors.New("request failed")})
	structuredFailure := channelTestResultFailed(testResult{newAPIError: relaytypes.NewOpenAIError(errors.New("upstream failed"), relaytypes.ErrorCodeBadResponse, http.StatusBadGateway)})
	success := channelTestResultFailed(testResult{})

	assert.True(t, localFailure)
	assert.True(t, structuredFailure)
	assert.False(t, success)
}

func TestTestChannelFailureDoesNotPersistUpstreamResponseBody(t *testing.T) {
	result := testResult{
		localErr:    fmt.Errorf("upstream returned HTTP status 500"),
		newAPIError: relaytypes.NewOpenAIError(fmt.Errorf("upstream returned HTTP status 500"), relaytypes.ErrorCodeBadResponseStatusCode, http.StatusInternalServerError),
	}
	assert.NotContains(t, result.newAPIError.Error(), "Bearer")
	assert.NotContains(t, result.newAPIError.Error(), "api_key")
}

func TestRecordChannelTestErrorPersistsOnlySafeErrorClassification(t *testing.T) {
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousMainDBType, previousLogDBType := common.MainDatabaseType(), common.LogDatabaseType()
	previousRedisEnabled := common.RedisEnabled
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Log{}))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	model.DB, model.LOG_DB = db, db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousMainDBType, previousLogDBType)
		common.RedisEnabled = previousRedisEnabled
		_ = sqlDB.Close()
	})

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions?api_key=test-sensitive-value", nil)
	sensitiveErr := errors.New("Authorization: Bearer test-sensitive-value; api_key=test-sensitive-value")
	result := testResult{
		context:     ctx,
		localErr:    sensitiveErr,
		newAPIError: relaytypes.NewOpenAIError(sensitiveErr, relaytypes.ErrorCodeDoRequestFailed, http.StatusBadGateway),
	}

	recordChannelTestError(context.Background(), &model.Channel{Id: 42}, 7, "test-model", false, time.Second, result)

	var log model.Log
	require.NoError(t, db.Where("token_name = ?", "模型测试").First(&log).Error)
	assert.Equal(t, "status_code=502, error_code=do_request_failed", log.Content)
	assert.NotContains(t, log.Content, "test-sensitive-value")
	assert.NotContains(t, log.Other, "test-sensitive-value")
	assert.NotContains(t, log.Other, "api_key")
	assert.NotContains(t, log.Other, "Authorization")
}

func TestDetectErrorFromTestResponseBodyDoesNotReturnUpstreamMessage(t *testing.T) {
	secrets := []string{
		"Bearer test-sensitive-value",
		"Authorization: test-sensitive-value",
		"api_key=test-sensitive-value",
		"https://example.test/callback?token=test-sensitive-value",
	}
	for _, secret := range secrets {
		t.Run(secret[:min(12, len(secret))], func(t *testing.T) {
			body, err := json.Marshal(map[string]any{
				"error": map[string]any{"message": secret},
			})
			require.NoError(t, err)

			detectedErr := detectErrorFromTestResponseBody(body)
			require.EqualError(t, detectedErr, "upstream returned error payload")
			assert.NotContains(t, detectedErr.Error(), "test-sensitive-value")
		})
	}
}

func TestDetectErrorFromStreamTestResponseBodyDoesNotReturnUpstreamMessage(t *testing.T) {
	payload := `data: {"error":{"message":"Bearer test-sensitive-value"}}\n\n`

	detectedErr := detectErrorFromTestResponseBody([]byte(payload))

	require.EqualError(t, detectedErr, "upstream returned error payload")
	assert.NotContains(t, detectedErr.Error(), "test-sensitive-value")
}

func TestChannelTestPublicErrorMessageDoesNotReturnUnderlyingError(t *testing.T) {
	sensitiveErr := errors.New("Authorization: Bearer test-sensitive-value; api_key=test-sensitive-value")

	withCode := channelTestPublicErrorMessage(testResult{
		localErr:    sensitiveErr,
		newAPIError: relaytypes.NewOpenAIError(sensitiveErr, relaytypes.ErrorCodeBadResponseBody, http.StatusBadGateway),
	})
	localOnly := channelTestPublicErrorMessage(testResult{localErr: sensitiveErr})

	assert.Equal(t, "channel test failed: bad_response_body", withCode)
	assert.Equal(t, "channel test failed", localOnly)
	assert.NotContains(t, withCode, "test-sensitive-value")
	assert.NotContains(t, localOnly, "test-sensitive-value")
}

func TestResolveChannelTestUserIDUsesRequestUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("id", 2)

	userID, err := resolveChannelTestUserID(ctx)

	require.NoError(t, err)
	require.Equal(t, 2, userID)
}

func TestSelectChannelsForAutomaticTestPassiveRecoveryOnlyUsesAutoDisabled(t *testing.T) {
	channels := []*model.Channel{
		{Id: 1, Status: common.ChannelStatusEnabled},
		{Id: 2, Status: common.ChannelStatusAutoDisabled},
		{Id: 3, Status: common.ChannelStatusManuallyDisabled},
	}

	selected := selectChannelsForAutomaticTest(channels, operation_setting.ChannelTestModePassiveRecovery)

	require.Len(t, selected, 1)
	require.Equal(t, 2, selected[0].Id)
}

func TestSelectChannelsForAutomaticTestScheduledSkipsManualDisabled(t *testing.T) {
	channels := []*model.Channel{
		{Id: 1, Status: common.ChannelStatusEnabled},
		{Id: 2, Status: common.ChannelStatusAutoDisabled},
		{Id: 3, Status: common.ChannelStatusManuallyDisabled},
	}

	selected := selectChannelsForAutomaticTest(channels, operation_setting.ChannelTestModeScheduledAll)

	require.Len(t, selected, 2)
	require.Equal(t, 1, selected[0].Id)
	require.Equal(t, 2, selected[1].Id)
}

func TestSelectChannelsForAutomaticTestAutoBanOnlyUsesEligibleChannels(t *testing.T) {
	autoBanEnabled := 1
	autoBanDisabled := 0
	channels := []*model.Channel{
		{Id: 1, Status: common.ChannelStatusEnabled, AutoBan: &autoBanEnabled},
		{Id: 2, Status: common.ChannelStatusEnabled, AutoBan: &autoBanDisabled},
		{Id: 3, Status: common.ChannelStatusAutoDisabled, AutoBan: &autoBanEnabled},
		{Id: 4, Status: common.ChannelStatusManuallyDisabled, AutoBan: &autoBanEnabled},
		{Id: 5, Status: common.ChannelStatusEnabled},
	}

	selected := selectChannelsForAutomaticTest(channels, operation_setting.ChannelTestModeAutoBanOnly)

	require.Len(t, selected, 2)
	require.Equal(t, 1, selected[0].Id)
	require.Equal(t, 3, selected[1].Id)
}

func TestRunChannelTestWorkersHonorsConfiguredConcurrency(t *testing.T) {
	originalInterval := common.RequestInterval
	common.RequestInterval = 0
	t.Cleanup(func() { common.RequestInterval = originalInterval })

	channels := []*model.Channel{
		{Id: 1, Status: common.ChannelStatusEnabled},
		{Id: 2, Status: common.ChannelStatusEnabled},
		{Id: 3, Status: common.ChannelStatusEnabled},
		{Id: 4, Status: common.ChannelStatusEnabled},
	}
	started := make(chan struct{}, len(channels))
	release := make(chan struct{})
	var active atomic.Int32
	var maxActive atomic.Int32
	progress := make([]int, 0, len(channels)+1)
	summaryResult := make(chan channelTestSummary, 1)

	go func() {
		summaryResult <- runChannelTestWorkers(
			context.Background(),
			channels,
			2,
			func(_ context.Context, _ *model.Channel) channelTestSummary {
				current := active.Add(1)
				defer active.Add(-1)
				for {
					observed := maxActive.Load()
					if current <= observed || maxActive.CompareAndSwap(observed, current) {
						break
					}
				}
				started <- struct{}{}
				<-release
				return channelTestSummary{Tested: 1, Succeeded: 1}
			},
			func(processed, _ int) {
				progress = append(progress, processed)
			},
		)
	}()

	<-started
	<-started
	select {
	case <-started:
		t.Fatal("started more channel tests than the configured concurrency")
	default:
	}
	close(release)

	summary := <-summaryResult

	assert.Equal(t, int32(2), maxActive.Load())
	assert.Equal(t, channelTestSummary{Tested: 4, Succeeded: 4}, summary)
	assert.Equal(t, []int{0, 1, 2, 3, 4}, progress)
}

func TestRunChannelTestWorkersStopsAfterCancellation(t *testing.T) {
	originalInterval := common.RequestInterval
	common.RequestInterval = 0
	t.Cleanup(func() { common.RequestInterval = originalInterval })

	ctx, cancel := context.WithCancel(context.Background())
	channels := []*model.Channel{
		{Id: 1, Status: common.ChannelStatusEnabled},
		{Id: 2, Status: common.ChannelStatusEnabled},
		{Id: 3, Status: common.ChannelStatusEnabled},
		{Id: 4, Status: common.ChannelStatusEnabled},
	}
	started := make(chan struct{}, len(channels))
	progress := make([]int, 0, 1)
	summaryResult := make(chan channelTestSummary, 1)

	go func() {
		summaryResult <- runChannelTestWorkers(
			ctx,
			channels,
			2,
			func(ctx context.Context, _ *model.Channel) channelTestSummary {
				started <- struct{}{}
				<-ctx.Done()
				return channelTestSummary{Tested: 1, Succeeded: 1}
			},
			func(processed, _ int) {
				progress = append(progress, processed)
			},
		)
	}()

	<-started
	<-started
	cancel()

	summary := <-summaryResult

	select {
	case <-started:
		t.Fatal("started another channel test after cancellation")
	default:
	}
	assert.Equal(t, channelTestSummary{Tested: 2, Succeeded: 2}, summary)
	assert.Equal(t, []int{0}, progress)
}

func TestTestAllChannelsRejectsExistingActiveTask(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.SystemTask{}, &model.SystemTaskLock{}))

	existing, err := model.CreateSystemTask(model.SystemTaskTypeChannelTest, nil, nil)
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/channel/test", nil)

	TestAllChannels(ctx)

	require.Equal(t, http.StatusConflict, recorder.Code)
	require.Contains(t, recorder.Body.String(), existing.TaskID)
	require.Contains(t, recorder.Body.String(), "已有通道测试任务正在运行或等待中")
}

func TestSuccessfulChannelTestStatusAcceptsEvery2xxResponse(t *testing.T) {
	for _, statusCode := range []int{199, 200, 201, 204, 299, 300} {
		expected := statusCode >= 200 && statusCode < 300
		assert.Equal(t, expected, isSuccessfulChannelTestStatus(statusCode), "status %d", statusCode)
	}
}

func TestTestChannelCreatedResponseWritesOneSuccessLog(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Log{}))
	withSelfUseModeEnabled(t)

	previousLogConsumeEnabled := common.LogConsumeEnabled
	previousBatchUpdateEnabled := common.BatchUpdateEnabled
	common.LogConsumeEnabled = true
	common.BatchUpdateEnabled = false
	t.Cleanup(func() {
		common.LogConsumeEnabled = previousLogConsumeEnabled
		common.BatchUpdateEnabled = previousBatchUpdateEnabled
	})

	user := &model.User{
		Id:       7001,
		Username: "channel-test-created",
		Password: "test-password",
		Role:     common.RoleRootUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
		Quota:    1_000_000,
	}
	require.NoError(t, db.Create(user).Error)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"chatcmpl-test","object":"chat.completion","created":1,"model":"gpt-4o-mini","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	t.Cleanup(upstream.Close)

	channel := &model.Channel{
		Id:      7101,
		Type:    constant.ChannelTypeOpenAI,
		Key:     "test-key",
		Status:  common.ChannelStatusEnabled,
		Name:    "created response",
		BaseURL: common.GetPointer(upstream.URL),
		Models:  "gpt-4o-mini",
		Group:   "default",
	}

	result := testChannel(context.Background(), channel, user.Id, "gpt-4o-mini", "", false)

	require.NoError(t, result.localErr)
	require.Nil(t, result.newAPIError)
	assert.Equal(t, http.StatusCreated, result.statusCode)

	var logs []model.Log
	require.NoError(t, db.Where("token_name = ?", "模型测试").Find(&logs).Error)
	require.Len(t, logs, 1)
	assert.Equal(t, model.LogTypeConsume, logs[0].Type)
	var other map[string]any
	require.NoError(t, json.Unmarshal([]byte(logs[0].Other), &other))
	assert.EqualValues(t, http.StatusCreated, other["status_code"])
}

func TestTestChannelPanicWritesOneFailureLog(t *testing.T) {
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousMainDBType, previousLogDBType := common.MainDatabaseType(), common.LogDatabaseType()
	previousRedisEnabled := common.RedisEnabled
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Log{}))
	model.DB, model.LOG_DB = db, db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousMainDBType, previousLogDBType)
		common.RedisEnabled = previousRedisEnabled
	})

	result := testChannel(context.Background(), nil, 7, "panic-model", "", false)

	require.Error(t, result.localErr)
	require.NotNil(t, result.newAPIError)
	assert.Equal(t, relaytypes.ErrorCodeBadResponse, result.newAPIError.GetErrorCode())
	var logs []model.Log
	require.NoError(t, db.Where("token_name = ?", "模型测试").Find(&logs).Error)
	require.Len(t, logs, 1)
	assert.Equal(t, model.LogTypeError, logs[0].Type)
	assert.NotContains(t, logs[0].Content, "panic-model")
}

func TestCancelledChannelTestDoesNotWriteFailureLog(t *testing.T) {
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousMainDBType, previousLogDBType := common.MainDatabaseType(), common.LogDatabaseType()
	previousRedisEnabled := common.RedisEnabled
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Log{}))
	model.DB, model.LOG_DB = db, db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousMainDBType, previousLogDBType)
		common.RedisEnabled = previousRedisEnabled
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result := testChannel(ctx, &model.Channel{Id: 42, Type: constant.ChannelTypeMidjourney}, 7, "cancelled-model", "", false)

	require.Error(t, result.localErr)
	var count int64
	require.NoError(t, db.Model(&model.Log{}).Where("token_name = ?", "模型测试").Count(&count).Error)
	assert.Zero(t, count)
}

func TestChannelTestTaskAllowsDisableOnlyForAutomaticRuns(t *testing.T) {
	assert.False(t, channelTestTaskAllowsDisable(operation_setting.ChannelTestModeScheduledAll, true), "manual test-all must not disable channels")
	assert.False(t, channelTestTaskAllowsDisable(operation_setting.ChannelTestModePassiveRecovery, false), "passive recovery must not disable channels")
	assert.True(t, channelTestTaskAllowsDisable(operation_setting.ChannelTestModeScheduledAll, false), "scheduled automatic checks may disable channels")
	assert.True(t, channelTestTaskAllowsDisable(operation_setting.ChannelTestModeAutoBanOnly, false), "auto-ban checks may disable channels")
}
