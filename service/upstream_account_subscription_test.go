package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFetchChatGPTAccountSubscriptionInfoPrefersMatchingAccount(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer token", r.Header.Get("Authorization"))
		require.Contains(t, r.Header.Get("User-Agent"), "Chrome/")
		_, _ = w.Write([]byte(`{"accounts":{"other":{"account":{"plan_type":"free","is_default":true},"entitlement":{"expires_at":"2027-01-01T00:00:00Z"}},"acct-paid":{"account":{"plan_type":"pro"},"entitlement":{"expires_at":"2026-12-31T00:00:00Z"}}}}`))
	}))
	defer server.Close()
	setChatGPTSubscriptionURLsForTest(t, server.URL, server.URL)

	info := fetchChatGPTAccountSubscriptionInfo(context.Background(), "token", "", "acct-paid")
	require.NotNil(t, info)
	require.Equal(t, "pro", info.PlanType)
	require.Equal(t, "2026-12-31T00:00:00Z", info.ExpiresAt)
}

func TestFetchChatGPTAccountSubscriptionInfoFallsBackToSubscriptions(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/check", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"accounts":{"acct/plus":{"account":{"plan_type":"plus"},"entitlement":{}}}}`))
	})
	mux.HandleFunc("/subscriptions", func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "acct/plus", r.URL.Query().Get("account_id"))
		require.Empty(t, r.Header.Get("chatgpt-account-id"))
		_, _ = w.Write([]byte(`{"plan_type":"plus","active_until":"2026-08-31T12:30:00Z","will_renew":true}`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	setChatGPTSubscriptionURLsForTest(t, server.URL+"/check", server.URL+"/subscriptions")

	info := fetchChatGPTAccountSubscriptionInfo(context.Background(), "token", "", "acct/plus")
	require.NotNil(t, info)
	require.Equal(t, "plus", info.PlanType)
	require.Equal(t, "2026-08-31T12:30:00Z", info.ExpiresAt)
}

func TestFetchChatGPTAccountSubscriptionInfoRejectsInvalidFallbackDate(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/check", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"accounts":{"acct":{"account":{"plan_type":"plus"},"entitlement":{}}}}`))
	})
	mux.HandleFunc("/subscriptions", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"active_until":"not-a-date"}`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	setChatGPTSubscriptionURLsForTest(t, server.URL+"/check", server.URL+"/subscriptions")

	info := fetchChatGPTAccountSubscriptionInfo(context.Background(), "token", "", "acct")
	require.NotNil(t, info)
	require.Equal(t, "plus", info.PlanType)
	require.Empty(t, info.ExpiresAt)
}

func TestEnrichUpstreamOpenAICredentialsKeepsJWTMetadataWhenWebEndpointsRejectToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()
	setChatGPTSubscriptionURLsForTest(t, server.URL+"/check", server.URL+"/subscriptions")

	credentials := map[string]any{
		"access_token": codexTestJWT(t, map[string]any{
			"https://api.openai.com/profile": map[string]any{"email": "oauth@example.com"},
			codexJWTClaimPath: map[string]any{
				"chatgpt_account_id": "acct-oauth",
				"chatgpt_plan_type":  "pro",
			},
		}),
		"account_id": "acct-oauth",
	}

	enrichUpstreamOpenAICredentials(context.Background(), credentials, "")
	require.Equal(t, "oauth@example.com", credentials["email"])
	require.Equal(t, "pro", credentials["plan_type"])
	require.NotContains(t, credentials, "subscription_expires_at")
}

func TestEnrichUpstreamOpenAICredentialsUsesIDTokenSubscriptionMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/check" {
			_, _ = w.Write([]byte(`{"accounts":{}}`))
			return
		}
		http.Error(w, "unexpected subscription request", http.StatusInternalServerError)
	}))
	defer server.Close()
	setChatGPTSubscriptionURLsForTest(t, server.URL+"/check", server.URL+"/subscriptions")

	credentials := map[string]any{
		"id_token": codexTestJWT(t, map[string]any{
			codexJWTClaimPath: map[string]any{
				"chatgpt_plan_type":                 "pro",
				"chatgpt_subscription_active_until": "2027-03-04T05:06:07Z",
			},
		}),
		"access_token": codexTestJWT(t, map[string]any{
			"email":           "id-token@example.com",
			codexJWTClaimPath: map[string]any{"chatgpt_account_id": "acct-id-token"},
		}),
	}

	enrichUpstreamOpenAICredentials(context.Background(), credentials, "")
	require.Equal(t, "id-token@example.com", credentials["email"])
	require.Equal(t, "pro", credentials["plan_type"])
	require.Equal(t, "2027-03-04T05:06:07Z", credentials["subscription_expires_at"])
}

func setChatGPTSubscriptionURLsForTest(t *testing.T, accountsURL, subscriptionsURL string) {
	originalAccountsURL := chatGPTAccountsCheckURL
	originalSubscriptionsURL := chatGPTSubscriptionsURL
	chatGPTAccountsCheckURL = accountsURL
	chatGPTSubscriptionsURL = subscriptionsURL
	t.Cleanup(func() {
		chatGPTAccountsCheckURL = originalAccountsURL
		chatGPTSubscriptionsURL = originalSubscriptionsURL
	})
}
