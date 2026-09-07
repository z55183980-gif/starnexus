package service

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/go-redis/redis/v8"
)

const (
	upstreamModelTransientTTL           = 30 * time.Minute
	upstreamModelTransientFirstBackoff  = 15 * time.Second
	upstreamModelTransientSecondBackoff = 10 * time.Second
	upstreamModelTransientLaterBackoff  = 45 * time.Second
	upstreamCapacityRetryBaseDelay      = 500 * time.Millisecond
	upstreamCapacityRetryMaximumDelay   = 8 * time.Second
	upstreamCapacityRetryDefaultCount   = 2
	upstreamModelTransientMaxEntries    = 100_000

	upstreamModelTransientRedisKeyPrefix = "new-api:upstream-account:capacity:v1:"
	upstreamModelTransientRedisTimeout   = 250 * time.Millisecond
	upstreamModelTransientJitterPercent  = 20
)

type upstreamModelTransientState struct {
	Failures     int
	BlockedUntil time.Time
	ExpiresAt    time.Time
	// RedisSynced distinguishes an authoritative Redis mirror from a
	// process-local fallback created while Redis was unavailable. A successful
	// Redis read of "not blocked" may discard the former, but must not bypass
	// the latter until its local deadline expires.
	RedisSynced bool
	Revision    uint64
}

var upstreamModelTransientRegistry = struct {
	sync.Mutex
	entries  map[string]upstreamModelTransientState
	revision uint64
}{entries: make(map[string]upstreamModelTransientState)}

// Redis stores the failure counter and blocked-until timestamp in one hash.
// The script uses Redis TIME so all application nodes share the same clock.
var upstreamModelTransientRecordScript = redis.NewScript(`
local key = KEYS[1]
local nowParts = redis.call('TIME')
local nowMs = tonumber(nowParts[1]) * 1000 + math.floor(tonumber(nowParts[2]) / 1000)
local failures = tonumber(redis.call('HGET', key, 'failures') or '0') + 1
local previousBlockedUntilMs = tonumber(redis.call('HGET', key, 'blocked_until_ms') or '0')
local baseMs
if failures == 1 then
  baseMs = tonumber(ARGV[1])
elseif failures == 2 then
  baseMs = tonumber(ARGV[2])
else
  baseMs = tonumber(ARGV[3])
end
local jitterPercent = tonumber(ARGV[4]) or 0
local backoffMs = math.max(1, math.floor(baseMs * (100 + jitterPercent) / 100))
local blockedUntilMs = nowMs + backoffMs
if previousBlockedUntilMs > blockedUntilMs then
  blockedUntilMs = previousBlockedUntilMs
end
redis.call('HSET', key, 'failures', failures, 'blocked_until_ms', blockedUntilMs)
redis.call('PEXPIRE', key, tonumber(ARGV[5]))
return {failures, blockedUntilMs - nowMs}
`)

var upstreamModelTransientBlockedScript = redis.NewScript(`
local key = KEYS[1]
local blockedUntilMs = tonumber(redis.call('HGET', key, 'blocked_until_ms') or '0')
if blockedUntilMs <= 0 then
  return {0, 0}
end
local nowParts = redis.call('TIME')
local nowMs = tonumber(nowParts[1]) * 1000 + math.floor(tonumber(nowParts[2]) / 1000)
if blockedUntilMs > nowMs then
  return {1, blockedUntilMs - nowMs}
end
return {0, 0}
`)

func upstreamModelTransientKey(accountID int, modelName string) string {
	return strconv.Itoa(accountID) + ":" + strings.ToLower(strings.TrimSpace(modelName))
}

func upstreamModelTransientRedisKey(accountID int, modelName string) string {
	key := upstreamModelTransientKey(accountID, modelName)
	if accountID <= 0 || strings.TrimSpace(modelName) == "" || key == "" {
		return ""
	}
	return upstreamModelTransientRedisKeyPrefix + key
}

func withUpstreamModelTransientRedisContext(ctx context.Context, fn func(context.Context) error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	redisCtx, cancel := context.WithTimeout(ctx, upstreamModelTransientRedisTimeout)
	defer cancel()
	return fn(redisCtx)
}

