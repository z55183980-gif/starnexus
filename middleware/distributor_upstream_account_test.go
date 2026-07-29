package middleware

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/require"
)

func TestLocalUpstreamAllowedAccountTypes(t *testing.T) {
	require.ElementsMatch(t,
		[]string{constant.UpstreamAccountTypeOAuth, constant.UpstreamAccountTypeAPIKey},
		localUpstreamAllowedAccountTypes(constant.ChannelTypeOpenAI, "/v1/responses", false, false),
	)
	require.ElementsMatch(t,
		[]string{constant.UpstreamAccountTypeOAuth, constant.UpstreamAccountTypeAPIKey},
		localUpstreamAllowedAccountTypes(constant.ChannelTypeOpenAI, "/v1/responses/compact", false, false),
	)
	require.Equal(t,
		[]string{constant.UpstreamAccountTypeOAuth},
		localUpstreamAllowedAccountTypes(constant.ChannelTypeOpenAI, "/v1/alpha/search", false, false),
	)
	require.ElementsMatch(t,
		[]string{constant.UpstreamAccountTypeOAuth, constant.UpstreamAccountTypeAPIKey},
		localUpstreamAllowedAccountTypes(constant.ChannelTypeOpenAI, "/v1/alpha/search", false, true),
	)
	require.ElementsMatch(t,
		[]string{constant.UpstreamAccountTypeOAuth, constant.UpstreamAccountTypeAPIKey},
		localUpstreamAllowedAccountTypes(constant.ChannelTypeOpenAI, "/v1/chat/completions", true, false),
	)
	require.Equal(t,
		[]string{constant.UpstreamAccountTypeAPIKey},
		localUpstreamAllowedAccountTypes(constant.ChannelTypeOpenAI, "/v1/chat/completions", false, false),
	)
	require.Equal(t,
		[]string{constant.UpstreamAccountTypeAPIKey},
		localUpstreamAllowedAccountTypes(constant.ChannelTypeOpenAI, "/v1/embeddings", true, false),
	)
}

func TestLocalUpstreamAccountTypeSelectsEffectiveChannel(t *testing.T) {
	channelType, err := localUpstreamChannelType(constant.UpstreamPlatformOpenAI, constant.UpstreamAccountTypeOAuth)
	require.NoError(t, err)
	require.Equal(t, constant.ChannelTypeCodex, channelType)

	channelType, err = localUpstreamChannelType(constant.UpstreamPlatformOpenAI, constant.UpstreamAccountTypeAPIKey)
	require.NoError(t, err)
	require.Equal(t, constant.ChannelTypeOpenAI, channelType)

	channelType, err = localUpstreamChannelType(constant.UpstreamPlatformAnthropic, constant.UpstreamAccountTypeOAuth)
	require.NoError(t, err)
	require.Equal(t, constant.ChannelTypeAnthropic, channelType)

	channelType, err = localUpstreamChannelType(constant.UpstreamPlatformAnthropic, constant.UpstreamAccountTypeBedrock)
	require.NoError(t, err)
	require.Equal(t, constant.ChannelTypeAws, channelType)

	channelType, err = localUpstreamChannelType(constant.UpstreamPlatformAnthropic, constant.UpstreamAccountTypeServiceAccount)
	require.NoError(t, err)
	require.Equal(t, constant.ChannelTypeVertexAi, channelType)

	_, err = localUpstreamChannelType(constant.UpstreamPlatformOpenAI, "unsupported")
	require.Error(t, err)
}

func TestLocalUpstreamChannelKeyUsesEffectiveChannelType(t *testing.T) {
	apiKey, err := localUpstreamChannelKey(constant.UpstreamPlatformOpenAI, constant.UpstreamAccountTypeAPIKey, map[string]any{"api_key": "sk-test"})
	require.NoError(t, err)
	require.Equal(t, "sk-test", apiKey)

	oauthKey, err := localUpstreamChannelKey(constant.UpstreamPlatformOpenAI, constant.UpstreamAccountTypeOAuth, map[string]any{
		"access_token": "token", "account_id": "account",
	})
	require.NoError(t, err)
	require.JSONEq(t, `{"access_token":"token","account_id":"account"}`, oauthKey)

	claudeKey, err := localUpstreamChannelKey(constant.UpstreamPlatformAnthropic, constant.UpstreamAccountTypeSetupToken, map[string]any{
		"access_token": "claude-token",
	})
	require.NoError(t, err)
	require.Equal(t, "claude-token", claudeKey)

	bedrockKey, err := localUpstreamChannelKey(constant.UpstreamPlatformAnthropic, constant.UpstreamAccountTypeBedrock, map[string]any{
		"auth_mode": "sigv4", "aws_access_key_id": "ak", "aws_secret_access_key": "sk", "aws_session_token": "session", "aws_region": "us-east-1",
	})
	require.NoError(t, err)
	require.Equal(t, "ak|sk|session|us-east-1", bedrockKey)
}

func TestLocalUpstreamBaseURLUsesSelectedAccountType(t *testing.T) {
	require.Equal(t,
		constant.ChannelBaseURLs[constant.ChannelTypeCodex],
		localUpstreamBaseURL(constant.ChannelTypeCodex, map[string]any{"base_url": "https://ignored.example"}, "https://channel.example"),
	)
	require.Equal(t,
		"https://api.example.com",
		localUpstreamBaseURL(constant.ChannelTypeOpenAI, map[string]any{"base_url": "https://api.example.com"}, "https://channel.example"),
	)
	require.Equal(t,
		"https://channel.example",
		localUpstreamBaseURL(constant.ChannelTypeOpenAI, nil, "https://channel.example"),
	)
	require.Equal(t,
		constant.ChannelBaseURLs[constant.ChannelTypeOpenAI],
		localUpstreamBaseURL(constant.ChannelTypeOpenAI, nil, ""),
	)
}
