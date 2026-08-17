package service

import (
	"encoding/base64"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupUpstreamAdminTestDB(t *testing.T) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.Channel{},
		&model.Ability{},
		&model.UpstreamProxy{},
		&model.UpstreamAccountPool{},
		&model.UpstreamAccount{},
		&model.UpstreamAccountPoolMember{},
		&model.UpstreamAccountEvent{},
		&model.UpstreamOAuthSession{},
		&model.UpstreamAccountScheduledTestPlan{},
		&model.UpstreamAccountScheduledTestResult{},
	))
	originalDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = originalDB })

	key := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	rawKeyring, err := common.Marshal(map[string]string{"1": key})
	require.NoError(t, err)
	t.Setenv(upstreamCredentialKeysEnv, string(rawKeyring))
	t.Setenv(upstreamCredentialActiveVersionEnv, "1")
}

func TestCreateUpstreamProxyWithoutCredentialKeyring(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	t.Setenv(upstreamCredentialKeysEnv, "")
	t.Setenv(upstreamCredentialActiveVersionEnv, "")

	input := UpstreamProxyCreateInput{
		Proxy: model.UpstreamProxy{
			Name: "plain-proxy", Protocol: constant.UpstreamProxyProtocolSOCKS5,
			Host: "127.0.0.1", Port: 1080, Status: constant.UpstreamStatusActive,
			FallbackMode: constant.UpstreamProxyFallbackNone,
		},
		Auth: UpstreamProxyAuthInput{Username: "proxy-user", Password: "proxy-password"},
	}
	require.NoError(t, CreateUpstreamProxy(&input))

	var stored model.UpstreamProxy
	require.NoError(t, model.DB.First(&stored, input.Proxy.Id).Error)
	require.Equal(t, "proxy-user", stored.Username)
	require.Equal(t, "proxy-password", stored.Password)
	require.Empty(t, stored.AuthCiphertext)
	require.Empty(t, stored.AuthNonce)
	require.Zero(t, stored.AuthKeyVersion)

	auth, err := DecryptUpstreamProxyAuth(&stored)
	require.NoError(t, err)
	require.Equal(t, input.Auth, *auth)

	view, err := GetUpstreamProxy(input.Proxy.Id)
	require.NoError(t, err)
	require.True(t, view.AuthConfigured)
	require.Equal(t, "proxy-user", view.AuthUsername)
	require.Equal(t, "proxy-password", view.AuthPassword)
	encoded, err := common.Marshal(view)
	require.NoError(t, err)
	require.Contains(t, string(encoded), "proxy-user")
	require.Contains(t, string(encoded), "proxy-password")
}

func TestCreateUpstreamAccountWithoutCredentialKeyring(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	t.Setenv(upstreamCredentialKeysEnv, "")
	t.Setenv(upstreamCredentialActiveVersionEnv, "")

	input := UpstreamAccountCreateInput{
		Account: model.UpstreamAccount{
			Name: "plain-account", Platform: constant.UpstreamPlatformOpenAI,
			Type: constant.UpstreamAccountTypeAPIKey, Extra: "{}", Concurrency: 1,
			Priority: 50, Weight: 1, Status: constant.UpstreamStatusActive,
			Schedulable: true, AutoPauseOnExpired: true,
		},
		Credentials: map[string]any{"api_key": "plain-secret"},
	}
	require.NoError(t, CreateUpstreamAccount(&input))

	var stored model.UpstreamAccount
	require.NoError(t, model.DB.First(&stored, input.Account.Id).Error)
	require.Zero(t, stored.CredentialKeyVersion)
	require.Empty(t, stored.CredentialNonce)
	require.Contains(t, stored.CredentialCiphertext, "plain-secret")

	credentials, err := DecryptUpstreamAccountCredentials(&stored)
	require.NoError(t, err)
	require.Equal(t, "plain-secret", credentials["api_key"])

	view, err := GetUpstreamAccount(input.Account.Id)
	require.NoError(t, err)
	require.True(t, view.CredentialConfigured)
	views, total, err := ListUpstreamAccounts(UpstreamAccountListFilter{Page: 1, PageSize: 10})
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, views, 1)
	require.NotNil(t, views[0].PoolIds)
	require.Empty(t, views[0].PoolIds)
}

