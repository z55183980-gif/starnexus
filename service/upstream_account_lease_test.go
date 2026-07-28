package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLocalUpstreamAccountLeaseLimitRefreshAndRelease(t *testing.T) {
	manager := NewLocalUpstreamAccountLeaseManager()
	ctx := context.Background()
	first, err := manager.Acquire(ctx, 9, 1, 40*time.Millisecond)
	require.NoError(t, err)
	_, err = manager.Acquire(ctx, 9, 1, time.Second)
	require.ErrorIs(t, err, ErrUpstreamAccountConcurrencyFull)
	require.NoError(t, first.Refresh(ctx, 80*time.Millisecond))
	require.NoError(t, first.Release(ctx))
	require.NoError(t, first.Release(ctx))
	second, err := manager.Acquire(ctx, 9, 1, time.Second)
	require.NoError(t, err)
	require.NotEqual(t, first.Id, second.Id)
	require.NoError(t, second.Release(ctx))
}

func TestLocalUpstreamAccountLeaseExpires(t *testing.T) {
	manager := NewLocalUpstreamAccountLeaseManager()
	ctx := context.Background()
	_, err := manager.Acquire(ctx, 11, 1, 10*time.Millisecond)
	require.NoError(t, err)
	time.Sleep(20 * time.Millisecond)
	lease, err := manager.Acquire(ctx, 11, 1, time.Second)
	require.NoError(t, err)
	require.False(t, errors.Is(err, ErrUpstreamAccountConcurrencyFull))
	require.NoError(t, lease.Release(ctx))
}

func TestLocalUpstreamAccountLeaseBatchCount(t *testing.T) {
	manager := NewLocalUpstreamAccountLeaseManager()
	ctx := context.Background()
	first, err := manager.Acquire(ctx, 21, 2, time.Second)
	require.NoError(t, err)
	second, err := manager.Acquire(ctx, 21, 2, time.Second)
	require.NoError(t, err)
	third, err := manager.Acquire(ctx, 22, 1, time.Second)
	require.NoError(t, err)
	counts, err := manager.CountBatch(ctx, []int{21, 22, 23, 21})
	require.NoError(t, err)
	require.Equal(t, map[int]int{21: 2, 22: 1, 23: 0}, counts)
	require.NoError(t, first.Release(ctx))
	require.NoError(t, second.Release(ctx))
	require.NoError(t, third.Release(ctx))
}
