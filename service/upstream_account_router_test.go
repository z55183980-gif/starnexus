package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestUpstreamAccountRouterSelectsDistinctCapacityAndResolvesProxy(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	ctx := context.Background()

	poolProxy := createRouterTestProxy(t, "pool-proxy", 1080)
	accountProxy := createRouterTestProxy(t, "account-proxy", 1081)
	pool := model.UpstreamAccountPool{
		Name: "router-pool", Platform: constant.UpstreamPlatformOpenAI,
		CredentialType: constant.UpstreamAccountTypeOAuth, Status: constant.UpstreamStatusActive,
		DefaultProxyId: &poolProxy.Id, SchedulerConfig: "{}",
	}
	require.NoError(t, CreateUpstreamAccountPool(&pool))

	first := createRouterTestAccount(t, "first", pool.Id, &accountProxy.Id)
	second := createRouterTestAccount(t, "second", pool.Id, nil)
	router, err := NewUpstreamAccountRouter(NewLocalUpstreamAccountLeaseManager(), time.Minute)
	require.NoError(t, err)

	selectionOne, err := router.Select(ctx, UpstreamAccountSelectionRequest{PoolId: pool.Id, ChannelType: constant.ChannelTypeCodex})
	require.NoError(t, err)
	require.Equal(t, "acct-"+selectionOne.Account.Name, selectionOne.Credentials["account_id"])
	require.NotNil(t, selectionOne.Proxy)
	if selectionOne.Account.Id == first.Id {
		require.Equal(t, accountProxy.Id, selectionOne.Proxy.Id)
	} else {
		require.Equal(t, second.Id, selectionOne.Account.Id)
		require.Equal(t, poolProxy.Id, selectionOne.Proxy.Id)
	}

	selectionTwo, err := router.Select(ctx, UpstreamAccountSelectionRequest{PoolId: pool.Id, ChannelType: constant.ChannelTypeCodex})
	require.NoError(t, err)
	require.NotEqual(t, selectionOne.Account.Id, selectionTwo.Account.Id)
	require.NotNil(t, selectionTwo.Proxy)
	if selectionTwo.Account.Id == first.Id {
		require.Equal(t, accountProxy.Id, selectionTwo.Proxy.Id)
	} else {
		require.Equal(t, poolProxy.Id, selectionTwo.Proxy.Id)
	}

	_, err = router.Select(ctx, UpstreamAccountSelectionRequest{PoolId: pool.Id, ChannelType: constant.ChannelTypeCodex})
	require.Error(t, err)
	require.Contains(t, err.Error(), "concurrency_full")
	require.NoError(t, selectionOne.Lease.Release(ctx))
	require.NoError(t, selectionTwo.Lease.Release(ctx))
}

func TestUpstreamAccountRouterUsesChannelProxyWhenNoLocalProxy(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	pool := model.UpstreamAccountPool{
		Name: "direct-pool", Platform: constant.UpstreamPlatformOpenAI,
		CredentialType: constant.UpstreamAccountTypeOAuth, Status: constant.UpstreamStatusActive,
		SchedulerConfig: "{}",
	}
	require.NoError(t, CreateUpstreamAccountPool(&pool))
	createRouterTestAccount(t, "only", pool.Id, nil)
	router, err := NewUpstreamAccountRouter(NewLocalUpstreamAccountLeaseManager(), time.Minute)
	require.NoError(t, err)
	selection, err := router.Select(context.Background(), UpstreamAccountSelectionRequest{
		PoolId: pool.Id, ChannelType: constant.ChannelTypeCodex, ChannelProxy: "http://channel-proxy:8080",
	})
	require.NoError(t, err)
	require.Nil(t, selection.Proxy)
	require.Equal(t, "http://channel-proxy:8080", selection.ProxyURL)
	require.NoError(t, selection.Lease.Release(context.Background()))
}

