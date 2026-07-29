package helper

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
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