func TestUpstreamAccountAdminCredentialLifecycleAndSafeDelete(t *testing.T) {
	setupUpstreamAdminTestDB(t)

	proxyInput := UpstreamProxyCreateInput{
		Proxy: model.UpstreamProxy{
			Name: "proxy-one", Protocol: constant.UpstreamProxyProtocolSOCKS5H,
			Host: "127.0.0.1", Port: 1080, Status: constant.UpstreamStatusActive,
			FallbackMode: constant.UpstreamProxyFallbackNone,
		},
		Auth: UpstreamProxyAuthInput{Username: "proxy-user", Password: "proxy-secret"},
	}
	require.NoError(t, CreateUpstreamProxy(&proxyInput))
	require.Positive(t, proxyInput.Proxy.Id)

	pool := model.UpstreamAccountPool{
		Name: "codex", Platform: constant.UpstreamPlatformOpenAI, CredentialType: constant.UpstreamAccountTypeOAuth,
		Status: constant.UpstreamStatusActive, DefaultProxyId: &proxyInput.Proxy.Id, SchedulerConfig: "{}",
	}
	require.NoError(t, CreateUpstreamAccountPool(&pool))

	accountInput := UpstreamAccountCreateInput{
		Account: model.UpstreamAccount{
			Name: "oauth-account", Platform: constant.UpstreamPlatformOpenAI, Type: constant.UpstreamAccountTypeOAuth,
			Extra: "{}", ProxyId: &proxyInput.Proxy.Id, Concurrency: 2, Priority: 50, Weight: 1,
			Status: constant.UpstreamStatusActive, Schedulable: true, AutoPauseOnExpired: true,
		},
		Credentials: map[string]any{"access_token": "account-secret", "refresh_token": "refresh-secret", "account_id": "acct-1"},
		PoolIds:     []int{pool.Id},
	}
	require.NoError(t, CreateUpstreamAccount(&accountInput))

	stored, err := GetUpstreamAccount(accountInput.Account.Id)
	require.NoError(t, err)
	require.True(t, stored.CredentialConfigured)
	require.Equal(t, []int{pool.Id}, stored.PoolIds)
	encoded, err := common.Marshal(stored)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "account-secret")
	require.NotContains(t, string(encoded), "refresh-secret")
	require.NotContains(t, string(encoded), "credential_ciphertext")
	require.NotContains(t, string(encoded), "credential_nonce")

	var encrypted model.UpstreamAccount
	require.NoError(t, model.DB.First(&encrypted, accountInput.Account.Id).Error)
	credentials, err := DecryptUpstreamAccountCredentials(&encrypted)
	require.NoError(t, err)
	require.Equal(t, "account-secret", credentials["access_token"])
	oldVersion := encrypted.CredentialVersion

	updatedCredentials := map[string]any{"access_token": "account-secret-2", "refresh_token": "refresh-secret-2", "account_id": "acct-1"}
	update := UpstreamAccountUpdateInput{Account: encrypted, Credentials: &updatedCredentials}
	require.NoError(t, UpdateUpstreamAccount(&update))
	require.NoError(t, model.DB.First(&encrypted, accountInput.Account.Id).Error)
	require.Equal(t, oldVersion+1, encrypted.CredentialVersion)
	credentials, err = DecryptUpstreamAccountCredentials(&encrypted)
	require.NoError(t, err)
	require.Equal(t, "account-secret-2", credentials["access_token"])

	require.Error(t, DeleteUpstreamProxy(proxyInput.Proxy.Id))
	require.NoError(t, DeleteUpstreamAccount(accountInput.Account.Id))
	pool.DefaultProxyId = nil
	require.NoError(t, UpdateUpstreamAccountPool(&pool))
	require.NoError(t, DeleteUpstreamAccountPool(pool.Id))
	require.NoError(t, DeleteUpstreamProxy(proxyInput.Proxy.Id))
}

func TestCreateUpstreamAccountPreservesExplicitZeroValues(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	input := UpstreamAccountCreateInput{
		Account: model.UpstreamAccount{
			Name: "paused-account", Platform: constant.UpstreamPlatformOpenAI, Type: constant.UpstreamAccountTypeAPIKey,
			Extra: "{}", Concurrency: 1, Priority: 0, Weight: 1,
			Status: constant.UpstreamStatusError, Schedulable: false, AutoPauseOnExpired: false,
		},
		Credentials: map[string]any{"api_key": "secret"},
	}
	require.NoError(t, CreateUpstreamAccount(&input))

	var stored model.UpstreamAccount
	require.NoError(t, model.DB.First(&stored, input.Account.Id).Error)
	require.Zero(t, stored.Priority)
	require.False(t, stored.Schedulable)
	require.False(t, stored.AutoPauseOnExpired)
	require.Equal(t, constant.UpstreamOAuthRefreshOwnerExternal, stored.OAuthRefreshOwner)
}

func TestApplyUpstreamAccountPoolCapacitiesAggregatesSharedAccounts(t *testing.T) {
	first := &UpstreamAccountPoolView{}
	second := &UpstreamAccountPoolView{}
	views := map[int]*UpstreamAccountPoolView{1: first, 2: second}
	rows := []upstreamAccountPoolCapacityRow{
		{PoolId: 1, AccountId: 10, Concurrency: 10},
		{PoolId: 1, AccountId: 11, Concurrency: 4},
		{PoolId: 2, AccountId: 10, Concurrency: 10},
	}

	applyUpstreamAccountPoolCapacities(views, rows, map[int]int{10: 2, 11: 1})

	require.Equal(t, 14, first.Concurrency)
	require.Equal(t, 3, first.CurrentConcurrency)
	require.Equal(t, 10, second.Concurrency)
	require.Equal(t, 2, second.CurrentConcurrency)
}

