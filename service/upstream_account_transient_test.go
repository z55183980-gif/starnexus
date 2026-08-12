package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestUpstreamAccountModelTransientBackoffAndClear(t *testing.T) {
	accountID := 91001
	modelName := "gpt-5.4"
	ClearUpstreamAccountModelTransient(accountID, modelName)
	t.Cleanup(func() { ClearUpstreamAccountModelTransient(accountID, modelName) })

	require.Zero(t, RecordUpstreamAccountModelTransientFailure(accountID, modelName))
	require.False(t, IsUpstreamAccountModelTransientBlocked(accountID, modelName))
	require.Equal(t, upstreamModelTransientSecondBackoff, RecordUpstreamAccountModelTransientFailure(accountID, modelName))
	require.True(t, IsUpstreamAccountModelTransientBlocked(accountID, modelName))
	require.False(t, IsUpstreamAccountModelTransientBlocked(accountID, "gpt-5.3"))

	ClearUpstreamAccountModelTransient(accountID, modelName)
	require.False(t, IsUpstreamAccountModelTransientBlocked(accountID, modelName))
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
