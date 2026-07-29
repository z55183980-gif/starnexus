package claude

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestApplyClaudeCodeHeadersAddsOAuthProtocolHeaders(t *testing.T) {
	header := http.Header{}
	header.Set("anthropic-beta", "existing-beta")
	applyClaudeCodeHeaders(&header)

	require.Equal(t, "claude-cli/2.1.220 (external, cli)", header.Get("User-Agent"))
	require.Equal(t, "cli", header.Get("X-App"))
	require.Contains(t, strings.Split(header.Get("anthropic-beta"), ","), "existing-beta")
	require.Contains(t, strings.Split(header.Get("anthropic-beta"), ","), "oauth-2025-04-20")
	require.Contains(t, strings.Split(header.Get("anthropic-beta"), ","), "claude-code-20250219")
}
