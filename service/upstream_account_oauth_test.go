package service

import (
	"context"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestStartUpstreamCodexOAuthStoresEncryptedServerState(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	result, err := StartUpstreamCodexOAuth(UpstreamOAuthStartInput{})
	require.NoError(t, err)
	require.NotEmpty(t, result.AuthorizeURL)

	parsed, err := url.Parse(result.AuthorizeURL)
	require.NoError(t, err)
	state := parsed.Query().Get("state")
	require.NotEmpty(t, state)

	var session model.UpstreamOAuthSession
	require.NoError(t, model.DB.First(&session).Error)
	require.NotEqual(t, state, session.StateHash)
	require.Equal(t, hashUpstreamOAuthState(state), session.StateHash)
	require.NotEmpty(t, session.VerifierCiphertext)
	require.NotEmpty(t, session.VerifierNonce)
	require.Greater(t, session.ExpiresAt, session.CreatedAt)

	keyring, err := LoadUpstreamCredentialKeyringFromEnv()
	require.NoError(t, err)
	var payload map[string]string
	require.NoError(t, keyring.DecryptJSON(UpstreamCredentialEnvelope{
		Ciphertext: session.VerifierCiphertext, Nonce: session.VerifierNonce, KeyVersion: session.VerifierKeyVersion,
	}, upstreamOAuthSessionRecordKind, session.Id, session.VerifierVersion, &payload))
	require.NotEmpty(t, payload["verifier"])
}

func TestStartUpstreamClaudeSetupTokenStoresPlatformAndScope(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	result, err := StartUpstreamAccountOAuth(UpstreamOAuthStartInput{
		Platform: constant.UpstreamPlatformAnthropic, CredentialType: constant.UpstreamAccountTypeSetupToken,
	})
	require.NoError(t, err)
	parsed, err := url.Parse(result.AuthorizeURL)
	require.NoError(t, err)
	require.Equal(t, claudeSetupTokenScope, parsed.Query().Get("scope"))
	require.Equal(t, claudeOAuthClientID, parsed.Query().Get("client_id"))

	var session model.UpstreamOAuthSession
	require.NoError(t, model.DB.First(&session).Error)
	keyring, err := LoadUpstreamCredentialKeyringFromEnv()
	require.NoError(t, err)
	var payload map[string]string
	require.NoError(t, keyring.DecryptJSON(UpstreamCredentialEnvelope{
		Ciphertext: session.VerifierCiphertext, Nonce: session.VerifierNonce, KeyVersion: session.VerifierKeyVersion,
	}, upstreamOAuthSessionRecordKind, session.Id, session.VerifierVersion, &payload))
	require.Equal(t, constant.UpstreamPlatformAnthropic, payload["platform"])
	require.Equal(t, constant.UpstreamAccountTypeSetupToken, payload["credential_type"])
	require.Equal(t, claudeSetupTokenScope, payload["scope"])
}

func TestStartUpstreamCodexOAuthCanExplicitlyClearExistingProxy(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	proxy := createRouterTestProxy(t, "oauth-proxy", 18080)
	account := createRouterTestAccountWithoutPool(t, "oauth-account")
	require.NoError(t, model.DB.Model(&model.UpstreamAccount{}).Where("id = ?", account.Id).
		Update("proxy_id", proxy.Id).Error)

	_, err := StartUpstreamCodexOAuth(UpstreamOAuthStartInput{AccountId: &account.Id})
	require.NoError(t, err)
	var inherited model.UpstreamOAuthSession
	require.NoError(t, model.DB.Order("id DESC").First(&inherited).Error)
	require.NotNil(t, inherited.ProxyId)
	require.Equal(t, proxy.Id, *inherited.ProxyId)

	_, err = StartUpstreamCodexOAuth(UpstreamOAuthStartInput{AccountId: &account.Id, ProxyIdPresent: true})
	require.NoError(t, err)
	var direct model.UpstreamOAuthSession
	require.NoError(t, model.DB.Order("id DESC").First(&direct).Error)
	require.Nil(t, direct.ProxyId)
}

func TestReplaceUpstreamOAuthCredentialRecoversAccount(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	account := createRouterTestAccountWithoutPool(t, "refresh-account")
	require.NoError(t, model.DB.Model(&model.UpstreamAccount{}).Where("id = ?", account.Id).Updates(map[string]any{
		"status": constant.UpstreamStatusError, "schedulable": false, "error_message": "old error",
	}).Error)
	var before model.UpstreamAccount
	require.NoError(t, model.DB.First(&before, account.Id).Error)

	expiresAt := int64(123456789)
	credentials := map[string]any{
		"access_token": "new-access", "refresh_token": "new-refresh", "account_id": "acct-refresh",
	}
	require.NoError(t, replaceUpstreamOAuthCredential(account.Id, credentials, expiresAt, nil))
	var after model.UpstreamAccount
	require.NoError(t, model.DB.First(&after, account.Id).Error)
	require.Equal(t, before.CredentialVersion+1, after.CredentialVersion)
	require.Equal(t, constant.UpstreamStatusActive, after.Status)
	require.True(t, after.Schedulable)
	require.Empty(t, after.ErrorMessage)
	require.Equal(t, constant.UpstreamOAuthRefreshOwnerStarNexus, after.OAuthRefreshOwner)
	require.Equal(t, expiresAt, *after.ExpiresAt)
	decoded, err := DecryptUpstreamAccountCredentials(&after)
	require.NoError(t, err)
	require.Equal(t, "new-access", decoded["access_token"])
}

func TestCreateManagedOAuthAccountDerivesMissingExpiryFromCredential(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	expiresAt := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)
	input := UpstreamAccountCreateInput{
		Account: model.UpstreamAccount{
			Name: "managed-expiry", Platform: constant.UpstreamPlatformOpenAI, Type: constant.UpstreamAccountTypeOAuth,
			Extra: "{}", Concurrency: 1, Priority: 50, Weight: 1, Status: constant.UpstreamStatusActive,
			Schedulable: true, AutoPauseOnExpired: true, OAuthRefreshOwner: constant.UpstreamOAuthRefreshOwnerStarNexus,
		},
		Credentials: map[string]any{
			"access_token": "access", "refresh_token": "refresh", "account_id": "acct",
			"expired": expiresAt.Format(time.RFC3339),
		},
	}
	require.NoError(t, CreateUpstreamAccount(&input))

	var stored model.UpstreamAccount
	require.NoError(t, model.DB.First(&stored, input.Account.Id).Error)
	require.NotNil(t, stored.ExpiresAt)
	require.Equal(t, expiresAt.Unix(), *stored.ExpiresAt)
}

