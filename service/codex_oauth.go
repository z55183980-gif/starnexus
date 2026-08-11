package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
)

const (
	codexOAuthClientID     = "app_EMoamEEZ73f0CkXaXp7hrann"
	codexOAuthAuthorizeURL = "https://auth.openai.com/oauth/authorize"
	codexOAuthTokenURL     = "https://auth.openai.com/oauth/token"
	codexOAuthRedirectURI  = "http://localhost:1455/auth/callback"
	codexOAuthScope        = "openid profile email offline_access"
	codexJWTClaimPath      = "https://api.openai.com/auth"
	defaultHTTPTimeout     = 20 * time.Second
)

type CodexOAuthTokenResult struct {
	AccessToken  string
	RefreshToken string
	IDToken      string
	TokenType    string
	Scope        string
	ExpiresAt    time.Time
}

type CodexOAuthIdentity struct {
	AccountID             string
	Email                 string
	PlanType              string
	SubscriptionExpiresAt string
}

type CodexOAuthAuthorizationFlow struct {
	State        string
	Verifier     string
	Challenge    string
	AuthorizeURL string
}

func RefreshCodexOAuthToken(ctx context.Context, refreshToken string) (*CodexOAuthTokenResult, error) {
	return RefreshCodexOAuthTokenWithProxy(ctx, refreshToken, "")
}

func RefreshCodexOAuthTokenWithProxy(ctx context.Context, refreshToken string, proxyURL string) (*CodexOAuthTokenResult, error) {
	client, err := getCodexOAuthHTTPClient(proxyURL)
	if err != nil {
		return nil, err
	}
	return refreshCodexOAuthToken(ctx, client, codexOAuthTokenURL, codexOAuthClientID, refreshToken)
}

func ExchangeCodexAuthorizationCode(ctx context.Context, code string, verifier string) (*CodexOAuthTokenResult, error) {
	return ExchangeCodexAuthorizationCodeWithProxy(ctx, code, verifier, "")
}

func ExchangeCodexAuthorizationCodeWithProxy(ctx context.Context, code string, verifier string, proxyURL string) (*CodexOAuthTokenResult, error) {
	client, err := getCodexOAuthHTTPClient(proxyURL)
	if err != nil {
		return nil, err
	}
	return exchangeCodexAuthorizationCode(ctx, client, codexOAuthTokenURL, codexOAuthClientID, code, verifier, codexOAuthRedirectURI)
}

func CreateCodexOAuthAuthorizationFlow() (*CodexOAuthAuthorizationFlow, error) {
	state, err := createStateHex(16)
	if err != nil {
		return nil, err
	}
	verifier, challenge, err := generatePKCEPair()
	if err != nil {
		return nil, err
	}
	u, err := buildCodexAuthorizeURL(state, challenge)
	if err != nil {
		return nil, err
	}
	return &CodexOAuthAuthorizationFlow{
		State:        state,
		Verifier:     verifier,
		Challenge:    challenge,
		AuthorizeURL: u,
	}, nil
}

