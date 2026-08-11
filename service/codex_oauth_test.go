package service

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func codexTestJWT(t *testing.T, claims map[string]any) string {
	t.Helper()
	header, err := common.Marshal(map[string]any{"alg": "none"})
	require.NoError(t, err)
	payload, err := common.Marshal(claims)
	require.NoError(t, err)
	return base64.RawURLEncoding.EncodeToString(header) + "." +
		base64.RawURLEncoding.EncodeToString(payload) + ".signature"
}

func TestExtractCodexOAuthIdentityFromNamespacedClaims(t *testing.T) {
	token := codexTestJWT(t, map[string]any{
		"https://api.openai.com/profile": map[string]any{"email": " person@example.com "},
		codexJWTClaimPath: map[string]any{
			"chatgpt_account_id":                " acct-1 ",
			"chatgpt_plan_type":                 " pro ",
			"chatgpt_subscription_active_until": "2027-01-02T03:04:05Z",
		},
	})

	identity, ok := ExtractCodexOAuthIdentityFromJWT(token)
	require.True(t, ok)
	require.Equal(t, "acct-1", identity.AccountID)
	require.Equal(t, "person@example.com", identity.Email)
	require.Equal(t, "pro", identity.PlanType)
	require.Equal(t, "2027-01-02T03:04:05Z", identity.SubscriptionExpiresAt)

	email, ok := ExtractEmailFromJWT(token)
	require.True(t, ok)
	require.Equal(t, "person@example.com", email)
}

func TestExtractCodexOAuthIdentityKeepsTopLevelEmailCompatibility(t *testing.T) {
	token := codexTestJWT(t, map[string]any{
		"email":           "legacy@example.com",
		codexJWTClaimPath: map[string]any{"chatgpt_account_id": "acct-legacy"},
	})
	identity, ok := ExtractCodexOAuthIdentityFromJWT(token)
	require.True(t, ok)
	require.Equal(t, "legacy@example.com", identity.Email)
}

func TestExchangeCodexAuthorizationCodePreservesTokenMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		require.Equal(t, "authorization_code", r.Form.Get("grant_type"))
		_, _ = w.Write([]byte(`{"access_token":"access","refresh_token":"refresh","id_token":"id","token_type":"Bearer","scope":"openid email","expires_in":3600}`))
	}))
	defer server.Close()

	result, err := exchangeCodexAuthorizationCode(context.Background(), server.Client(), server.URL, "client", "code", "verifier", "http://localhost/callback")
	require.NoError(t, err)
	require.Equal(t, "id", result.IDToken)
	require.Equal(t, "Bearer", result.TokenType)
	require.Equal(t, "openid email", result.Scope)
}

func TestRefreshCodexOAuthTokenAllowsUnrotatedRefreshToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"access_token":"new-access","expires_in":3600}`))
	}))
	defer server.Close()

	result, err := refreshCodexOAuthToken(context.Background(), server.Client(), server.URL, "client", "existing-refresh")
	require.NoError(t, err)
	require.Equal(t, "new-access", result.AccessToken)
	require.Empty(t, result.RefreshToken)
}

func TestApplyCodexChannelTokenRefreshPreservesUnrotatedTokens(t *testing.T) {
	key := &CodexOAuthKey{RefreshToken: "existing-refresh", IDToken: "existing-id"}
	result := &CodexOAuthTokenResult{AccessToken: "new-access", ExpiresAt: time.Now().Add(time.Hour)}
	applyCodexChannelTokenRefresh(key, result)
	require.Equal(t, "new-access", key.AccessToken)
	require.Equal(t, "existing-refresh", key.RefreshToken)
	require.Equal(t, "existing-id", key.IDToken)
}

func TestUpstreamOAuthCredentialExpiryFallsBackToJWT(t *testing.T) {
	expiresAt := time.Now().Add(time.Hour).Unix()
	token := codexTestJWT(t, map[string]any{"exp": float64(expiresAt)})
	actual, ok := upstreamOAuthCredentialExpiry(map[string]any{"access_token": token})
	require.True(t, ok)
	require.Equal(t, expiresAt, actual)
}
