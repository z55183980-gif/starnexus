package controller

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestDashboardQueueWaitsAndReleases(t *testing.T) {
	slots, waiters := make(chan struct{}, 1), make(chan struct{}, 1)
	release, err := acquireDashboardSlot(context.Background(), slots, waiters, time.Second)
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() {
		nextRelease, nextErr := acquireDashboardSlot(context.Background(), slots, waiters, time.Second)
		if nextErr == nil {
			nextRelease()
		}
		done <- nextErr
	}()
	require.Eventually(t, func() bool { return len(waiters) == 1 }, time.Second, time.Millisecond)
	// Queue capacity is bounded even while the database remains busy.
	_, err = acquireDashboardSlot(context.Background(), slots, waiters, time.Second)
	require.ErrorIs(t, err, errDashboardBusy)
	release()
	require.NoError(t, <-done)
	require.Empty(t, slots)
	require.Empty(t, waiters)
}

func TestDashboardQueueTimeoutAndCancellation(t *testing.T) {
	slots, waiters := make(chan struct{}, 1), make(chan struct{}, 1)
	slots <- struct{}{}
	_, err := acquireDashboardSlot(context.Background(), slots, waiters, time.Millisecond)
	require.ErrorIs(t, err, errDashboardBusy)
	require.Empty(t, waiters)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := acquireDashboardSlot(ctx, slots, waiters, time.Second)
		done <- err
	}()
	require.Eventually(t, func() bool { return len(waiters) == 1 }, time.Second, time.Millisecond)
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	require.Empty(t, waiters)
	<-slots
	_, err = acquireDashboardSlot(ctx, slots, waiters, time.Second)
	require.ErrorIs(t, err, context.Canceled)
	require.Empty(t, slots)
}

func TestDashboardBusyResponse(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	dashboardQueryError(c, errors.Join(errors.New("query dashboard data"), errDashboardBusy))
	var response struct {
		Success bool   `json:"success"`
		Code    string `json:"code"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.False(t, response.Success)
	require.Equal(t, "dashboard_busy", response.Code)
	require.Equal(t, "1", recorder.Header().Get("Retry-After"))
}

func TestDashboardStaleCacheHasAgeLimit(t *testing.T) {
	cache := newTTLCache[int](time.Second, 10)
	cache.items["recent"] = ttlCacheItem[int]{value: 42, expiresAt: time.Now().Add(-time.Minute)}
	cache.items["old"] = ttlCacheItem[int]{value: 1, expiresAt: time.Now().Add(-3 * time.Minute)}
	value, ok := cache.GetStale("recent")
	require.True(t, ok)
	require.Equal(t, 42, value)
	_, ok = cache.GetStale("old")
	require.False(t, ok)
}
