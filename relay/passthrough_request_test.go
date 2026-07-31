package relay

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestPreparePassthroughRequestBodyAppliesAccountMappingAndChannelPolicy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{
		"model":"public-model",
		"temperature":0.8,
		"service_tier":"priority",
		"safety_identifier":"user-1",
		"store":true
	}`))
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	common.SetContextKey(c, constant.ContextKeyUpstreamAccountMappedModel, "upstream-model")

	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{
		ChannelSetting: dto.ChannelSettings{AccountPassThroughBodyEnabled: true},
		ChannelOtherSettings: dto.ChannelOtherSettings{
			DisableStore: true,
		},
		ParamOverride: map[string]interface{}{"temperature": 0.2},
	}}

	body, closer, apiErr := preparePassthroughRequestBody(c, info)
	require.Nil(t, apiErr)
	if closer != nil {
		defer closer.Close()
	}
	data, err := io.ReadAll(body)
	require.NoError(t, err)
	require.Equal(t, "upstream-model", gjson.GetBytes(data, "model").String())
	require.Equal(t, 0.2, gjson.GetBytes(data, "temperature").Float())
	require.False(t, gjson.GetBytes(data, "service_tier").Exists())
	require.False(t, gjson.GetBytes(data, "safety_identifier").Exists())
	require.False(t, gjson.GetBytes(data, "store").Exists())
}

func TestPreparePassthroughRequestBodyKeepsExplicitChannelBypass(t *testing.T) {
	gin.SetMode(gin.TestMode)
	raw := `{"model":"public-model","service_tier":"priority"}`
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(raw))
	t.Cleanup(func() { common.CleanupBodyStorage(c) })

	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{
		ChannelSetting: dto.ChannelSettings{PassThroughBodyEnabled: true},
	}}
	body, closer, apiErr := preparePassthroughRequestBody(c, info)
	require.Nil(t, apiErr)
	if closer != nil {
		defer closer.Close()
	}
	data, err := io.ReadAll(body)
	require.NoError(t, err)
	require.JSONEq(t, raw, string(data))
}
