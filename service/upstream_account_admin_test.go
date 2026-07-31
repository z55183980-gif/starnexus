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
	require.EqualValues(t, 2, view.AttemptCount24h)
	require.EqualValues(t, 1, view.SuccessCount24h)
	require.EqualValues(t, 1, view.ErrorCount24h)
	require.NotNil(t, view.LastEventAt)
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