func upstreamModelTransientRedisAvailable() bool {
	return common.RedisEnabled && common.RDB != nil
}

func upstreamModelTransientJitterPercentValue() int {
	percent := common.GetEnvOrDefault(
		"UPSTREAM_ACCOUNT_CAPACITY_COOLDOWN_JITTER_PERCENT",
		upstreamModelTransientJitterPercent,
	)
	if percent < 0 {
		return 0
	}
	if percent > 100 {
		return 100
	}
	return percent
}

func upstreamModelTransientFirstBackoffDuration() time.Duration {
	seconds := common.GetEnvOrDefault(
		"UPSTREAM_ACCOUNT_CAPACITY_COOLDOWN_FIRST_SECONDS",
		int(upstreamModelTransientFirstBackoff/time.Second),
	)
	if seconds <= 0 {
		seconds = int(upstreamModelTransientFirstBackoff / time.Second)
	}
	return time.Duration(seconds) * time.Second
}

func upstreamModelTransientBackoffBase(failures int) time.Duration {
	switch {
	case failures <= 1:
		return upstreamModelTransientFirstBackoffDuration()
	case failures == 2:
		return upstreamModelTransientSecondBackoff
	default:
		return upstreamModelTransientLaterBackoff
	}
}

// upstreamModelTransientBackoffWithJitter is deterministic for a supplied
// ratio, which lets tests verify the policy without relying on wall-clock or
// random state. The production path supplies a random ratio in the configured
// +/- jitter range.
func upstreamModelTransientBackoffWithJitter(failures int, jitterRatio float64) time.Duration {
	base := upstreamModelTransientBackoffBase(failures)
	limit := float64(upstreamModelTransientJitterPercentValue()) / 100
	if jitterRatio < -limit {
		jitterRatio = -limit
	}
	if jitterRatio > limit {
		jitterRatio = limit
	}
	backoff := time.Duration(float64(base) * (1 + jitterRatio))
	if backoff < time.Millisecond {
		return time.Millisecond
	}
	return backoff
}

func upstreamModelTransientSeededRandInt63n(n int64) int64 {
	if n <= 0 {
		return 0
	}
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return time.Now().UnixNano() % n
	}
	v := int64(binary.LittleEndian.Uint64(buf[:]) & ^uint64(1<<63))
	return v % n
}

func upstreamModelTransientRandomBackoff(failures int) time.Duration {
	limit := float64(upstreamModelTransientJitterPercentValue()) / 100
	ratio := 0.0
	if limit > 0 {
		ratio = (float64(upstreamModelTransientSeededRandInt63n(2_000_000_001))/1_000_000_000 - 1) * limit
	}
	return upstreamModelTransientBackoffWithJitter(failures, ratio)
}

func upstreamModelTransientRandomJitterPercent() int {
	limit := upstreamModelTransientJitterPercentValue()
	if limit <= 0 {
		return 0
	}
	return int(upstreamModelTransientSeededRandInt63n(int64(2*limit+1))) - limit
}

func redisInt64(value interface{}) int64 {
	switch typed := value.(type) {
	case int64:
		return typed
	case int:
		return int64(typed)
	case string:
		parsed, _ := strconv.ParseInt(typed, 10, 64)
		return parsed
	case []byte:
		parsed, _ := strconv.ParseInt(string(typed), 10, 64)
		return parsed
	default:
		return 0
	}
}

func upstreamModelTransientRedisRecord(ctx context.Context, accountID int, modelName string) (time.Duration, int, bool) {
	if !upstreamModelTransientRedisAvailable() {
		return 0, 0, false
	}
	key := upstreamModelTransientRedisKey(accountID, modelName)
	if key == "" {
		return 0, 0, false
	}
	var result []interface{}
	err := withUpstreamModelTransientRedisContext(ctx, func(redisCtx context.Context) error {
		var runErr error
		result, runErr = upstreamModelTransientRecordScript.Run(
			redisCtx,
			common.RDB,
			[]string{key},
			upstreamModelTransientFirstBackoffDuration().Milliseconds(),
			upstreamModelTransientSecondBackoff.Milliseconds(),
			upstreamModelTransientLaterBackoff.Milliseconds(),
			upstreamModelTransientRandomJitterPercent(),
			upstreamModelTransientTTL.Milliseconds(),
		).Slice()
		return runErr
	})
	if err != nil || len(result) < 2 {
		return 0, 0, false
	}
	failures := int(redisInt64(result[0]))
	backoffMs := redisInt64(result[1])
	if failures <= 0 || backoffMs <= 0 {
		return 0, 0, false
	}
	return time.Duration(backoffMs) * time.Millisecond, failures, true
}

