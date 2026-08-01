package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

const (
	upstreamOAuthSessionRecordKind = "oauth_session"
	upstreamOAuthSessionTTL        = 10 * time.Minute
)

type upstreamOAuthTokenRefreshError struct {
	Provider    string
	Operation   string
	StatusCode  int
	Code        string
	Description string
}

func (err *upstreamOAuthTokenRefreshError) Error() string {
	if err == nil {
		return "OAuth token refresh failed"
	}
	operation := strings.TrimSpace(err.Operation)
	if operation == "" {
		operation = "refresh"
	}
	code := strings.TrimSpace(err.Code)
	if code != "" {
		return fmt.Sprintf("%s OAuth %s failed: status=%d code=%s", err.Provider, operation, err.StatusCode, code)
	}
	return fmt.Sprintf("%s OAuth %s failed: status=%d", err.Provider, operation, err.StatusCode)
}

type UpstreamOAuthStartInput struct {
	AccountId      *int
	ProxyId        *int
	ProxyIdPresent bool
	Platform       string
	CredentialType string
}

type UpstreamOAuthStartResult struct {
	AuthorizeURL string `json:"authorize_url"`
	ExpiresAt    int64  `json:"expires_at"`
}

type UpstreamOAuthCompleteInput struct {
	State   string
	Code    string
	Name    string
	PoolIds []int
	ProxyId *int
}

func StartUpstreamCodexOAuth(input UpstreamOAuthStartInput) (*UpstreamOAuthStartResult, error) {
	input.Platform = constant.UpstreamPlatformOpenAI
	input.CredentialType = constant.UpstreamAccountTypeOAuth
	return StartUpstreamAccountOAuth(input)
}

func StartUpstreamAccountOAuth(input UpstreamOAuthStartInput) (*UpstreamOAuthStartResult, error) {
	platform := strings.ToLower(strings.TrimSpace(input.Platform))
	accountType := strings.ToLower(strings.TrimSpace(input.CredentialType))
	if platform == "" {
		platform = constant.UpstreamPlatformOpenAI
	}
	if accountType == "" {
		accountType = constant.UpstreamAccountTypeOAuth
	}
	if input.AccountId != nil {
		var account model.UpstreamAccount
		if err := model.DB.First(&account, *input.AccountId).Error; err != nil {
			return nil, err
		}
		if !isRefreshableUpstreamOAuthAccount(account.Platform, account.Type) {
			return nil, errors.New("account does not support OAuth authorization")
		}
		platform = account.Platform
		accountType = account.Type
		if !input.ProxyIdPresent {
			input.ProxyId = account.ProxyId
		}
	}
	if input.ProxyId != nil {
		var proxy model.UpstreamProxy
		if err := model.DB.First(&proxy, *input.ProxyId).Error; err != nil {
			return nil, fmt.Errorf("OAuth proxy is invalid: %w", err)
		}
	}
	var state, verifier, authorizeURL, scope string
	switch platform {
	case constant.UpstreamPlatformOpenAI:
		if accountType != constant.UpstreamAccountTypeOAuth {
			return nil, errors.New("OpenAI authorization requires an OAuth account")
		}
		flow, err := CreateCodexOAuthAuthorizationFlow()
		if err != nil {
			return nil, err
		}
		state, verifier, authorizeURL = flow.State, flow.Verifier, flow.AuthorizeURL
	case constant.UpstreamPlatformAnthropic:
		if accountType != constant.UpstreamAccountTypeOAuth && accountType != constant.UpstreamAccountTypeSetupToken {
			return nil, errors.New("Anthropic authorization requires OAuth or Setup Token")
		}
		flow, err := CreateClaudeOAuthAuthorizationFlow(accountType == constant.UpstreamAccountTypeSetupToken)
		if err != nil {
			return nil, err
		}
		state, verifier, authorizeURL, scope = flow.State, flow.Verifier, flow.AuthorizeURL, flow.Scope
	default:
		return nil, errors.New("unsupported OAuth platform")
	}
	keyring, err := LoadUpstreamCredentialKeyringFromEnv()
	if err != nil {
		return nil, err
	}
	now := common.GetTimestamp()
	session := model.UpstreamOAuthSession{
		StateHash: hashUpstreamOAuthState(state), AccountId: input.AccountId, ProxyId: input.ProxyId,
		VerifierVersion: 1, VerifierKeyVersion: keyring.ActiveVersion(), CreatedAt: now,
		ExpiresAt: now + int64(upstreamOAuthSessionTTL.Seconds()),
	}
	if err := model.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&session).Error; err != nil {
			return err
		}
		envelope, err := keyring.EncryptJSON(upstreamOAuthSessionRecordKind, session.Id, session.VerifierVersion, map[string]string{
			"verifier": verifier, "platform": platform, "credential_type": accountType, "scope": scope,
		})
		if err != nil {
			return err
		}
		session.VerifierCiphertext = envelope.Ciphertext
		session.VerifierNonce = envelope.Nonce
		session.VerifierKeyVersion = envelope.KeyVersion
		return tx.Model(&model.UpstreamOAuthSession{}).Where("id = ?", session.Id).Updates(map[string]any{
			"verifier_ciphertext":  envelope.Ciphertext,
			"verifier_nonce":       envelope.Nonce,
			"verifier_key_version": envelope.KeyVersion,
		}).Error
	}); err != nil {
		return nil, err
	}
	return &UpstreamOAuthStartResult{AuthorizeURL: authorizeURL, ExpiresAt: session.ExpiresAt}, nil
}