func TestUpstreamAccountMetadataFallsBackToCodexJWTIdentity(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	token := codexTestJWT(t, map[string]any{
		"https://api.openai.com/profile": map[string]any{"email": "jwt@example.com"},
		codexJWTClaimPath: map[string]any{
			"chatgpt_account_id": "acct-jwt",
			"chatgpt_plan_type":  "pro",
		},
		"exp": float64(time.Now().Add(time.Hour).Unix()),
	})
	input := UpstreamAccountCreateInput{
		Account: model.UpstreamAccount{
			Name: "jwt-metadata", Platform: constant.UpstreamPlatformOpenAI, Type: constant.UpstreamAccountTypeOAuth,
			Extra: "{}", Concurrency: 1, Priority: 50, Weight: 1, Status: constant.UpstreamStatusActive,
			Schedulable: true, AutoPauseOnExpired: true, OAuthRefreshOwner: constant.UpstreamOAuthRefreshOwnerStarNexus,
		},
		Credentials: map[string]any{"access_token": token, "refresh_token": "refresh", "account_id": "acct-jwt"},
	}
	require.NoError(t, CreateUpstreamAccount(&input))

	view, err := GetUpstreamAccount(input.Account.Id)
	require.NoError(t, err)
	require.Equal(t, "jwt@example.com", view.Metadata.Email)
	require.Equal(t, "pro", view.Metadata.PlanType)
}

func TestUpdateManagedOAuthAccountRestoresClearedCredentialExpiry(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	expiresAt := time.Now().Add(3 * time.Hour).UTC().Truncate(time.Second)
	input := UpstreamAccountCreateInput{
		Account: model.UpstreamAccount{
			Name: "managed-update-expiry", Platform: constant.UpstreamPlatformOpenAI, Type: constant.UpstreamAccountTypeOAuth,
			Extra: "{}", Concurrency: 1, Priority: 50, Weight: 1, Status: constant.UpstreamStatusActive,
			Schedulable: true, AutoPauseOnExpired: true, OAuthRefreshOwner: constant.UpstreamOAuthRefreshOwnerStarNexus,
		},
		Credentials: map[string]any{
			"access_token": "access", "refresh_token": "refresh", "account_id": "acct",
			"expired": expiresAt.Format(time.RFC3339),
		},
	}
	require.NoError(t, CreateUpstreamAccount(&input))
	require.NoError(t, model.DB.Model(&model.UpstreamAccount{}).Where("id = ?", input.Account.Id).Update("expires_at", nil).Error)

	var account model.UpstreamAccount
	require.NoError(t, model.DB.First(&account, input.Account.Id).Error)
	account.Name = "managed-update-expiry-renamed"
	require.NoError(t, UpdateUpstreamAccount(&UpstreamAccountUpdateInput{Account: account}))

	require.NoError(t, model.DB.First(&account, input.Account.Id).Error)
	require.NotNil(t, account.ExpiresAt)
	require.Equal(t, expiresAt.Unix(), *account.ExpiresAt)
}