func upstreamModelTransientRedisBlocked(ctx context.Context, accountID int, modelName string) (bool, bool) {
	if !upstreamModelTransientRedisAvailable() {
		return false, false
	}
	key := upstreamModelTransientRedisKey(accountID, modelName)
	if key == "" {
		return false, false
	}
	var result []interface{}
	err := withUpstreamModelTransientRedisContext(ctx, func(redisCtx context.Context) error {
		var runErr error
		result, runErr = upstreamModelTransientBlockedScript.Run(redisCtx, common.RDB, []string{key}).Slice()
		return runErr
	})
	if err != nil || len(result) < 1 {
		return false, false
	}
	return redisInt64(result[0]) == 1, true
}

func upstreamModelTransientRedisClear(ctx context.Context, accountID int, modelName string) {
	upstreamModelTransientRedisClearResult(ctx, accountID, modelName)
}

// upstreamModelTransientRedisClearResult performs the deletion as one bounded
// Redis command and reports whether Redis accepted it. Callers deliberately do
// not retry asynchronously: an old queued DEL could race a newer failure HSET
// and erase a cooldown that belongs to a request which happened later.
func upstreamModelTransientRedisClearResult(ctx context.Context, accountID int, modelName string) bool {
	if !upstreamModelTransientRedisAvailable() {
		return false
	}
	key := upstreamModelTransientRedisKey(accountID, modelName)
	if key == "" {
		return false
	}
	return withUpstreamModelTransientRedisContext(ctx, func(redisCtx context.Context) error {
		return common.RDB.Del(redisCtx, key).Err()
	}) == nil
}

func upstreamModelTransientRemainingDuration(blockedUntil, now time.Time) time.Duration {
	remaining := blockedUntil.Sub(now)
	if remaining <= 0 {
		return 0
	}
	// Redis stores milliseconds. Round up to that same precision so a freshly
	// recorded local cooldown reports its configured duration instead of losing
	// a few nanoseconds to wall-clock elapsed time.
	return ((remaining + time.Millisecond - 1) / time.Millisecond) * time.Millisecond
}

func upstreamModelTransientLocalRecord(key string, backoff time.Duration, failuresOverride int, redisSynced bool) time.Duration {
	now := time.Now()
	upstreamModelTransientRegistry.Lock()
	defer upstreamModelTransientRegistry.Unlock()
	upstreamModelTransientPruneLocked(now, key)
	state := upstreamModelTransientRegistry.entries[key]
	if !state.ExpiresAt.IsZero() && now.After(state.ExpiresAt) {
		state = upstreamModelTransientState{}
	}
	newBlockedUntil := now.Add(backoff)
	preservesLocalFallback := !state.RedisSynced && state.BlockedUntil.After(newBlockedUntil)
	if failuresOverride > 0 {
		state.Failures = failuresOverride
	} else {
		state.Failures++
	}
	state.ExpiresAt = now.Add(upstreamModelTransientTTL)
	blockedUntil := newBlockedUntil
	if blockedUntil.Before(state.BlockedUntil) {
		blockedUntil = state.BlockedUntil
	}
	state.BlockedUntil = blockedUntil
	// A successful Redis write only makes the mirror authoritative when the
	// Redis deadline covers it. If a longer local-only cooldown was created
	// during an outage, keep treating that remainder as fallback state.
	state.RedisSynced = redisSynced && !preservesLocalFallback
	upstreamModelTransientRegistry.revision++
	state.Revision = upstreamModelTransientRegistry.revision
	upstreamModelTransientRegistry.entries[key] = state
	return upstreamModelTransientRemainingDuration(state.BlockedUntil, now)
}

