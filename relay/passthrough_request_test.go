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
	"github.com/QuantumNous/new-api/relay/channel/codex"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
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
	raw := `{"model":"public-model","service_tier":"priority","input":[{"type":"reasoning","id":"item_explicit_bypass"},{"type":"message","id":"item_explicit_message","role":"assistant","content":"preserve id"},{"role":"system","content":"preserve exactly"}]}`
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

func TestPrepareAccountPassthroughRepairsCodexInvalidLocalItems(t *testing.T) {
	gin.SetMode(gin.TestMode)
	raw := `{
		"model":"gpt-5.6-sol",
		"stream":false,
		"custom_zero":0,
		"custom_false":false,
		"input":[
			{"type":"reasoning","id":"item_local_reasoning"},
			{"type":"item_reference","id":"item_local_reference"},
			{"type":"reasoning","id":"rs_valid","encrypted_content":"valid"},
			{"type":"message","id":"item_invalid_message","role":"assistant","content":"keep message"},
			{"type":"message","role":"user","content":{"id":"item_nested"}},
			{"type":"function_call","id":"fc_1","call_id":"call_1","name":"lookup","arguments":"{}"},
			{"type":"function_call_output","call_id":"call_1","output":"ok"}
		]
	}`
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(raw))
	t.Cleanup(func() { common.CleanupBodyStorage(c) })

	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeResponses,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:    constant.ChannelTypeCodex,
			ChannelSetting: dto.ChannelSettings{AccountPassThroughBodyEnabled: true},
		},
	}
	body, closer, apiErr := preparePassthroughRequestBody(c, info)
	require.Nil(t, apiErr)
	if closer != nil {
		defer closer.Close()
	}
	data, err := io.ReadAll(body)
	require.NoError(t, err)
	require.True(t, gjson.GetBytes(data, "stream").Bool())
	require.Equal(t, int64(4), gjson.GetBytes(data, "input.#").Int())
	require.False(t, gjson.GetBytes(data, `input.#(id=="item_local_reasoning")`).Exists())
	require.False(t, gjson.GetBytes(data, `input.#(id=="item_local_reference")`).Exists())
	require.False(t, gjson.GetBytes(data, `input.#(id=="rs_valid")`).Exists())
	require.False(t, gjson.GetBytes(data, "input.0.id").Exists())
	require.Equal(t, "keep message", gjson.GetBytes(data, "input.0.content").String())
	require.Equal(t, "item_nested", gjson.GetBytes(data, "input.1.content.id").String())
	require.True(t, gjson.GetBytes(data, "custom_zero").Exists())
	require.Zero(t, gjson.GetBytes(data, "custom_zero").Int())
	require.True(t, gjson.GetBytes(data, "custom_false").Exists())
	require.False(t, gjson.GetBytes(data, "custom_false").Bool())
	repairInfo, exists := c.Get("codex_input_repair_admin_info")
	require.True(t, exists)
	require.Equal(t, 1, repairInfo.(map[string]interface{})["dropped_reasoning_items"])
	require.Equal(t, 1, repairInfo.(map[string]interface{})["dropped_item_references"])
	require.Equal(t, 1, repairInfo.(map[string]interface{})["invalid_message_ids_removed"])
	require.Equal(t, false, repairInfo.(map[string]interface{})["upstream_validation_retry"])
}

func TestPrepareAccountPassthroughRejectsCodexRepairWithOrphanToolOutput(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{
		"model":"gpt-5.6-sol",
		"input":[
			{"type":"message","role":"user","content":"continue"},
			{"type":"item_reference","id":"item_local_call"},
			{"type":"function_call_output","call_id":"call_1","output":"ok"}
		]
	}`))
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeResponses,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:    constant.ChannelTypeCodex,
			ChannelSetting: dto.ChannelSettings{AccountPassThroughBodyEnabled: true},
		},
	}

	_, _, apiErr := preparePassthroughRequestBody(c, info)
	require.NotNil(t, apiErr)
	require.Equal(t, http.StatusConflict, apiErr.StatusCode)
}

func TestPreparePassthroughRequestBodyForcesCodexResponsesStream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5","stream":false,"store":true,"input":[{"role":"system","content":"preserve exactly"}]}`))
	t.Cleanup(func() { common.CleanupBodyStorage(c) })

	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeResponses,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:    constant.ChannelTypeCodex,
			ChannelSetting: dto.ChannelSettings{PassThroughBodyEnabled: true},
		},
	}
	body, closer, apiErr := preparePassthroughRequestBody(c, info)
	require.Nil(t, apiErr)
	if closer != nil {
		defer closer.Close()
	}
	data, err := io.ReadAll(body)
	require.NoError(t, err)
	require.True(t, gjson.GetBytes(data, "stream").Bool())
	require.False(t, gjson.GetBytes(data, "store").Bool())
	require.Equal(t, "system", gjson.GetBytes(data, "input.0.role").String())
	require.Equal(t, "preserve exactly", gjson.GetBytes(data, "input.0.content").String())
}

func TestPreparePassthroughRequestBodyNormalizesResponsesLite(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{
		"model":"gpt-5.6-sol",
		"input":"hello",
		"instructions":"Use the repository tools.",
		"reasoning":{"effort":"high","context":"current_turn"},
		"store":true,
		"tools":[{"type":"namespace","name":"collaboration","tools":[]}]
	}`))
	c.Request.Header.Set(codex.ResponsesLiteHeader, "true")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	common.SetContextKey(c, constant.ContextKeyUpstreamAccountPlatform, constant.UpstreamPlatformOpenAI)
	common.SetContextKey(c, constant.ContextKeyUpstreamAccountType, constant.UpstreamAccountTypeOAuth)

	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeResponses,
		ChannelMeta: &relaycommon.ChannelMeta{
			ApiType:        constant.APITypeCodex,
			ChannelType:    constant.ChannelTypeCodex,
			ChannelSetting: dto.ChannelSettings{PassThroughBodyEnabled: true},
		},
	}
	body, closer, apiErr := preparePassthroughRequestBody(c, info)
	require.Nil(t, apiErr)
	if closer != nil {
		defer closer.Close()
	}
	data, err := io.ReadAll(body)
	require.NoError(t, err)
	require.Equal(t, "all_turns", gjson.GetBytes(data, "reasoning.context").String())
	require.True(t, gjson.GetBytes(data, "stream").Bool())
	require.Equal(t, "reasoning.encrypted_content", gjson.GetBytes(data, "include.0").String())
	require.False(t, gjson.GetBytes(data, "store").Bool())
	require.Equal(t, "", gjson.GetBytes(data, "instructions").String())
	require.False(t, gjson.GetBytes(data, "tools").Exists())
	require.Equal(t, "collaboration", gjson.GetBytes(data, `input.#(type=="additional_tools").tools.0.name`).String())
	require.Equal(t, "developer", gjson.GetBytes(data, "input.1.role").String())
	require.Equal(t, "Use the repository tools.", gjson.GetBytes(data, "input.1.content.0.text").String())
}
