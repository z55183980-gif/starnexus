package service

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStableCodexReleaseVersion(t *testing.T) {
	require.Equal(t, "0.146.0", stableCodexReleaseVersion(codexGitHubRelease{TagName: "rust-v0.146.0"}))
	require.Empty(t, stableCodexReleaseVersion(codexGitHubRelease{TagName: "rust-v0.147.0-alpha.1", Prerelease: true}))
	require.Empty(t, stableCodexReleaseVersion(codexGitHubRelease{TagName: "v0.147.0", Draft: true}))
}

func TestApplyCodexOutboundIdentityUsesFallbackTuple(t *testing.T) {
	header := make(http.Header)
	header.Set("User-Agent", "spoof")
	header.Set("originator", "spoof")
	header.Set("version", "0.1.0")

	ApplyCodexOutboundIdentity(header)

	require.Equal(t, "codex-tui/0.146.0 (Ubuntu 22.4.0; x86_64) xterm-256color", header.Get("User-Agent"))
	require.Equal(t, "codex-tui", header.Get("originator"))
	require.Equal(t, "0.146.0", header.Get("version"))
}

func TestNormalizeCodexClientVersion(t *testing.T) {
	require.Equal(t, "0.146.0", NormalizeCodexClientVersion(" 0.146.0 "))
	require.Empty(t, NormalizeCodexClientVersion("0.146.0\r\nspoof"))
	require.Less(t, compareCodexVersions("0.145.9", "0.146.0"), 0)
	require.Greater(t, compareCodexVersions("0.147.0", "0.146.0"), 0)
}