func TestListUpstreamAccountPoolsCapacityIncludesOnlyReadyMembers(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	pool := model.UpstreamAccountPool{
		Name: "capacity", Platform: constant.UpstreamPlatformOpenAI,
		CredentialType: constant.UpstreamAccountTypeAPIKey,
		Status:         constant.UpstreamStatusActive, SchedulerConfig: "{}",
	}
	require.NoError(t, CreateUpstreamAccountPool(&pool))
	now := common.GetTimestamp()
	limitedUntil := now + 300
	accounts := []model.UpstreamAccount{
		{
			Name: "ready", Platform: constant.UpstreamPlatformOpenAI, Type: constant.UpstreamAccountTypeAPIKey,
			Extra: "{}", Concurrency: 10, Priority: 50, Weight: 1,
			Status: constant.UpstreamStatusActive, Schedulable: true,
		},
		{
			Name: "disabled", Platform: constant.UpstreamPlatformOpenAI, Type: constant.UpstreamAccountTypeAPIKey,
			Extra: "{}", Concurrency: 20, Priority: 50, Weight: 1,
			Status: constant.UpstreamStatusActive, Schedulable: false,
		},
		{
			Name: "limited", Platform: constant.UpstreamPlatformOpenAI, Type: constant.UpstreamAccountTypeAPIKey,
			Extra: "{}", Concurrency: 30, Priority: 50, Weight: 1,
			Status: constant.UpstreamStatusActive, Schedulable: true, RateLimitResetAt: &limitedUntil,
		},
	}
	require.NoError(t, model.DB.Create(&accounts).Error)
	require.NoError(t, model.DB.Model(&model.UpstreamAccount{}).
		Where("id = ?", accounts[1].Id).Update("schedulable", false).Error)
	for _, account := range accounts {
		require.NoError(t, model.DB.Create(&model.UpstreamAccountPoolMember{
			PoolId: pool.Id, AccountId: account.Id, Priority: account.Priority, Weight: account.Weight, CreatedAt: now,
		}).Error)
	}

	views, err := ListUpstreamAccountPools()
	require.NoError(t, err)
	require.Len(t, views, 1)
	require.EqualValues(t, 1, views[0].ReadyCount)
	require.Equal(t, 10, views[0].Concurrency)
	require.Zero(t, views[0].CurrentConcurrency)
}

func TestUpdateUpstreamAccountCredentialPatchPreservesSecretsAndDeletesFields(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	input := UpstreamAccountCreateInput{
		Account: model.UpstreamAccount{
			Name: "patch-account", Platform: constant.UpstreamPlatformOpenAI, Type: constant.UpstreamAccountTypeAPIKey,
			Extra: `{"compact_model_mapping":{"legacy":"legacy"}}`, Concurrency: 1, Priority: 50, Weight: 1,
			Status: constant.UpstreamStatusActive, Schedulable: true, AutoPauseOnExpired: true,
		},
		Credentials: map[string]any{
			"api_key": "secret", "base_url": "https://old.example.com",
			"model_mapping": map[string]any{"gpt-5.2": "gpt-5.2"},
		},
	}
	require.NoError(t, CreateUpstreamAccount(&input))

	patch := map[string]any{
		"base_url":                  nil,
		"model_mapping":             map[string]any{"gpt-5.*": "gpt-5.4"},
		"compact_model_mapping":     map[string]any{"gpt-5.*": "gpt-5.4-compact"},
		"openai_capabilities":       []any{"chat_completions"},
		"intercept_warmup_requests": true,
		"header_override_enabled":   true,
		"header_overrides":          map[string]any{"user-agent": "starnexus-test"},
	}
	require.NoError(t, UpdateUpstreamAccount(&UpstreamAccountUpdateInput{Account: input.Account, CredentialPatch: &patch}))

	var stored model.UpstreamAccount
	require.NoError(t, model.DB.First(&stored, input.Account.Id).Error)
	credentials, err := DecryptUpstreamAccountCredentials(&stored)
	require.NoError(t, err)
	require.Equal(t, "secret", credentials["api_key"])
	require.NotContains(t, credentials, "base_url")
	require.Equal(t, map[string]any{"gpt-5.*": "gpt-5.4"}, credentials["model_mapping"])
	require.Equal(t, map[string]any{"gpt-5.*": "gpt-5.4-compact"}, credentials["compact_model_mapping"])

	view, err := GetUpstreamAccount(input.Account.Id)
	require.NoError(t, err)
	require.True(t, view.Metadata.CredentialReadable)
	require.Equal(t, map[string]string{"gpt-5.*": "gpt-5.4"}, view.Metadata.ModelMapping)
	require.Equal(t, map[string]string{"gpt-5.*": "gpt-5.4-compact"}, view.Metadata.CompactModelMapping)
	require.Equal(t, []string{"chat_completions"}, view.Metadata.OpenAIEndpointCapabilities)
	require.True(t, view.Metadata.InterceptWarmupRequests)
	require.True(t, view.Metadata.HeaderOverrideEnabled)
	require.Equal(t, map[string]string{"user-agent": "starnexus-test"}, view.Metadata.HeaderOverrides)

	invalidPatch := map[string]any{"api_key": nil}
	credentialVersion := stored.CredentialVersion
	require.Error(t, UpdateUpstreamAccount(&UpstreamAccountUpdateInput{Account: stored, CredentialPatch: &invalidPatch}))
	require.NoError(t, model.DB.First(&stored, input.Account.Id).Error)
	require.Equal(t, credentialVersion, stored.CredentialVersion)
}

