package common

import (
	"context"
	"time"

	"github.com/go-redis/redis/v8"
)

const BusinessMonitorConcurrencyKey = "new-api:business-monitor:concurrency"

var (
	businessMonitorConcurrencyAcquireScript = redis.NewScript(`
local key = KEYS[1]
local requestID = ARGV[1]
local ttl = tonumber(ARGV[2])
local timeResult = redis.call('TIME')
local expiresAt = tonumber(timeResult[1]) + ttl
redis.call('ZREMRANGEBYSCORE', key, '-inf', tonumber(timeResult[1]))
redis.call('ZADD', key, expiresAt, requestID)
redis.call('EXPIRE', key, ttl)
return 1
`)
	businessMonitorConcurrencyRefreshScript = redis.NewScript(`
local key = KEYS[1]
local requestID = ARGV[1]
local ttl = tonumber(ARGV[2])
local timeResult = redis.call('TIME')
local expiresAt = tonumber(timeResult[1]) + ttl
redis.call('ZADD', key, expiresAt, requestID)
redis.call('EXPIRE', key, ttl)
return 1
`)
	businessMonitorConcurrencyCountScript = redis.NewScript(`
local key = KEYS[1]
local timeResult = redis.call('TIME')
redis.call('ZREMRANGEBYSCORE', key, '-inf', tonumber(timeResult[1]))
return redis.call('ZCARD', key)
`)
)

func BusinessMonitorConcurrencyAcquire(ctx context.Context, requestID string, ttl time.Duration) error {
	if !RedisEnabled || RDB == nil {
		return nil
	}
	_, err := businessMonitorConcurrencyAcquireScript.Run(
		ctx,
		RDB,
		[]string{BusinessMonitorConcurrencyKey},
		requestID,
		int(ttl.Seconds()),
	).Result()
	return err
}

func BusinessMonitorConcurrencyRefresh(ctx context.Context, requestID string, ttl time.Duration) error {
	if !RedisEnabled || RDB == nil {
		return nil
	}
	_, err := businessMonitorConcurrencyRefreshScript.Run(
		ctx,
		RDB,
		[]string{BusinessMonitorConcurrencyKey},
		requestID,
		int(ttl.Seconds()),
	).Result()
	return err
}

func BusinessMonitorConcurrencyRelease(ctx context.Context, requestID string) error {
	if !RedisEnabled || RDB == nil {
		return nil
	}
	return RDB.ZRem(ctx, BusinessMonitorConcurrencyKey, requestID).Err()
}

func BusinessMonitorConcurrencyCount(ctx context.Context) (int, error) {
	if !RedisEnabled || RDB == nil {
		return 0, nil
	}
	count, err := businessMonitorConcurrencyCountScript.Run(
		ctx,
		RDB,
		[]string{BusinessMonitorConcurrencyKey},
	).Int()
	return count, err
}