func upstreamModelTransientLocalRecordFailure(key string) time.Duration {
	now := time.Now()
	upstreamModelTransientRegistry.Lock()
	defer upstreamModelTransientRegistry.Unlock()
	upstreamModelTransientPruneLocked(now, key)
	state := upstreamModelTransientRegistry.entries[key]
	if !state.ExpiresAt.IsZero() && now.After(state.ExpiresAt) {
		state = upstreamModelTransientState{}
	}
	state.Failures++
	backoff := upstreamModelTransientRandomBackoff(state.Failures)
	state.ExpiresAt = now.Add(upstreamModelTransientTTL)
	blockedUntil := now.Add(backoff)
	if blockedUntil.Before(state.BlockedUntil) {
		blockedUntil = state.BlockedUntil
	}
	state.BlockedUntil = blockedUntil
	state.RedisSynced = false
	upstreamModelTransientRegistry.revision++
	state.Revision = upstreamModelTransientRegistry.revision
	upstreamModelTransientRegistry.entries[key] = state
	return upstreamModelTransientRemainingDuration(state.BlockedUntil, now)
}

func upstreamModelTransientPruneLocked(now time.Time, key string) {
	for existingKey, existingState := range upstreamModelTransientRegistry.entries {
		if now.After(existingState.ExpiresAt) {
			delete(upstreamModelTransientRegistry.entries, existingKey)
		}
	}
	if _, exists := upstreamModelTransientRegistry.entries[key]; !exists &&
		len(upstreamModelTransientRegistry.entries) >= upstreamModelTransientMaxEntries {
		var oldestKey string
		var oldestExpiry time.Time
		for existingKey, existingState := range upstreamModelTransientRegistry.entries {
			if oldestKey == "" || existingState.ExpiresAt.Before(oldestExpiry) {
				oldestKey = existingKey
				oldestExpiry = existingState.ExpiresAt
			}
		}
		if oldestKey != "" {
			delete(upstreamModelTransientRegistry.entries, oldestKey)
		}
	}
}

// RecordUpstreamAccountModelTransientFailureContext records a model-scoped
// capacity signal in Redis when available. Redis failures fail open to the
// process-local registry, preserving single-node behavior during an outage.
func RecordUpstreamAccountModelTransientFailureContext(ctx context.Context, accountID int, modelName string) time.Duration {
	key := upstreamModelTransientKey(accountID, modelName)
	if accountID <= 0 || strings.TrimSpace(modelName) == "" || key == "" {
		return 0
	}
	if backoff, failures, ok := upstreamModelTransientRedisRecord(ctx, accountID, modelName); ok {
		// Keep a local mirror so a later Redis outage does not forget the recent
		// shared failure state.
		return upstreamModelTransientLocalRecord(key, backoff, failures, true)
	}
	return upstreamModelTransientLocalRecordFailure(key)
}

// RecordUpstreamAccountModelTransientFailure remains source-compatible with
// existing callers that do not carry a request context.
func RecordUpstreamAccountModelTransientFailure(accountID int, modelName string) time.Duration {
	return RecordUpstreamAccountModelTransientFailureContext(context.Background(), accountID, modelName)
}

func IsUpstreamAccountModelTransientBlockedContext(ctx context.Context, accountID int, modelName string) bool {
	key := upstreamModelTransientKey(accountID, modelName)
	if accountID <= 0 || strings.TrimSpace(modelName) == "" || key == "" {
		return false
	}
	// Snapshot before the Redis command. If the revision changes while Redis
	// is queried, the newer local state belongs to a concurrent operation and
	// must not be discarded based on the older Redis result.
	localBlockedBefore, localRedisSyncedBefore, localRevisionBefore := upstreamModelTransientLocalBlockedState(key)
	redisBlocked, redisOK := upstreamModelTransientRedisBlocked(ctx, accountID, modelName)
	if !redisOK {
		localBlocked, _, _ := upstreamModelTransientLocalBlockedState(key)
		return localBlocked
	}
	if redisBlocked {
		return true
	}
	localBlockedAfter, localRedisSyncedAfter, localRevisionAfter := upstreamModelTransientLocalBlockedState(key)
	if localRevisionAfter != localRevisionBefore {
		return localBlockedAfter
	}
	if localRedisSyncedBefore {
		// A successful Redis miss/expiry is authoritative for a mirror that was
		// populated from Redis. Remove it only if no newer local failure replaced
		// that mirror after the second snapshot.
		if !upstreamModelTransientLocalClearIfRevision(key, localRevisionBefore) {
			latestBlocked, latestRedisSynced, _ := upstreamModelTransientLocalBlockedState(key)
			return upstreamModelTransientMergeBlocked(redisBlocked, redisOK, latestBlocked, latestRedisSynced)
		}
		return false
	}
	return upstreamModelTransientMergeBlocked(
		redisBlocked,
		redisOK,
		localBlockedBefore,
		localRedisSyncedAfter,
	)
}

