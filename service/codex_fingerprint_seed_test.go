package service

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestCodexFingerprintSeedLifecycle(t *testing.T) {
	created := &model.UpstreamAccount{
		Platform: constant.UpstreamPlatformOpenAI,
		Type:     constant.UpstreamAccountTypeOAuth,
		Extra:    `{"codex_fingerprint_mode":"full","codex_fingerprint_seed":"99999999-9999-4999-8999-999999999999"}`,
	}
	require.NoError(t, PrepareCodexFingerprintExtraForCreate(created))
	seed, ok := CodexFingerprintSeed(created.Extra)
	require.True(t, ok)
	require.NotEqual(t, "99999999-9999-4999-8999-999999999999", seed)

	old := created.Extra
	updated := &model.UpstreamAccount{
		Platform: constant.UpstreamPlatformOpenAI,
		Type:     constant.UpstreamAccountTypeOAuth,
		Extra:    `{"codex_fingerprint_mode":"session","codex_fingerprint_seed":"88888888-8888-4888-8888-888888888888"}`,
	}
	require.NoError(t, PrepareCodexFingerprintExtraForUpdate(created, updated))
	updatedSeed, ok := CodexFingerprintSeed(updated.Extra)
	require.True(t, ok)
	require.Equal(t, seed, updatedSeed)
	require.NotContains(t, RedactCodexFingerprintSeed(updated.Extra), CodexFingerprintSeedExtraKey)
	require.NotEqual(t, old, updated.Extra)
}

func TestCodexFingerprintSeedOnlyMintsForExplicitMode(t *testing.T) {
	account := &model.UpstreamAccount{
		Platform: constant.UpstreamPlatformOpenAI,
		Type:     constant.UpstreamAccountTypeOAuth,
		Extra:    `{"codex_fingerprint_mode":"off"}`,
	}
	require.NoError(t, PrepareCodexFingerprintExtraForCreate(account))
	_, ok := CodexFingerprintSeed(account.Extra)
	require.False(t, ok)
}
