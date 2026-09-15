package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/google/uuid"
)

const (
	UserConcurrencySlotTTL  = 20 * time.Minute
	UserConcurrencyWaitTTL  = 61 * time.Second
	UserConcurrencyWaitMax  = 1000
	UserConcurrencyWaitTime = 60 * time.Second
)

var (
	ErrUserConcurrencyLimit       = errors.New("user concurrency limit exceeded")
	ErrUserConcurrencyUnavailable = errors.New("user concurrency storage unavailable")
	ErrUserConcurrencyInvalidUser = errors.New("invalid user id")
)

type UserConcurrencyStatus struct {
	Limit     int    `json:"limit"`
	Used      int    `json:"used"`
	Queued    int    `json:"queued"`
	Remaining int    `json:"remaining"`
	Unlimited bool   `json:"unlimited"`
	Status    string `json:"status,omitempty"`
	AsOf      int64  `json:"as_of"`
}

type userConcurrencyResult int

const (
	userConcurrencyAcquired userConcurrencyResult = 1
	userConcurrencyQueued   userConcurrencyResult = 2
	userConcurrencyFull     userConcurrencyResult = 3
)

func userConcurrencyKeys(userID int) (string, string) {
	return fmt.Sprintf("concurrency:user:%d", userID), fmt.Sprintf("concurrency:wait:%d", userID)
}

func userConcurrencySequenceKey(userID int) string {
	return fmt.Sprintf("concurrency:seq:%d", userID)
}

func userConcurrencyQueueLimit(limit int) int {
	queueLimit := limit * 4
	if queueLimit < 20 {
		queueLimit = 20
	}
	if queueLimit > UserConcurrencyWaitMax {
		queueLimit = UserConcurrencyWaitMax
	}
	return queueLimit
}

func AcquireUserConcurrency(ctx context.Context, userID, limit int, requestID string) (bool, error) {
	if userID <= 0 {
		return false, ErrUserConcurrencyInvalidUser
	}
	if limit < 0 || limit > common.MaxUserConcurrency {
		return false, fmt.Errorf("invalid user concurrency limit: %d", limit)
	}
	if limit == 0 {
		return true, nil
	}
	if requestID == "" {
		requestID = uuid.NewString()
	}
	if common.RDB == nil {
		return false, ErrUserConcurrencyUnavailable
	}
	activeKey, waitKey := userConcurrencyKeys(userID)
	sequenceKey := userConcurrencySequenceKey(userID)
	const script = `
local now = tonumber(ARGV[4])
local slotTTL = tonumber(ARGV[5])
local waitTTL = tonumber(ARGV[6])
local slotTTLSeconds = tonumber(ARGV[7])
local expired = redis.call('ZRANGEBYSCORE', KEYS[1], '-inf', now - slotTTL)
for _, member in ipairs(expired) do redis.call('ZREM', KEYS[1], member) end
local active = redis.call('ZCARD', KEYS[1])
local position = redis.call('ZRANK', KEYS[2], ARGV[1])
local queued = redis.call('ZCARD', KEYS[2])
if active < tonumber(ARGV[2]) and position == false and queued == 0 then
  redis.call('ZREM', KEYS[2], ARGV[1])
  redis.call('ZADD', KEYS[1], now, ARGV[1])
  redis.call('EXPIRE', KEYS[1], slotTTLSeconds)
  redis.call('EXPIRE', KEYS[2], waitTTL)
  return 1
end
if position == false then
  if queued >= tonumber(ARGV[3]) then return 3 end
  local sequence = redis.call('INCR', KEYS[3])
  redis.call('ZADD', KEYS[2], sequence, ARGV[1])
  redis.call('EXPIRE', KEYS[3], waitTTL)
end
redis.call('EXPIRE', KEYS[2], waitTTL)
return 2`
	result, err := common.RDB.Eval(ctx, script, []string{activeKey, waitKey, sequenceKey}, requestID, limit, userConcurrencyQueueLimit(limit), time.Now().UnixMilli(), int(UserConcurrencySlotTTL/time.Millisecond), int(UserConcurrencyWaitTTL/time.Second), int(UserConcurrencySlotTTL/time.Second)).Int()
	if err != nil {
		return false, fmt.Errorf("acquire user concurrency: %w", err)
	}
	switch userConcurrencyResult(result) {
	case userConcurrencyAcquired:
		return true, nil
	case userConcurrencyFull:
		return false, ErrUserConcurrencyLimit
	default:
	}
	deadline := time.NewTimer(UserConcurrencyWaitTime)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			removeUserConcurrencyWait(context.Background(), userID, requestID)
			return false, ctx.Err()
		case <-deadline.C:
			removeUserConcurrencyWait(context.Background(), userID, requestID)
			return false, ErrUserConcurrencyLimit
		case <-ticker.C:
			acquired, err := tryAcquireQueuedUserConcurrency(ctx, userID, limit, requestID)
			if err != nil {
				removeUserConcurrencyWait(context.Background(), userID, requestID)
				return false, err
			}
			if acquired {
				return true, nil
			}
		}
	}
}

