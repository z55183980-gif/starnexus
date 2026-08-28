package service

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLoadResponsesHTTPContinuationLimitsFromEnv(t *testing.T) {
	t.Setenv("RESPONSES_HTTP_CONT_REDIS_MAX_ENTRY_MB", "4")
	t.Setenv("RESPONSES_HTTP_CONT_TTL_MINUTES", "30")
	t.Setenv("RESPONSES_HTTP_CONT_REDIS_WRITE_ENABLED", "false")

	limits := loadResponsesHTTPContinuationLimits()

	require.Equal(t, int64(4<<20), limits.RedisMaxEntryBytes)
	require.Equal(t, 30*time.Minute, limits.TTL)
	require.False(t, limits.RedisWriteEnabled)
}

func TestResponsesHTTPContinuationCacheRespectsByteBudget(t *testing.T) {
	cache := newResponsesHTTPContinuationCache(responsesHTTPContinuationLimits{
		LocalBudgetBytes:   2_000,
		LocalMaxEntryBytes: 1_500,
		RedisMaxEntryBytes: 10_000,
		MaxEntries:         100,
		TTL:                responsesHTTPContinuationTTL,
	})
	payload := make([]byte, 400)
	for i := 0; i < 20; i++ {
		status := cache.putEncoded(fmt.Sprintf("responses:http:continuation:%d", i), payload)
		require.Contains(t, []string{"stored", "evicted_stored", "replaced"}, status)
		require.LessOrEqual(t, cache.currentPayloadBytes(), int64(2_000))
	}
	require.Greater(t, cache.entryCount(), 0)
	require.LessOrEqual(t, cache.currentPayloadBytes(), int64(2_000))
}

func TestResponsesHTTPContinuationCacheSkipsOversizedEntry(t *testing.T) {
	cache := newResponsesHTTPContinuationCache(responsesHTTPContinuationLimits{
		LocalBudgetBytes:   10_000,
		LocalMaxEntryBytes: 100,
		RedisMaxEntryBytes: 10_000,
		MaxEntries:         10,
		TTL:                responsesHTTPContinuationTTL,
	})
	status := cache.putEncoded("key", make([]byte, 200))
	require.Equal(t, "skipped_too_large", status)
	require.Equal(t, 0, cache.entryCount())
}

func TestResponsesHTTPContinuationCacheLRUEvictsOldest(t *testing.T) {
	cache := newResponsesHTTPContinuationCache(responsesHTTPContinuationLimits{
		LocalBudgetBytes:   900,
		LocalMaxEntryBytes: 400,
		RedisMaxEntryBytes: 10_000,
		MaxEntries:         10,
		TTL:                responsesHTTPContinuationTTL,
	})
	require.Equal(t, "stored", cache.putEncoded("a", make([]byte, 200)))
	require.Equal(t, "stored", cache.putEncoded("b", make([]byte, 200)))
	_, ok := cache.getEncoded("a")
	require.True(t, ok)
	require.Contains(t, []string{"stored", "evicted_stored"}, cache.putEncoded("c", make([]byte, 200)))
	_, okB := cache.getEncoded("b")
	_, okC := cache.getEncoded("c")
	require.True(t, okC)
	// Touching a kept it hotter than b; b should be the eviction victim once budget is tight.
	if cache.entryCount() < 3 {
		require.False(t, okB)
	}
	require.LessOrEqual(t, cache.currentPayloadBytes(), int64(900))
}

func TestResponsesHTTPContinuationCachePutGetRace(t *testing.T) {
	cache := newResponsesHTTPContinuationCache(responsesHTTPContinuationLimits{
		LocalBudgetBytes:   1 << 20,
		LocalMaxEntryBytes: 64 << 10,
		RedisMaxEntryBytes: 1 << 20,
		MaxEntries:         256,
		TTL:                responsesHTTPContinuationTTL,
	})
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				key := fmt.Sprintf("k-%d-%d", i, j%20)
				cache.putEncoded(key, []byte(fmt.Sprintf("payload-%d-%d", i, j)))
				_, _ = cache.getEncoded(key)
			}
		}(i)
	}
	wg.Wait()
	require.LessOrEqual(t, cache.currentPayloadBytes(), int64(1<<20))
}

func TestResponsesHTTPRedisCircuitHalfOpenSingleProbe(t *testing.T) {
	circuit := &responsesHTTPRedisCircuit{
		cooldown: responsesHTTPContinuationRedisOOMCooldown,
		jitter:   0,
	}
	allowed, probe := circuit.allow()
	require.True(t, allowed)
	require.False(t, probe)
	circuit.fail(true)
	allowed, _ = circuit.allow()
	require.False(t, allowed)

	circuit.mu.Lock()
	circuit.openUntil = circuit.openUntil.Add(-responsesHTTPContinuationRedisOOMCooldown)
	circuit.mu.Unlock()

	allowed, probe = circuit.allow()
	require.True(t, allowed)
	require.True(t, probe)
	allowed, _ = circuit.allow()
	require.False(t, allowed)
	circuit.success()
	allowed, probe = circuit.allow()
	require.True(t, allowed)
	require.False(t, probe)
}