func TestUpstreamAccountPoolRuntimeStats(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	pool := model.UpstreamAccountPool{
		Name: "stats", Platform: constant.UpstreamPlatformOpenAI, CredentialType: constant.UpstreamAccountTypeOAuth,
		Status: constant.UpstreamStatusActive, SchedulerConfig: "{}",
	}
	require.NoError(t, CreateUpstreamAccountPool(&pool))
	account := createRouterTestAccount(t, "stats-account", pool.Id, nil)
	for _, event := range []struct {
		eventType string
		result    string
	}{
		{"request_selected", "selected"},
		{"request_success", "success"},
		{"request_selected", "selected"},
		{"request_error", "error"},
	} {
		RecordUpstreamAccountEvent(UpstreamAccountEventInput{
			AccountId: account.Id, PoolId: pool.Id, ChannelId: 9,
			RequestId: "request-stats", LeaseId: "lease-stats",
			EventType: event.eventType, Result: event.result,
		})
	}
	view, err := GetUpstreamAccountPool(pool.Id)
	require.NoError(t, err)
	require.EqualValues(t, 1, view.ActiveCount)
	require.EqualValues(t, 1, view.ReadyCount)
	require.EqualValues(t, 2, view.AttemptCount24h)
	require.EqualValues(t, 1, view.SuccessCount24h)
	require.EqualValues(t, 1, view.ErrorCount24h)
	require.NotNil(t, view.LastEventAt)

	resetAt := common.GetTimestamp() + 60
	require.NoError(t, model.DB.Model(&model.UpstreamAccount{}).Where("id = ?", account.Id).
		Update("rate_limit_reset_at", resetAt).Error)
	view, err = GetUpstreamAccountPool(pool.Id)
	require.NoError(t, err)
	require.EqualValues(t, 1, view.ActiveCount)
	require.Zero(t, view.ReadyCount)
}

func TestGetUpstreamAccountPoolCapabilities(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	pool := model.UpstreamAccountPool{
		Name: "capabilities", Platform: constant.UpstreamPlatformOpenAI, CredentialType: "mixed",
		Status: constant.UpstreamStatusActive, SchedulerConfig: "{}",
	}
	require.NoError(t, CreateUpstreamAccountPool(&pool))
	proxy := UpstreamProxyCreateInput{Proxy: model.UpstreamProxy{
		Name: "capability-proxy", Protocol: constant.UpstreamProxyProtocolHTTP,
		Host: "127.0.0.1", Port: 8080, Status: constant.UpstreamStatusActive,
		FallbackMode: constant.UpstreamProxyFallbackNone,
	}}
	require.NoError(t, CreateUpstreamProxy(&proxy))

	createAccount := func(name string, schedulable bool, extra string, credentials map[string]any, proxyID *int) {
		t.Helper()
		input := UpstreamAccountCreateInput{
			Account: model.UpstreamAccount{
				Name: name, Platform: constant.UpstreamPlatformOpenAI, Type: constant.UpstreamAccountTypeAPIKey,
				Extra: extra, ProxyId: proxyID, Concurrency: 1, Priority: 50, Weight: 1,
				Status: constant.UpstreamStatusActive, Schedulable: schedulable, AutoPauseOnExpired: true,
			},
			Credentials: credentials,
			PoolIds:     []int{pool.Id},
		}
		require.NoError(t, CreateUpstreamAccount(&input))
	}

	createAccount("mapped", true, "{}", map[string]any{
		"api_key": "mapped-key",
		"model_mapping": map[string]any{
			"public-model": "upstream-model",
			"gpt-*":        "gpt-5.6",
		},
		"header_override_enabled": true,
		"header_overrides":        map[string]any{"user-agent": "capability-test"},
	}, nil)
	createAccount("passthrough", true, `{"openai_passthrough":true}`, map[string]any{
		"api_key": "passthrough-key",
		"model_mapping": map[string]any{
			"stale-model": "must-not-be-exposed",
		},
	}, &proxy.Proxy.Id)
	createAccount("unrestricted", true, "{}", map[string]any{"api_key": "unrestricted-key"}, nil)
	createAccount("not-schedulable", false, "{}", map[string]any{
		"api_key":       "disabled-key",
		"model_mapping": map[string]any{"hidden-model": "hidden-upstream"},
	}, nil)

	capabilities, err := GetUpstreamAccountPoolCapabilities(pool.Id)
	require.NoError(t, err)
	require.Equal(t, 4, capabilities.AccountCount)
	require.Equal(t, 3, capabilities.SchedulableAccountCount)
	require.Zero(t, capabilities.UnreadableAccountCount)
	require.Equal(t, []string{"public-model"}, capabilities.Models)
	require.Equal(t, 3, capabilities.WildcardModelAccountCount)
	require.Equal(t, 1, capabilities.PassthroughAccountCount)
	require.Equal(t, 1, capabilities.HeaderOverrideAccountCount)
	require.Equal(t, 1, capabilities.ProxyConfiguredAccountCount)
}

