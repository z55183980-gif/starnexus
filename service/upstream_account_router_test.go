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

func TestUpstreamAccountRouterSessionAffinityReusesAccount(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	pool := model.UpstreamAccountPool{
		Name: "affinity-pool", Platform: constant.UpstreamPlatformOpenAI,
		CredentialType: constant.UpstreamAccountTypeOAuth, Status: constant.UpstreamStatusActive,
		SchedulerConfig: `{"version":2,"session_affinity_enabled":true,"session_affinity_ttl_seconds":3600,"session_affinity_wait_ms":20,"session_affinity_max_waiters":2}`,
	}
	require.NoError(t, CreateUpstreamAccountPool(&pool))
	createRouterTestAccount(t, "affinity-first", pool.Id, nil)
	createRouterTestAccount(t, "affinity-second", pool.Id, nil)
	router, err := NewUpstreamAccountRouter(NewLocalUpstreamAccountLeaseManager(), time.Minute)
	require.NoError(t, err)

	request := UpstreamAccountSelectionRequest{
		PoolId: pool.Id, ChannelType: constant.ChannelTypeCodex, Model: "gpt-5.6-sol",
		RequestPath: "/v1/responses", AccountAffinitySeed: "stable-session", AccountAffinityKeyFP: "abc12345",
	}
	first, err := router.Select(context.Background(), request)
	require.NoError(t, err)
	require.NotNil(t, first.Affinity)
	require.False(t, first.Affinity.CacheHit)
	require.Equal(t, "bound_new", first.Affinity.Outcome)
	require.NoError(t, first.Lease.Release(context.Background()))

	second, err := router.Select(context.Background(), request)
	require.NoError(t, err)
	require.Equal(t, first.Account.Id, second.Account.Id)
	require.NotNil(t, second.Affinity)
	require.True(t, second.Affinity.CacheHit)
	require.Equal(t, "sticky_hit", second.Affinity.Outcome)
	require.NoError(t, second.Lease.Release(context.Background()))
}

func TestUpstreamAccountRouterSessionAffinityFallsBackWhenStickyAccountBusy(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	pool := model.UpstreamAccountPool{
		Name: "affinity-busy-pool", Platform: constant.UpstreamPlatformOpenAI,
		CredentialType: constant.UpstreamAccountTypeOAuth, Status: constant.UpstreamStatusActive,
		SchedulerConfig: `{"version":2,"session_affinity_enabled":true,"session_affinity_ttl_seconds":3600,"session_affinity_wait_ms":10,"session_affinity_max_waiters":1}`,
	}
	require.NoError(t, CreateUpstreamAccountPool(&pool))
	createRouterTestAccount(t, "affinity-busy-first", pool.Id, nil)
	createRouterTestAccount(t, "affinity-busy-second", pool.Id, nil)
	router, err := NewUpstreamAccountRouter(NewLocalUpstreamAccountLeaseManager(), time.Minute)
	require.NoError(t, err)

	request := UpstreamAccountSelectionRequest{
		PoolId: pool.Id, ChannelType: constant.ChannelTypeCodex, Model: "gpt-5.6-sol",
		RequestPath: "/v1/responses", AccountAffinitySeed: "busy-session", AccountAffinityKeyFP: "def67890",
	}
	first, err := router.Select(context.Background(), request)
	require.NoError(t, err)
	second, err := router.Select(context.Background(), request)
	require.NoError(t, err)
	require.NotEqual(t, first.Account.Id, second.Account.Id)
	require.NotNil(t, second.Affinity)
	require.True(t, second.Affinity.CacheHit)
	require.Equal(t, "sticky_fallback", second.Affinity.Outcome)
	require.NoError(t, first.Lease.Release(context.Background()))
	require.NoError(t, second.Lease.Release(context.Background()))
	third, err := router.Select(context.Background(), request)
	require.NoError(t, err)
	require.Equal(t, first.Account.Id, third.Account.Id, "temporary concurrency fallback must preserve the original session binding")
	require.NoError(t, third.Lease.Release(context.Background()))
}

