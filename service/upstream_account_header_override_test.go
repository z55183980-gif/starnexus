package service

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestResolveUpstreamAccountHeaderOverridesFiltersUnsafeHeaders(t *testing.T) {
	account := &model.UpstreamAccount{Platform: constant.UpstreamPlatformOpenAI, Type: constant.UpstreamAccountTypeAPIKey}
	credentials := map[string]any{
		"header_override_enabled": true,
		"header_overrides": map[string]any{
			"X-Relay-Key":   " relay-secret ",
			"Authorization": "Bearer forbidden",
		},
	}
	overrides := ResolveUpstreamAccountHeaderOverrides(account, credentials)
	require.Equal(t, "relay-secret", overrides["x-relay-key"])
	require.NotContains(t, overrides, "authorization")
	require.Error(t, ValidateUpstreamAccountHeaderOverrides(account, credentials))
}

func TestMergeUpstreamAccountHeaderOverridesAccountWins(t *testing.T) {
	account := &model.UpstreamAccount{Platform: constant.UpstreamPlatformAnthropic, Type: constant.UpstreamAccountTypeAPIKey}
	credentials := map[string]any{
		"header_override_enabled": true,
		"header_overrides":        map[string]any{"x-relay-key": "account"},
	}
	merged := MergeUpstreamAccountHeaderOverrides(map[string]any{
		"X-Relay-Key": "channel",
		"x-channel":   "kept",
	}, account, credentials)
	require.Equal(t, "account", merged["x-relay-key"])
	require.Equal(t, "kept", merged["x-channel"])
	require.NotContains(t, merged, "X-Relay-Key")
}

func TestUpstreamAccountHeaderOverridesIgnoreOAuthAccounts(t *testing.T) {
	account := &model.UpstreamAccount{Platform: constant.UpstreamPlatformOpenAI, Type: constant.UpstreamAccountTypeOAuth}
	credentials := map[string]any{
		"header_override_enabled": true,
		"header_overrides":        map[string]any{"x-relay-key": "secret"},
	}
	require.Nil(t, ResolveUpstreamAccountHeaderOverrides(account, credentials))
	require.Error(t, ValidateUpstreamAccountHeaderOverrides(account, credentials))
}
