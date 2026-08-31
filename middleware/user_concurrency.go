package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
)

const (
	userConcurrencySlotKeyPrefix = "concurrency:user:"
	userConcurrencySlotTTL       = 15 * time.Minute
)

var (
	userConcurrencyRequestSeq atomic.Uint64

	userConcurrencyAcquireScript = redis.NewScript(`
redis.replicate_commands()
local key = KEYS[1]
local maxConcurrency = tonumber(ARGV[1])
local ttl = tonumber(ARGV[2])
local requestID = ARGV[3]

local timeResult = redis.call('TIME')
local now = tonumber(timeResult[1])
local expireBefore = now - ttl

redis.call('ZREMRANGEBYSCORE', key, '-inf', expireBefore)

local exists = redis.call('ZSCORE', key, requestID)
if exists ~= false then
  redis.call('ZADD', key, now, requestID)
  redis.call('EXPIRE', key, ttl)
  return 1
end

local count = redis.call('ZCARD', key)
if count < maxConcurrency then
  redis.call('ZADD', key, now, requestID)
  redis.call('EXPIRE', key, ttl)
  return 1
end

return 0
`)
)

func userConcurrencySlotKey(userID int) string {
	return userConcurrencySlotKeyPrefix + strconv.Itoa(userID)
}

func newUserConcurrencyRequestID(userID int) string {
	seq := userConcurrencyRequestSeq.Add(1)
	return fmt.Sprintf("%d-%d-%d", time.Now().UnixNano(), userID, seq)
}

func tryAcquireUserConcurrencySlot(ctx context.Context, userID int, maxConcurrency int) (func(), bool, error) {
	if maxConcurrency <= 0 || userID <= 0 {
		return func() {}, true, nil
	}
	if !common.RedisEnabled || common.RDB == nil {
		return func() {}, true, nil
	}

	requestID := newUserConcurrencyRequestID(userID)
	key := userConcurrencySlotKey(userID)
	result, err := userConcurrencyAcquireScript.Run(
		ctx,
		common.RDB,
		[]string{key},
		maxConcurrency,
		int(userConcurrencySlotTTL.Seconds()),
		requestID,
	).Int()
	if err != nil {
		return nil, false, err
	}
	if result != 1 {
		return nil, false, nil
	}

	release := func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := common.RDB.ZRem(releaseCtx, key, requestID).Err(); err != nil {
			logger.LogWarn(releaseCtx, fmt.Sprintf("release user concurrency slot failed user_id=%d req=%s err=%v", userID, requestID, err))
		}
	}
	return release, true, nil
}

func releaseOnRequestDone(ctx context.Context, releaseFunc func()) func() {
	if releaseFunc == nil {
		return nil
	}
	var once sync.Once
	var stop func() bool
	release := func() {
		once.Do(func() {
			if stop != nil {
				_ = stop()
			}
			releaseFunc()
		})
	}
	stop = context.AfterFunc(ctx, release)
	return release
}

// UserConcurrencyLimit limits in-flight relay requests per authenticated user.
// It differs from ModelRequestRateLimit: slots are released when the request finishes.
func UserConcurrencyLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		release, acquired, err := AcquireUserConcurrency(c)
		if err != nil {
			c.Next()
			return
		}
		if !acquired {
			abortWithOpenAiMessage(c, http.StatusTooManyRequests, "user concurrency limit exceeded", types.ErrorCodeUserConcurrencyLimit)
			return
		}

		release = releaseOnRequestDone(c.Request.Context(), release)
		if release != nil {
			defer release()
		}
		c.Next()
	}
}

// AcquireUserConcurrency acquires one in-flight request slot for a single
// relay turn. Long-lived protocols should call it per turn, not per connection.
func AcquireUserConcurrency(c *gin.Context) (func(), bool, error) {
	userID := c.GetInt("id")
	maxConcurrency := common.GetContextKeyInt(c, constant.ContextKeyUserConcurrency)
	if userID <= 0 || maxConcurrency <= 0 {
		return func() {}, true, nil
	}

	release, acquired, err := tryAcquireUserConcurrencySlot(c.Request.Context(), userID, maxConcurrency)
	if err != nil {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("user concurrency check failed user_id=%d err=%v", userID, err))
		return nil, false, err
	}
	return release, acquired, nil
}
