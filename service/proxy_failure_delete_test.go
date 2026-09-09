package service

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestDeleteProxyRealRequestFailure(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	proxyID, otherProxyID := 16, 17
	metadata, err := common.Marshal(ProxyFailureMetadata{ProxyFailure: true, ProxyTimeout: true})
	require.NoError(t, err)
	events := []model.UpstreamAccountEvent{
		{ProxyId: &proxyID, EventType: "request_error", Metadata: string(metadata), CreatedAt: time.Now().Unix()},
		{ProxyId: &otherProxyID, EventType: "request_error", Metadata: string(metadata)},
		{ProxyId: &proxyID, EventType: "request_success", Metadata: string(metadata)},
		{ProxyId: &proxyID, EventType: "request_error", Metadata: "{}"},
	}
	for i := range events {
		require.NoError(t, model.DB.Create(&events[i]).Error)
	}
	ctx := context.Background()
	require.Error(t, DeleteProxyRealRequestFailure(ctx, 0, events[0].Id))
	require.Error(t, DeleteProxyRealRequestFailure(ctx, proxyID, 0))
	for _, event := range events[1:] {
		require.Error(t, DeleteProxyRealRequestFailure(ctx, proxyID, event.Id))
	}
	health, err := LoadProxyRealRequestHealth([]int{proxyID}, 24*time.Hour)
	require.NoError(t, err)
	require.EqualValues(t, 1, health[proxyID].FailureCount24h)
	require.NoError(t, DeleteProxyRealRequestFailure(ctx, proxyID, events[0].Id))
	require.Error(t, DeleteProxyRealRequestFailure(ctx, proxyID, events[0].Id))
	var remaining int64
	require.NoError(t, model.DB.Model(&model.UpstreamAccountEvent{}).Count(&remaining).Error)
	require.EqualValues(t, 3, remaining)
	items, err := ListProxyRealRequestFailures(proxyID, 50)
	require.NoError(t, err)
	require.Empty(t, items)
	health, err = LoadProxyRealRequestHealth([]int{proxyID}, 24*time.Hour)
	require.NoError(t, err)
	require.Zero(t, health[proxyID].FailureCount24h)
	require.Zero(t, health[proxyID].TimeoutCount24h)
	require.Zero(t, health[proxyID].ConsecutiveFailures)
	require.Nil(t, health[proxyID].LastFailureAt)
}