func CompleteUpstreamCodexOAuth(ctx context.Context, input UpstreamOAuthCompleteInput) (*UpstreamAccountView, error) {
	return CompleteUpstreamAccountOAuth(ctx, input)
}

func CompleteUpstreamAccountOAuth(ctx context.Context, input UpstreamOAuthCompleteInput) (*UpstreamAccountView, error) {
	state := strings.TrimSpace(input.State)
	code := strings.TrimSpace(input.Code)
	if state == "" || code == "" {
		return nil, errors.New("OAuth state and authorization code are required")
	}
	now := common.GetTimestamp()
	var session model.UpstreamOAuthSession
	if err := model.DB.Where("state_hash = ? AND used_at = 0 AND expires_at > ?", hashUpstreamOAuthState(state), now).First(&session).Error; err != nil {
		return nil, errors.New("OAuth session is missing, expired, or already used")
	}
	claim := model.DB.Model(&model.UpstreamOAuthSession{}).Where("id = ? AND used_at = 0", session.Id).Update("used_at", now)
	if claim.Error != nil {
		return nil, claim.Error
	}
	if claim.RowsAffected != 1 {
		return nil, errors.New("OAuth session is already being completed")
	}
	releaseClaim := true
	defer func() {
		if releaseClaim {
			_ = model.DB.Model(&model.UpstreamOAuthSession{}).Where("id = ? AND used_at = ?", session.Id, now).Update("used_at", 0).Error
		}
	}()

	keyring, err := LoadUpstreamCredentialKeyringFromEnv()
	if err != nil {
		return nil, err
	}
	var verifierPayload map[string]string
	if err := keyring.DecryptJSON(UpstreamCredentialEnvelope{
		Ciphertext: session.VerifierCiphertext, Nonce: session.VerifierNonce, KeyVersion: session.VerifierKeyVersion,
	}, upstreamOAuthSessionRecordKind, session.Id, session.VerifierVersion, &verifierPayload); err != nil {
		return nil, err
	}
	verifier := strings.TrimSpace(verifierPayload["verifier"])
	if verifier == "" {
		return nil, errors.New("OAuth session verifier is invalid")
	}
	proxyURL, err := resolveOAuthProxyURL(session.ProxyId)
	if err != nil {
		return nil, err
	}
	platform := strings.ToLower(strings.TrimSpace(verifierPayload["platform"]))
	accountType := strings.ToLower(strings.TrimSpace(verifierPayload["credential_type"]))
	if platform == "" {
		platform = constant.UpstreamPlatformOpenAI
	}
	if accountType == "" {
		accountType = constant.UpstreamAccountTypeOAuth
	}
	credentials := map[string]any{}
	email := ""
	accountExternalId := ""
	expiresAt := int64(0)
	switch platform {
	case constant.UpstreamPlatformOpenAI:
		tokenResult, exchangeErr := ExchangeCodexAuthorizationCodeWithProxy(ctx, code, verifier, proxyURL)
		if exchangeErr != nil {
			return nil, exchangeErr
		}
		accountExternalId, _ = ExtractCodexAccountIDFromJWT(tokenResult.AccessToken)
		if accountExternalId == "" {
			return nil, errors.New("failed to extract account_id from OAuth access token")
		}
		email, _ = ExtractEmailFromJWT(tokenResult.AccessToken)
		credentials = map[string]any{
			"access_token": tokenResult.AccessToken, "refresh_token": tokenResult.RefreshToken,
			"account_id": accountExternalId, "email": email, "type": "codex",
			"last_refresh": time.Now().Format(time.RFC3339), "expired": tokenResult.ExpiresAt.Format(time.RFC3339),
		}
		expiresAt = tokenResult.ExpiresAt.Unix()
	case constant.UpstreamPlatformAnthropic:
		tokenResult, exchangeErr := ExchangeClaudeAuthorizationCodeWithProxy(ctx, code, verifier, state, proxyURL)
		if exchangeErr != nil {
			return nil, exchangeErr
		}
		email = tokenResult.Email
		accountExternalId = tokenResult.AccountID
		credentials = map[string]any{
			"access_token": tokenResult.AccessToken, "refresh_token": tokenResult.RefreshToken,
			"token_type": tokenResult.TokenType, "scope": tokenResult.Scope,
			"org_uuid": tokenResult.Organization, "account_id": tokenResult.AccountID, "email": tokenResult.Email,
			"last_refresh": time.Now().Format(time.RFC3339), "expired": tokenResult.ExpiresAt.Format(time.RFC3339),
		}
		expiresAt = tokenResult.ExpiresAt.Unix()
	default:
		return nil, errors.New("unsupported OAuth platform")
	}
	var accountId int
	if session.AccountId != nil {
		if err := replaceUpstreamOAuthCredential(*session.AccountId, credentials, expiresAt, session.ProxyId); err != nil {
			return nil, err
		}
		accountId = *session.AccountId
	} else {
		name := strings.TrimSpace(input.Name)
		if name == "" {
			name = strings.TrimSpace(email)
		}
		if name == "" {
			name = accountExternalId
		}
		accountInput := UpstreamAccountCreateInput{
			Account: model.UpstreamAccount{
				Name: name, Platform: platform, Type: accountType,
				Extra: "{}", ProxyId: input.ProxyId, Concurrency: 1, Priority: 50, Weight: 1,
				Status: constant.UpstreamStatusActive, Schedulable: true, ExpiresAt: &expiresAt, AutoPauseOnExpired: true,
				OAuthRefreshOwner: constant.UpstreamOAuthRefreshOwnerStarNexus,
			},
			Credentials: credentials, PoolIds: input.PoolIds,
		}
		if accountInput.Account.ProxyId == nil {
			accountInput.Account.ProxyId = session.ProxyId
		}
		if err := CreateUpstreamAccount(&accountInput); err != nil {
			return nil, err
		}
		accountId = accountInput.Account.Id
	}
	releaseClaim = false
	return GetUpstreamAccount(accountId)
}