func TestRestoreMissingManagedOAuthExpirations(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	expiresAt := time.Now().Add(4 * time.Hour).UTC().Truncate(time.Second)
	input := UpstreamAccountCreateInput{
		Account: model.UpstreamAccount{
			Name: "managed-backfill-expiry", Platform: constant.UpstreamPlatformOpenAI, Type: constant.UpstreamAccountTypeOAuth,
			Extra: "{}", Concurrency: 1, Priority: 50, Weight: 1, Status: constant.UpstreamStatusActive,
			Schedulable: true, AutoPauseOnExpired: true, OAuthRefreshOwner: constant.UpstreamOAuthRefreshOwnerStarNexus,
		},
		Credentials: map[string]any{
			"access_token": "access", "refresh_token": "refresh", "account_id": "acct",
			"expired": expiresAt.Format(time.RFC3339),
		},
	}
	require.NoError(t, CreateUpstreamAccount(&input))
	require.NoError(t, model.DB.Model(&model.UpstreamAccount{}).Where("id = ?", input.Account.Id).Update("expires_at", nil).Error)

	restored, err := restoreMissingManagedOAuthExpirations()
	require.NoError(t, err)
	require.EqualValues(t, 1, restored)
	var account model.UpstreamAccount
	require.NoError(t, model.DB.First(&account, input.Account.Id).Error)
	require.NotNil(t, account.ExpiresAt)
	require.Equal(t, expiresAt.Unix(), *account.ExpiresAt)
}

func TestShouldRefreshUpstreamOAuthAccount(t *testing.T) {
	expiresAt := int64(200)
	account := &model.UpstreamAccount{
		Id: 1, Platform: constant.UpstreamPlatformOpenAI, Type: constant.UpstreamAccountTypeOAuth,
		Status: constant.UpstreamStatusActive, ExpiresAt: &expiresAt, CredentialCiphertext: "configured",
		OAuthRefreshOwner: constant.UpstreamOAuthRefreshOwnerStarNexus,
	}
	require.True(t, shouldRefreshUpstreamOAuthAccount(account, 200))
	require.False(t, shouldRefreshUpstreamOAuthAccount(account, 199))
	account.Status = constant.UpstreamStatusInactive
	require.False(t, shouldRefreshUpstreamOAuthAccount(account, 200))
	account.Status = constant.UpstreamStatusError
	require.True(t, shouldRefreshUpstreamOAuthAccount(account, 200), "refresh can recover an errored OAuth account")
	backoffUntil := common.GetTimestamp() + 60
	account.TempUnschedulableReason = "oauth_refresh_failed"
	account.TempUnschedulableUntil = &backoffUntil
	require.False(t, shouldRefreshUpstreamOAuthAccount(account, 200), "refresh failure backoff must suppress repeated refresh attempts")
	backoffUntil = common.GetTimestamp() - 1
	require.True(t, shouldRefreshUpstreamOAuthAccount(account, 200))
	account.OAuthRefreshOwner = constant.UpstreamOAuthRefreshOwnerExternal
	require.False(t, shouldRefreshUpstreamOAuthAccount(account, 200))
}

func TestRefreshUpstreamOAuthAccountRejectsExternalOwner(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	account := createRouterTestAccountWithoutPool(t, "external-refresh-owner")
	_, err := refreshUpstreamOAuthAccountUnlocked(context.Background(), account.Id)
	require.EqualError(t, err, "OAuth refresh is owned by an external system")
}

func TestWithUpstreamOAuthRefreshLockRejectsConcurrentLocalRefresh(t *testing.T) {
	originalRedisEnabled := common.RedisEnabled
	originalRDB := common.RDB
	common.RedisEnabled = false
	common.RDB = nil
	t.Cleanup(func() {
		common.RedisEnabled = originalRedisEnabled
		common.RDB = originalRDB
	})
	t.Setenv("UPSTREAM_ACCOUNT_ALLOW_LOCAL_LEASES", "true")

	err := withUpstreamOAuthRefreshLock(context.Background(), 42, time.Minute, func(ctx context.Context) error {
		innerErr := withUpstreamOAuthRefreshLock(ctx, 42, time.Minute, func(context.Context) error {
			return nil
		})
		require.ErrorIs(t, innerErr, errUpstreamOAuthRefreshLocked)
		return nil
	})
	require.NoError(t, err)

	err = withUpstreamOAuthRefreshLock(context.Background(), 42, time.Minute, func(context.Context) error {
		return errors.New("sentinel")
	})
	require.EqualError(t, err, "sentinel")
}

func TestCleanupUpstreamOAuthSessions(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	now := common.GetTimestamp()
	sessions := []model.UpstreamOAuthSession{
		{StateHash: "expired", VerifierCiphertext: "x", VerifierNonce: "y", VerifierKeyVersion: 1, VerifierVersion: 1, CreatedAt: now - 100, ExpiresAt: now - 1},
		{StateHash: "used-old", VerifierCiphertext: "x", VerifierNonce: "y", VerifierKeyVersion: 1, VerifierVersion: 1, CreatedAt: now - 100000, ExpiresAt: now + 100, UsedAt: now - int64((25 * time.Hour).Seconds())},
		{StateHash: "active", VerifierCiphertext: "x", VerifierNonce: "y", VerifierKeyVersion: 1, VerifierVersion: 1, CreatedAt: now, ExpiresAt: now + 100},
	}
	require.NoError(t, model.DB.Create(&sessions).Error)
	cleanupUpstreamOAuthSessions(now)
	var remaining []model.UpstreamOAuthSession
	require.NoError(t, model.DB.Order("id ASC").Find(&remaining).Error)
	require.Len(t, remaining, 1)
	require.Equal(t, "active", remaining[0].StateHash)
}