func TestResolveUpstreamProxyRechecksMigratedFallbackOrigin(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	origin := createRouterTestProxy(t, "origin-proxy", 1082)
	backup := createRouterTestProxy(t, "backup-proxy", 1083)
	expiredAt := time.Now().Add(-time.Hour).Unix()
	origin.ExpiresAt = &expiredAt
	origin.FallbackMode = constant.UpstreamProxyFallbackProxy
	origin.BackupProxyId = &backup.Id
	require.NoError(t, UpdateUpstreamProxy(&UpstreamProxyUpdateInput{Proxy: origin}))
	account := model.UpstreamAccount{ProxyId: &backup.Id, ProxyFallbackOriginId: &origin.Id}

	resolved, _, err := resolveUpstreamProxy(context.Background(), &account, nil, "")
	require.NoError(t, err)
	require.Equal(t, backup.Id, resolved.Id)

	renewedUntil := time.Now().Add(time.Hour).Unix()
	origin.ExpiresAt = &renewedUntil
	require.NoError(t, UpdateUpstreamProxy(&UpstreamProxyUpdateInput{Proxy: origin}))
	resolved, _, err = resolveUpstreamProxy(context.Background(), &account, nil, "")
	require.NoError(t, err)
	require.Equal(t, origin.Id, resolved.Id)
}

func TestUpstreamAccountRouterHonorsSchedulerTopK(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	pool := model.UpstreamAccountPool{
		Name: "top-k-pool", Platform: constant.UpstreamPlatformOpenAI,
		CredentialType: constant.UpstreamAccountTypeOAuth, Status: constant.UpstreamStatusActive,
		SchedulerConfig: `{"version":1,"top_k":1}`,
	}
	require.NoError(t, CreateUpstreamAccountPool(&pool))
	busy := createRouterTestAccount(t, "busy", pool.Id, nil)
	idle := createRouterTestAccount(t, "idle", pool.Id, nil)
	require.NoError(t, model.DB.Model(&model.UpstreamAccount{}).Where("id = ?", busy.Id).Update("concurrency", 2).Error)

	manager := NewLocalUpstreamAccountLeaseManager()
	busyLease, err := manager.Acquire(context.Background(), busy.Id, 2, time.Minute)
	require.NoError(t, err)
	router, err := NewUpstreamAccountRouter(manager, time.Minute)
	require.NoError(t, err)
	selection, err := router.Select(context.Background(), UpstreamAccountSelectionRequest{
		PoolId: pool.Id, ChannelType: constant.ChannelTypeCodex,
	})
	require.NoError(t, err)
	require.Equal(t, idle.Id, selection.Account.Id)
	require.NoError(t, selection.Release(context.Background()))
	require.NoError(t, busyLease.Release(context.Background()))
}

func TestUpstreamAccountRouterTreatsZeroMembershipPriorityAsHighest(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	pool := model.UpstreamAccountPool{
		Name: "priority-pool", Platform: constant.UpstreamPlatformOpenAI,
		CredentialType: constant.UpstreamAccountTypeOAuth, Status: constant.UpstreamStatusActive,
		SchedulerConfig: "{}",
	}
	require.NoError(t, CreateUpstreamAccountPool(&pool))
	zero := createRouterTestAccount(t, "zero", pool.Id, nil)
	other := createRouterTestAccount(t, "other", pool.Id, nil)
	require.NoError(t, model.DB.Model(&model.UpstreamAccountPoolMember{}).
		Where("pool_id = ? AND account_id = ?", pool.Id, zero.Id).Update("priority", 0).Error)
	require.NoError(t, model.DB.Model(&model.UpstreamAccountPoolMember{}).
		Where("pool_id = ? AND account_id = ?", pool.Id, other.Id).Update("priority", 1).Error)

	router, err := NewUpstreamAccountRouter(NewLocalUpstreamAccountLeaseManager(), time.Minute)
	require.NoError(t, err)
	selection, err := router.Select(context.Background(), UpstreamAccountSelectionRequest{
		PoolId: pool.Id, ChannelType: constant.ChannelTypeCodex,
	})
	require.NoError(t, err)
	require.Equal(t, zero.Id, selection.Account.Id)
	require.NoError(t, selection.Release(context.Background()))
}