func RefreshUpstreamOAuthAccount(ctx context.Context, accountId int) (*UpstreamAccountView, error) {
	var result *UpstreamAccountView
	err := withUpstreamOAuthRefreshLock(ctx, accountId, 60*time.Second, func(lockCtx context.Context) error {
		var refreshErr error
		result, refreshErr = refreshUpstreamOAuthAccountUnlocked(lockCtx, accountId)
		return refreshErr
	})
	if shouldRecordUpstreamOAuthRefreshFailure(accountId, err) {
		recordUpstreamOAuthRefreshFailure(accountId, err)
	}
	return result, err
}

func refreshUpstreamOAuthAccountUnlocked(ctx context.Context, accountId int) (*UpstreamAccountView, error) {
	if accountId <= 0 {
		return nil, errors.New("invalid upstream account id")
	}
	var account model.UpstreamAccount
	if err := model.DB.First(&account, accountId).Error; err != nil {
		return nil, err
	}
	if !isRefreshableUpstreamOAuthAccount(account.Platform, account.Type) {
		return nil, errUpstreamOAuthRefreshUnsupported
	}
	if account.OAuthRefreshOwner != constant.UpstreamOAuthRefreshOwnerStarNexus {
		return nil, errUpstreamOAuthRefreshExternallyManaged
	}
	credentials, err := DecryptUpstreamAccountCredentials(&account)
	if err != nil {
		return nil, err
	}
	refreshToken := upstreamCredentialMapString(credentials, "refresh_token")
	if refreshToken == "" {
		return nil, errors.New("OAuth account has no refresh_token")
	}
	proxyURL, err := resolveOAuthProxyURL(account.ProxyId)
	if err != nil {
		return nil, err
	}
	var expiresAt int64
	switch account.Platform {
	case constant.UpstreamPlatformOpenAI:
		result, refreshErr := RefreshCodexOAuthTokenWithProxy(ctx, refreshToken, proxyURL)
		if refreshErr != nil {
			return nil, refreshErr
		}
		credentials["access_token"] = result.AccessToken
		credentials["refresh_token"] = result.RefreshToken
		credentials["last_refresh"] = time.Now().Format(time.RFC3339)
		credentials["expired"] = result.ExpiresAt.Format(time.RFC3339)
		if upstreamCredentialMapString(credentials, "account_id") == "" {
			if value, ok := ExtractCodexAccountIDFromJWT(result.AccessToken); ok {
				credentials["account_id"] = value
			}
		}
		if upstreamCredentialMapString(credentials, "email") == "" {
			if value, ok := ExtractEmailFromJWT(result.AccessToken); ok {
				credentials["email"] = value
			}
		}
		expiresAt = result.ExpiresAt.Unix()
	case constant.UpstreamPlatformAnthropic:
		result, refreshErr := RefreshClaudeOAuthTokenWithProxy(ctx, refreshToken, proxyURL)
		if refreshErr != nil {
			return nil, refreshErr
		}
		credentials["access_token"] = result.AccessToken
		if result.RefreshToken != "" {
			credentials["refresh_token"] = result.RefreshToken
		}
		credentials["token_type"] = result.TokenType
		credentials["scope"] = result.Scope
		credentials["last_refresh"] = time.Now().Format(time.RFC3339)
		credentials["expired"] = result.ExpiresAt.Format(time.RFC3339)
		expiresAt = result.ExpiresAt.Unix()
	default:
		return nil, errUpstreamOAuthRefreshUnsupported
	}
	if err := replaceUpstreamOAuthCredential(account.Id, credentials, expiresAt, account.ProxyId); err != nil {
		return nil, err
	}
	recordUpstreamAccountEvent(account.Id, 0, "oauth_refresh", "success", "credential refreshed", nil)
	return GetUpstreamAccount(account.Id)
}