func TestUpstreamAccountRouterSessionAffinityExplicitZeroDisablesPreferredWait(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	pool := model.UpstreamAccountPool{
		Name: "affinity-zero-wait-pool", Platform: constant.UpstreamPlatformOpenAI,
		CredentialType: constant.UpstreamAccountTypeOAuth, Status: constant.UpstreamStatusActive,
		SchedulerConfig: `{"version":2,"session_affinity_enabled":true,"session_affinity_ttl_seconds":3600,"session_affinity_wait_ms":0,"session_affinity_max_waiters":0}`,
	}
	require.NoError(t, CreateUpstreamAccountPool(&pool))
	createRouterTestAccount(t, "affinity-zero-first", pool.Id, nil)
	createRouterTestAccount(t, "affinity-zero-second", pool.Id, nil)
	router, err := NewUpstreamAccountRouter(NewLocalUpstreamAccountLeaseManager(), time.Minute)
	require.NoError(t, err)

	request := UpstreamAccountSelectionRequest{
		PoolId: pool.Id, ChannelType: constant.ChannelTypeCodex, Model: "gpt-5.6-sol",
		RequestPath: "/v1/responses", AccountAffinitySeed: "zero-wait-session",
	}
	first, err := router.Select(context.Background(), request)
	require.NoError(t, err)
	startedAt := time.Now()
	second, err := router.Select(context.Background(), request)
	require.NoError(t, err)
	require.Less(t, time.Since(startedAt), 100*time.Millisecond)
	require.NotEqual(t, first.Account.Id, second.Account.Id)
	require.Equal(t, "sticky_fallback", second.Affinity.Outcome)
	require.NoError(t, first.Lease.Release(context.Background()))
	require.NoError(t, second.Lease.Release(context.Background()))
}

func TestUpstreamAccountRouterResponseAffinityWaitsThenFallsBack(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	pool := model.UpstreamAccountPool{
		Name: "response-affinity-pool", Platform: constant.UpstreamPlatformOpenAI,
		CredentialType: constant.UpstreamAccountTypeOAuth, Status: constant.UpstreamStatusActive,
		SchedulerConfig: `{"version":2,"session_affinity_enabled":true,"session_affinity_ttl_seconds":3600,"session_affinity_wait_ms":10,"session_affinity_max_waiters":1}`,
	}
	require.NoError(t, CreateUpstreamAccountPool(&pool))
	preferred := createRouterTestAccount(t, "response-preferred", pool.Id, nil)
	fallback := createRouterTestAccount(t, "response-fallback", pool.Id, nil)
	manager := NewLocalUpstreamAccountLeaseManager()
	busyLease, err := manager.Acquire(context.Background(), preferred.Id, preferred.Concurrency, time.Minute)
	require.NoError(t, err)
	router, err := NewUpstreamAccountRouter(manager, time.Minute)
	require.NoError(t, err)

	selection, err := router.Select(context.Background(), UpstreamAccountSelectionRequest{
		PoolId: pool.Id, ChannelType: constant.ChannelTypeCodex, Model: "gpt-5.6-sol",
		RequestPath: "/v1/responses", PreferredAccountId: preferred.Id,
	})
	require.NoError(t, err)
	require.Equal(t, fallback.Id, selection.Account.Id)
	require.NotNil(t, selection.Affinity)
	require.Equal(t, "response", selection.Affinity.Source)
	require.Equal(t, "response_fallback", selection.Affinity.Outcome)
	require.Equal(t, preferred.Id, selection.Affinity.PreferredAccountID)
	require.NoError(t, busyLease.Release(context.Background()))
	require.NoError(t, selection.Lease.Release(context.Background()))
}

func TestUpstreamAccountRouterResponseAffinityUsesPreferredAccount(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	pool := model.UpstreamAccountPool{
		Name: "response-affinity-hit-pool", Platform: constant.UpstreamPlatformOpenAI,
		CredentialType: constant.UpstreamAccountTypeOAuth, Status: constant.UpstreamStatusActive,
		SchedulerConfig: `{"version":2,"session_affinity_enabled":true,"session_affinity_ttl_seconds":3600,"session_affinity_wait_ms":10,"session_affinity_max_waiters":1}`,
	}
	require.NoError(t, CreateUpstreamAccountPool(&pool))
	preferred := createRouterTestAccount(t, "response-hit-preferred", pool.Id, nil)
	createRouterTestAccount(t, "response-hit-other", pool.Id, nil)
	router, err := NewUpstreamAccountRouter(NewLocalUpstreamAccountLeaseManager(), time.Minute)
	require.NoError(t, err)

	selection, err := router.Select(context.Background(), UpstreamAccountSelectionRequest{
		PoolId: pool.Id, ChannelType: constant.ChannelTypeCodex, Model: "gpt-5.6-sol",
		RequestPath: "/v1/responses", PreferredAccountId: preferred.Id,
	})
	require.NoError(t, err)
	require.Equal(t, preferred.Id, selection.Account.Id)
	require.NotNil(t, selection.Affinity)
	require.Equal(t, "response", selection.Affinity.Source)
	require.Equal(t, "response_hit", selection.Affinity.Outcome)
	require.NoError(t, selection.Lease.Release(context.Background()))
}