func tryAcquireQueuedUserConcurrency(ctx context.Context, userID, limit int, requestID string) (bool, error) {
	if common.RDB == nil {
		return false, ErrUserConcurrencyUnavailable
	}
	activeKey, waitKey := userConcurrencyKeys(userID)
	const script = `
local now = tonumber(ARGV[4])
local expired = redis.call('ZRANGEBYSCORE', KEYS[1], '-inf', now - tonumber(ARGV[5]))
for _, member in ipairs(expired) do redis.call('ZREM', KEYS[1], member) end
local active = redis.call('ZCARD', KEYS[1])
local first = redis.call('ZRANGE', KEYS[2], 0, 0)[1]
if first == ARGV[1] and active < tonumber(ARGV[2]) then
  redis.call('ZREM', KEYS[2], ARGV[1])
  redis.call('ZADD', KEYS[1], now, ARGV[1])
  redis.call('EXPIRE', KEYS[1], ARGV[6])
  return 1
end
return 0`
	result, err := common.RDB.Eval(ctx, script, []string{activeKey, waitKey}, requestID, limit, userConcurrencyQueueLimit(limit), time.Now().UnixMilli(), int(UserConcurrencySlotTTL/time.Millisecond), int(UserConcurrencySlotTTL/time.Second)).Int()
	return result == 1, err
}

func ReleaseUserConcurrency(ctx context.Context, userID int, requestID string) error {
	if userID <= 0 || requestID == "" {
		return nil
	}
	if common.RDB == nil {
		return ErrUserConcurrencyUnavailable
	}
	activeKey, waitKey := userConcurrencyKeys(userID)
	const script = `
redis.call('ZREM', KEYS[1], ARGV[1])
redis.call('ZREM', KEYS[2], ARGV[1])
if redis.call('ZCARD', KEYS[1]) == 0 then redis.call('DEL', KEYS[1]) end
if redis.call('ZCARD', KEYS[2]) == 0 then redis.call('DEL', KEYS[2]) end
return 1`
	return common.RDB.Eval(ctx, script, []string{activeKey, waitKey}, requestID).Err()
}

func removeUserConcurrencyWait(ctx context.Context, userID int, requestID string) {
	if common.RDB == nil || userID <= 0 || requestID == "" {
		return
	}
	_, waitKey := userConcurrencyKeys(userID)
	_ = common.RDB.ZRem(ctx, waitKey, requestID).Err()
}

