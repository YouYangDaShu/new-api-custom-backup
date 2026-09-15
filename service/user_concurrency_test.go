package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

func setupConcurrencyRedis(t *testing.T) {
	t.Helper()
	server := miniredis.RunT(t)
	original := common.RDB
	originalEnabled := common.RedisEnabled
	common.RDB = redis.NewClient(&redis.Options{Addr: server.Addr()})
	common.RedisEnabled = true
	t.Cleanup(func() { _ = common.RDB.Close(); common.RDB = original; common.RedisEnabled = originalEnabled })
}

func TestUserConcurrencyAcquireReleaseAndIsolation(t *testing.T) {
	setupConcurrencyRedis(t)
	ctx := context.Background()
	ok, err := AcquireUserConcurrency(ctx, 1, 1, "a")
	require.True(t, ok)
	require.NoError(t, err)
	waitCtx, cancel := context.WithCancel(ctx)
	waitDone := make(chan error, 1)
	go func() {
		_, waitErr := AcquireUserConcurrency(waitCtx, 1, 1, "b")
		waitDone <- waitErr
	}()
	deadline := time.After(2 * time.Second)
	for {
		status, statusErr := GetUserConcurrencyStatus(ctx, 1, 1)
		require.NoError(t, statusErr)
		if status.Queued == 1 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("request did not enter the wait queue")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
	cancel()
	require.ErrorIs(t, <-waitDone, context.Canceled)
	deadline = time.After(2 * time.Second)
	for {
		status, statusErr := GetUserConcurrencyStatus(ctx, 1, 1)
		require.NoError(t, statusErr)
		if status.Queued == 0 {
			require.Equal(t, 1, status.Used)
			break
		}
		select {
		case <-deadline:
			t.Fatal("cancelled request remained in the wait queue")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
	waitCtx2, cancel2 := context.WithCancel(ctx)
	waitDone2 := make(chan error, 1)
	go func() {
		_, waitErr := AcquireUserConcurrency(waitCtx2, 1, 1, "b")
		waitDone2 <- waitErr
	}()
	deadline = time.After(2 * time.Second)
	for {
		status, statusErr := GetUserConcurrencyStatus(ctx, 1, 1)
		require.NoError(t, statusErr)
		if status.Queued == 1 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("second request did not enter the wait queue")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
	ok, err = AcquireUserConcurrency(ctx, 2, 1, "other")
	require.True(t, ok)
	require.NoError(t, err)
	require.NoError(t, ReleaseUserConcurrency(ctx, 1, "a"))
	require.NoError(t, ReleaseUserConcurrency(ctx, 1, "a"))
	require.NoError(t, <-waitDone2)
	cancel2()
}

func TestUserConcurrencyUnlimitedAndValidation(t *testing.T) {
	setupConcurrencyRedis(t)
	ok, err := AcquireUserConcurrency(context.Background(), 1, 0, "unlimited")
	require.True(t, ok)
	require.NoError(t, err)
	_, err = AcquireUserConcurrency(context.Background(), 0, 1, "bad")
	require.ErrorIs(t, err, ErrUserConcurrencyInvalidUser)
	_, err = ConcurrencyLimitFromString("-1")
	require.Error(t, err)
	_, err = ConcurrencyLimitFromString("10001")
	require.Error(t, err)
}

func TestUserConcurrencyRedisFailureIsDistinct(t *testing.T) {
	setupConcurrencyRedis(t)
	_ = common.RDB.Close()
	_, err := AcquireUserConcurrency(context.Background(), 1, 1, "failed")
	require.Error(t, err)
	require.False(t, errors.Is(err, ErrUserConcurrencyLimit))
}

func TestUserConcurrencySlotTTLIsBounded(t *testing.T) {
	setupConcurrencyRedis(t)
	ctx := context.Background()
	ok, err := AcquireUserConcurrency(ctx, 1, 1, "ttl")
	require.True(t, ok)
	require.NoError(t, err)
	activeKey, _ := userConcurrencyKeys(1)
	ttl, err := common.RDB.PTTL(ctx, activeKey).Result()
	require.NoError(t, err)
	require.Greater(t, ttl, time.Duration(0))
	require.LessOrEqual(t, ttl, UserConcurrencySlotTTL)
}