func TestUpstreamAccountRouterResponseAffinityFallsBackToSessionBindingFirst(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	pool := model.UpstreamAccountPool{
		Name: "response-session-affinity-pool", Platform: constant.UpstreamPlatformOpenAI,
		CredentialType: constant.UpstreamAccountTypeOAuth, Status: constant.UpstreamStatusActive,
		SchedulerConfig: `{"version":2,"session_affinity_enabled":true,"session_affinity_ttl_seconds":3600,"session_affinity_wait_ms":10,"session_affinity_max_waiters":1}`,
	}
	require.NoError(t, CreateUpstreamAccountPool(&pool))
	responseAccount := createRouterTestAccount(t, "response-chain-account", pool.Id, nil)
	sessionAccount := createRouterTestAccount(t, "session-binding-account", pool.Id, nil)
	manager := NewLocalUpstreamAccountLeaseManager()
	responseBusy, err := manager.Acquire(context.Background(), responseAccount.Id, responseAccount.Concurrency, time.Minute)
	require.NoError(t, err)
	router, err := NewUpstreamAccountRouter(manager, time.Minute)
	require.NoError(t, err)
	request := UpstreamAccountSelectionRequest{
		PoolId: pool.Id, ChannelType: constant.ChannelTypeCodex, Model: "gpt-5.6-sol",
		RequestPath: "/v1/responses", AccountAffinitySeed: "combined-session", AccountAffinityKeyFP: "combined1",
	}
	bound, err := router.Select(context.Background(), request)
	require.NoError(t, err)
	require.Equal(t, sessionAccount.Id, bound.Account.Id)
	require.NoError(t, bound.Lease.Release(context.Background()))
	require.NoError(t, responseBusy.Release(context.Background()))

	responseBusy, err = manager.Acquire(context.Background(), responseAccount.Id, responseAccount.Concurrency, time.Minute)
	require.NoError(t, err)
	request.PreferredAccountId = responseAccount.Id
	selection, err := router.Select(context.Background(), request)
	require.NoError(t, err)
	require.Equal(t, sessionAccount.Id, selection.Account.Id)
	require.Equal(t, "response", selection.Affinity.Source)
	require.Equal(t, "response_fallback_session_hit", selection.Affinity.Outcome)
	require.NoError(t, responseBusy.Release(context.Background()))
	require.NoError(t, selection.Lease.Release(context.Background()))
}

func TestUpstreamAccountRouterSessionAffinityDisabledIsNoop(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	pool := model.UpstreamAccountPool{
		Name: "affinity-disabled-pool", Platform: constant.UpstreamPlatformOpenAI,
		CredentialType: constant.UpstreamAccountTypeOAuth, Status: constant.UpstreamStatusActive,
		SchedulerConfig: `{"version":2}`,
	}
	require.NoError(t, CreateUpstreamAccountPool(&pool))
	createRouterTestAccount(t, "affinity-disabled", pool.Id, nil)
	router, err := NewUpstreamAccountRouter(NewLocalUpstreamAccountLeaseManager(), time.Minute)
	require.NoError(t, err)
	selection, err := router.Select(context.Background(), UpstreamAccountSelectionRequest{
		PoolId: pool.Id, ChannelType: constant.ChannelTypeCodex, Model: "gpt-5.6-sol",
		RequestPath: "/v1/responses", AccountAffinitySeed: "ignored-session",
	})
	require.NoError(t, err)
	require.Nil(t, selection.Affinity)
	require.NoError(t, selection.Lease.Release(context.Background()))
}

