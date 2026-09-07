package service

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestUpstreamAccountModelTransientBackoffAndClear(t *testing.T) {
	originalRedisEnabled, originalRDB := common.RedisEnabled, common.RDB
	common.RedisEnabled = false
	common.RDB = nil
	t.Cleanup(func() {
		common.RedisEnabled, common.RDB = originalRedisEnabled, originalRDB
	})
	t.Setenv("UPSTREAM_ACCOUNT_CAPACITY_COOLDOWN_JITTER_PERCENT", "0")
	accountID := 91001
	modelName := "gpt-5.4"
	ClearUpstreamAccountModelTransient(accountID, modelName)
	t.Cleanup(func() { ClearUpstreamAccountModelTransient(accountID, modelName) })

	require.Equal(t, 15*time.Second, RecordUpstreamAccountModelTransientFailure(accountID, modelName))
	require.True(t, IsUpstreamAccountModelTransientBlocked(accountID, modelName))
	require.GreaterOrEqual(t, RecordUpstreamAccountModelTransientFailure(accountID, modelName), upstreamModelTransientSecondBackoff)
	require.True(t, IsUpstreamAccountModelTransientBlocked(accountID, modelName))
	require.False(t, IsUpstreamAccountModelTransientBlocked(accountID, "gpt-5.3"))

	ClearUpstreamAccountModelTransient(accountID, modelName)
	require.False(t, IsUpstreamAccountModelTransientBlocked(accountID, modelName))
}

func TestClearUpstreamAccountModelTransientClearsLocalState(t *testing.T) {
	originalRedisEnabled, originalRDB := common.RedisEnabled, common.RDB
	common.RedisEnabled = false
	common.RDB = nil
	t.Cleanup(func() {
		common.RedisEnabled, common.RDB = originalRedisEnabled, originalRDB
	})
	t.Setenv("UPSTREAM_ACCOUNT_CAPACITY_COOLDOWN_JITTER_PERCENT", "0")
	accountID := 91004
	modelName := "gpt-5.4"
	ClearUpstreamAccountModelTransient(accountID, modelName)
	t.Cleanup(func() { ClearUpstreamAccountModelTransient(accountID, modelName) })

	ClearUpstreamAccountModelTransient(accountID, modelName)
	require.False(t, IsUpstreamAccountModelTransientBlocked(accountID, modelName))
	require.Equal(t, 15*time.Second, RecordUpstreamAccountModelTransientFailure(accountID, modelName))
	require.True(t, IsUpstreamAccountModelTransientBlocked(accountID, modelName))
	ClearUpstreamAccountModelTransient(accountID, modelName)
	require.False(t, IsUpstreamAccountModelTransientBlocked(accountID, modelName))
}

func TestUpstreamAccountModelTransientKeepsLongerBlockedUntil(t *testing.T) {
	originalRedisEnabled, originalRDB := common.RedisEnabled, common.RDB
	common.RedisEnabled = false
	common.RDB = nil
	t.Cleanup(func() {
		common.RedisEnabled, common.RDB = originalRedisEnabled, originalRDB
	})
	t.Setenv("UPSTREAM_ACCOUNT_CAPACITY_COOLDOWN_JITTER_PERCENT", "0")
	accountID := 91003
	modelName := "gpt-5.4"
	ClearUpstreamAccountModelTransient(accountID, modelName)
	t.Cleanup(func() { ClearUpstreamAccountModelTransient(accountID, modelName) })

	require.Equal(t, 15*time.Second, RecordUpstreamAccountModelTransientFailure(accountID, modelName))
	require.GreaterOrEqual(t, RecordUpstreamAccountModelTransientFailure(accountID, modelName), 15*time.Second)
	require.True(t, IsUpstreamAccountModelTransientBlocked(accountID, modelName))
}

func TestUpstreamAccountModelTransientClearDoesNotEraseNewerLocalFailure(t *testing.T) {
	originalRedisEnabled, originalRDB := common.RedisEnabled, common.RDB
	common.RedisEnabled = false
	common.RDB = nil
	t.Cleanup(func() {
		common.RedisEnabled, common.RDB = originalRedisEnabled, originalRDB
	})
	t.Setenv("UPSTREAM_ACCOUNT_CAPACITY_COOLDOWN_JITTER_PERCENT", "0")
	key := upstreamModelTransientKey(91005, "gpt-5.4")

	upstreamModelTransientLocalRecord(key, 15*time.Second, 1, true)
	clearRevision := upstreamModelTransientLocalRevision(key)
	upstreamModelTransientLocalRecordFailure(key)

	require.False(t, upstreamModelTransientLocalClearIfRevision(key, clearRevision))
	blocked, redisSynced, _ := upstreamModelTransientLocalBlockedState(key)
	require.True(t, blocked)
	require.False(t, redisSynced)
	upstreamModelTransientRegistry.Lock()
	delete(upstreamModelTransientRegistry.entries, key)
	upstreamModelTransientRegistry.Unlock()
}

