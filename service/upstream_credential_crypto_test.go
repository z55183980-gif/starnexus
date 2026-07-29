package service

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestUpstreamCredentialKeyringRoundTripAndAAD(t *testing.T) {
	key := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	raw, err := common.Marshal(map[string]string{"1": key})
	require.NoError(t, err)
	keyring, err := ParseUpstreamCredentialKeyring(string(raw), "1")
	require.NoError(t, err)

	credential := map[string]any{"access_token": "secret", "account_id": "acct"}
	envelope, err := keyring.EncryptJSON("account", 7, 3, credential)
	require.NoError(t, err)
	require.NotContains(t, envelope.Ciphertext, "secret")

	var decoded map[string]any
	require.NoError(t, keyring.DecryptJSON(*envelope, "account", 7, 3, &decoded))
	require.Equal(t, "secret", decoded["access_token"])
	require.Error(t, keyring.DecryptJSON(*envelope, "account", 8, 3, &decoded))
	require.Error(t, keyring.DecryptJSON(*envelope, "account", 7, 4, &decoded))
}

func TestUpstreamCredentialKeyringRejectsTamperingAndBadConfig(t *testing.T) {
	key := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	keyring, err := ParseUpstreamCredentialKeyring(`{"1":"`+key+`"}`, "1")
	require.NoError(t, err)

	envelope, err := keyring.Encrypt("proxy", 4, 1, []byte(`{"username":"u","password":"p"}`))
	require.NoError(t, err)
	envelope.Ciphertext = strings.Repeat("A", len(envelope.Ciphertext))
	_, err = keyring.Decrypt(*envelope, "proxy", 4, 1)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "password")

	_, err = ParseUpstreamCredentialKeyring("", "1")
	require.Error(t, err)
	_, err = ParseUpstreamCredentialKeyring(`{"1":"bad"}`, "1")
	require.Error(t, err)
	_, err = ParseUpstreamCredentialKeyring(`{"1":"`+key+`"}`, "2")
	require.Error(t, err)
}

func TestUpstreamCredentialKeyringUsesPlaintextWithoutConfiguration(t *testing.T) {
	t.Setenv(upstreamCredentialKeysEnv, "")
	t.Setenv(upstreamCredentialActiveVersionEnv, "")

	keyring, err := LoadUpstreamCredentialKeyringFromEnv()
	require.NoError(t, err)
	require.Zero(t, keyring.ActiveVersion())

	credential := map[string]any{"api_key": "plain-secret"}
	envelope, err := keyring.EncryptJSON("account", 7, 1, credential)
	require.NoError(t, err)
	require.Zero(t, envelope.KeyVersion)
	require.Empty(t, envelope.Nonce)
	require.Contains(t, envelope.Ciphertext, "plain-secret")

	var decoded map[string]any
	require.NoError(t, keyring.DecryptJSON(*envelope, "account", 7, 1, &decoded))
	require.Equal(t, "plain-secret", decoded["api_key"])
}