func TestUpstreamAccountRouterSessionAffinityDoesNotAffectNativeWebSocket(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	pool := model.UpstreamAccountPool{
		Name: "affinity-websocket-pool", Platform: constant.UpstreamPlatformOpenAI,
		CredentialType: constant.UpstreamAccountTypeOAuth, Status: constant.UpstreamStatusActive,
		SchedulerConfig: `{"version":2,"session_affinity_enabled":true}`,
	}
	require.NoError(t, CreateUpstreamAccountPool(&pool))
	createRouterTestAccountWithExtra(t, "affinity-websocket", pool.Id, `{"openai_oauth_responses_websockets_v2_mode":"ctx_pool"}`)
	router, err := NewUpstreamAccountRouter(NewLocalUpstreamAccountLeaseManager(), time.Minute)
	require.NoError(t, err)
	selection, err := router.Select(context.Background(), UpstreamAccountSelectionRequest{
		PoolId: pool.Id, ChannelType: constant.ChannelTypeCodex, Model: "gpt-5.6-sol",
		RequestPath: "/v1/responses", ResponsesWebSocket: true,
		RequiredWebSocketMode: model.UpstreamOpenAIWSModeContextPool,
		AccountAffinitySeed:   "ignored-websocket-session",
	})
	require.NoError(t, err)
	require.Nil(t, selection.Affinity)
	require.NoError(t, selection.Lease.Release(context.Background()))
}

func TestResolveUpstreamAccountModelUsesClaudeGroupDefaultOnlyForClaudeFamilies(t *testing.T) {
	mapped, matched, supported := resolveUpstreamAccountModel(nil, nil, model.UpstreamAccountOptions{}, "claude-opus-4-6", false, "gpt-5.4")
	require.True(t, matched)
	require.True(t, supported)
	require.Equal(t, "gpt-5.4", mapped)

	mapped, matched, supported = resolveUpstreamAccountModel(nil, nil, model.UpstreamAccountOptions{}, "gpt6", false, "gpt-5.4")
	require.False(t, matched)
	require.True(t, supported)
	require.Equal(t, "gpt6", mapped)
}

func TestResolveUpstreamAccountModelUsesCompactMappingFirst(t *testing.T) {
	options := model.UpstreamAccountOptions{CompactModelMapping: map[string]string{"gpt-5.*": "gpt-5.4-compact"}}
	mapped, matched, supported := resolveUpstreamAccountModel(
		nil,
		map[string]any{"model_mapping": map[string]any{"gpt-5.*": "gpt-5.4"}},
		options,
		"gpt-5.6-sol",
		true,
		"",
	)
	require.True(t, matched)
	require.True(t, supported)
	require.Equal(t, "gpt-5.4-compact", mapped)
}

func TestResolveUpstreamAccountModelTreatsIdentityMappingsAsWhitelistEntries(t *testing.T) {
	credentials := map[string]any{
		"model_mapping": map[string]any{"gpt-5.6-luna": "gpt-5.6-luna"},
	}
	mapped, matched, supported := resolveUpstreamAccountModel(
		nil,
		credentials,
		model.UpstreamAccountOptions{},
		"gpt-5.6-luna",
		false,
		"",
	)
	require.False(t, matched)
	require.True(t, supported)
	require.Equal(t, "gpt-5.6-luna", mapped)

	options := model.UpstreamAccountOptions{
		CompactModelMapping: map[string]string{"gpt-5.6-luna": "gpt-5.6-luna"},
	}
	mapped, matched, supported = resolveUpstreamAccountModel(
		nil,
		nil,
		options,
		"gpt-5.6-luna",
		true,
		"",
	)
	require.False(t, matched)
	require.True(t, supported)
	require.Equal(t, "gpt-5.6-luna", mapped)
}

func TestPreferUpstreamAccountCandidateIsSoftAndStable(t *testing.T) {
	candidates := []upstreamAccountCandidate{
		{account: model.UpstreamAccount{Id: 1}},
		{account: model.UpstreamAccount{Id: 2}},
		{account: model.UpstreamAccount{Id: 3}},
	}
	ordered := preferUpstreamAccountCandidate(candidates, 2)
	require.Equal(t, []int{2, 1, 3}, []int{ordered[0].account.Id, ordered[1].account.Id, ordered[2].account.Id})

	missing := preferUpstreamAccountCandidate(ordered, 99)
	require.Equal(t, []int{2, 1, 3}, []int{missing[0].account.Id, missing[1].account.Id, missing[2].account.Id})
}