func GetUserConcurrencyStatus(ctx context.Context, userID, limit int) (UserConcurrencyStatus, error) {
	status := UserConcurrencyStatus{Limit: limit, Unlimited: limit == 0, AsOf: time.Now().Unix()}
	if userID <= 0 {
		return status, ErrUserConcurrencyInvalidUser
	}
	if limit == 0 {
		return status, nil
	}
	if common.RDB == nil {
		status.Status = "unavailable"
		return status, ErrUserConcurrencyUnavailable
	}
	activeKey, waitKey := userConcurrencyKeys(userID)
	const script = `
local now = tonumber(ARGV[1])
local expired = redis.call('ZRANGEBYSCORE', KEYS[1], '-inf', now - tonumber(ARGV[2]))
for _, member in ipairs(expired) do redis.call('ZREM', KEYS[1], member) end
return {redis.call('ZCARD', KEYS[1]), redis.call('ZCARD', KEYS[2])}`
	values, err := common.RDB.Eval(ctx, script, []string{activeKey, waitKey}, time.Now().UnixMilli(), int(UserConcurrencySlotTTL/time.Millisecond)).Int64Slice()
	if err != nil {
		status.Status = "unavailable"
		return status, fmt.Errorf("read user concurrency status: %w", err)
	}
	status.Used = int(values[0])
	status.Queued = int(values[1])
	status.Remaining = limit - status.Used
	if status.Remaining < 0 {
		status.Remaining = 0
	}
	return status, nil
}

func GetUsersConcurrencyStatus(ctx context.Context, limits map[int]int) (map[int]UserConcurrencyStatus, error) {
	result := make(map[int]UserConcurrencyStatus, len(limits))
	limitedIDs := make([]int, 0, len(limits))
	for id, limit := range limits {
		if id <= 0 {
			return nil, ErrUserConcurrencyInvalidUser
		}
		result[id] = UserConcurrencyStatus{Limit: limit, Unlimited: limit == 0, AsOf: time.Now().Unix()}
		if limit > 0 {
			limitedIDs = append(limitedIDs, id)
		}
	}
	if len(limitedIDs) == 0 {
		return result, nil
	}
	if common.RDB == nil {
		return nil, ErrUserConcurrencyUnavailable
	}
	sort.Ints(limitedIDs)
	keys := make([]string, 0, len(limitedIDs)*2)
	for _, id := range limitedIDs {
		activeKey, waitKey := userConcurrencyKeys(id)
		keys = append(keys, activeKey, waitKey)
	}
	const script = `
local now = tonumber(ARGV[1])
local slotTTL = tonumber(ARGV[2])
local output = {}
for i = 1, #KEYS, 2 do
  local expired = redis.call('ZRANGEBYSCORE', KEYS[i], '-inf', now - slotTTL)
  for _, member in ipairs(expired) do redis.call('ZREM', KEYS[i], member) end
  table.insert(output, redis.call('ZCARD', KEYS[i]))
  table.insert(output, redis.call('ZCARD', KEYS[i + 1]))
end
return output`
	values, err := common.RDB.Eval(ctx, script, keys, time.Now().UnixMilli(), int(UserConcurrencySlotTTL/time.Millisecond)).Int64Slice()
	if err != nil {
		return nil, fmt.Errorf("read users concurrency status: %w", err)
	}
	for index, id := range limitedIDs {
		status := result[id]
		status.Used = int(values[index*2])
		status.Queued = int(values[index*2+1])
		status.Remaining = status.Limit - status.Used
		if status.Remaining < 0 {
			status.Remaining = 0
		}
		result[id] = status
	}
	return result, nil
}

func AcquireUserSlot(ctx context.Context, userID, limit int, requestID string) (bool, error) {
	return AcquireUserConcurrency(ctx, userID, limit, requestID)
}

func ReleaseUserSlot(ctx context.Context, userID int, requestID string) error {
	return ReleaseUserConcurrency(ctx, userID, requestID)
}

func GetUserConcurrency(ctx context.Context, userID, limit int) (UserConcurrencyStatus, error) {
	return GetUserConcurrencyStatus(ctx, userID, limit)
}

func GetUsersConcurrency(ctx context.Context, limits map[int]int) (map[int]UserConcurrencyStatus, error) {
	return GetUsersConcurrencyStatus(ctx, limits)
}

func ConcurrencyLimitFromString(value string) (int, error) {
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 || parsed > common.MaxUserConcurrency {
		return 0, fmt.Errorf("concurrency must be between 0 and %d", common.MaxUserConcurrency)
	}
	return parsed, nil
}
