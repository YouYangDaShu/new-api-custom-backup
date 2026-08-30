package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupMidjourneyRefundTest(t *testing.T) *gorm.DB {
	t.Helper()

	oldDB, oldLogDB := model.DB, model.LOG_DB
	oldMainDBType, oldLogDBType := common.MainDatabaseType(), common.LogDatabaseType()
	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	oldBatchUpdateEnabled := common.BatchUpdateEnabled
	oldRedisEnabled := common.RedisEnabled

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "midjourney-refund.db")+"?_pragma=busy_timeout(5000)"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.Midjourney{},
		&model.TaskBillingOperation{},
		&model.User{},
		&model.Channel{},
		&model.Log{},
		&model.Option{},
		&model.UserLotteryProgress{},
		&model.LotteryConsumptionEvent{},
	))

	model.DB = db
	model.LOG_DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.MemoryCacheEnabled = false
	common.BatchUpdateEnabled = false
	common.RedisEnabled = false
	service.InitHttpClient()

	t.Cleanup(func() {
		model.DB = oldDB
		model.LOG_DB = oldLogDB
		common.SetDatabaseTypes(oldMainDBType, oldLogDBType)
		common.MemoryCacheEnabled = oldMemoryCacheEnabled
		common.BatchUpdateEnabled = oldBatchUpdateEnabled
		common.RedisEnabled = oldRedisEnabled
		sqlDB, closeErr := db.DB()
		if closeErr == nil {
			require.NoError(t, sqlDB.Close())
		}
	})

	return db
}

func seedMidjourneyRefundTest(t *testing.T, db *gorm.DB, baseURL string) {
	t.Helper()

	baseURLCopy := baseURL
	require.NoError(t, db.Create(&model.User{
		Id:       901,
		Username: "mj-refund",
		Password: "not-used",
		AffCode:  "mj-refund",
		Quota:    1_000,
	}).Error)
	require.NoError(t, db.Create(&model.Channel{
		Id:      902,
		Name:    "mj-refund",
		Key:     "secret",
		BaseURL: &baseURLCopy,
	}).Error)
	require.NoError(t, db.Create(&model.Midjourney{
		Id:         903,
		UserId:     901,
		ChannelId:  902,
		MjId:       "mj-refund-task",
		Action:     "IMAGINE",
		Status:     "IN_PROGRESS",
		Progress:   "50%",
		SubmitTime: time.Now().UnixMilli(),
		Quota:      100,
	}).Error)
}

func TestMidjourneyFailureRefundIsIdempotent(t *testing.T) {
	db := setupMidjourneyRefundTest(t)

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"id":"mj-refund-task","status":"FAILURE","progress":"100%","failReason":"upstream failed"},
			{"id":"mj-refund-task","status":"SUCCESS","progress":"100%"}
		]`))
	}))
	t.Cleanup(server.Close)
	seedMidjourneyRefundTest(t, db, server.URL)

	runMidjourneyTaskUpdateOnce(context.Background(), nil)
	runMidjourneyTaskUpdateOnce(context.Background(), nil)

	var user model.User
	require.NoError(t, db.First(&user, 901).Error)
	assert.Equal(t, 1_100, user.Quota)

	var task model.Midjourney
	require.NoError(t, db.First(&task, 903).Error)
	assert.Equal(t, "FAILURE", task.Status)
	assert.Equal(t, "100%", task.Progress)
	assert.Zero(t, task.Quota)
	assert.EqualValues(t, 1, requests.Load())

	var refundLogs int64
	require.NoError(t, db.Model(&model.Log{}).Where("type = ?", model.LogTypeRefund).Count(&refundLogs).Error)
	assert.EqualValues(t, 1, refundLogs)

	var lotteryEvents int64
	require.NoError(t, db.Model(&model.LotteryConsumptionEvent{}).Count(&lotteryEvents).Error)
	assert.Zero(t, lotteryEvents)
}

func TestMidjourneyFailureCASRejectsStaleSuccess(t *testing.T) {
	db := setupMidjourneyRefundTest(t)

	var requests atomic.Int32
	bothRequested := make(chan struct{})
	allowSuccess := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestNumber := requests.Add(1)
		if requestNumber == 2 {
			close(bothRequested)
		}
		<-bothRequested
		w.Header().Set("Content-Type", "application/json")
		if requestNumber == 1 {
			_, _ = w.Write([]byte(`[{"id":"mj-refund-task","status":"FAILURE","progress":"100%","failReason":"upstream failed"}]`))
			return
		}
		<-allowSuccess
		_, _ = w.Write([]byte(`[{"id":"mj-refund-task","status":"SUCCESS","progress":"100%"}]`))
	}))
	t.Cleanup(server.Close)
	seedMidjourneyRefundTest(t, db, server.URL)

	done := make(chan struct{}, 2)
	go func() {
		runMidjourneyTaskUpdateOnce(context.Background(), nil)
		done <- struct{}{}
	}()
	go func() {
		runMidjourneyTaskUpdateOnce(context.Background(), nil)
		done <- struct{}{}
	}()

	<-done
	close(allowSuccess)
	<-done

	var user model.User
	require.NoError(t, db.First(&user, 901).Error)
	assert.Equal(t, 1_100, user.Quota)

	var task model.Midjourney
	require.NoError(t, db.First(&task, 903).Error)
	assert.Equal(t, "FAILURE", task.Status)
	assert.Zero(t, task.Quota)

	var refundLogs int64
	require.NoError(t, db.Model(&model.Log{}).Where("type = ?", model.LogTypeRefund).Count(&refundLogs).Error)
	assert.EqualValues(t, 1, refundLogs)

	var lotteryEvents int64
	require.NoError(t, db.Model(&model.LotteryConsumptionEvent{}).Count(&lotteryEvents).Error)
	assert.Zero(t, lotteryEvents)
}

func TestMidjourneyConcurrentFailurePollsRefundOnce(t *testing.T) {
	db := setupMidjourneyRefundTest(t)
	ready := make(chan struct{})
	release := make(chan struct{})
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) == 2 {
			close(ready)
		}
		<-release
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"mj-refund-task","status":"FAILURE","progress":"100%","failReason":"upstream failed"}]`))
	}))
	t.Cleanup(server.Close)
	seedMidjourneyRefundTest(t, db, server.URL)

	done := make(chan struct{}, 2)
	for i := 0; i < 2; i++ {
		go func() {
			runMidjourneyTaskUpdateOnce(context.Background(), nil)
			done <- struct{}{}
		}()
	}
	<-ready
	close(release)
	<-done
	<-done

	var user model.User
	require.NoError(t, db.First(&user, 901).Error)
	assert.Equal(t, 1_100, user.Quota)
	var logs int64
	require.NoError(t, db.Model(&model.Log{}).Where("type = ?", model.LogTypeRefund).Count(&logs).Error)
	assert.EqualValues(t, 1, logs)
	var operations int64
	require.NoError(t, db.Model(&model.TaskBillingOperation{}).Where("state = ?", model.TaskBillingOperationDone).Count(&operations).Error)
	assert.EqualValues(t, 1, operations)
}