func TestResolveUpstreamAccountModelRejectsForeignModelsForOpenAIOAuthWithoutMapping(t *testing.T) {
	account := &model.UpstreamAccount{Platform: constant.UpstreamPlatformOpenAI, Type: constant.UpstreamAccountTypeOAuth}

	mapped, matched, supported := resolveUpstreamAccountModel(account, nil, model.UpstreamAccountOptions{}, "deepseek-v3", false, "")
	require.False(t, matched)
	require.False(t, supported)
	require.Equal(t, "deepseek-v3", mapped)

	_, _, supported = resolveUpstreamAccountModel(account, nil, model.UpstreamAccountOptions{}, "openai/gpt-5.6-sol", false, "")
	require.True(t, supported)

	_, _, supported = resolveUpstreamAccountModel(account, nil, model.UpstreamAccountOptions{OpenAIPassthrough: true}, "gemini-3-pro", false, "")
	require.True(t, supported)
}

func TestResolveUpstreamAccountModelPassthroughIgnoresStoredModelMapping(t *testing.T) {
	account := &model.UpstreamAccount{Platform: constant.UpstreamPlatformOpenAI, Type: constant.UpstreamAccountTypeOAuth}
	credentials := map[string]any{
		"model_mapping": map[string]any{"gpt-5.*": "gpt-5.4"},
	}
	options := model.UpstreamAccountOptions{OpenAIPassthrough: true}

	mapped, matched, supported := resolveUpstreamAccountModel(account, credentials, options, "gemini-3-pro", false, "")
	require.True(t, supported)
	require.False(t, matched)
	require.Equal(t, "gemini-3-pro", mapped)

	mapped, matched, supported = resolveUpstreamAccountModel(account, credentials, options, "gpt-5.6-sol", false, "")
	require.True(t, supported)
	require.False(t, matched)
	require.Equal(t, "gpt-5.6-sol", mapped)
}

func TestResolveUpstreamAccountModelPassthroughKeepsCompactMapping(t *testing.T) {
	account := &model.UpstreamAccount{Platform: constant.UpstreamPlatformOpenAI, Type: constant.UpstreamAccountTypeAPIKey}
	options := model.UpstreamAccountOptions{
		OpenAIPassthrough:   true,
		CompactModelMapping: map[string]string{"gpt-5.*": "gpt-5.4-compact"},
	}

	mapped, matched, supported := resolveUpstreamAccountModel(account, nil, options, "gpt-5.6-sol", true, "")
	require.True(t, supported)
	require.True(t, matched)
	require.Equal(t, "gpt-5.4-compact", mapped)
}

