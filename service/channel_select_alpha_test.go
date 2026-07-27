package service

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"

	"github.com/stretchr/testify/require"
)

func TestChannelSupportsRequestPathAlphaSearch(t *testing.T) {
	const modelName = "gpt-5.6-sol"
	require.True(t, ChannelSupportsRequestPath(&model.Channel{Type: constant.ChannelTypeCodex}, "/v1/alpha/search", modelName))
	require.True(t, ChannelSupportsRequestPath(&model.Channel{Type: constant.ChannelTypeSub2API}, "/v1/alpha/search", modelName))
	require.True(t, ChannelSupportsRequestPath(&model.Channel{Type: constant.ChannelTypeNewAPI}, "/v1/alpha/search", modelName))
	require.False(t, ChannelSupportsRequestPath(&model.Channel{Type: constant.ChannelTypeOpenAI}, "/v1/alpha/search", modelName))
	openAI := &model.Channel{Type: constant.ChannelTypeOpenAI}
	openAI.SetOtherSettings(dto.ChannelOtherSettings{AlphaSearchEnabled: true})
	require.True(t, ChannelSupportsRequestPath(openAI, "/v1/alpha/search", modelName))
	require.True(t, ChannelSupportsRequestPath(&model.Channel{Type: constant.ChannelTypeOpenAI}, "/v1/responses", modelName))
	require.False(t, ChannelSupportsRequestPath(nil, "/v1/alpha/search", modelName))
	require.NotNil(t, ChannelFilterForRequestPath("/v1/alpha/search", modelName))

	advanced := &model.Channel{Type: constant.ChannelTypeAdvancedCustom}
	advanced.SetOtherSettings(dto.ChannelOtherSettings{AdvancedCustom: &dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{{
		IncomingPath: "/v1/alpha/search",
		UpstreamPath: "/v1/alpha/search",
		Models:       []string{modelName},
	}}}})
	require.True(t, ChannelSupportsRequestPath(advanced, "/v1/alpha/search", modelName))
	require.False(t, ChannelSupportsRequestPath(advanced, "/v1/alpha/search", "gpt-other"))
	require.False(t, ChannelSupportsRequestPath(advanced, "/v1/responses", modelName))
}