func TestPublishUpstreamAccountPoolChannelProjectsAndSyncsAccountSettings(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	pool := model.UpstreamAccountPool{
		Name: "publish-pool", Platform: constant.UpstreamPlatformOpenAI,
		CredentialType: constant.UpstreamAccountTypeAPIKey,
		Status:         constant.UpstreamStatusActive, SchedulerConfig: "{}",
	}
	require.NoError(t, CreateUpstreamAccountPool(&pool))
	accountInput := UpstreamAccountCreateInput{
		Account: model.UpstreamAccount{
			Name: "publish-account", Platform: constant.UpstreamPlatformOpenAI,
			Type: constant.UpstreamAccountTypeAPIKey, Extra: "{}", Concurrency: 1,
			Priority: 50, Weight: 1, Status: constant.UpstreamStatusActive,
			Schedulable: true, AutoPauseOnExpired: true,
		},
		Credentials: map[string]any{
			"api_key": "secret",
			"model_mapping": map[string]any{
				"model-b": "upstream-b",
				"model-a": "upstream-a",
			},
			"base_url": "https://account.example.com",
		},
		PoolIds: []int{pool.Id},
	}
	require.NoError(t, CreateUpstreamAccount(&accountInput))

	created, err := PublishUpstreamAccountPoolChannel(pool.Id, []string{"test3", "default", "test3"}, []string{"model-a"})
	require.NoError(t, err)
	require.True(t, created.Created)
	require.Equal(t, []string{"model-a"}, created.Models)
	require.Equal(t, []string{"test3", "default"}, created.Groups)

	var channel model.Channel
	require.NoError(t, model.DB.First(&channel, created.ChannelId).Error)
	require.Equal(t, pool.Name, channel.Name)
	require.Equal(t, constant.ChannelTypeOpenAI, channel.Type)
	require.Equal(t, constant.ChannelCredentialSourceAccountPool, channel.CredentialSource)
	require.NotNil(t, channel.UpstreamAccountPoolId)
	require.Equal(t, pool.Id, *channel.UpstreamAccountPoolId)
	require.Equal(t, "model-a", channel.Models)
	require.Equal(t, "test3,default", channel.Group)
	require.Empty(t, channel.Key)
	require.Nil(t, channel.BaseURL)
	require.Nil(t, channel.ModelMapping)
	require.NotNil(t, channel.AutoBan)
	require.Zero(t, *channel.AutoBan)
	publishedView, err := GetUpstreamAccountPool(pool.Id)
	require.NoError(t, err)
	require.EqualValues(t, 1, publishedView.ChannelCount)
	require.NotNil(t, publishedView.PublishedChannelId)
	require.Equal(t, channel.Id, *publishedView.PublishedChannelId)
	require.NotNil(t, publishedView.PublishedChannelStatus)
	require.Equal(t, common.ChannelStatusEnabled, *publishedView.PublishedChannelStatus)
	require.Equal(t, 1, publishedView.PublishedModelCount)
	capabilities, err := GetUpstreamAccountPoolCapabilities(pool.Id)
	require.NoError(t, err)
	require.Equal(t, []string{"model-a", "model-b"}, capabilities.Models)
	require.Equal(t, []string{"model-a"}, capabilities.PublishedModels)

	var abilities []model.Ability
	require.NoError(t, model.DB.Where("channel_id = ?", channel.Id).Order("model ASC").Find(&abilities).Error)
	require.Len(t, abilities, 2)

	require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", channel.Id).
		Update("status", common.ChannelStatusManuallyDisabled).Error)
	updated, err := PublishUpstreamAccountPoolChannel(pool.Id, []string{"vip"}, []string{"model-b"})
	require.NoError(t, err)
	require.False(t, updated.Created)
	require.Equal(t, channel.Id, updated.ChannelId)
	var channelCount int64
	require.NoError(t, model.DB.Model(&model.Channel{}).
		Where("credential_source = ? AND upstream_account_pool_id = ?", constant.ChannelCredentialSourceAccountPool, pool.Id).
		Count(&channelCount).Error)
	require.EqualValues(t, 1, channelCount)
	require.NoError(t, model.DB.First(&channel, channel.Id).Error)
	require.Equal(t, "vip", channel.Group)
	require.Equal(t, "model-b", channel.Models)
	require.Equal(t, common.ChannelStatusManuallyDisabled, channel.Status)
	publishedView, err = GetUpstreamAccountPool(pool.Id)
	require.NoError(t, err)
	require.NotNil(t, publishedView.PublishedChannelStatus)
	require.Equal(t, common.ChannelStatusManuallyDisabled, *publishedView.PublishedChannelStatus)
	require.NoError(t, model.DB.Where("channel_id = ?", channel.Id).Find(&abilities).Error)
	require.Len(t, abilities, 1)

	var account model.UpstreamAccount
	require.NoError(t, model.DB.First(&account, accountInput.Account.Id).Error)
	updatedCredentials := map[string]any{
		"api_key": "secret",
		"model_mapping": map[string]any{
			"model-c": "upstream-c",
		},
	}
	require.NoError(t, UpdateUpstreamAccount(&UpstreamAccountUpdateInput{
		Account: account, Credentials: &updatedCredentials,
	}))
	require.NoError(t, model.DB.First(&channel, channel.Id).Error)
	require.Equal(t, "model-b", channel.Models)
	require.Equal(t, common.ChannelStatusManuallyDisabled, channel.Status)
	require.NoError(t, model.DB.Where("channel_id = ?", channel.Id).Find(&abilities).Error)
	require.Len(t, abilities, 1)

	republished, err := PublishUpstreamAccountPoolChannel(pool.Id, []string{"vip"}, []string{"model-c"})
	require.NoError(t, err)
	require.False(t, republished.Created)
	require.NoError(t, model.DB.First(&channel, channel.Id).Error)
	require.Equal(t, "model-c", channel.Models)
	require.Equal(t, common.ChannelStatusManuallyDisabled, channel.Status)

	require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", channel.Id).
		Update("status", common.ChannelStatusEnabled).Error)
	require.NoError(t, model.DB.First(&channel, channel.Id).Error)
	require.NoError(t, channel.UpdateAbilities(model.DB))

	require.NoError(t, DeleteUpstreamAccount(account.Id))
	require.NoError(t, model.DB.First(&channel, channel.Id).Error)
	require.Equal(t, "model-c", channel.Models)
	require.Equal(t, common.ChannelStatusEnabled, channel.Status)
	require.NoError(t, model.DB.Where("channel_id = ?", channel.Id).Find(&abilities).Error)
	require.Len(t, abilities, 1)

	require.NoError(t, UnpublishUpstreamAccountPoolChannel(pool.Id))
	require.Error(t, model.DB.First(&channel, channel.Id).Error)
	var storedPool model.UpstreamAccountPool
	require.NoError(t, model.DB.First(&storedPool, pool.Id).Error)
}