func TestMidjourneyRefundFailureStaysPendingAndRecovers(t *testing.T) {
	db := setupMidjourneyRefundTest(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		require.NoError(t, db.Exec("ALTER TABLE users RENAME TO users_hidden").Error)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"mj-refund-task","status":"FAILURE","progress":"100%","failReason":"upstream failed"}]`))
	}))
	t.Cleanup(server.Close)
	seedMidjourneyRefundTest(t, db, server.URL)

	runMidjourneyTaskUpdateOnce(context.Background(), nil)
	require.NoError(t, db.Exec("ALTER TABLE users_hidden RENAME TO users").Error)

	var task model.Midjourney
	require.NoError(t, db.First(&task, 903).Error)
	assert.Equal(t, model.MidjourneyBillingStatePending, task.BillingState)
	assert.Equal(t, 100, task.PendingRefundQuota)
	assert.Equal(t, 100, task.Quota)

	var operation model.TaskBillingOperation
	require.NoError(t, db.Where("operation_key = ?", midjourneyRefundOperationKey(&task)).First(&operation).Error)
	assert.Equal(t, model.TaskBillingOperationPending, operation.State)

	runMidjourneyTaskUpdateOnce(context.Background(), nil)
	require.NoError(t, db.First(&task, 903).Error)
	assert.Equal(t, model.MidjourneyBillingStateDone, task.BillingState)
	assert.Zero(t, task.PendingRefundQuota)
	assert.Zero(t, task.Quota)
	var user model.User
	require.NoError(t, db.First(&user, 901).Error)
	assert.Equal(t, 1_100, user.Quota)
}

func TestMidjourneyPendingRefundRecoversAfterTerminalCAS(t *testing.T) {
	db := setupMidjourneyRefundTest(t)
	seedMidjourneyRefundTest(t, db, "http://unused.invalid")
	require.NoError(t, db.Model(&model.Midjourney{}).Where("id = ?", 903).Updates(map[string]any{
		"status": "FAILURE", "progress": "100%", "fail_reason": "upstream failed",
		"billing_state": model.MidjourneyBillingStatePending, "pending_refund_quota": 100,
	}).Error)

	runMidjourneyTaskUpdateOnce(context.Background(), nil)

	var task model.Midjourney
	require.NoError(t, db.First(&task, 903).Error)
	assert.Equal(t, model.MidjourneyBillingStateDone, task.BillingState)
	assert.Zero(t, task.Quota)
	var user model.User
	require.NoError(t, db.First(&user, 901).Error)
	assert.Equal(t, 1_100, user.Quota)

	runMidjourneyTaskUpdateOnce(context.Background(), nil)
	require.NoError(t, db.First(&user, 901).Error)
	assert.Equal(t, 1_100, user.Quota)
}

func TestMidjourneyExpiredUnknownRefundBecomesManual(t *testing.T) {
	db := setupMidjourneyRefundTest(t)
	seedMidjourneyRefundTest(t, db, "http://unused.invalid")
	require.NoError(t, db.Model(&model.Midjourney{}).Where("id = ?", 903).Updates(map[string]any{
		"status": "FAILURE", "progress": "100%", "fail_reason": "upstream failed",
		"billing_state": model.MidjourneyBillingStatePending, "pending_refund_quota": 100,
	}).Error)
	var task model.Midjourney
	require.NoError(t, db.First(&task, 903).Error)
	require.NoError(t, db.Create(&model.TaskBillingOperation{
		OperationKey: midjourneyRefundOperationKey(&task), TaskID: int64(task.Id),
		State: model.TaskBillingOperationInProgress, Owner: "crashed-worker", LeaseUntil: time.Now().Unix() - 1,
	}).Error)

	runMidjourneyTaskUpdateOnce(context.Background(), nil)

	require.NoError(t, db.First(&task, 903).Error)
	assert.Equal(t, model.MidjourneyBillingStateManual, task.BillingState)
	assert.Equal(t, 100, task.Quota)
	var operation model.TaskBillingOperation
	require.NoError(t, db.Where("operation_key = ?", midjourneyRefundOperationKey(&task)).First(&operation).Error)
	assert.Equal(t, model.TaskBillingOperationManual, operation.State)
	var user model.User
	require.NoError(t, db.First(&user, 901).Error)
	assert.Equal(t, 1_000, user.Quota)
}
