package service

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"

	"github.com/stretchr/testify/require"
)

func TestChannelSupportsRequestPathAlphaSearch(t *testing.T) {
	require.True(t, ChannelSupportsRequestPath(&model.Channel{Type: constant.ChannelTypeCodex}, "/v1/alpha/search"))
	require.False(t, ChannelSupportsRequestPath(&model.Channel{Type: constant.ChannelTypeOpenAI}, "/v1/alpha/search"))
	require.True(t, ChannelSupportsRequestPath(&model.Channel{Type: constant.ChannelTypeOpenAI}, "/v1/responses"))
	require.False(t, ChannelSupportsRequestPath(nil, "/v1/alpha/search"))
	require.NotNil(t, ChannelFilterForRequestPath("/v1/alpha/search"))
	require.Nil(t, ChannelFilterForRequestPath("/v1/responses"))
}
