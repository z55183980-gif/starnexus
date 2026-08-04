package service

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestLocalUpstreamAccountAffinityWaiterLimit(t *testing.T) {
	originalRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = originalRedisEnabled })

	store := &defaultUpstreamAccountAffinityStore{waiters: make(map[string]int)}
	key := buildUpstreamAccountAffinityWaiterKey(3, 9)
	acquired, err := store.AcquireWaiter(context.Background(), key, 1, time.Minute)
	require.NoError(t, err)
	require.True(t, acquired)

	acquired, err = store.AcquireWaiter(context.Background(), key, 1, time.Minute)
	require.NoError(t, err)
	require.False(t, acquired)

	require.NoError(t, store.ReleaseWaiter(context.Background(), key))
	acquired, err = store.AcquireWaiter(context.Background(), key, 1, time.Minute)
	require.NoError(t, err)
	require.True(t, acquired)
	require.NoError(t, store.ReleaseWaiter(context.Background(), key))
}