func refreshCodexOAuthToken(
	ctx context.Context,
	client *http.Client,
	tokenURL string,
	clientID string,
	refreshToken string,
) (*CodexOAuthTokenResult, error) {
	rt := strings.TrimSpace(refreshToken)
	if rt == "" {
		return nil, errors.New("empty refresh_token")
	}

	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", rt)
	form.Set("client_id", clientID)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var payload struct {
		AccessToken      string `json:"access_token"`
		RefreshToken     string `json:"refresh_token"`
		IDToken          string `json:"id_token"`
		TokenType        string `json:"token_type"`
		Scope            string `json:"scope"`
		ExpiresIn        int    `json:"expires_in"`
		Error            any    `json:"error"`
		ErrorDescription string `json:"error_description"`
		Message          string `json:"message"`
	}

	if err := common.DecodeJson(resp.Body, &payload); err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errorPayload, _ := payload.Error.(map[string]any)
		return nil, &upstreamOAuthTokenRefreshError{
			Provider: "Codex", Operation: "refresh", StatusCode: resp.StatusCode,
			Code:        firstUpstreamErrorString(payload.Error, errorPayload["code"], errorPayload["type"]),
			Description: firstUpstreamErrorString(payload.ErrorDescription, payload.Message, errorPayload["message"]),
		}
	}

	if strings.TrimSpace(payload.AccessToken) == "" || payload.ExpiresIn <= 0 {
		return nil, errors.New("codex oauth refresh response missing fields")
	}

	return &CodexOAuthTokenResult{
		AccessToken:  strings.TrimSpace(payload.AccessToken),
		RefreshToken: strings.TrimSpace(payload.RefreshToken),
		IDToken:      strings.TrimSpace(payload.IDToken),
		TokenType:    strings.TrimSpace(payload.TokenType),
		Scope:        strings.TrimSpace(payload.Scope),
		ExpiresAt:    time.Now().Add(time.Duration(payload.ExpiresIn) * time.Second),
	}, nil
}

