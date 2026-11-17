package relay

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/codex"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestResponsesLiteHistoryReachesUpstream(t *testing.T) {
	const input = `[{"type":"additional_tools","role":"developer","tools":[{"type":"namespace","name":"functions","tools":[{"type":"function","name":"first"}]}]},{"type":"message","role":"user","content":"Continue"},{"type":"additional_tools","role":"developer","tools":[{"type":"namespace","name":"functions","tools":[{"type":"function","name":"second"}]}]}]`
	const raw = `{"model":"gpt-6-sol","input":` + input + `,"client_metadata":{"ws_request_header_x_openai_internal_codex_responses_lite":"true"}}`
	for _, path := range []string{"http", "passthrough", "websocket", "http_bridge"} {
		for _, status := range []int{http.StatusOK, http.StatusBadRequest} {
			t.Run(path+"/"+http.StatusText(status), func(t *testing.T) {
				seen := make(chan []byte, 1)
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					body, _ := io.ReadAll(r.Body)
					seen <- body
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(status)
					_, _ = io.WriteString(w, `{"result":"upstream-sentinel"}`)
				}))
				defer upstream.Close()
				mode := model.UpstreamOpenAIWSModeContextPool
				if path == "http_bridge" {
					mode = model.UpstreamOpenAIWSModeHTTPBridge
				}
				c := newResponsesLiteRelayContext(t, mode)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(raw))
				c.Request.Header.Set(codex.ResponsesLiteHeader, "true")
				common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, upstream.URL)
				t.Cleanup(func() { common.CleanupBodyStorage(c) })
				info := &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeResponses, OriginModelName: "gpt-6-sol"}
				info.InitChannelMeta(c)
				var prepared []byte
				var err error
				switch path {
				case "http":
					prepared, _, err = normalizeSelectedResponsesLitePayload(c, info, []byte(raw), false)
					require.NoError(t, err)
				case "passthrough":
					info.ChannelSetting.PassThroughBodyEnabled = true
					body, closer, apiErr := preparePassthroughRequestBody(c, info)
					require.Nil(t, apiErr)
					if closer != nil {
						defer closer.Close()
					}
					prepared, err = io.ReadAll(body)
					require.NoError(t, err)
				default:
					var request dto.OpenAIResponsesRequest
					require.NoError(t, common.Unmarshal([]byte(raw), &request))
					var apiErr error
					preparedBody, _, prepareErr := PrepareResponsesWebSocketRequest(c, info, &request, path == "http_bridge")
					if prepareErr != nil {
						apiErr = prepareErr
					}
					require.NoError(t, apiErr)
					prepared = preparedBody
				}
				response, err := channel.DoApiRequestWithContext(&codex.Adaptor{}, c, info, bytes.NewReader(prepared))
				require.NoError(t, err)
				defer response.Body.Close()
				require.Equal(t, status, response.StatusCode)
				responseBody, err := io.ReadAll(response.Body)
				require.NoError(t, err)
				require.Equal(t, `{"result":"upstream-sentinel"}`, string(responseBody))
				require.JSONEq(t, input, gjson.GetBytes(<-seen, "input").Raw)
				require.Empty(t, seen, "one outbound request per turn, including upstream rejection")
			})
		}
	}
}