func replaceUpstreamOAuthCredential(accountId int, credentials map[string]any, expiresAt int64, proxyId *int) error {
	keyring, err := LoadUpstreamCredentialKeyringFromEnv()
	if err != nil {
		return err
	}
	return model.DB.Transaction(func(tx *gorm.DB) error {
		var current model.UpstreamAccount
		if err := tx.First(&current, accountId).Error; err != nil {
			return err
		}
		if !isRefreshableUpstreamOAuthAccount(current.Platform, current.Type) {
			return errors.New("account is not an OAuth account")
		}
		if err := validateUpstreamCredentialPayload(current.Platform, current.Type, credentials); err != nil {
			return err
		}
		newVersion := current.CredentialVersion + 1
		envelope, err := keyring.EncryptJSON(upstreamAccountCredentialRecordKind, current.Id, newVersion, credentials)
		if err != nil {
			return err
		}
		result := tx.Model(&model.UpstreamAccount{}).Where("id = ? AND credential_version = ?", current.Id, current.CredentialVersion).Updates(map[string]any{
			"credential_ciphertext": envelope.Ciphertext, "credential_nonce": envelope.Nonce,
			"credential_key_version": envelope.KeyVersion, "credential_version": newVersion,
			"expires_at": expiresAt, "proxy_id": proxyId, "status": constant.UpstreamStatusActive,
			"schedulable": true, "error_message": "", "temp_unschedulable_until": nil,
			"temp_unschedulable_reason": "", "updated_at": common.GetTimestamp(),
			"oauth_refresh_owner": constant.UpstreamOAuthRefreshOwnerStarNexus,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("OAuth account credential was updated concurrently")
		}
		return nil
	})
}

func isRefreshableUpstreamOAuthAccount(platform string, accountType string) bool {
	platform = strings.ToLower(strings.TrimSpace(platform))
	accountType = strings.ToLower(strings.TrimSpace(accountType))
	return (platform == constant.UpstreamPlatformOpenAI && accountType == constant.UpstreamAccountTypeOAuth) ||
		(platform == constant.UpstreamPlatformAnthropic && (accountType == constant.UpstreamAccountTypeOAuth || accountType == constant.UpstreamAccountTypeSetupToken))
}

func shouldRecordUpstreamOAuthRefreshFailure(accountId int, err error) bool {
	return accountId > 0 && err != nil &&
		!errors.Is(err, errUpstreamOAuthRefreshLocked) &&
		!errors.Is(err, errUpstreamOAuthRefreshUnsupported) &&
		!errors.Is(err, errUpstreamOAuthRefreshExternallyManaged) &&
		!errors.Is(err, gorm.ErrRecordNotFound)
}

func isNonRetryableUpstreamOAuthRefreshError(err error) bool {
	if err == nil {
		return false
	}
	var tokenErr *upstreamOAuthTokenRefreshError
	if errors.As(err, &tokenErr) {
		code := strings.ToLower(strings.TrimSpace(tokenErr.Code))
		switch code {
		case "invalid_grant", "invalid_refresh_token", "token_expired", "app_session_terminated",
			"refresh_token_reused", "refresh_token_invalidated", "invalid_client",
			"unauthorized_client", "access_denied":
			return true
		}
		return tokenErr.StatusCode == 400 || tokenErr.StatusCode == 401 || tokenErr.StatusCode == 403
	}
	message := strings.ToLower(err.Error())
	for _, fragment := range []string{
		"no refresh_token", "no refresh token", "empty refresh_token",
		"invalid_grant", "invalid_refresh_token", "token_expired",
		"refresh token expired", "refresh token is expired", "refresh token revoked",
		"refresh token reused", "refresh token invalid", "refresh_token_reused",
		"refresh_token_invalidated", "invalid_client", "unauthorized_client", "access_denied",
	} {
		if strings.Contains(message, fragment) {
			return true
		}
	}
	return false
}

func parseUpstreamOAuthTokenErrorBody(body []byte) (string, string) {
	if len(body) == 0 {
		return "", ""
	}
	var payload map[string]any
	if err := common.Unmarshal(body, &payload); err != nil {
		return "", ""
	}
	errorPayload, _ := payload["error"].(map[string]any)
	code := firstUpstreamErrorString(payload["error"], errorPayload["code"], errorPayload["type"], payload["code"])
	description := firstUpstreamErrorString(
		payload["error_description"], errorPayload["message"], payload["detail"], payload["message"],
	)
	return code, description
}

func resolveOAuthProxyURL(proxyId *int) (string, error) {
	if proxyId == nil {
		return "", nil
	}
	dummy := &model.UpstreamAccount{ProxyId: proxyId}
	_, proxyURL, err := resolveUpstreamProxy(context.Background(), dummy, nil, "")
	return proxyURL, err
}

func hashUpstreamOAuthState(state string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(state)))
	return hex.EncodeToString(sum[:])
}

func upstreamCredentialMapString(values map[string]any, key string) string {
	value, ok := values[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%v", value))
}