func TestUpstreamAccountCapacityRetryDelay(t *testing.T) {
	delay, ok := UpstreamAccountCapacityRetryDelay(0)
	require.True(t, ok)
	require.Equal(t, 500*time.Millisecond, delay)
	delay, ok = UpstreamAccountCapacityRetryDelay(1)
	require.True(t, ok)
	require.Equal(t, time.Second, delay)
	_, ok = UpstreamAccountCapacityRetryDelay(2)
	require.False(t, ok)
}

func TestWaitForUpstreamAccountRetryHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.False(t, WaitForUpstreamAccountRetry(ctx, time.Second))
}

func TestUpstreamAccountModelTransientRedisUnavailableFallsBackToLocal(t *testing.T) {
	originalRedisEnabled, originalRDB := common.RedisEnabled, common.RDB
	common.RedisEnabled = true
	common.RDB = nil
	t.Cleanup(func() {
		common.RedisEnabled, common.RDB = originalRedisEnabled, originalRDB
	})
	t.Setenv("UPSTREAM_ACCOUNT_CAPACITY_COOLDOWN_JITTER_PERCENT", "0")
	accountID := 91002
	modelName := "gpt-5.4"
	ClearUpstreamAccountModelTransient(accountID, modelName)
	t.Cleanup(func() { ClearUpstreamAccountModelTransient(accountID, modelName) })

	require.Equal(t, 15*time.Second, RecordUpstreamAccountModelTransientFailureContext(context.Background(), accountID, modelName))
	require.True(t, IsUpstreamAccountModelTransientBlockedContext(context.Background(), accountID, modelName))
	ClearUpstreamAccountModelTransientContext(context.Background(), accountID, modelName)
	require.False(t, IsUpstreamAccountModelTransientBlockedContext(context.Background(), accountID, modelName))
}

func TestUpstreamModelTransientMergeBlockedPreservesLocalMirror(t *testing.T) {
	// Redis explicitly blocked is authoritative regardless of local state.
	require.True(t, upstreamModelTransientMergeBlocked(true, true, false, false))
	// A successful Redis miss releases a mirror which originally came from
	// Redis, including stale mirrors left on other application nodes.
	require.False(t, upstreamModelTransientMergeBlocked(false, true, true, true))
	// A local-only failure recorded during a Redis outage remains protective
	// when Redis recovers with no key.
	require.True(t, upstreamModelTransientMergeBlocked(false, true, true, false))
	// A Redis query failure falls back to either kind of valid local mirror.
	require.True(t, upstreamModelTransientMergeBlocked(false, false, true, true))
	require.True(t, upstreamModelTransientMergeBlocked(false, false, true, false))
	require.False(t, upstreamModelTransientMergeBlocked(false, false, false, false))
}

func TestUpstreamAccountModelTransientBackoffJitterIsClamped(t *testing.T) {
	t.Setenv("UPSTREAM_ACCOUNT_CAPACITY_COOLDOWN_JITTER_PERCENT", "20")
	require.Equal(t, 12*time.Second, upstreamModelTransientBackoffWithJitter(1, -1))
	require.Equal(t, 18*time.Second, upstreamModelTransientBackoffWithJitter(1, 1))
	require.Equal(t, 8*time.Second, upstreamModelTransientBackoffWithJitter(2, -1))
	require.Equal(t, 54*time.Second, upstreamModelTransientBackoffWithJitter(3, 1))
}

func TestUpstreamAccountModelTransientRandomJitterVaries(t *testing.T) {
	t.Setenv("UPSTREAM_ACCOUNT_CAPACITY_COOLDOWN_JITTER_PERCENT", "20")
	values := make(map[int]struct{})
	for range 32 {
		value := upstreamModelTransientRandomJitterPercent()
		require.LessOrEqual(t, value, 20)
		require.GreaterOrEqual(t, value, -20)
		values[value] = struct{}{}
	}
	require.Greater(t, len(values), 1)
}
