package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
)

const (
	claudeOAuthClientID      = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
	claudeOAuthAuthorizeURL  = "https://claude.ai/oauth/authorize"
	claudeOAuthTokenURL      = "https://platform.claude.com/v1/oauth/token"
	claudeOAuthRedirectURI   = "https://platform.claude.com/oauth/code/callback"
	claudeOAuthScope         = "org:create_api_key user:profile user:inference user:sessions:claude_code user:mcp_servers user:file_upload"
	claudeSetupTokenScope    = "user:inference"
	claudeOAuthResponseLimit = 64 * 1024
)

type ClaudeOAuthAuthorizationFlow struct {
	State        string
	Verifier     string
	Challenge    string
	Scope        string
	AuthorizeURL string
}

type ClaudeOAuthTokenResult struct {
	AccessToken  string
	RefreshToken string
	TokenType    string
	Scope        string
	Organization string
	AccountID    string
	Email        string
	ExpiresAt    time.Time
}

func CreateClaudeOAuthAuthorizationFlow(setupToken bool) (*ClaudeOAuthAuthorizationFlow, error) {
	state, err := createStateHex(32)
	if err != nil {
		return nil, err
	}
	verifier, challenge, err := generatePKCEPair()
	if err != nil {
		return nil, err
	}
	scope := claudeOAuthScope
	if setupToken {
		scope = claudeSetupTokenScope
	}
	parsed, err := url.Parse(claudeOAuthAuthorizeURL)
	if err != nil {
		return nil, err
	}
	query := parsed.Query()
	query.Set("code", "true")
	query.Set("client_id", claudeOAuthClientID)
	query.Set("response_type", "code")
	query.Set("redirect_uri", claudeOAuthRedirectURI)
	query.Set("scope", scope)
	query.Set("code_challenge", challenge)
	query.Set("code_challenge_method", "S256")
	query.Set("state", state)
	parsed.RawQuery = query.Encode()
	return &ClaudeOAuthAuthorizationFlow{
		State: state, Verifier: verifier, Challenge: challenge, Scope: scope, AuthorizeURL: parsed.String(),
	}, nil
}

func ExchangeClaudeAuthorizationCodeWithProxy(ctx context.Context, code string, verifier string, state string, proxyURL string) (*ClaudeOAuthTokenResult, error) {
	payload := map[string]any{
		"code": strings.TrimSpace(code), "grant_type": "authorization_code",
		"client_id": claudeOAuthClientID, "redirect_uri": claudeOAuthRedirectURI,
		"code_verifier": strings.TrimSpace(verifier),
	}
	if strings.TrimSpace(state) != "" {
		payload["state"] = strings.TrimSpace(state)
	}
	return requestClaudeOAuthToken(ctx, payload, proxyURL, "exchange")
}

func RefreshClaudeOAuthTokenWithProxy(ctx context.Context, refreshToken string, proxyURL string) (*ClaudeOAuthTokenResult, error) {
	refreshToken = strings.TrimSpace(refreshToken)
	if refreshToken == "" {
		return nil, errors.New("empty refresh_token")
	}
	return requestClaudeOAuthToken(ctx, map[string]any{
		"grant_type": "refresh_token", "refresh_token": refreshToken, "client_id": claudeOAuthClientID,
	}, proxyURL, "refresh")
}

func requestClaudeOAuthToken(ctx context.Context, payload map[string]any, proxyURL string, operation string) (*ClaudeOAuthTokenResult, error) {
	body, err := common.Marshal(payload)
	if err != nil {
		return nil, err
	}
	client, err := getCodexOAuthHTTPClient(proxyURL)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, claudeOAuthTokenURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "axios/1.13.6")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, claudeOAuthResponseLimit))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("Claude OAuth %s failed: status=%d", operation, resp.StatusCode)
	}
	var response struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		Scope        string `json:"scope"`
		ExpiresIn    int64  `json:"expires_in"`
		Organization *struct {
			UUID string `json:"uuid"`
		} `json:"organization"`
		Account *struct {
			UUID         string `json:"uuid"`
			EmailAddress string `json:"email_address"`
		} `json:"account"`
	}
	if err := common.Unmarshal(responseBody, &response); err != nil {
		return nil, err
	}
	if strings.TrimSpace(response.AccessToken) == "" || response.ExpiresIn <= 0 {
		return nil, errors.New("Claude OAuth token response missing fields")
	}
	result := &ClaudeOAuthTokenResult{
		AccessToken: strings.TrimSpace(response.AccessToken), RefreshToken: strings.TrimSpace(response.RefreshToken),
		TokenType: strings.TrimSpace(response.TokenType), Scope: strings.TrimSpace(response.Scope),
		ExpiresAt: time.Now().Add(time.Duration(response.ExpiresIn) * time.Second),
	}
	if response.Organization != nil {
		result.Organization = strings.TrimSpace(response.Organization.UUID)
	}
	if response.Account != nil {
		result.AccountID = strings.TrimSpace(response.Account.UUID)
		result.Email = strings.TrimSpace(response.Account.EmailAddress)
	}
	return result, nil
}
