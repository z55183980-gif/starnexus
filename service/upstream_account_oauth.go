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

type UpstreamOAuthStartInput struct {
	AccountId      *int
	ProxyId        *int
	ProxyIdPresent bool
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
	if input.AccountId != nil {
		var account model.UpstreamAccount
		if err := model.DB.First(&account, *input.AccountId).Error; err != nil {
			return nil, err
		}
		if account.Platform != constant.UpstreamPlatformOpenAI || account.Type != constant.UpstreamAccountTypeOAuth {
			return nil, errors.New("account is not an OpenAI OAuth account")
		}
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
	flow, err := CreateCodexOAuthAuthorizationFlow()
	if err != nil {
		return nil, err
	}
	keyring, err := LoadUpstreamCredentialKeyringFromEnv()
	if err != nil {
		return nil, err
	}
	now := common.GetTimestamp()
	session := model.UpstreamOAuthSession{
		StateHash: hashUpstreamOAuthState(flow.State), AccountId: input.AccountId, ProxyId: input.ProxyId,
		VerifierVersion: 1, VerifierKeyVersion: keyring.ActiveVersion(), CreatedAt: now,
		ExpiresAt: now + int64(upstreamOAuthSessionTTL.Seconds()),
	}
	if err := model.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&session).Error; err != nil {
			return err
		}
		envelope, err := keyring.EncryptJSON(upstreamOAuthSessionRecordKind, session.Id, session.VerifierVersion, map[string]string{"verifier": flow.Verifier})
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
	return &UpstreamOAuthStartResult{AuthorizeURL: flow.AuthorizeURL, ExpiresAt: session.ExpiresAt}, nil
}

func CompleteUpstreamCodexOAuth(ctx context.Context, input UpstreamOAuthCompleteInput) (*UpstreamAccountView, error) {
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
	tokenResult, err := ExchangeCodexAuthorizationCodeWithProxy(ctx, code, verifier, proxyURL)
	if err != nil {
		return nil, err
	}
	accountExternalId, ok := ExtractCodexAccountIDFromJWT(tokenResult.AccessToken)
	if !ok {
		return nil, errors.New("failed to extract account_id from OAuth access token")
	}
	email, _ := ExtractEmailFromJWT(tokenResult.AccessToken)
	credentials := map[string]any{
		"access_token": tokenResult.AccessToken, "refresh_token": tokenResult.RefreshToken,
		"account_id": accountExternalId, "email": email, "type": "codex",
		"last_refresh": time.Now().Format(time.RFC3339), "expired": tokenResult.ExpiresAt.Format(time.RFC3339),
	}
	expiresAt := tokenResult.ExpiresAt.Unix()
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
				Name: name, Platform: constant.UpstreamPlatformOpenAI, Type: constant.UpstreamAccountTypeOAuth,
				Extra: "{}", ProxyId: input.ProxyId, Concurrency: 1, Priority: 50, Weight: 1,
				Status: constant.UpstreamStatusActive, Schedulable: true, ExpiresAt: &expiresAt, AutoPauseOnExpired: true,
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
	if account.Platform != constant.UpstreamPlatformOpenAI || account.Type != constant.UpstreamAccountTypeOAuth {
		return nil, errors.New("account is not an OpenAI OAuth account")
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
	result, err := RefreshCodexOAuthTokenWithProxy(ctx, refreshToken, proxyURL)
	if err != nil {
		return nil, err
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
	if err := replaceUpstreamOAuthCredential(account.Id, credentials, result.ExpiresAt.Unix(), account.ProxyId); err != nil {
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
		if current.Type != constant.UpstreamAccountTypeOAuth {
			return errors.New("account is not an OAuth account")
		}
		if err := validateUpstreamCredentialPayload(current.Type, credentials); err != nil {
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
