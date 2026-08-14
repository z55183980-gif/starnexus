package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestResponsesWSContinuationStoreEvictsLeastRecentlyUsedByByteBudget(t *testing.T) {
	ctx := newResponsesWSContinuationStoreTestContext()
	state := responsesWSContinuationState{
		model:             "gpt-5",
		replayInput:       []json.RawMessage{json.RawMessage(`{"role":"user","content":"payload"}`)},
		replayInputExists: true,
	}
	firstID := "resp_lru_a"
	secondID := "resp_lru_b"
	thirdID := "resp_lru_c"
	maxBytes := estimateResponsesWSContinuationEntryBytes(responsesWSContinuationKey(ctx, firstID), state) +
		estimateResponsesWSContinuationEntryBytes(responsesWSContinuationKey(ctx, secondID), state)
	store := newResponsesWSContinuationStore(10, maxBytes, time.Hour)

	store.put(ctx, firstID, state)
	store.put(ctx, secondID, state)
	_, found := store.get(ctx, firstID)
	require.True(t, found)
	store.put(ctx, thirdID, state)

	_, found = store.get(ctx, secondID)
	require.False(t, found)
	_, found = store.get(ctx, firstID)
	require.True(t, found)
	_, found = store.get(ctx, thirdID)
	require.True(t, found)
	stats := store.snapshotStats()
	require.Equal(t, 2, stats.Entries)
	require.LessOrEqual(t, stats.EstimatedBytes, stats.MaxBytes)
	require.Equal(t, uint64(1), stats.ByteLimitEvictionsTotal)
	require.Zero(t, stats.EntryLimitEvictionsTotal)
}

func TestResponsesWSContinuationStoreRetainsEntryCountLimit(t *testing.T) {
	ctx := newResponsesWSContinuationStoreTestContext()
	store := newResponsesWSContinuationStore(2, 1024*1024, time.Hour)
	state := responsesWSContinuationState{replayInput: []json.RawMessage{json.RawMessage(`{"input":"small"}`)}}

	store.put(ctx, "resp_count_a", state)
	store.put(ctx, "resp_count_b", state)
	_, found := store.get(ctx, "resp_count_a")
	require.True(t, found)
	store.put(ctx, "resp_count_c", state)

	_, found = store.get(ctx, "resp_count_b")
	require.False(t, found)
	require.Equal(t, uint64(1), store.snapshotStats().EntryLimitEvictionsTotal)
}

func TestResponsesWSContinuationStoreRejectsOversizedEntry(t *testing.T) {
	ctx := newResponsesWSContinuationStoreTestContext()
	state := responsesWSContinuationState{replayInput: []json.RawMessage{json.RawMessage(`{"input":"too-large"}`)}}
	key := responsesWSContinuationKey(ctx, "resp_oversized")
	store := newResponsesWSContinuationStore(10, estimateResponsesWSContinuationEntryBytes(key, state)-1, time.Hour)

	store.put(ctx, "resp_oversized", state)

	stats := store.snapshotStats()
	require.Zero(t, stats.Entries)
	require.Zero(t, stats.EstimatedBytes)
	require.Equal(t, uint64(1), stats.OversizedRejectionsTotal)
}

func TestResponsesWSContinuationStoreReplacementAndExpiryAccounting(t *testing.T) {
	ctx := newResponsesWSContinuationStoreTestContext()
	store := newResponsesWSContinuationStore(10, 1024*1024, time.Hour)
	responseID := "resp_replace"
	store.put(ctx, responseID, responsesWSContinuationState{replayInput: []json.RawMessage{json.RawMessage(`{"input":"old"}`)}})
	newState := responsesWSContinuationState{replayInput: []json.RawMessage{json.RawMessage(`{"input":"new-and-larger"}`)}}
	store.put(ctx, responseID, newState)

	key := responsesWSContinuationKey(ctx, responseID)
	stats := store.snapshotStats()
	require.Equal(t, 1, stats.Entries)
	require.Equal(t, estimateResponsesWSContinuationEntryBytes(key, newState), stats.EstimatedBytes)
	require.Equal(t, uint64(1), stats.ReplacementsTotal)

	store.mu.Lock()
	store.entries[key].state.expiresAt = time.Now().Add(-time.Second)
	store.mu.Unlock()
	stats = store.snapshotStats()
	require.Zero(t, stats.Entries)
	require.Zero(t, stats.EstimatedBytes)
	require.Equal(t, uint64(1), stats.ExpiredEvictionsTotal)
	require.Greater(t, stats.CleanupRunsTotal, uint64(0))

	store.resetStats()
	stats = store.snapshotStats()
	require.Zero(t, stats.HitsTotal)
	require.Zero(t, stats.MissesTotal)
	require.Zero(t, stats.PutsTotal)
}

func TestResponsesWSContinuationStoreCanBeDisabled(t *testing.T) {
	ctx := newResponsesWSContinuationStoreTestContext()
	store := newResponsesWSContinuationStore(10, 0, time.Hour)

	store.put(ctx, "resp_disabled", responsesWSContinuationState{})

	stats := store.snapshotStats()
	require.Zero(t, stats.Entries)
	require.Equal(t, uint64(1), stats.DisabledRejectionsTotal)
}

func newResponsesWSContinuationStoreTestContext() *gin.Context {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	common.SetContextKey(ctx, constant.ContextKeyUserId, 70001)
	common.SetContextKey(ctx, constant.ContextKeyTokenId, 110001)
	return ctx
}
