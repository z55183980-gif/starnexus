package claude

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestClaudeResponsesProxy(t *testing.T) {
	a := &Adaptor{}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: "https://gateway.example/", ApiKey: "test-key"}, RelayMode: relayconstant.RelayModeResponses}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	_, err := a.GetRequestURL(info)
	require.Error(t, err, "disabled even when request-body passthrough skips conversion")
	_, err = a.ConvertOpenAIResponsesRequest(c, info, dto.OpenAIResponsesRequest{})
	require.Error(t, err)
	info.ChannelOtherSettings.ClaudeResponsesProxyEnabled = true
	url, err := a.GetRequestURL(info)
	require.NoError(t, err)
	require.Equal(t, "https://gateway.example/v1/responses", url)
	h := http.Header{}
	require.NoError(t, a.SetupRequestHeader(c, &h, info))
	require.Equal(t, "Bearer test-key", h.Get("Authorization"))
	require.Empty(t, h.Get("x-api-key"))
	var req dto.OpenAIResponsesRequest
	require.NoError(t, common.UnmarshalJsonStr(`{"model":"claude-opus-5-5","input":[{"type":"custom_tool_call_output","call_id":"call_1","output":"fixture result"}],"tools":[{"type":"custom","name":"apply_patch","description":"Client supplied grammar","format":{"type":"grammar","syntax":"lark","definition":"start: /.+/"}}],"parallel_tool_calls":false,"store":false,"reasoning":{"effort":"high"}}`, &req))
	converted, err := a.ConvertOpenAIResponsesRequest(c, info, req)
	require.NoError(t, err)
	before, err := common.Marshal(req)
	require.NoError(t, err)
	after, err := common.Marshal(converted)
	require.NoError(t, err)
	require.JSONEq(t, string(before), string(after))
	require.Contains(t, string(after), `"parallel_tool_calls":false`)
	require.Contains(t, string(after), `"custom_tool_call_output"`)
	require.Equal(t, "high", info.ReasoningEffort)
	info.RelayMode = relayconstant.RelayModeResponsesCompact
	_, err = a.GetRequestURL(info)
	require.Error(t, err)
	info.RelayMode = relayconstant.RelayModeChatCompletions
	url, err = a.GetRequestURL(info)
	require.NoError(t, err)
	require.True(t, strings.HasSuffix(url, "/v1/messages"))
	require.NoError(t, a.SetupRequestHeader(c, &h, info))
	require.Equal(t, "test-key", h.Get("x-api-key"))
	require.Empty(t, h.Get("Authorization"))
}

func TestClaudeResponsesProxyResponse(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "json", true: "sse"}[stream], func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
			info := relaycommon.GenRelayInfoOpenAI(c, nil)
			info.ChannelMeta = &relaycommon.ChannelMeta{}
			info.ChannelOtherSettings.ClaudeResponsesProxyEnabled = true
			info.RelayMode = relayconstant.RelayModeResponses
			info.RelayFormat = types.RelayFormatOpenAIResponses
			info.IsStream = stream
			info.DisablePing = true
			body := `{"id":"resp_fixture","object":"response","status":"completed","model":"claude-opus-5-5","output":[],"usage":{"input_tokens":2,"output_tokens":3,"total_tokens":5}}`
			if stream {
				body = "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":" + body + "}\n\n"
			}
			usage, apiErr := (&Adaptor{}).DoResponse(c, &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, info)
			require.Nil(t, apiErr)
			require.Equal(t, 3, usage.(*dto.Usage).CompletionTokens)
			require.Equal(t, types.RelayFormat(types.RelayFormatOpenAIResponses), info.FinalRequestRelayFormat)
			require.Contains(t, w.Body.String(), "resp_fixture")
			if stream {
				require.Contains(t, w.Body.String(), "response.completed")
			}
		})
	}
}
