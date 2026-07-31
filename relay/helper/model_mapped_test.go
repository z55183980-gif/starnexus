package helper

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestModelMappedHelperUsesSelectedAccountMapping(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(nil)
	context.Set(string(constant.ContextKeyChannelModelMapping), `{"gpt-client":"gpt-channel"}`)
	context.Set(string(constant.ContextKeyUpstreamAccountMappedModel), "gpt-account")
	request := &dto.GeneralOpenAIRequest{Model: "gpt-client"}
	info := &relaycommon.RelayInfo{
		OriginModelName: "gpt-client",
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "gpt-client"},
	}

	require.NoError(t, ModelMappedHelper(context, info, request))
	require.True(t, info.IsModelMapped)
	require.Equal(t, "gpt-account", info.UpstreamModelName)
	require.Equal(t, "gpt-account", request.Model)
}

func TestModelMappedHelperPreservesCompactBillingModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(nil)
	context.Set(string(constant.ContextKeyUpstreamAccountMappedModel), "gpt-5.4-compact")
	request := &dto.GeneralOpenAIRequest{Model: "gpt-5.6-sol-openai-compact"}
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponsesCompact,
		OriginModelName: "gpt-5.6-sol-openai-compact",
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5.6-sol"},
	}

	require.NoError(t, ModelMappedHelper(context, info, request))
	require.True(t, info.IsModelMapped)
	require.Equal(t, "gpt-5.6-sol-openai-compact", info.OriginModelName)
	require.Equal(t, "gpt-5.4-compact", info.UpstreamModelName)
	require.Equal(t, "gpt-5.4-compact", request.Model)
}
