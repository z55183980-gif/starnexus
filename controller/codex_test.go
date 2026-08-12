package controller

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeCodexSettingValue(t *testing.T) {
	value, err := normalizeCodexSettingValue("codex_setting.client_version", " 0.146.0 ")
	require.NoError(t, err)
	require.Equal(t, "0.146.0", value)

	_, err = normalizeCodexSettingValue("codex_setting.client_version", "not-a-version")
	require.Error(t, err)

	value, err = normalizeCodexSettingValue("codex_setting.version_auto_sync_enabled", true)
	require.NoError(t, err)
	require.Equal(t, "true", value)

	_, err = normalizeCodexSettingValue("codex_setting.fingerprint_default_mode", "invalid")
	require.Error(t, err)
}
