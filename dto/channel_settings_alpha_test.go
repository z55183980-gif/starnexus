package dto

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/require"
)

func TestChannelOtherSettingsSupportsAlphaSearch(t *testing.T) {
	const modelName = "gpt-5.6-sol"

	require.True(t, ChannelOtherSettings{}.SupportsAlphaSearch(constant.ChannelTypeCodex, modelName))
	require.True(t, ChannelOtherSettings{}.SupportsAlphaSearch(constant.ChannelTypeSub2API, modelName))
	require.True(t, ChannelOtherSettings{}.SupportsAlphaSearch(constant.ChannelTypeNewAPI, modelName))
	require.False(t, ChannelOtherSettings{}.SupportsAlphaSearch(constant.ChannelTypeOpenAI, modelName))
	require.True(t, ChannelOtherSettings{AlphaSearchEnabled: true}.SupportsAlphaSearch(constant.ChannelTypeOpenAI, modelName))
	require.False(t, ChannelOtherSettings{AlphaSearchEnabled: true}.SupportsAlphaSearch(constant.ChannelTypeAnthropic, modelName))
}

func TestAdvancedCustomConfigSupportsAlphaSearchByModel(t *testing.T) {
	config := &AdvancedCustomConfig{Routes: []AdvancedCustomRoute{
		{
			IncomingPath: "/v1/alpha/search",
			UpstreamPath: "/v1/alpha/search",
			Models:       []string{"gpt-5.6-sol"},
		},
		{
			IncomingPath: "/v1/alpha/search",
			UpstreamPath: "/v1/alpha/search",
			Models:       []string{"re:^gpt-5\\.6-"},
		},
	}}

	require.NoError(t, config.Validate())
	require.True(t, config.SupportsPathForModel("/v1/alpha/search", "gpt-5.6-sol"))
	require.True(t, config.SupportsPathForModel("/v1/alpha/search", "gpt-5.6-terra"))
	require.False(t, config.SupportsPathForModel("/v1/alpha/search", "gpt-5.5"))
	require.False(t, config.SupportsPathForModel("/v1/responses", "gpt-5.6-sol"))
}

func TestAdvancedCustomConfigRejectsUnsupportedConverter(t *testing.T) {
	config := &AdvancedCustomConfig{Routes: []AdvancedCustomRoute{{
		IncomingPath: "/v1/alpha/search",
		UpstreamPath: "/v1/alpha/search",
		Converter:    "openai_chat_to_responses",
	}}}
	require.Error(t, config.Validate())
}