func TestPublishUpstreamAccountPoolChannelRequiresConcreteModels(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	pool := model.UpstreamAccountPool{
		Name: "wildcard-only", Platform: constant.UpstreamPlatformOpenAI,
		CredentialType: constant.UpstreamAccountTypeAPIKey,
		Status:         constant.UpstreamStatusActive, SchedulerConfig: "{}",
	}
	require.NoError(t, CreateUpstreamAccountPool(&pool))
	input := UpstreamAccountCreateInput{
		Account: model.UpstreamAccount{
			Name: "unrestricted", Platform: constant.UpstreamPlatformOpenAI,
			Type: constant.UpstreamAccountTypeAPIKey, Extra: "{}", Concurrency: 1,
			Priority: 50, Weight: 1, Status: constant.UpstreamStatusActive,
			Schedulable: true, AutoPauseOnExpired: true,
		},
		Credentials: map[string]any{"api_key": "secret"},
		PoolIds:     []int{pool.Id},
	}
	require.NoError(t, CreateUpstreamAccount(&input))
	_, err := PublishUpstreamAccountPoolChannel(pool.Id, []string{"default"})
	require.ErrorContains(t, err, "no concrete account models")
}

func TestPublishUpstreamAccountPoolChannelRejectsInvalidModelSelection(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	pool := model.UpstreamAccountPool{
		Name: "model-selection", Platform: constant.UpstreamPlatformOpenAI,
		CredentialType: constant.UpstreamAccountTypeAPIKey,
		Status:         constant.UpstreamStatusActive, SchedulerConfig: "{}",
	}
	require.NoError(t, CreateUpstreamAccountPool(&pool))
	input := UpstreamAccountCreateInput{
		Account: model.UpstreamAccount{
			Name: "model-selection-account", Platform: constant.UpstreamPlatformOpenAI,
			Type: constant.UpstreamAccountTypeAPIKey, Extra: "{}", Concurrency: 1,
			Priority: 50, Weight: 1, Status: constant.UpstreamStatusActive,
			Schedulable: true, AutoPauseOnExpired: true,
		},
		Credentials: map[string]any{
			"api_key":       "secret",
			"model_mapping": map[string]any{"model-a": "upstream-a"},
		},
		PoolIds: []int{pool.Id},
	}
	require.NoError(t, CreateUpstreamAccount(&input))

	_, err := PublishUpstreamAccountPoolChannel(pool.Id, []string{"default"}, []string{})
	require.ErrorContains(t, err, "at least one channel model")
	_, err = PublishUpstreamAccountPoolChannel(pool.Id, []string{"default"}, []string{"unknown-model"})
	require.ErrorContains(t, err, "not available from this account pool")
}