func upstreamModelTransientLocalBlockedState(key string) (blocked bool, redisSynced bool, revision uint64) {
	now := time.Now()
	upstreamModelTransientRegistry.Lock()
	defer upstreamModelTransientRegistry.Unlock()
	state, ok := upstreamModelTransientRegistry.entries[key]
	if !ok {
		return false, false, 0
	}
	if now.After(state.ExpiresAt) {
		delete(upstreamModelTransientRegistry.entries, key)
		return false, false, 0
	}
	return now.Before(state.BlockedUntil), state.RedisSynced, state.Revision
}

func upstreamModelTransientLocalClearIfRevision(key string, revision uint64) bool {
	upstreamModelTransientRegistry.Lock()
	defer upstreamModelTransientRegistry.Unlock()
	state, exists := upstreamModelTransientRegistry.entries[key]
	if !exists {
		return revision == 0
	}
	if state.Revision != revision {
		return false
	}
	delete(upstreamModelTransientRegistry.entries, key)
	return true
}

func upstreamModelTransientLocalRevision(key string) uint64 {
	upstreamModelTransientRegistry.Lock()
	defer upstreamModelTransientRegistry.Unlock()
	if state, exists := upstreamModelTransientRegistry.entries[key]; exists {
		return state.Revision
	}
	return 0
}

func upstreamModelTransientMergeBlocked(redisBlocked, redisOK, localBlocked, localRedisSynced bool) bool {
	if !redisOK {
		// Redis errors must fall back to every still-valid local mirror.
		return localBlocked
	}
	if redisBlocked {
		return true
	}
	// A successful Redis miss is authoritative only for state which was
	// mirrored from Redis. Outage fallback state remains protective locally.
	return localBlocked && !localRedisSynced
}

func IsUpstreamAccountModelTransientBlocked(accountID int, modelName string) bool {
	return IsUpstreamAccountModelTransientBlockedContext(context.Background(), accountID, modelName)
}

func ClearUpstreamAccountModelTransientContext(ctx context.Context, accountID int, modelName string) {
	key := upstreamModelTransientKey(accountID, modelName)
	if accountID <= 0 || strings.TrimSpace(modelName) == "" || key == "" {
		return
	}
	localRevision := upstreamModelTransientLocalRevision(key)
	upstreamModelTransientRedisClear(ctx, accountID, modelName)
	// Do not erase a failure recorded while the bounded Redis DEL was in
	// flight. Its newer revision remains available as a local outage fallback.
	upstreamModelTransientLocalClearIfRevision(key, localRevision)
}

func ClearUpstreamAccountModelTransient(accountID int, modelName string) {
	ClearUpstreamAccountModelTransientContext(context.Background(), accountID, modelName)
}

// UpstreamAccountCapacityRetryDelay returns a context-friendly same-account
// retry delay. retryCount is zero-based (0 => 500ms, 1 => 1s).
func UpstreamAccountCapacityRetryDelay(retryCount int) (time.Duration, bool) {
	maxRetries := common.GetEnvOrDefault("UPSTREAM_ACCOUNT_CAPACITY_SAME_ACCOUNT_RETRIES", upstreamCapacityRetryDefaultCount)
	if retryCount < 0 || retryCount >= maxRetries {
		return 0, false
	}
	delay := upstreamCapacityRetryBaseDelay << retryCount
	if delay > upstreamCapacityRetryMaximumDelay {
		delay = upstreamCapacityRetryMaximumDelay
	}
	return delay, true
}

func WaitForUpstreamAccountRetry(ctx context.Context, delay time.Duration) bool {
	if delay <= 0 {
		return true
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}
