package service

import (
	"context"
	"encoding/base64"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestRotateUpstreamCredentialsAcrossRecordTypes(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	accountInput := UpstreamAccountCreateInput{
		Account: model.UpstreamAccount{
			Name: "rotate-account", Platform: constant.UpstreamPlatformOpenAI, Type: constant.UpstreamAccountTypeAPIKey,
			Extra: "{}", Concurrency: 1, Priority: 1, Weight: 1,
			Status: constant.UpstreamStatusActive, Schedulable: true, AutoPauseOnExpired: true,
		},
		Credentials: map[string]any{"api_key": "rotation-secret"},
	}
	require.NoError(t, CreateUpstreamAccount(&accountInput))
	proxyInput := UpstreamProxyCreateInput{
		Proxy: model.UpstreamProxy{
			Name: "rotate-proxy", Protocol: constant.UpstreamProxyProtocolHTTP,
			Host: "127.0.0.1", Port: 8080, Status: constant.UpstreamStatusActive,
			FallbackMode: constant.UpstreamProxyFallbackNone,
		},
		Auth: UpstreamProxyAuthInput{Username: "rotation-user", Password: "rotation-password"},
	}
	require.NoError(t, CreateUpstreamProxy(&proxyInput))

	keyring, err := LoadUpstreamCredentialKeyringFromEnv()
	require.NoError(t, err)
	session := model.UpstreamOAuthSession{
		StateHash: "rotation-state", VerifierVersion: 1, VerifierKeyVersion: 1,
		CreatedAt: 1, ExpiresAt: 9999999999,
	}
	require.NoError(t, model.DB.Create(&session).Error)
	envelope, err := keyring.EncryptJSON(upstreamOAuthSessionRecordKind, session.Id, 1, map[string]string{"verifier": "rotation-verifier"})
	require.NoError(t, err)
	require.NoError(t, model.DB.Model(&session).Updates(map[string]any{
		"verifier_ciphertext":  envelope.Ciphertext,
		"verifier_nonce":       envelope.Nonce,
		"verifier_key_version": envelope.KeyVersion,
	}).Error)

	keyOne := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	keyTwo := base64.StdEncoding.EncodeToString([]byte("abcdef0123456789abcdef0123456789"))
	rawKeyring, err := common.Marshal(map[string]string{"1": keyOne, "2": keyTwo})
	require.NoError(t, err)
	t.Setenv(upstreamCredentialKeysEnv, string(rawKeyring))
	t.Setenv(upstreamCredentialActiveVersionEnv, "2")

	plan, err := InspectUpstreamCredentialRotation(context.Background())
	require.NoError(t, err)
	require.EqualValues(t, 2, plan.RemainingRecords)
	report, err := RotateUpstreamCredentials(context.Background(), 1)
	require.NoError(t, err)
	require.EqualValues(t, 1, report.RotatedAccounts)
	require.Zero(t, report.RotatedProxies)
	require.EqualValues(t, 1, report.RotatedSessions)
	require.Zero(t, report.RemainingRecords)

	var account model.UpstreamAccount
	var proxy model.UpstreamProxy
	require.NoError(t, model.DB.First(&account, accountInput.Account.Id).Error)
	require.NoError(t, model.DB.First(&proxy, proxyInput.Proxy.Id).Error)
	require.NoError(t, model.DB.First(&session, session.Id).Error)
	require.Equal(t, 2, account.CredentialKeyVersion)
	require.Zero(t, proxy.AuthKeyVersion)
	require.Equal(t, 2, session.VerifierKeyVersion)
	credentials, err := DecryptUpstreamAccountCredentials(&account)
	require.NoError(t, err)
	require.Equal(t, "rotation-secret", credentials["api_key"])
	auth, err := DecryptUpstreamProxyAuth(&proxy)
	require.NoError(t, err)
	require.Equal(t, "rotation-password", auth.Password)
	verified, err := VerifyUpstreamCredentials(context.Background(), 1)
	require.NoError(t, err)
	require.Zero(t, verified.RemainingRecords)
}