func exchangeCodexAuthorizationCode(
	ctx context.Context,
	client *http.Client,
	tokenURL string,
	clientID string,
	code string,
	verifier string,
	redirectURI string,
) (*CodexOAuthTokenResult, error) {
	c := strings.TrimSpace(code)
	v := strings.TrimSpace(verifier)
	if c == "" {
		return nil, errors.New("empty authorization code")
	}
	if v == "" {
		return nil, errors.New("empty code_verifier")
	}

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", clientID)
	form.Set("code", c)
	form.Set("code_verifier", v)
	form.Set("redirect_uri", redirectURI)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var payload struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		IDToken      string `json:"id_token"`
		TokenType    string `json:"token_type"`
		Scope        string `json:"scope"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := common.DecodeJson(resp.Body, &payload); err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("codex oauth code exchange failed: status=%d", resp.StatusCode)
	}
	if strings.TrimSpace(payload.AccessToken) == "" || strings.TrimSpace(payload.RefreshToken) == "" || payload.ExpiresIn <= 0 {
		return nil, errors.New("codex oauth token response missing fields")
	}
	return &CodexOAuthTokenResult{
		AccessToken:  strings.TrimSpace(payload.AccessToken),
		RefreshToken: strings.TrimSpace(payload.RefreshToken),
		IDToken:      strings.TrimSpace(payload.IDToken),
		TokenType:    strings.TrimSpace(payload.TokenType),
		Scope:        strings.TrimSpace(payload.Scope),
		ExpiresAt:    time.Now().Add(time.Duration(payload.ExpiresIn) * time.Second),
	}, nil
}

func getCodexOAuthHTTPClient(proxyURL string) (*http.Client, error) {
	baseClient, err := GetHttpClientWithProxy(strings.TrimSpace(proxyURL))
	if err != nil {
		return nil, err
	}
	if baseClient == nil {
		return &http.Client{Timeout: defaultHTTPTimeout}, nil
	}
	clientCopy := *baseClient
	clientCopy.Timeout = defaultHTTPTimeout
	return &clientCopy, nil
}

func buildCodexAuthorizeURL(state string, challenge string) (string, error) {
	u, err := url.Parse(codexOAuthAuthorizeURL)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("response_type", "code")
	q.Set("client_id", codexOAuthClientID)
	q.Set("redirect_uri", codexOAuthRedirectURI)
	q.Set("scope", codexOAuthScope)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	q.Set("state", state)
	q.Set("id_token_add_organizations", "true")
	q.Set("codex_cli_simplified_flow", "true")
	q.Set("originator", "codex_cli_rs")
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func createStateHex(nBytes int) (string, error) {
	if nBytes <= 0 {
		return "", errors.New("invalid state bytes length")
	}
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", b), nil
}

func generatePKCEPair() (verifier string, challenge string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	verifier = base64.RawURLEncoding.EncodeToString(b)
	sum := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(sum[:])
	return verifier, challenge, nil
}

func ExtractCodexAccountIDFromJWT(token string) (string, bool) {
	identity, ok := ExtractCodexOAuthIdentityFromJWT(token)
	return identity.AccountID, ok && identity.AccountID != ""
}

func ExtractEmailFromJWT(token string) (string, bool) {
	identity, ok := ExtractCodexOAuthIdentityFromJWT(token)
	return identity.Email, ok && identity.Email != ""
}

func extractCodexOAuthIdentityFromCredentials(credentials map[string]any) (CodexOAuthIdentity, bool) {
	identity := CodexOAuthIdentity{}
	found := false
	for _, tokenKey := range []string{"id_token", "access_token"} {
		candidate, ok := ExtractCodexOAuthIdentityFromJWT(upstreamCredentialMapString(credentials, tokenKey))
		if !ok {
			continue
		}
		found = true
		if identity.AccountID == "" {
			identity.AccountID = candidate.AccountID
		}
		if identity.Email == "" {
			identity.Email = candidate.Email
		}
		if identity.PlanType == "" {
			identity.PlanType = candidate.PlanType
		}
		if identity.SubscriptionExpiresAt == "" {
			identity.SubscriptionExpiresAt = candidate.SubscriptionExpiresAt
		}
	}
	return identity, found
}

func ExtractCodexOAuthIdentityFromJWT(token string) (CodexOAuthIdentity, bool) {
	claims, ok := decodeJWTClaims(token)
	if !ok {
		return CodexOAuthIdentity{}, false
	}
	identity := CodexOAuthIdentity{Email: jwtClaimString(claims, "email")}
	if profile, _ := claims["https://api.openai.com/profile"].(map[string]any); identity.Email == "" {
		identity.Email = jwtClaimString(profile, "email")
	}
	if auth, _ := claims[codexJWTClaimPath].(map[string]any); auth != nil {
		identity.AccountID = jwtClaimString(auth, "chatgpt_account_id")
		identity.PlanType = jwtClaimString(auth, "chatgpt_plan_type", "plan_type")
		for _, key := range []string{"chatgpt_subscription_active_until", "chatgpt_subscription_expires_at", "subscription_expires_at"} {
			candidate := jwtClaimString(auth, key)
			if validRFC3339(candidate) {
				identity.SubscriptionExpiresAt = candidate
				break
			}
		}
	}
	return identity, true
}

func jwtClaimString(claims map[string]any, keys ...string) string {
	for _, key := range keys {
		value, _ := claims[key].(string)
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func upstreamOAuthCredentialExpiry(credentials map[string]any) (int64, bool) {
	for _, key := range []string{"expired", "expires_at"} {
		if value, ok := oauthExpiryValue(credentials[key]); ok {
			return value, true
		}
	}
	claims, ok := decodeJWTClaims(upstreamCredentialMapString(credentials, "access_token"))
	if !ok {
		return 0, false
	}
	return oauthExpiryValue(claims["exp"])
}

func oauthExpiryValue(value any) (int64, bool) {
	switch typed := value.(type) {
	case int64:
		return typed, typed > 0
	case int:
		return int64(typed), typed > 0
	case float64:
		return int64(typed), typed > 0
	case string:
		raw := strings.TrimSpace(typed)
		if raw == "" {
			return 0, false
		}
		if parsed, err := strconv.ParseInt(raw, 10, 64); err == nil {
			return parsed, parsed > 0
		}
		if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
			return parsed.Unix(), true
		}
	}
	return 0, false
}

func decodeJWTClaims(token string) (map[string]any, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, false
	}
	payloadRaw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, false
	}
	var claims map[string]any
	if err := common.Unmarshal(payloadRaw, &claims); err != nil {
		return nil, false
	}
	return claims, true
}