func TestUpstreamAccountRouterHonorsCompactAndCodexRestrictions(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	pool := model.UpstreamAccountPool{
		Name: "account-option-pool", Platform: constant.UpstreamPlatformOpenAI,
		CredentialType: constant.UpstreamAccountTypeOAuth, Status: constant.UpstreamStatusActive,
		SchedulerConfig: "{}",
	}
	require.NoError(t, CreateUpstreamAccountPool(&pool))
	createRouterTestAccountWithExtra(t, "compact-off", pool.Id, `{"openai_compact_mode":"force_off"}`)
	allowed := createRouterTestAccountWithExtra(t, "compact-on", pool.Id, `{"openai_compact_mode":"force_on","codex_cli_only":true}`)
	router, err := NewUpstreamAccountRouter(NewLocalUpstreamAccountLeaseManager(), time.Minute)
	require.NoError(t, err)

	_, err = router.Select(context.Background(), UpstreamAccountSelectionRequest{
		PoolId: pool.Id, ChannelType: constant.ChannelTypeCodex, Model: "gpt-5.6-sol",
		CompactRequest: true,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "codex_client_restricted")

	selection, err := router.Select(context.Background(), UpstreamAccountSelectionRequest{
		PoolId: pool.Id, ChannelType: constant.ChannelTypeCodex, Model: "gpt-5.6-sol",
		CompactRequest: true, CodexClient: true,
	})
	require.NoError(t, err)
	require.Equal(t, allowed.Id, selection.Account.Id)
	require.NoError(t, selection.Release(context.Background()))
}

func TestUpstreamAccountRouterFiltersMixedPoolByAllowedAccountTypes(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	pool := model.UpstreamAccountPool{
		Name: "mixed-pool", Platform: constant.UpstreamPlatformOpenAI,
		CredentialType: "mixed", Status: constant.UpstreamStatusActive,
		SchedulerConfig: "{}",
	}
	require.NoError(t, CreateUpstreamAccountPool(&pool))
	oauth := createRouterTestAccount(t, "oauth", pool.Id, nil)
	apiKey := createRouterTestAPIKeyAccount(t, "apikey", pool.Id)
	router, err := NewUpstreamAccountRouter(NewLocalUpstreamAccountLeaseManager(), time.Minute)
	require.NoError(t, err)

	oauthSelection, err := router.Select(context.Background(), UpstreamAccountSelectionRequest{
		PoolId: pool.Id, AllowedAccountTypes: []string{constant.UpstreamAccountTypeOAuth},
	})
	require.NoError(t, err)
	require.Equal(t, oauth.Id, oauthSelection.Account.Id)
	require.NoError(t, oauthSelection.Release(context.Background()))

	apiKeySelection, err := router.Select(context.Background(), UpstreamAccountSelectionRequest{
		PoolId: pool.Id, AllowedAccountTypes: []string{constant.UpstreamAccountTypeAPIKey},
	})
	require.NoError(t, err)
	require.Equal(t, apiKey.Id, apiKeySelection.Account.Id)
	require.Equal(t, "sk-apikey", apiKeySelection.Credentials["api_key"])
	require.NoError(t, apiKeySelection.Release(context.Background()))
}

func TestUpstreamAccountRouterFiltersOffModeAndPinsPreferredWSAccount(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	pool := model.UpstreamAccountPool{
		Name: "ws-mode-pool", Platform: constant.UpstreamPlatformOpenAI,
		CredentialType: constant.UpstreamAccountTypeOAuth, Status: constant.UpstreamStatusActive,
		SchedulerConfig: "{}",
	}
	require.NoError(t, CreateUpstreamAccountPool(&pool))
	createRouterTestAccountWithExtra(t, "off", pool.Id, `{"openai_oauth_responses_websockets_v2_mode":"off"}`)
	first := createRouterTestAccountWithExtra(t, "ctx-one", pool.Id, `{"openai_oauth_responses_websockets_v2_mode":"ctx_pool"}`)
	second := createRouterTestAccountWithExtra(t, "ctx-two", pool.Id, `{"openai_oauth_responses_websockets_v2_mode":"ctx_pool"}`)
	bridge := createRouterTestAccountWithExtra(t, "bridge", pool.Id, `{"openai_oauth_responses_websockets_v2_mode":"http_bridge"}`)
	router, err := NewUpstreamAccountRouter(NewLocalUpstreamAccountLeaseManager(), time.Minute)
	require.NoError(t, err)

	selection, err := router.Select(context.Background(), UpstreamAccountSelectionRequest{
		PoolId: pool.Id, ChannelType: constant.ChannelTypeCodex, ResponsesWebSocket: true,
		PreferredAccountId: second.Id, RequirePreferred: true,
	})
	require.NoError(t, err)
	require.Equal(t, second.Id, selection.Account.Id)
	require.NotEqual(t, first.Id, selection.Account.Id)
	require.NoError(t, selection.Release(context.Background()))

	bridgeSelection, err := router.Select(context.Background(), UpstreamAccountSelectionRequest{
		PoolId: pool.Id, ChannelType: constant.ChannelTypeCodex, ResponsesWebSocket: true,
		RequiredWebSocketMode: model.UpstreamOpenAIWSModeHTTPBridge,
	})
	require.NoError(t, err)
	require.Equal(t, bridge.Id, bridgeSelection.Account.Id)
	require.NoError(t, bridgeSelection.Release(context.Background()))
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

func TestUpstreamAccountRouterFallsBackWhenHighestPriorityIsFull(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	pool := model.UpstreamAccountPool{
		Name: "priority-fallback-pool", Platform: constant.UpstreamPlatformOpenAI,
		CredentialType: constant.UpstreamAccountTypeOAuth, Status: constant.UpstreamStatusActive,
		SchedulerConfig: "{}",
	}
	require.NoError(t, CreateUpstreamAccountPool(&pool))
	primary := createRouterTestAccount(t, "primary", pool.Id, nil)
	fallback := createRouterTestAccount(t, "fallback", pool.Id, nil)
	require.NoError(t, model.DB.Model(&model.UpstreamAccountPoolMember{}).
		Where("pool_id = ? AND account_id = ?", pool.Id, primary.Id).Update("priority", 1).Error)
	require.NoError(t, model.DB.Model(&model.UpstreamAccountPoolMember{}).
		Where("pool_id = ? AND account_id = ?", pool.Id, fallback.Id).Update("priority", 2).Error)

	manager := NewLocalUpstreamAccountLeaseManager()
	busyLease, err := manager.Acquire(context.Background(), primary.Id, primary.Concurrency, time.Minute)
	require.NoError(t, err)
	router, err := NewUpstreamAccountRouter(manager, time.Minute)
	require.NoError(t, err)
	selection, err := router.Select(context.Background(), UpstreamAccountSelectionRequest{
		PoolId: pool.Id, ChannelType: constant.ChannelTypeCodex,
	})
	require.NoError(t, err)
	require.Equal(t, fallback.Id, selection.Account.Id)
	require.NoError(t, selection.Release(context.Background()))
	require.NoError(t, busyLease.Release(context.Background()))
}

func TestUpstreamAccountRouterLoadScorePrefersLowerLoadBeforeCapacityIsFull(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	pool := model.UpstreamAccountPool{
		Name: "load-score-pool", Platform: constant.UpstreamPlatformOpenAI,
		CredentialType: constant.UpstreamAccountTypeOAuth, Status: constant.UpstreamStatusActive,
		SchedulerConfig: `{"version":1,"strategy":"load_score","top_k":1,"priority_source":"account"}`,
	}
	require.NoError(t, CreateUpstreamAccountPool(&pool))
	busy := createRouterTestAccount(t, "busy-not-full", pool.Id, nil)
	idle := createRouterTestAccount(t, "idle", pool.Id, nil)
	require.NoError(t, model.DB.Model(&model.UpstreamAccount{}).
		Where("id IN ?", []int{busy.Id, idle.Id}).Updates(map[string]any{"priority": 2, "concurrency": 2}).Error)
	require.NoError(t, model.DB.Model(&model.UpstreamAccountPoolMember{}).
		Where("pool_id = ? AND account_id = ?", pool.Id, busy.Id).Update("priority", 1).Error)
	require.NoError(t, model.DB.Model(&model.UpstreamAccountPoolMember{}).
		Where("pool_id = ? AND account_id = ?", pool.Id, idle.Id).Update("priority", 2).Error)

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

func TestUpstreamAccountRouterAppliesCredentialModelMapping(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	pool := model.UpstreamAccountPool{
		Name: "model-mapping-pool", Platform: constant.UpstreamPlatformOpenAI,
		CredentialType: constant.UpstreamAccountTypeOAuth, Status: constant.UpstreamStatusActive,
		SchedulerConfig: "{}",
	}
	require.NoError(t, CreateUpstreamAccountPool(&pool))
	createRouterTestMappedAccount(t, "unsupported", pool.Id, map[string]any{"gpt-4.*": "gpt-4.1"})
	supported := createRouterTestMappedAccount(t, "supported", pool.Id, map[string]any{"gpt-5.*": "gpt-5.4"})
	router, err := NewUpstreamAccountRouter(NewLocalUpstreamAccountLeaseManager(), time.Minute)
	require.NoError(t, err)

	selection, err := router.Select(context.Background(), UpstreamAccountSelectionRequest{
		PoolId: pool.Id, ChannelType: constant.ChannelTypeCodex, Model: "gpt-5.6-sol",
	})
	require.NoError(t, err)
	require.Equal(t, supported.Id, selection.Account.Id)
	require.True(t, selection.ModelMapped)
	require.Equal(t, "gpt-5.4", selection.MappedModel)
	require.NoError(t, selection.Release(context.Background()))
}

func TestUpstreamAccountRouterUsesImportedSourceModelRouting(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	sourceSystem := "sub2api"
	pool := model.UpstreamAccountPool{
		Name: "source-routing-pool", Platform: constant.UpstreamPlatformOpenAI,
		CredentialType: constant.UpstreamAccountTypeOAuth, Status: constant.UpstreamStatusActive,
		SourceSystem:    &sourceSystem,
		SchedulerConfig: `{"model_routing_enabled":true,"model_routing":{"gpt-5.*":[902]},"model_routing_id_space":"source"}`,
	}
	require.NoError(t, CreateUpstreamAccountPool(&pool))
	first := createRouterTestAccount(t, "source-first", pool.Id, nil)
	second := createRouterTestAccount(t, "source-second", pool.Id, nil)
	require.NoError(t, model.DB.Model(&model.UpstreamAccount{}).Where("id = ?", first.Id).Update("source_id", 901).Error)
	require.NoError(t, model.DB.Model(&model.UpstreamAccount{}).Where("id = ?", second.Id).Update("source_id", 902).Error)
	router, err := NewUpstreamAccountRouter(NewLocalUpstreamAccountLeaseManager(), time.Minute)
	require.NoError(t, err)

	selection, err := router.Select(context.Background(), UpstreamAccountSelectionRequest{
		PoolId: pool.Id, ChannelType: constant.ChannelTypeCodex, Model: "gpt-5.6-sol",
	})
	require.NoError(t, err)
	require.Equal(t, second.Id, selection.Account.Id)
	require.NoError(t, selection.Release(context.Background()))
}

func TestUpstreamAccountRouterWaitsForConcurrencySlot(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	pool := model.UpstreamAccountPool{
		Name: "wait-pool", Platform: constant.UpstreamPlatformOpenAI,
		CredentialType: constant.UpstreamAccountTypeOAuth, Status: constant.UpstreamStatusActive,
		SchedulerConfig: "{}",
	}
	require.NoError(t, CreateUpstreamAccountPool(&pool))
	account := createRouterTestAccount(t, "wait-account", pool.Id, nil)
	manager := NewLocalUpstreamAccountLeaseManager()
	busyLease, err := manager.Acquire(context.Background(), account.Id, 1, time.Minute)
	require.NoError(t, err)
	router, err := NewUpstreamAccountRouter(manager, time.Minute)
	require.NoError(t, err)
	router.concurrencyWaitTimeout = 300 * time.Millisecond
	router.concurrencyWaitPoll = 10 * time.Millisecond
	router.maxWaiters = 1
	releaseDone := make(chan error, 1)
	go func() {
		time.Sleep(60 * time.Millisecond)
		releaseDone <- busyLease.Release(context.Background())
	}()

	selection, err := router.Select(context.Background(), UpstreamAccountSelectionRequest{
		PoolId: pool.Id, ChannelType: constant.ChannelTypeCodex,
	})
	require.NoError(t, err)
	require.Equal(t, account.Id, selection.Account.Id)
	require.NoError(t, <-releaseDone)
	require.NoError(t, selection.Release(context.Background()))
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

func createRouterTestAccountWithExtra(t *testing.T, name string, poolId int, extra string) model.UpstreamAccount {
	t.Helper()
	input := UpstreamAccountCreateInput{
		Account: model.UpstreamAccount{
			Name: name, Platform: constant.UpstreamPlatformOpenAI, Type: constant.UpstreamAccountTypeOAuth,
			Extra: extra, Concurrency: 1, Priority: 1, Weight: 1,
			Status: constant.UpstreamStatusActive, Schedulable: true, AutoPauseOnExpired: true,
		},
		Credentials: map[string]any{"access_token": name + "-secret", "account_id": "acct-" + name},
		PoolIds:     []int{poolId},
	}
	require.NoError(t, CreateUpstreamAccount(&input))
	return input.Account
}

func createRouterTestAPIKeyAccount(t *testing.T, name string, poolId int) model.UpstreamAccount {
	t.Helper()
	input := UpstreamAccountCreateInput{
		Account: model.UpstreamAccount{
			Name: name, Platform: constant.UpstreamPlatformOpenAI, Type: constant.UpstreamAccountTypeAPIKey,
			Extra: "{}", Concurrency: 1, Priority: 1, Weight: 1,
			Status: constant.UpstreamStatusActive, Schedulable: true, AutoPauseOnExpired: true,
		},
		Credentials: map[string]any{"api_key": "sk-" + name},
		PoolIds:     []int{poolId},
	}
	require.NoError(t, CreateUpstreamAccount(&input))
	return input.Account
}

func createRouterTestMappedAccount(t *testing.T, name string, poolId int, modelMapping map[string]any) model.UpstreamAccount {
	t.Helper()
	input := UpstreamAccountCreateInput{
		Account: model.UpstreamAccount{
			Name: name, Platform: constant.UpstreamPlatformOpenAI, Type: constant.UpstreamAccountTypeOAuth,
			Extra: "{}", Concurrency: 1, Priority: 1, Weight: 1,
			Status: constant.UpstreamStatusActive, Schedulable: true, AutoPauseOnExpired: true,
		},
		Credentials: map[string]any{
			"access_token": name + "-secret", "account_id": "acct-" + name, "model_mapping": modelMapping,
		},
		PoolIds: []int{poolId},
	}
	require.NoError(t, CreateUpstreamAccount(&input))
	return input.Account
}