func TestUpstreamAccountSupportsModelCapabilities(t *testing.T) {
	tests := []struct {
		name      string
		extra     string
		modelName string
		want      bool
	}{
		{name: "empty allows all", extra: "{}", modelName: "gpt-5.6-sol", want: true},
		{name: "exact supported", extra: `{"supported_models":["gpt-5.6-sol"]}`, modelName: "gpt-5.6-sol", want: true},
		{name: "wildcard supported", extra: `{"models":["gpt-5.*"]}`, modelName: "gpt-5.6-sol", want: true},
		{name: "prefix supported", extra: `{"model_prefixes":["gpt-5"]}`, modelName: "gpt-5.6-sol", want: true},
		{name: "not supported", extra: `{"models":["gpt-4.1"]}`, modelName: "gpt-5.6-sol", want: false},
		{name: "excluded wins", extra: `{"models":["gpt-5.*"],"excluded_models":["gpt-5.6-sol"]}`, modelName: "gpt-5.6-sol", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := upstreamAccountSupportsModel(test.extra, test.modelName)
			require.NoError(t, err)
			require.Equal(t, test.want, got)
		})
	}
	_, err := upstreamAccountSupportsModel(`{"models":42}`, "gpt-5")
	require.Error(t, err)
}

type failingLeaseRefreshBackend struct{}

func (failingLeaseRefreshBackend) refresh(context.Context, *UpstreamAccountLease, time.Duration) error {
	return errors.New("lease missing")
}

func (failingLeaseRefreshBackend) release(context.Context, *UpstreamAccountLease) error {
	return nil
}

func TestUpstreamAccountSelectionCancelsRequestWhenLeaseRefreshIsLost(t *testing.T) {
	originalMinInterval := upstreamAccountLeaseRefreshMinInterval
	upstreamAccountLeaseRefreshMinInterval = 5 * time.Millisecond
	t.Cleanup(func() { upstreamAccountLeaseRefreshMinInterval = originalMinInterval })

	selection := &UpstreamAccountSelection{
		Lease:    &UpstreamAccountLease{Id: "lost", AccountId: 7, backend: failingLeaseRefreshBackend{}},
		leaseTTL: 30 * time.Millisecond,
	}
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(context.Canceled)
	selection.StartLeaseRefresh(ctx, cancel)
	select {
	case <-ctx.Done():
		require.ErrorContains(t, context.Cause(ctx), "upstream account lease lost")
	case <-time.After(time.Second):
		t.Fatal("lease refresh failure did not cancel the request")
	}
	require.NoError(t, selection.Release(context.Background()))
}

func createRouterTestProxy(t *testing.T, name string, port int) model.UpstreamProxy {
	t.Helper()
	input := UpstreamProxyCreateInput{
		Proxy: model.UpstreamProxy{
			Name: name, Protocol: constant.UpstreamProxyProtocolSOCKS5H, Host: "127.0.0.1", Port: port,
			Status: constant.UpstreamStatusActive, FallbackMode: constant.UpstreamProxyFallbackNone,
		},
		Auth: UpstreamProxyAuthInput{Username: name, Password: name + "-secret"},
	}
	require.NoError(t, CreateUpstreamProxy(&input))
	return input.Proxy
}

func createRouterTestAccount(t *testing.T, name string, poolId int, proxyId *int) model.UpstreamAccount {
	t.Helper()
	input := UpstreamAccountCreateInput{
		Account: model.UpstreamAccount{
			Name: name, Platform: constant.UpstreamPlatformOpenAI, Type: constant.UpstreamAccountTypeOAuth,
			Extra: "{}", ProxyId: proxyId, Concurrency: 1, Priority: 1, Weight: 1,
			Status: constant.UpstreamStatusActive, Schedulable: true, AutoPauseOnExpired: true,
		},
		Credentials: map[string]any{"access_token": name + "-secret", "account_id": "acct-" + name},
		PoolIds:     []int{poolId},
	}
	require.NoError(t, CreateUpstreamAccount(&input))
	return input.Account
}