func TestUpdateUpstreamAccountPreservesExistingPoolMemberOverrides(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	pool := model.UpstreamAccountPool{
		Name: "primary", Platform: constant.UpstreamPlatformOpenAI, CredentialType: constant.UpstreamAccountTypeAPIKey,
		Status: constant.UpstreamStatusActive, SchedulerConfig: "{}",
	}
	require.NoError(t, CreateUpstreamAccountPool(&pool))
	secondPool := model.UpstreamAccountPool{
		Name: "secondary", Platform: constant.UpstreamPlatformOpenAI, CredentialType: constant.UpstreamAccountTypeAPIKey,
		Status: constant.UpstreamStatusActive, SchedulerConfig: "{}",
	}
	require.NoError(t, CreateUpstreamAccountPool(&secondPool))
	input := UpstreamAccountCreateInput{
		Account: model.UpstreamAccount{
			Name: "api-key", Platform: constant.UpstreamPlatformOpenAI, Type: constant.UpstreamAccountTypeAPIKey,
			Extra: "{}", Concurrency: 1, Priority: 50, Weight: 3,
			Status: constant.UpstreamStatusActive, Schedulable: true, AutoPauseOnExpired: true,
		},
		Credentials: map[string]any{"api_key": "secret"},
		PoolIds:     []int{pool.Id},
	}
	require.NoError(t, CreateUpstreamAccount(&input))
	require.NoError(t, model.DB.Model(&model.UpstreamAccountPoolMember{}).
		Where("pool_id = ? AND account_id = ?", pool.Id, input.Account.Id).
		Updates(map[string]any{"priority": 7, "weight": 9}).Error)

	account := input.Account
	account.Name = "api-key-updated"
	poolIds := []int{pool.Id, secondPool.Id}
	require.NoError(t, UpdateUpstreamAccount(&UpstreamAccountUpdateInput{Account: account, PoolIds: &poolIds}))

	var primary model.UpstreamAccountPoolMember
	require.NoError(t, model.DB.Where("pool_id = ? AND account_id = ?", pool.Id, account.Id).First(&primary).Error)
	require.Equal(t, 7, primary.Priority)
	require.Equal(t, 9, primary.Weight)
	var secondary model.UpstreamAccountPoolMember
	require.NoError(t, model.DB.Where("pool_id = ? AND account_id = ?", secondPool.Id, account.Id).First(&secondary).Error)
	require.Equal(t, account.Priority, secondary.Priority)
	require.Equal(t, account.Weight, secondary.Weight)
}

func TestReplaceUpstreamAccountPoolMembersPreservesCreatedAt(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	pool := model.UpstreamAccountPool{
		Name: "membership-diff", Platform: constant.UpstreamPlatformOpenAI, CredentialType: constant.UpstreamAccountTypeOAuth,
		Status: constant.UpstreamStatusActive, SchedulerConfig: "{}",
	}
	require.NoError(t, CreateUpstreamAccountPool(&pool))
	account := createRouterTestAccount(t, "membership-account", pool.Id, nil)
	const originalCreatedAt int64 = 123456789
	require.NoError(t, model.DB.Model(&model.UpstreamAccountPoolMember{}).
		Where("pool_id = ? AND account_id = ?", pool.Id, account.Id).
		Update("created_at", originalCreatedAt).Error)
	var before model.UpstreamAccountPoolMember
	require.NoError(t, model.DB.Where("pool_id = ? AND account_id = ?", pool.Id, account.Id).First(&before).Error)

	require.NoError(t, ReplaceUpstreamAccountPoolMembers(pool.Id, []UpstreamAccountPoolMemberInput{{
		AccountId: account.Id,
		Priority:  7,
		Weight:    9,
	}}))

	var after model.UpstreamAccountPoolMember
	require.NoError(t, model.DB.Where("pool_id = ? AND account_id = ?", pool.Id, account.Id).First(&after).Error)
	require.Equal(t, before.CreatedAt, after.CreatedAt)
	require.Equal(t, 7, after.Priority)
	require.Equal(t, 9, after.Weight)

	views, err := ListUpstreamAccountPoolMembers(pool.Id)
	require.NoError(t, err)
	require.Len(t, views, 1)
	require.Equal(t, account.Name, views[0].Name)
	require.Equal(t, originalCreatedAt, views[0].CreatedAt)
}

