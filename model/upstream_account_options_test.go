package model

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/require"
)

func TestValidateUpstreamAccountOptions(t *testing.T) {
	account := &UpstreamAccount{
		Platform: constant.UpstreamPlatformOpenAI,
		Type:     constant.UpstreamAccountTypeAPIKey,
		Extra: `{
			"openai_apikey_responses_websockets_v2_mode":"ctx_pool",
			"openai_compact_mode":"force_on",
			"compact_model_mapping":{"gpt-5.*":"gpt-5.4"},
			"openai_capabilities":["chat_completions"]
		}`,
	}
	require.NoError(t, ValidateUpstreamAccountOptions(account))

	options, err := ParseUpstreamAccountOptions(account.Extra)
	require.NoError(t, err)
	require.Equal(t, UpstreamOpenAIWSModeContextPool, options.OpenAIWSMode(account.Type))
	require.True(t, options.AllowsOpenAICompact())
	require.True(t, options.SupportsOpenAIEndpoint("/v1/responses"))
	require.False(t, options.SupportsOpenAIEndpoint("/v1/embeddings"))
}

func TestValidateUpstreamAccountOptionsAllowsHTTPBridge(t *testing.T) {
	account := &UpstreamAccount{
		Platform: constant.UpstreamPlatformOpenAI,
		Type:     constant.UpstreamAccountTypeOAuth,
		Extra:    `{"openai_oauth_responses_websockets_v2_mode":"http_bridge"}`,
	}
	require.NoError(t, ValidateUpstreamAccountOptions(account))
	options, err := ParseUpstreamAccountOptions(account.Extra)
	require.NoError(t, err)
	require.Equal(t, UpstreamOpenAIWSModeHTTPBridge, options.OpenAIWSMode(account.Type))
}

func TestOpenAIResponsesSupportProbeControlsAutoMode(t *testing.T) {
	unsupported := false
	options := UpstreamAccountOptions{OpenAIResponsesSupported: &unsupported}
	require.Equal(t, UpstreamOpenAIResponsesModeForceChatCompletions, options.EffectiveOpenAIResponsesMode())

	options.OpenAIResponsesMode = UpstreamOpenAIResponsesModeForceResponses
	require.Equal(t, UpstreamOpenAIResponsesModeForceResponses, options.EffectiveOpenAIResponsesMode())
}

func TestParseUpstreamAccountOptionsWithCredentialsPrefersSub2Locations(t *testing.T) {
	options, err := ParseUpstreamAccountOptionsWithCredentials(
		`{"intercept_warmup_requests":false,"compact_model_mapping":{"legacy":"legacy"},"openai_capabilities":["embeddings"]}`,
		map[string]any{
			"intercept_warmup_requests": true,
			"compact_model_mapping":     map[string]any{"gpt-5.*": "gpt-5.4"},
			"openai_capabilities":       []any{"chat_completions"},
		},
	)
	require.NoError(t, err)
	require.True(t, options.InterceptWarmupRequests)
	require.Equal(t, map[string]string{"gpt-5.*": "gpt-5.4"}, options.CompactModelMapping)
	require.Equal(t, []string{"chat_completions"}, options.OpenAIEndpointCapabilities)
}

func TestParseUpstreamAccountOptionsWithCredentialsRejectsInvalidValues(t *testing.T) {
	_, err := ParseUpstreamAccountOptionsWithCredentials("{}", map[string]any{
		"compact_model_mapping": map[string]any{"gpt-5.*": 42},
	})
	require.Error(t, err)
}

func TestParseUpstreamAccountOptionsWithCredentialsTreatsNullAsUnset(t *testing.T) {
	options, err := ParseUpstreamAccountOptionsWithCredentials(
		`{"intercept_warmup_requests":true,"compact_model_mapping":{"legacy":"legacy"},"openai_capabilities":["embeddings"]}`,
		map[string]any{
			"intercept_warmup_requests": nil,
			"compact_model_mapping":     nil,
			"openai_capabilities":       nil,
		},
	)
	require.NoError(t, err)
	require.False(t, options.InterceptWarmupRequests)
	require.Nil(t, options.CompactModelMapping)
	require.Nil(t, options.OpenAIEndpointCapabilities)
}

func TestValidateUpstreamAccountOptionsRejectsInvalidCombinations(t *testing.T) {
	tests := []struct {
		name  string
		type_ string
		extra string
	}{
		{name: "codex app server without restriction", type_: constant.UpstreamAccountTypeOAuth, extra: `{"codex_cli_only_allow_app_server":true}`},
		{name: "codex restriction on API key", type_: constant.UpstreamAccountTypeAPIKey, extra: `{"codex_cli_only":true}`},
		{name: "invalid websocket mode", type_: constant.UpstreamAccountTypeOAuth, extra: `{"openai_oauth_responses_websockets_v2_mode":"shared"}`},
		{name: "invalid compact mapping", type_: constant.UpstreamAccountTypeOAuth, extra: `{"compact_model_mapping":{"":"gpt-5.4"}}`},
		{name: "API key websocket mode on OAuth", type_: constant.UpstreamAccountTypeOAuth, extra: `{"openai_apikey_responses_websockets_v2_mode":"off"}`},
		{name: "API key responses support on OAuth", type_: constant.UpstreamAccountTypeOAuth, extra: `{"openai_responses_supported":false}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			account := &UpstreamAccount{Platform: constant.UpstreamPlatformOpenAI, Type: test.type_, Extra: test.extra}
			require.Error(t, ValidateUpstreamAccountOptions(account))
		})
	}
}