func TestUpstreamAccountAdminRejectsIncompatibleTypeChanges(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	pool := model.UpstreamAccountPool{
		Name: "oauth-only", Platform: constant.UpstreamPlatformOpenAI, CredentialType: constant.UpstreamAccountTypeOAuth,
		Status: constant.UpstreamStatusActive, SchedulerConfig: "{}",
	}
	require.NoError(t, CreateUpstreamAccountPool(&pool))
	account := createRouterTestAccount(t, "oauth-member", pool.Id, nil)

	pool.CredentialType = constant.UpstreamAccountTypeAPIKey
	require.ErrorContains(t, UpdateUpstreamAccountPool(&pool), "incompatible account")

	account.Type = constant.UpstreamAccountTypeAPIKey
	require.ErrorContains(t, UpdateUpstreamAccount(&UpstreamAccountUpdateInput{Account: account}), "replacement credentials")
}

func TestRecoverUpstreamAccountRuntimeState(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	now := common.GetTimestamp()
	resetAt := now + 300
	overloadUntil := now + 120
	tempUntil := now + 60
	account := model.UpstreamAccount{
		Name: "recover-account", Platform: constant.UpstreamPlatformOpenAI, Type: constant.UpstreamAccountTypeAPIKey,
		Extra: "{}", Concurrency: 1, Priority: 50, Weight: 1, Status: constant.UpstreamStatusError,
		Schedulable: false, ErrorMessage: "rate limited", RateLimitedAt: &now, RateLimitResetAt: &resetAt,
		OverloadUntil: &overloadUntil, TempUnschedulableUntil: &tempUntil, TempUnschedulableReason: "rate_limited",
		SessionWindowStart: &now, SessionWindowEnd: &resetAt, SessionWindowStatus: "rejected",
	}
	input := UpstreamAccountCreateInput{Account: account, Credentials: map[string]any{"api_key": "secret"}}
	require.NoError(t, CreateUpstreamAccount(&input))

	require.NoError(t, RecoverUpstreamAccountRuntimeState(input.Account.Id, UpstreamAccountRecoveryAll))
	var recovered model.UpstreamAccount
	require.NoError(t, model.DB.First(&recovered, input.Account.Id).Error)
	require.Equal(t, constant.UpstreamStatusActive, recovered.Status)
	require.True(t, recovered.Schedulable)
	require.Empty(t, recovered.ErrorMessage)
	require.Nil(t, recovered.RateLimitResetAt)
	require.Nil(t, recovered.OverloadUntil)
	require.Nil(t, recovered.TempUnschedulableUntil)
	require.Empty(t, recovered.TempUnschedulableReason)
	require.Nil(t, recovered.SessionWindowStart)
	require.Nil(t, recovered.SessionWindowEnd)
	require.Empty(t, recovered.SessionWindowStatus)
	require.True(t, recovered.IsSchedulableAt(now))
}

func TestRecoverUpstreamAccountKeepsExpiredAccountPaused(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	expiresAt := common.GetTimestamp() - 1
	account := model.UpstreamAccount{
		Name: "expired-account", Platform: constant.UpstreamPlatformOpenAI, Type: constant.UpstreamAccountTypeAPIKey,
		Extra: "{}", Concurrency: 1, Priority: 50, Weight: 1, Status: constant.UpstreamStatusError,
		Schedulable: false, ExpiresAt: &expiresAt, AutoPauseOnExpired: true,
	}
	input := UpstreamAccountCreateInput{Account: account, Credentials: map[string]any{"api_key": "secret"}}
	require.NoError(t, CreateUpstreamAccount(&input))
	require.NoError(t, RecoverUpstreamAccountRuntimeState(input.Account.Id, UpstreamAccountRecoveryAll))

	var recovered model.UpstreamAccount
	require.NoError(t, model.DB.First(&recovered, input.Account.Id).Error)
	require.Equal(t, constant.UpstreamStatusExpired, recovered.Status)
	require.False(t, recovered.Schedulable)
}
