package controller

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay"
	relaychannel "github.com/QuantumNous/new-api/relay/channel"
	openairelay "github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"gorm.io/gorm"
)

var responsesWSTestEntityID atomic.Int64

func newResponsesWSTestPair(t *testing.T) (*websocket.Conn, *websocket.Conn, func()) {
	t.Helper()
	serverConnCh := make(chan *websocket.Conn, 1)
	serverErrCh := make(chan error, 1)
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			serverErrCh <- err
			return
		}
		serverConnCh <- conn
	}))

	clientConn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	require.NoError(t, err)

	var serverConn *websocket.Conn
	select {
	case serverConn = <-serverConnCh:
	case err = <-serverErrCh:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for WebSocket test connection")
	}

	cleanup := func() {
		_ = clientConn.Close()
		_ = serverConn.Close()
		server.Close()
	}
	return serverConn, clientConn, cleanup
}

func requireResponsesWSTurnFailureAndAlive(t *testing.T, session *responsesWebSocketSession, clientPeer *websocket.Conn, expectedResponseID string) {
	t.Helper()
	require.NoError(t, clientPeer.SetReadDeadline(time.Now().Add(5*time.Second)))
	messageType, data, err := clientPeer.ReadMessage()
	require.NoError(t, err)
	require.Equal(t, websocket.TextMessage, messageType)
	var failedEvent struct {
		Type     string `json:"type"`
		Response struct {
			ID     string `json:"id"`
			Status string `json:"status"`
			Error  struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		} `json:"response"`
	}
	require.NoError(t, common.Unmarshal(data, &failedEvent))
	require.Equal(t, "response.failed", failedEvent.Type)
	require.NotEmpty(t, failedEvent.Response.ID)
	if expectedResponseID != "" {
		require.Equal(t, expectedResponseID, failedEvent.Response.ID)
	}
	require.Equal(t, "failed", failedEvent.Response.Status)
	require.NotEmpty(t, failedEvent.Response.Error.Code)
	require.NotEmpty(t, failedEvent.Response.Error.Message)

	session.clientWriteMu.Lock()
	err = session.client.WriteMessage(websocket.TextMessage, []byte(`{"type":"session.alive"}`))
	session.clientWriteMu.Unlock()
	require.NoError(t, err)
	messageType, data, err = clientPeer.ReadMessage()
	require.NoError(t, err)
	require.Equal(t, websocket.TextMessage, messageType)
	require.JSONEq(t, `{"type":"session.alive"}`, string(data))
	select {
	case <-session.closed:
		t.Fatal("downstream client session closed after upstream turn failure")
	default:
	}
}

func TestParseResponsesWSClientEvent(t *testing.T) {
	t.Parallel()

	event, request, envelope, apiErr := parseResponsesWSClientEvent([]byte(`{
		"type":"response.create",
		"model":"gpt-5",
		"input":"hello",
		"previous_response_id":"resp_1",
		"reasoning":{"effort":"high","context":"all_turns"},
		"client_metadata":{"ws_request_header_x_openai_internal_codex_responses_lite":"true"},
		"generate":false
	}`))
	require.Nil(t, apiErr)
	require.Equal(t, "response.create", event.Type)
	require.Equal(t, "gpt-5", request.Model)
	require.Equal(t, "resp_1", request.PreviousResponseID)
	require.NotNil(t, request.Reasoning)
	require.NotNil(t, request.Reasoning.Context)
	require.Equal(t, "all_turns", *request.Reasoning.Context)
	require.Equal(t, "true", gjson.GetBytes(request.ClientMetadata, "ws_request_header_x_openai_internal_codex_responses_lite").String())
	require.Contains(t, envelope, "generate")
}

func TestBuildResponsesWSOutboundEvent(t *testing.T) {
	t.Parallel()

	_, _, original, apiErr := parseResponsesWSClientEvent([]byte(`{
		"type":"response.create",
		"model":"client-model",
		"previous_response_id":null,
		"generate":false,
		"background":true
	}`))
	require.Nil(t, apiErr)

	outbound, err := buildResponsesWSOutboundEvent(original, []byte(`{"model":"upstream-model","store":false}`))
	require.NoError(t, err)
	require.JSONEq(t, `{
		"type":"response.create",
		"model":"upstream-model",
		"store":false,
		"previous_response_id":null,
		"generate":false
	}`, string(outbound))
}

func TestBuildResponsesWSOutboundEventKeepsLiteFieldsFromPreparedRequest(t *testing.T) {
	t.Parallel()
	_, request, original, apiErr := parseResponsesWSClientEvent([]byte(`{
		"type":"response.create",
		"model":"gpt-5.6-sol",
		"input":"hello",
		"reasoning":{"effort":"high","context":"all_turns"},
		"client_metadata":{"ws_request_header_x_openai_internal_codex_responses_lite":"true"}
	}`))
	require.Nil(t, apiErr)
	prepared, err := common.Marshal(request)
	require.NoError(t, err)
	outbound, err := buildResponsesWSOutboundEvent(original, prepared)
	require.NoError(t, err)
	require.Equal(t, "all_turns", gjson.GetBytes(outbound, "reasoning.context").String())
	require.Equal(t, "true", gjson.GetBytes(outbound, "client_metadata.ws_request_header_x_openai_internal_codex_responses_lite").String())
}

func TestSupportsResponsesWebSocketChannel(t *testing.T) {
	t.Parallel()

	require.False(t, supportsResponsesWebSocketChannel(nil))
	require.True(t, supportsResponsesWebSocketChannel(&model.Channel{Type: constant.ChannelTypeOpenAI}))
	require.True(t, supportsResponsesWebSocketChannel(&model.Channel{Type: constant.ChannelTypeSub2API}))
	require.False(t, supportsResponsesWebSocketChannel(&model.Channel{Type: constant.ChannelTypeCodex}))
	require.True(t, supportsResponsesWebSocketChannel(&model.Channel{
		Type:             constant.ChannelTypeCodex,
		CredentialSource: constant.ChannelCredentialSourceAccountPool,
	}))
	require.True(t, supportsResponsesWebSocketChannel(&model.Channel{
		Type:          constant.ChannelTypeCodex,
		OtherSettings: `{"responses_websocket_v2_enabled":true}`,
	}))
	require.False(t, supportsResponsesWebSocketChannel(&model.Channel{
		Type:          constant.ChannelTypeOpenAI,
		OtherSettings: `{"responses_websocket_v2_mode":"off"}`,
	}))
}

func TestResponsesWSUpstreamMode(t *testing.T) {
	t.Parallel()
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	channel := &model.Channel{Type: constant.ChannelTypeCodex, OtherSettings: `{"responses_websocket_v2_enabled":true}`}

	require.Equal(t, model.UpstreamOpenAIWSModeContextPool, responsesWSUpstreamMode(ctx, channel))
	require.True(t, responsesWSModeUsesUpstreamWebSocket(model.UpstreamOpenAIWSModeContextPool))
	require.True(t, responsesWSModeUsesUpstreamWebSocket(model.UpstreamOpenAIWSModePassthrough))
	require.False(t, responsesWSModeUsesUpstreamWebSocket(model.UpstreamOpenAIWSModeHTTPBridge))
	require.False(t, responsesWSModeUsesUpstreamWebSocket(model.UpstreamOpenAIWSModeOff))

	common.SetContextKey(ctx, constant.ContextKeyChannelOtherSetting, dto.ChannelOtherSettings{
		ResponsesWebSocketV2Enabled: true,
		ResponsesWebSocketV2Mode:    model.UpstreamOpenAIWSModeHTTPBridge,
	})
	require.Equal(t, model.UpstreamOpenAIWSModeHTTPBridge, responsesWSUpstreamMode(ctx, channel))
}

func TestResponsesWSUpstreamIdentityChangesWithLocalAccount(t *testing.T) {
	t.Parallel()
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	channel := &model.Channel{Id: 42, Type: constant.ChannelTypeCodex}
	common.SetContextKey(ctx, constant.ContextKeyChannelType, constant.ChannelTypeCodex)
	common.SetContextKey(ctx, constant.ContextKeyChannelBaseUrl, "https://chatgpt.com")
	common.SetContextKey(ctx, constant.ContextKeyChannelKey, "credential-a")
	common.SetContextKey(ctx, constant.ContextKeyUpstreamAccountId, 10)
	common.SetContextKey(ctx, constant.ContextKeyUpstreamAccountLeaseId, "lease-a")
	first := responsesWSUpstreamIdentity(ctx, channel)

	common.SetContextKey(ctx, constant.ContextKeyUpstreamAccountId, 11)
	second := responsesWSUpstreamIdentity(ctx, channel)
	require.NotEmpty(t, first)
	require.NotEqual(t, first, second)
}

func TestResponsesWSUpstreamIdentityIgnoresLeaseRotation(t *testing.T) {
	t.Parallel()
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	channel := &model.Channel{Id: 42, Type: constant.ChannelTypeCodex}
	common.SetContextKey(ctx, constant.ContextKeyChannelType, constant.ChannelTypeCodex)
	common.SetContextKey(ctx, constant.ContextKeyChannelBaseUrl, "https://chatgpt.com")
	common.SetContextKey(ctx, constant.ContextKeyChannelKey, "credential-a")
	common.SetContextKey(ctx, constant.ContextKeyUpstreamAccountId, 10)
	common.SetContextKey(ctx, constant.ContextKeyUpstreamAccountLeaseId, "lease-a")
	first := responsesWSUpstreamIdentity(ctx, channel)

	common.SetContextKey(ctx, constant.ContextKeyUpstreamAccountLeaseId, "lease-b")
	common.SetContextKey(ctx, constant.ContextKeyChannelKey, "credential-b")
	second := responsesWSUpstreamIdentity(ctx, channel)
	require.Equal(t, first, second)
}

func TestResponsesWSHTTPBridgeReplaysInputAndDropsPreviousResponseID(t *testing.T) {
	t.Parallel()
	session := &responsesWebSocketSession{
		replayInput:       []json.RawMessage{json.RawMessage(`{"type":"message","role":"user","content":"first"}`)},
		replayInputExists: true,
	}
	turn := &responsesWebSocketTurn{
		upstreamMode:    model.UpstreamOpenAIWSModeHTTPBridge,
		originalRequest: &dto.OpenAIResponsesRequest{PreviousResponseID: "resp_first"},
	}
	prepared, apiErr := session.prepareReplayPayload(turn, json.RawMessage(`{
		"model":"gpt-5","input":"second","previous_response_id":"resp_first","stream":true
	}`))
	require.Nil(t, apiErr)
	var payload map[string]json.RawMessage
	require.NoError(t, common.Unmarshal(prepared, &payload))
	require.NotContains(t, payload, "previous_response_id")
	var input []map[string]any
	require.NoError(t, common.Unmarshal(payload["input"], &input))
	require.Len(t, input, 2)
	require.Equal(t, "first", input[0]["content"])
	require.Equal(t, "second", input[1]["content"])
}

func TestResponsesWSNativeConnectionKeepsPreviousResponseID(t *testing.T) {
	t.Parallel()
	session := &responsesWebSocketSession{
		upstream:          &responsesWSUpstreamConnection{},
		replayInput:       []json.RawMessage{json.RawMessage(`{"type":"message","role":"user","content":"first"}`)},
		replayInputExists: true,
	}
	turn := &responsesWebSocketTurn{
		upstreamMode:    model.UpstreamOpenAIWSModeContextPool,
		originalRequest: &dto.OpenAIResponsesRequest{PreviousResponseID: "resp_first"},
	}
	prepared, apiErr := session.prepareReplayPayload(turn, json.RawMessage(`{"input":[{"role":"user","content":"second"}],"previous_response_id":"resp_first"}`))
	require.Nil(t, apiErr)
	require.Equal(t, "resp_first", gjson.GetBytes(prepared, "previous_response_id").String())
}

func TestResponsesWSNativeReconnectReplaysInputAndDropsPreviousResponseID(t *testing.T) {
	t.Parallel()
	session := &responsesWebSocketSession{
		replayInput:       []json.RawMessage{json.RawMessage(`{"type":"message","role":"user","content":"first"}`)},
		replayInputExists: true,
	}
	turn := &responsesWebSocketTurn{
		upstreamMode:    model.UpstreamOpenAIWSModePassthrough,
		originalRequest: &dto.OpenAIResponsesRequest{PreviousResponseID: "resp_first"},
	}
	prepared, apiErr := session.prepareReplayPayload(turn, json.RawMessage(`{"input":"second","previous_response_id":"resp_first"}`))
	require.Nil(t, apiErr)
	require.False(t, gjson.GetBytes(prepared, "previous_response_id").Exists())
	require.Equal(t, int64(2), gjson.GetBytes(prepared, "input.#").Int())
}

func TestResponsesWSReplayCollectsToolCallContextOnce(t *testing.T) {
	t.Parallel()
	turn := &responsesWebSocketTurn{}
	event := []byte(`{"type":"response.output_item.done","item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"lookup","arguments":"{}"}}`)
	turn.collectReplayContext(event)
	turn.collectReplayContext(event)
	require.Len(t, turn.replayToolContext, 1)
	require.Equal(t, "fc_1", gjson.GetBytes(turn.replayToolContext[0], "id").String())
}

func TestResponsesWSReplayPrefixUsesCanonicalJSON(t *testing.T) {
	t.Parallel()
	previous := []json.RawMessage{json.RawMessage(`{"type":"message","role":"user","content":"first"}`)}
	current := []json.RawMessage{json.RawMessage(`{ "content": "first", "role": "user", "type": "message" }`)}
	require.True(t, responsesWSRawMessagesHavePrefix(current, previous))
}

func TestResponsesWSHTTPBridgeRejectsToolOutputWithoutCallContext(t *testing.T) {
	t.Parallel()
	session := &responsesWebSocketSession{}
	turn := &responsesWebSocketTurn{
		upstreamMode:    model.UpstreamOpenAIWSModeHTTPBridge,
		originalRequest: &dto.OpenAIResponsesRequest{PreviousResponseID: "resp_missing"},
	}
	_, apiErr := session.prepareReplayPayload(turn, json.RawMessage(`{
		"input":[{"type":"function_call_output","call_id":"call_1","output":"ok"}],
		"previous_response_id":"resp_missing"
	}`))
	require.NotNil(t, apiErr)
	require.Equal(t, http.StatusConflict, apiErr.StatusCode)
}

func TestResponsesWSHTTPBridgeAllowsToolOutputWithCallContext(t *testing.T) {
	t.Parallel()
	session := &responsesWebSocketSession{
		replayInput: []json.RawMessage{
			json.RawMessage(`{"type":"message","role":"user","content":"lookup"}`),
			json.RawMessage(`{"type":"function_call","id":"fc_1","call_id":"call_1","name":"lookup","arguments":"{}"}`),
		},
		replayInputExists: true,
	}
	turn := &responsesWebSocketTurn{
		upstreamMode:    model.UpstreamOpenAIWSModeHTTPBridge,
		originalRequest: &dto.OpenAIResponsesRequest{PreviousResponseID: "resp_first"},
	}
	prepared, apiErr := session.prepareReplayPayload(turn, json.RawMessage(`{
		"input":[{"type":"function_call_output","call_id":"call_1","output":"ok"}],
		"previous_response_id":"resp_first"
	}`))
	require.Nil(t, apiErr)
	require.False(t, gjson.GetBytes(prepared, "previous_response_id").Exists())
	require.Equal(t, int64(3), gjson.GetBytes(prepared, "input.#").Int())
}

func TestResponsesWSNativeContinuationRollsBackUnansweredToolCall(t *testing.T) {
	t.Parallel()
	session := &responsesWebSocketSession{
		upstream: &responsesWSUpstreamConnection{},
		replayInput: []json.RawMessage{
			json.RawMessage(`{"type":"message","role":"user","content":"lookup"}`),
			json.RawMessage(`{"type":"function_call","id":"fc_1","call_id":"call_1","name":"lookup","arguments":"{}"}`),
		},
		replayInputExists: true,
	}
	turn := &responsesWebSocketTurn{
		upstreamMode:    model.UpstreamOpenAIWSModeContextPool,
		originalRequest: &dto.OpenAIResponsesRequest{PreviousResponseID: "resp_tool"},
	}
	prepared, apiErr := session.prepareReplayPayload(turn, json.RawMessage(`{
		"input":[{"type":"message","role":"user","content":"skip the tool"}],
		"previous_response_id":"resp_tool"
	}`))
	require.Nil(t, apiErr)
	require.False(t, gjson.GetBytes(prepared, "previous_response_id").Exists())
	require.False(t, gjson.GetBytes(prepared, `input.#(type=="function_call")`).Exists())
	require.Equal(t, "skip the tool", gjson.GetBytes(prepared, "input.1.content").String())
}

func TestResponsesWSHTTPBridgeSupportsLocalShellOutput(t *testing.T) {
	t.Parallel()
	session := &responsesWebSocketSession{
		replayInput: []json.RawMessage{
			json.RawMessage(`{"type":"message","role":"user","content":"run"}`),
			json.RawMessage(`{"type":"local_shell_call","id":"sh_1","call_id":"shell_1"}`),
		},
		replayInputExists: true,
	}
	turn := &responsesWebSocketTurn{
		upstreamMode:    model.UpstreamOpenAIWSModeHTTPBridge,
		originalRequest: &dto.OpenAIResponsesRequest{PreviousResponseID: "resp_shell"},
	}
	prepared, apiErr := session.prepareReplayPayload(turn, json.RawMessage(`{
		"input":[{"type":"local_shell_call_output","call_id":"shell_1","output":"ok"}],
		"previous_response_id":"resp_shell"
	}`))
	require.Nil(t, apiErr)
	require.Equal(t, int64(3), gjson.GetBytes(prepared, "input.#").Int())
}

func TestResponsesWSContinuationStoreIsScopedAndRestored(t *testing.T) {
	t.Parallel()
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	common.SetContextKey(ctx, constant.ContextKeyUserId, 7)
	common.SetContextKey(ctx, constant.ContextKeyTokenId, 11)
	responseID := "resp_scoped_7_11"
	defaultResponsesWSContinuationStore.put(ctx, responseID, responsesWSContinuationState{
		accountID: 6, channelID: 44, upstreamMode: model.UpstreamOpenAIWSModeHTTPBridge, model: "gpt-5",
		turnState: "turn-state", replayInput: []json.RawMessage{json.RawMessage(`{"role":"user","content":"first"}`)}, replayInputExists: true,
	})

	session := &responsesWebSocketSession{baseCtx: ctx}
	require.Nil(t, session.restoreContinuation(&dto.OpenAIResponsesRequest{Model: "gpt-5", PreviousResponseID: responseID}))
	require.Equal(t, 6, session.lockedAccountID)
	require.Equal(t, 44, session.lockedChannelID)
	require.Equal(t, model.UpstreamOpenAIWSModeHTTPBridge, session.lockedWSMode)
	require.Equal(t, "turn-state", session.turnState)
	require.True(t, session.replayInputExists)

	otherCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
	otherCtx.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	common.SetContextKey(otherCtx, constant.ContextKeyUserId, 8)
	common.SetContextKey(otherCtx, constant.ContextKeyTokenId, 11)
	_, found := defaultResponsesWSContinuationStore.get(otherCtx, responseID)
	require.False(t, found)
}

func TestResponsesWSInfersPreviousResponseIDForToolOutput(t *testing.T) {
	t.Parallel()
	session := &responsesWebSocketSession{lastResponseID: "resp_previous"}
	request := &dto.OpenAIResponsesRequest{Input: json.RawMessage(`[{"type":"function_call_output","call_id":"call_1","output":"ok"}]`)}
	envelope := map[string]json.RawMessage{}
	require.NoError(t, session.inferToolContinuation(request, envelope))
	require.Equal(t, "resp_previous", request.PreviousResponseID)
	var encodedPrevious string
	require.NoError(t, common.Unmarshal(envelope["previous_response_id"], &encodedPrevious))
	require.Equal(t, "resp_previous", encodedPrevious)
}

func TestResponsesWSInfersPreviousResponseIDForEmptyContinuation(t *testing.T) {
	t.Parallel()
	session := &responsesWebSocketSession{lastResponseID: "resp_previous"}
	request := &dto.OpenAIResponsesRequest{}
	envelope := map[string]json.RawMessage{}
	require.NoError(t, session.inferToolContinuation(request, envelope))
	require.Equal(t, "resp_previous", request.PreviousResponseID)
	var encodedPrevious string
	require.NoError(t, common.Unmarshal(envelope["previous_response_id"], &encodedPrevious))
	require.Equal(t, "resp_previous", encodedPrevious)
}

func TestResponsesWSEmptyContinuationRespectsExplicitPreviousResponseReset(t *testing.T) {
	t.Parallel()
	for name, raw := range map[string]json.RawMessage{
		"null":         json.RawMessage("null"),
		"empty string": json.RawMessage(`""`),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			session := &responsesWebSocketSession{lastResponseID: "resp_previous"}
			request := &dto.OpenAIResponsesRequest{}
			envelope := map[string]json.RawMessage{"previous_response_id": raw}

			require.NoError(t, session.inferToolContinuation(request, envelope))
			require.Empty(t, request.PreviousResponseID)
			require.Equal(t, raw, envelope["previous_response_id"])
		})
	}
}

func TestResponsesWSEmptyContinuationPreservesAlternativeCreateSource(t *testing.T) {
	t.Parallel()
	for name, source := range map[string]json.RawMessage{
		"prompt":       json.RawMessage(`{"id":"pmpt_1"}`),
		"conversation": json.RawMessage(`"conv_1"`),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			session := &responsesWebSocketSession{lastResponseID: "resp_previous"}
			request := &dto.OpenAIResponsesRequest{}
			envelope := map[string]json.RawMessage{name: source}

			require.NoError(t, session.inferToolContinuation(request, envelope))
			require.Empty(t, request.PreviousResponseID)
			require.NotContains(t, envelope, "previous_response_id")
			require.Equal(t, source, envelope[name])
		})
	}
}

func TestResponsesWSEmptyContinuationHTTPBridgeReplaysHistory(t *testing.T) {
	t.Parallel()
	session := &responsesWebSocketSession{
		lastResponseID:    "resp_previous",
		replayInput:       []json.RawMessage{json.RawMessage(`{"type":"message","role":"user","content":"first"}`)},
		replayInputExists: true,
	}
	request := &dto.OpenAIResponsesRequest{}
	envelope := map[string]json.RawMessage{}
	require.NoError(t, session.inferToolContinuation(request, envelope))

	turn := &responsesWebSocketTurn{
		upstreamMode:    model.UpstreamOpenAIWSModeHTTPBridge,
		originalRequest: request,
	}
	prepared, apiErr := session.prepareReplayPayload(turn, json.RawMessage(`{"model":"gpt-5","previous_response_id":"resp_previous","stream":true}`))
	require.Nil(t, apiErr)
	require.False(t, gjson.GetBytes(prepared, "previous_response_id").Exists())
	require.Equal(t, int64(1), gjson.GetBytes(prepared, "input.#").Int())
	require.Equal(t, "first", gjson.GetBytes(prepared, "input.0.content").String())
}

func TestResponsesWSTurnReplaySnapshotSurvivesFinish(t *testing.T) {
	t.Parallel()

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	turn := &responsesWebSocketTurn{
		ctx:         ctx,
		info:        relaycommon.GenRelayInfoOpenAI(ctx, nil),
		accumulator: openairelay.NewResponsesEventAccumulator(),
	}
	var reconnects atomic.Int32
	turn.setReplayState([]byte(`{"type":"response.create"}`), func() (*websocket.Conn, *http.Response, error) {
		reconnects.Add(1)
		return nil, nil, errors.New("snapshot reconnect")
	})

	outbound, reconnect, ok := turn.replaySnapshot()
	require.True(t, ok)
	turn.finish(false)
	require.False(t, turn.replayStateAvailable())
	require.JSONEq(t, `{"type":"response.create"}`, string(outbound))
	_, _, err := reconnect()
	require.EqualError(t, err, "snapshot reconnect")
	require.Equal(t, int32(1), reconnects.Load())
}

func TestResponsesWSTerminalEventWaitsForTurnRelease(t *testing.T) {
	clientServer, clientPeer, closeClient := newResponsesWSTestPair(t)
	defer closeClient()
	upstreamServer, upstreamPeer, closeUpstream := newResponsesWSTestPair(t)
	defer closeUpstream()

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	releaseStarted := make(chan struct{})
	releaseGate := make(chan struct{})
	upstream := newResponsesWSUpstreamConnection(upstreamServer)
	turn := &responsesWebSocketTurn{
		ctx:         ctx,
		info:        relaycommon.GenRelayInfoOpenAI(ctx, nil),
		accumulator: openairelay.NewResponsesEventAccumulator(),
		releaseUser: func() {
			close(releaseStarted)
			<-releaseGate
		},
	}
	session := &responsesWebSocketSession{
		baseCtx:    ctx,
		client:     clientServer,
		upstream:   upstream,
		activeTurn: turn,
		closed:     make(chan struct{}),
	}
	go session.readUpstream(upstream)

	readResult := make(chan error, 1)
	go func() {
		_, _, err := clientPeer.ReadMessage()
		readResult <- err
	}()
	require.NoError(t, upstreamPeer.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.failed","response":{"id":"resp_failed"}}`)))

	select {
	case <-releaseStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("turn release did not start")
	}
	select {
	case err := <-readResult:
		t.Fatalf("terminal event became visible before release completed: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(releaseGate)
	select {
	case err := <-readResult:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("terminal event was not delivered after release completed")
	}

	session.close()
}

func TestResponsesWSClientDisconnectDrainsUsageWithoutCommittingContinuation(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Channel{}, &model.Log{}, &model.Token{}))
	originalDB := model.DB
	originalLogDB := model.LOG_DB
	originalBatchUpdateEnabled := common.BatchUpdateEnabled
	originalRedisEnabled := common.RedisEnabled
	originalRDB := common.RDB
	originalLogConsumeEnabled := common.LogConsumeEnabled
	model.DB = db
	model.LOG_DB = db
	common.BatchUpdateEnabled = false
	common.RedisEnabled = false
	common.RDB = nil
	common.LogConsumeEnabled = false
	t.Cleanup(func() {
		model.DB = originalDB
		model.LOG_DB = originalLogDB
		common.BatchUpdateEnabled = originalBatchUpdateEnabled
		common.RedisEnabled = originalRedisEnabled
		common.RDB = originalRDB
		common.LogConsumeEnabled = originalLogConsumeEnabled
	})

	clientServer, _, closeClient := newResponsesWSTestPair(t)
	defer closeClient()
	upstreamServer, upstreamPeer, closeUpstream := newResponsesWSTestPair(t)
	defer closeUpstream()

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	common.SetContextKey(ctx, constant.ContextKeyUserId, 7001)
	common.SetContextKey(ctx, constant.ContextKeyTokenId, 11001)
	released := make(chan struct{})
	upstream := newResponsesWSUpstreamConnection(upstreamServer)
	info := relaycommon.GenRelayInfoOpenAI(ctx, nil)
	info.ChannelMeta = &relaycommon.ChannelMeta{}
	priorInput := json.RawMessage(`{"type":"message","role":"user","content":"prior delivered turn"}`)
	turn := &responsesWebSocketTurn{
		ctx:               ctx,
		info:              info,
		accumulator:       openairelay.NewResponsesEventAccumulator(),
		channel:           &model.Channel{Id: 707},
		upstreamMode:      model.UpstreamOpenAIWSModeContextPool,
		replayInput:       []json.RawMessage{priorInput, json.RawMessage(`{"type":"message","role":"user","content":"aborted turn"}`)},
		replayInputExists: true,
		releaseUser:       func() { close(released) },
	}
	session := &responsesWebSocketSession{
		baseCtx: ctx, client: clientServer, upstream: upstream, activeTurn: turn, closed: make(chan struct{}),
		replayInput: []json.RawMessage{priorInput}, replayInputExists: true,
	}
	go session.readUpstream(upstream)
	session.handleClientLoopExit()
	require.True(t, session.isClientGone())

	require.NoError(t, upstreamPeer.WriteMessage(websocket.TextMessage, []byte(`{
		"type":"response.completed",
		"response":{
			"id":"resp_drained_after_client_gone",
			"output":[{"type":"custom_tool_call","id":"ctc_hidden","call_id":"call_hidden","name":"exec","input":"{}"}],
			"usage":{"input_tokens":10,"output_tokens":2,"total_tokens":12}
		}
	}`)))
	select {
	case <-released:
	case <-time.After(5 * time.Second):
		t.Fatal("turn was not settled after downstream disconnect")
	}
	select {
	case <-session.closed:
	case <-time.After(5 * time.Second):
		t.Fatal("session did not close after draining terminal usage")
	}
	require.Equal(t, 12, turn.accumulator.Usage(turn.info).TotalTokens)
	require.Len(t, session.replayInput, 1)
	require.JSONEq(t, string(priorInput), string(session.replayInput[0]))

	state, found := defaultResponsesWSContinuationStore.get(ctx, "resp_drained_after_client_gone")
	require.True(t, found)
	require.True(t, state.replayInputExists)
	require.Len(t, state.replayInput, 1)
	require.JSONEq(t, string(priorInput), string(state.replayInput[0]))

	resumed := &responsesWebSocketSession{baseCtx: ctx}
	request := &dto.OpenAIResponsesRequest{Model: "gpt-5", PreviousResponseID: "resp_drained_after_client_gone"}
	require.Nil(t, resumed.restoreContinuation(request))
	resumedTurn := &responsesWebSocketTurn{
		upstreamMode:    model.UpstreamOpenAIWSModeContextPool,
		originalRequest: request,
	}
	prepared, apiErr := resumed.prepareReplayPayload(resumedTurn, json.RawMessage(`{
		"model":"gpt-5",
		"previous_response_id":"resp_drained_after_client_gone",
		"input":[{"type":"message","role":"user","content":"revised turn"}]
	}`))
	require.Nil(t, apiErr)
	require.False(t, gjson.GetBytes(prepared, "previous_response_id").Exists())
	require.Equal(t, "prior delivered turn", gjson.GetBytes(prepared, "input.0.content").String())
	require.Equal(t, "revised turn", gjson.GetBytes(prepared, "input.1.content").String())
	require.False(t, gjson.GetBytes(prepared, `input.#(type=="custom_tool_call")`).Exists())
}

func TestResponsesWSReadUpstreamRecordsFirstResponseTime(t *testing.T) {
	clientServer, clientPeer, closeClient := newResponsesWSTestPair(t)
	defer closeClient()
	upstreamServer, upstreamPeer, closeUpstream := newResponsesWSTestPair(t)
	defer closeUpstream()

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	info := relaycommon.GenRelayInfoOpenAI(ctx, nil)
	info.StartTime = time.Now().Add(-time.Second)
	info.FirstResponseTime = info.StartTime.Add(-time.Second)
	require.False(t, info.HasSendResponse())

	upstream := newResponsesWSUpstreamConnection(upstreamServer)
	session := &responsesWebSocketSession{
		baseCtx:  ctx,
		client:   clientServer,
		upstream: upstream,
		activeTurn: &responsesWebSocketTurn{
			ctx:         ctx,
			info:        info,
			accumulator: openairelay.NewResponsesEventAccumulator(),
		},
		closed: make(chan struct{}),
	}
	go session.readUpstream(upstream)

	// Transport-only events must not start first-response timing.
	require.NoError(t, upstreamPeer.WriteMessage(websocket.TextMessage, []byte(`{"type":"responsesapi.websocket_timing"}`)))
	require.NoError(t, clientPeer.SetReadDeadline(time.Now().Add(5*time.Second)))
	messageType, data, err := clientPeer.ReadMessage()
	require.NoError(t, err)
	require.Equal(t, websocket.TextMessage, messageType)
	require.JSONEq(t, `{"type":"responsesapi.websocket_timing"}`, string(data))
	session.mu.Lock()
	hasFirstResponse := info.HasSendResponse()
	session.mu.Unlock()
	require.False(t, hasFirstResponse)

	require.NoError(t, upstreamPeer.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.created","response":{"id":"resp_1"}}`)))
	messageType, data, err = clientPeer.ReadMessage()
	require.NoError(t, err)
	require.Equal(t, websocket.TextMessage, messageType)
	require.JSONEq(t, `{"type":"response.created","response":{"id":"resp_1"}}`, string(data))
	session.mu.Lock()
	hasFirstResponse = info.HasSendResponse()
	firstResponseTime := info.FirstResponseTime
	session.mu.Unlock()
	require.True(t, hasFirstResponse)

	require.NoError(t, upstreamPeer.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.output_text.delta","delta":"hello"}`)))
	messageType, data, err = clientPeer.ReadMessage()
	require.NoError(t, err)
	require.Equal(t, websocket.TextMessage, messageType)
	require.JSONEq(t, `{"type":"response.output_text.delta","delta":"hello"}`, string(data))
	session.mu.Lock()
	hasFirstResponse = info.HasSendResponse()
	unchangedFirstResponseTime := info.FirstResponseTime
	session.mu.Unlock()
	require.True(t, hasFirstResponse)
	require.Equal(t, firstResponseTime, unchangedFirstResponseTime, "later output must not replace response.created timing")

	session.mu.Lock()
	session.activeTurn = nil
	session.mu.Unlock()
	require.NoError(t, upstreamPeer.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "done"), time.Now().Add(time.Second)))
	select {
	case <-upstream.done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for upstream detach")
	}

	select {
	case <-session.closed:
		t.Fatal("client session closed after upstream disconnect")
	default:
	}
	session.clientWriteMu.Lock()
	err = session.client.WriteMessage(websocket.TextMessage, []byte(`{"type":"session.alive"}`))
	session.clientWriteMu.Unlock()
	require.NoError(t, err)
	_, data, err = clientPeer.ReadMessage()
	require.NoError(t, err)
	require.JSONEq(t, `{"type":"session.alive"}`, string(data))
	session.close()
}

func TestResponsesWSReadUpstreamProjectsUsageWithoutMutatingSettlement(t *testing.T) {
	clientServer, clientPeer, closeClient := newResponsesWSTestPair(t)
	defer closeClient()
	upstreamServer, upstreamPeer, closeUpstream := newResponsesWSTestPair(t)
	defer closeUpstream()

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	info := relaycommon.GenRelayInfoOpenAI(ctx, nil)
	info.PriceData.CacheBillingOffsetBps = 300
	turn := &responsesWebSocketTurn{
		ctx:         ctx,
		info:        info,
		accumulator: openairelay.NewResponsesEventAccumulator(),
	}
	upstream := newResponsesWSUpstreamConnection(upstreamServer)
	session := &responsesWebSocketSession{
		baseCtx: ctx, client: clientServer, upstream: upstream, activeTurn: turn, closed: make(chan struct{}),
	}
	go session.readUpstream(upstream)

	rawEvent := []byte(`{"type":"response.in_progress","response":{"id":"resp_projection","usage":{"input_tokens":100,"output_tokens":20,"total_tokens":120,"input_tokens_details":{"cached_tokens":90}}}}`)
	require.NoError(t, upstreamPeer.WriteMessage(websocket.TextMessage, rawEvent))
	require.NoError(t, clientPeer.SetReadDeadline(time.Now().Add(5*time.Second)))
	messageType, data, err := clientPeer.ReadMessage()
	require.NoError(t, err)
	require.Equal(t, websocket.TextMessage, messageType)
	require.EqualValues(t, 100, gjson.GetBytes(data, "response.usage.input_tokens").Int())
	require.EqualValues(t, 20, gjson.GetBytes(data, "response.usage.output_tokens").Int())
	require.EqualValues(t, 120, gjson.GetBytes(data, "response.usage.total_tokens").Int())
	require.EqualValues(t, 87, gjson.GetBytes(data, "response.usage.input_tokens_details.cached_tokens").Int())
	require.False(t, gjson.GetBytes(data, "response.usage.cache_billing_offset_bps").Exists())
	require.NotContains(t, string(data), "raw_")
	require.NotContains(t, string(data), "ratio")

	rawUsage := turn.accumulator.Usage(info)
	require.Equal(t, 100, rawUsage.PromptTokens)
	require.Equal(t, 20, rawUsage.CompletionTokens)
	require.Equal(t, 90, rawUsage.PromptTokensDetails.CachedTokens)

	session.mu.Lock()
	session.activeTurn = nil
	session.mu.Unlock()
	session.close()
	session.wg.Wait()
}

func TestResponsesWSCapacityPreludeFlushesBeforeFirstOutput(t *testing.T) {
	clientServer, clientPeer, closeClient := newResponsesWSTestPair(t)
	defer closeClient()
	upstreamServer, upstreamPeer, closeUpstream := newResponsesWSTestPair(t)
	defer closeUpstream()

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	info := relaycommon.GenRelayInfoOpenAI(ctx, nil)
	turn := &responsesWebSocketTurn{
		ctx:             ctx,
		info:            info,
		accumulator:     openairelay.NewResponsesEventAccumulator(),
		originalRequest: &dto.OpenAIResponsesRequest{Model: "gpt-5"},
		channel: &model.Channel{
			Id: 2, CredentialSource: constant.ChannelCredentialSourceAccountPool,
		},
		upstreamMode:      model.UpstreamOpenAIWSModeContextPool,
		failoverStartedAt: time.Now(),
	}
	turn.setReplayState([]byte(`{"type":"response.create","model":"gpt-5"}`), func() (*websocket.Conn, *http.Response, error) {
		return nil, nil, errors.New("not used")
	})
	upstream := newResponsesWSUpstreamConnection(upstreamServer)
	session := &responsesWebSocketSession{
		baseCtx: ctx, client: clientServer, upstream: upstream,
		channel: turn.channel, activeTurn: turn, closed: make(chan struct{}),
	}
	go session.readUpstream(upstream)

	require.NoError(t, upstreamPeer.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.created","response":{"id":"resp_normal"}}`)))
	require.Eventually(t, func() bool {
		session.mu.Lock()
		defer session.mu.Unlock()
		return len(turn.capacityPrelude) == 1
	}, 5*time.Second, 10*time.Millisecond)

	require.NoError(t, upstreamPeer.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.output_text.delta","delta":"hello"}`)))
	require.NoError(t, clientPeer.SetReadDeadline(time.Now().Add(5*time.Second)))
	messageType, data, err := clientPeer.ReadMessage()
	require.NoError(t, err)
	require.Equal(t, websocket.TextMessage, messageType)
	require.JSONEq(t, `{"type":"response.created","response":{"id":"resp_normal"}}`, string(data))
	messageType, data, err = clientPeer.ReadMessage()
	require.NoError(t, err)
	require.Equal(t, websocket.TextMessage, messageType)
	require.JSONEq(t, `{"type":"response.output_text.delta","delta":"hello"}`, string(data))

	session.mu.Lock()
	session.activeTurn = nil
	session.mu.Unlock()
	session.close()
	session.wg.Wait()
}

func TestResponsesWSRecoverableUpstreamFailureKeepsClientSession(t *testing.T) {
	clientServer, clientPeer, closeClient := newResponsesWSTestPair(t)
	defer closeClient()
	upstreamServer, _, closeUpstream := newResponsesWSTestPair(t)
	defer closeUpstream()

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	info := relaycommon.GenRelayInfoOpenAI(ctx, nil)
	var released atomic.Int32
	upstream := newResponsesWSUpstreamConnection(upstreamServer)
	failedChannel := &model.Channel{Id: 1}
	session := &responsesWebSocketSession{
		baseCtx:  ctx,
		client:   clientServer,
		upstream: upstream,
		channel:  failedChannel,
		activeTurn: &responsesWebSocketTurn{
			ctx:             ctx,
			info:            info,
			accumulator:     openairelay.NewResponsesEventAccumulator(),
			rateLimitFinish: func(bool) { released.Add(1) },
			releaseUser:     func() { released.Add(1) },
			releaseBusiness: func() { released.Add(1) },
		},
		closed: make(chan struct{}),
	}

	session.handleUpstreamFailure(upstream, errors.New("broken pipe"))
	require.Equal(t, int32(3), released.Load())
	require.Nil(t, session.getUpstream())
	require.Nil(t, session.getChannel())
	requireResponsesWSTurnFailureAndAlive(t, session, clientPeer, "")
	session.close()
}

func TestResponsesWSUpstreamFailureReplaysOnceBeforeDownstreamEvents(t *testing.T) {
	clientServer, clientPeer, closeClient := newResponsesWSTestPair(t)
	defer closeClient()
	upstreamServer, _, closeUpstream := newResponsesWSTestPair(t)
	defer closeUpstream()
	replacementServer, replacementPeer, closeReplacement := newResponsesWSTestPair(t)
	defer closeReplacement()

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	upstream := newResponsesWSUpstreamConnection(upstreamServer)
	selectedChannel := &model.Channel{Id: 2}
	var reconnects atomic.Int32
	info := relaycommon.GenRelayInfoOpenAI(ctx, nil)
	info.ChannelMeta = &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5"}
	turn := &responsesWebSocketTurn{
		ctx:               ctx,
		info:              info,
		accumulator:       openairelay.NewResponsesEventAccumulator(),
		outbound:          []byte(`{"type":"response.create","model":"gpt-5"}`),
		channel:           selectedChannel,
		replayEnabled:     true,
		requestDispatched: true,
		reconnect: func() (*websocket.Conn, *http.Response, error) {
			reconnects.Add(1)
			return replacementServer, nil, nil
		},
	}
	session := &responsesWebSocketSession{
		baseCtx:    ctx,
		client:     clientServer,
		upstream:   upstream,
		channel:    selectedChannel,
		activeTurn: turn,
		closed:     make(chan struct{}),
	}

	session.handleUpstreamFailure(upstream, &websocket.CloseError{Code: websocket.CloseInternalServerErr, Text: "keepalive ping timeout"})
	require.Equal(t, int32(1), reconnects.Load())
	require.Equal(t, 1, turn.replayCount)
	require.Same(t, selectedChannel, session.getChannel())
	require.NotNil(t, session.getUpstream())

	require.NoError(t, replacementPeer.SetReadDeadline(time.Now().Add(5*time.Second)))
	messageType, data, err := replacementPeer.ReadMessage()
	require.NoError(t, err)
	require.Equal(t, websocket.TextMessage, messageType)
	require.JSONEq(t, `{"type":"response.create","model":"gpt-5"}`, string(data))

	require.NoError(t, replacementPeer.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.created","response":{"id":"resp_recovered"}}`)))
	require.NoError(t, replacementPeer.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.output_item.added","output_index":0}`)))
	require.NoError(t, clientPeer.SetReadDeadline(time.Now().Add(5*time.Second)))
	messageType, data, err = clientPeer.ReadMessage()
	require.NoError(t, err)
	require.Equal(t, websocket.TextMessage, messageType)
	require.JSONEq(t, `{"type":"response.created","response":{"id":"resp_recovered"}}`, string(data))
	messageType, data, err = clientPeer.ReadMessage()
	require.NoError(t, err)
	require.Equal(t, websocket.TextMessage, messageType)
	require.JSONEq(t, `{"type":"response.output_item.added","output_index":0}`, string(data))
	session.mu.Lock()
	session.activeTurn = nil
	session.mu.Unlock()

	session.close()
	session.wg.Wait()
}

func TestShouldRetryResponsesWSCapacityIsStrictlyScoped(t *testing.T) {
	t.Setenv("UPSTREAM_ACCOUNT_MAX_FAILOVERS", "2")
	t.Setenv("UPSTREAM_ACCOUNT_FAILOVER_BUDGET_MS", "5000")
	newTurn := func(t *testing.T, event string) (*responsesWebSocketTurn, *types.NewAPIError) {
		t.Helper()
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
		info := relaycommon.GenRelayInfoOpenAI(ctx, nil)
		turn := &responsesWebSocketTurn{
			ctx:                ctx,
			info:               info,
			accumulator:        openairelay.NewResponsesEventAccumulator(),
			originalRequest:    &dto.OpenAIResponsesRequest{Model: "gpt-5"},
			channel:            &model.Channel{Id: 2, CredentialSource: constant.ChannelCredentialSourceAccountPool},
			upstreamMode:       model.UpstreamOpenAIWSModeContextPool,
			upstreamEvent:      true,
			upstreamEventCount: 1,
			failoverStartedAt:  time.Now(),
		}
		turn.setReplayState([]byte(`{"type":"response.create","model":"gpt-5"}`), func() (*websocket.Conn, *http.Response, error) {
			return nil, nil, errors.New("not called by eligibility check")
		})
		_, err := turn.accumulator.Consume(ctx, info, []byte(event))
		require.NoError(t, err)
		return turn, responsesWSTerminalAPIError(turn)
	}

	capacityEvent := `{"type":"response.failed","response":{"id":"resp_capacity","status":"failed","error":{"code":"server_error","message":"Selected model is at capacity. Please try a different model."}}}`
	overloadEvent := `{"type":"response.failed","response":{"id":"resp_overload","status":"failed","error":{"code":"server_error","message":"Our servers are currently overloaded. Please try again later."}}}`
	slowDownEvent := `{"type":"response.failed","response":{"id":"resp_slow_down","status":"failed","error":{"code":"slow_down","message":"Please retry later."}}}`

	t.Run("capacity signature", func(t *testing.T) {
		turn, apiErr := newTurn(t, capacityEvent)
		require.Equal(t, 529, apiErr.StatusCode)
		require.True(t, shouldRetryResponsesWSCapacity(turn, apiErr))
	})
	t.Run("observed overload signature", func(t *testing.T) {
		turn, apiErr := newTurn(t, overloadEvent)
		require.Equal(t, 529, apiErr.StatusCode)
		require.True(t, shouldRetryResponsesWSCapacity(turn, apiErr))
	})
	t.Run("slow down signature", func(t *testing.T) {
		turn, apiErr := newTurn(t, slowDownEvent)
		require.Equal(t, 529, apiErr.StatusCode)
		require.True(t, shouldRetryResponsesWSCapacity(turn, apiErr))
	})
	t.Run("terminal capacity error envelope", func(t *testing.T) {
		turn, apiErr := newTurn(t, `{"type":"error","error":{"code":"server_error","message":"Selected model is at capacity"}}`)
		require.Equal(t, 529, apiErr.StatusCode)
		decision := evaluateResponsesWSCapacityRetry(turn, apiErr)
		require.True(t, decision.Eligible)
		require.Equal(t, "allowed", decision.Reason)
		require.Equal(t, "error", decision.TerminalType)
	})
	t.Run("all-zero reported usage blocks replay", func(t *testing.T) {
		turn, apiErr := newTurn(t, `{"type":"response.failed","response":{"error":{"code":"server_error","message":"Selected model is at capacity"},"usage":{"input_tokens":0,"output_tokens":0,"total_tokens":0}}}`)
		decision := evaluateResponsesWSCapacityRetry(turn, apiErr)
		require.False(t, decision.Eligible)
		require.Equal(t, "usage_reported", decision.Reason)
	})
	t.Run("lifecycle preludes before capacity failure", func(t *testing.T) {
		turn, apiErr := newTurn(t, capacityEvent)
		turn.upstreamEventCount = 4
		turn.capacitySafeEventCount = 3
		require.True(t, shouldRetryResponsesWSCapacity(turn, apiErr))
	})
	t.Run("second capacity failover within budget", func(t *testing.T) {
		turn, apiErr := newTurn(t, capacityEvent)
		turn.capacityRetryCount = 1
		turn.replayCount = 1
		turn.accountFailovers = 1
		require.True(t, shouldRetryResponsesWSCapacity(turn, apiErr))
	})

	tests := []struct {
		name   string
		event  string
		mutate func(*responsesWebSocketTurn, *types.NewAPIError)
	}{
		{
			name:  "rate limit",
			event: `{"type":"response.failed","response":{"error":{"code":"rate_limit_exceeded","message":"Too many requests"}}}`,
		},
		{
			name:  "generic error even if status is forced to 529",
			event: `{"type":"response.failed","response":{"error":{"code":"server_error","message":"Internal server error"}}}`,
			mutate: func(_ *responsesWebSocketTurn, apiErr *types.NewAPIError) {
				apiErr.StatusCode = 529
			},
		},
		{
			name:  "substantive upstream frame before failure",
			event: capacityEvent,
			mutate: func(turn *responsesWebSocketTurn, _ *types.NewAPIError) {
				turn.upstreamEventCount = 2
				turn.capacityRetryBlocked = true
			},
		},
		{
			name:  "reported usage",
			event: `{"type":"response.failed","response":{"error":{"code":"server_error","message":"Selected model is at capacity"},"usage":{"input_tokens":1,"total_tokens":1}}}`,
		},
		{
			name:  "non account pool channel",
			event: capacityEvent,
			mutate: func(turn *responsesWebSocketTurn, _ *types.NewAPIError) {
				turn.channel.CredentialSource = constant.ChannelCredentialSourceKey
			},
		},
		{
			name:  "http bridge mode",
			event: capacityEvent,
			mutate: func(turn *responsesWebSocketTurn, _ *types.NewAPIError) {
				turn.upstreamMode = model.UpstreamOpenAIWSModeHTTPBridge
			},
		},
		{
			name:  "capacity failover budget exhausted",
			event: capacityEvent,
			mutate: func(turn *responsesWebSocketTurn, _ *types.NewAPIError) {
				turn.capacityRetryCount = 2
				turn.replayCount = 2
				turn.accountFailovers = 2
			},
		},
		{
			name:  "another replay already consumed",
			event: capacityEvent,
			mutate: func(turn *responsesWebSocketTurn, _ *types.NewAPIError) {
				turn.replayCount = 1
			},
		},
		{
			name:  "outside retry window",
			event: capacityEvent,
			mutate: func(turn *responsesWebSocketTurn, _ *types.NewAPIError) {
				turn.failoverStartedAt = time.Now().Add(-responsesWSCapacityRetryMaxAge - time.Second)
			},
		},
		{
			name:  "missing replay state",
			event: capacityEvent,
			mutate: func(turn *responsesWebSocketTurn, _ *types.NewAPIError) {
				turn.setReplayState(nil, nil)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			turn, apiErr := newTurn(t, test.event)
			if test.mutate != nil {
				test.mutate(turn, apiErr)
			}
			require.False(t, shouldRetryResponsesWSCapacity(turn, apiErr))
		})
	}
}

func TestResponsesWSTerminalRateLimitRequiresStructuredCode(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	info := relaycommon.GenRelayInfoOpenAI(ctx, nil)

	plainMessageTurn := &responsesWebSocketTurn{
		ctx:         ctx,
		info:        info,
		accumulator: openairelay.NewResponsesEventAccumulator(),
	}
	_, err := plainMessageTurn.accumulator.Consume(ctx, info, []byte(`{"type":"response.failed","response":{"error":{"code":"server_error","message":"too many requests while processing this operation"}}}`))
	require.NoError(t, err)
	plainErr := responsesWSTerminalAPIError(plainMessageTurn)
	require.NotEqual(t, http.StatusTooManyRequests, plainErr.StatusCode)

	structuredTurn := &responsesWebSocketTurn{
		ctx:         ctx,
		info:        info,
		accumulator: openairelay.NewResponsesEventAccumulator(),
	}
	_, err = structuredTurn.accumulator.Consume(ctx, info, []byte(`{"type":"response.failed","response":{"error":{"code":"rate_limit_exceeded","message":"request rejected"}}}`))
	require.NoError(t, err)
	structuredErr := responsesWSTerminalAPIError(structuredTurn)
	require.Equal(t, http.StatusTooManyRequests, structuredErr.StatusCode)
}

func TestResponsesWSPreviousResponseNotFoundReplaysAsFullCreate(t *testing.T) {
	clientServer, clientPeer, closeClient := newResponsesWSTestPair(t)
	defer closeClient()
	upstreamServer, upstreamPeer, closeUpstream := newResponsesWSTestPair(t)
	defer closeUpstream()
	replacementServer, replacementPeer, closeReplacement := newResponsesWSTestPair(t)
	defer closeReplacement()

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	info := relaycommon.GenRelayInfoOpenAI(ctx, nil)
	info.ChannelMeta = &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5"}
	selectedChannel := &model.Channel{Id: 2}
	upstream := newResponsesWSUpstreamConnection(upstreamServer)
	turn := &responsesWebSocketTurn{
		ctx:               ctx,
		info:              info,
		accumulator:       openairelay.NewResponsesEventAccumulator(),
		originalRequest:   &dto.OpenAIResponsesRequest{Model: "gpt-5", PreviousResponseID: "resp_stale"},
		outbound:          []byte(`{"type":"response.create","model":"gpt-5","previous_response_id":"resp_stale","input":[{"type":"message","role":"user","content":"second"}]}`),
		channel:           selectedChannel,
		replayEnabled:     true,
		requestDispatched: true,
		replayInput: []json.RawMessage{
			json.RawMessage(`{"type":"message","role":"user","content":"first"}`),
			json.RawMessage(`{"type":"message","role":"user","content":"second"}`),
		},
		replayInputExists: true,
		reconnect: func() (*websocket.Conn, *http.Response, error) {
			return replacementServer, nil, nil
		},
	}
	session := &responsesWebSocketSession{
		baseCtx: ctx, client: clientServer, upstream: upstream, channel: selectedChannel,
		activeTurn: turn, closed: make(chan struct{}),
	}
	go session.readUpstream(upstream)

	require.NoError(t, upstreamPeer.WriteMessage(websocket.TextMessage, []byte(`{
		"type":"response.failed",
		"response":{"id":"resp_failed","error":{"code":"previous_response_not_found","message":"Previous response not found"}}
	}`)))
	require.NoError(t, replacementPeer.SetReadDeadline(time.Now().Add(5*time.Second)))
	messageType, replayed, err := replacementPeer.ReadMessage()
	require.NoError(t, err)
	require.Equal(t, websocket.TextMessage, messageType)
	require.False(t, gjson.GetBytes(replayed, "previous_response_id").Exists())
	require.Equal(t, int64(2), gjson.GetBytes(replayed, "input.#").Int())
	require.Equal(t, "first", gjson.GetBytes(replayed, "input.0.content").String())
	require.Equal(t, "second", gjson.GetBytes(replayed, "input.1.content").String())

	require.NoError(t, replacementPeer.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.created","response":{"id":"resp_recovered"}}`)))
	require.NoError(t, clientPeer.SetReadDeadline(time.Now().Add(5*time.Second)))
	messageType, downstream, err := clientPeer.ReadMessage()
	require.NoError(t, err)
	require.Equal(t, websocket.TextMessage, messageType)
	require.Equal(t, "response.created", gjson.GetBytes(downstream, "type").String())
	require.Equal(t, 1, turn.replayCount)

	session.mu.Lock()
	session.activeTurn = nil
	session.mu.Unlock()
	session.close()
	session.wg.Wait()
}

func TestResponsesWSUpstreamFailureDoesNotReplayAfterUpstreamAcknowledgement(t *testing.T) {
	clientServer, clientPeer, closeClient := newResponsesWSTestPair(t)
	defer closeClient()
	upstreamServer, upstreamPeer, closeUpstream := newResponsesWSTestPair(t)
	defer closeUpstream()

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	upstream := newResponsesWSUpstreamConnection(upstreamServer)
	var reconnects atomic.Int32
	turn := &responsesWebSocketTurn{
		ctx:               ctx,
		info:              relaycommon.GenRelayInfoOpenAI(ctx, nil),
		accumulator:       openairelay.NewResponsesEventAccumulator(),
		outbound:          []byte(`{"type":"response.create","model":"gpt-5"}`),
		channel:           &model.Channel{Id: 2},
		replayEnabled:     true,
		requestDispatched: true,
		reconnect: func() (*websocket.Conn, *http.Response, error) {
			reconnects.Add(1)
			return nil, nil, errors.New("must not reconnect after acknowledgement")
		},
	}
	session := &responsesWebSocketSession{
		baseCtx:    ctx,
		client:     clientServer,
		upstream:   upstream,
		channel:    turn.channel,
		activeTurn: turn,
		closed:     make(chan struct{}),
	}
	go session.readUpstream(upstream)

	require.NoError(t, upstreamPeer.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.created","response":{"id":"resp_accepted"}}`)))
	require.NoError(t, clientPeer.SetReadDeadline(time.Now().Add(5*time.Second)))
	_, data, err := clientPeer.ReadMessage()
	require.NoError(t, err)
	require.JSONEq(t, `{"type":"response.created","response":{"id":"resp_accepted"}}`, string(data))

	require.NoError(t, upstreamPeer.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseInternalServerErr, "keepalive ping timeout"), time.Now().Add(time.Second)))
	requireResponsesWSTurnFailureAndAlive(t, session, clientPeer, "resp_accepted")
	require.Zero(t, reconnects.Load())
	require.True(t, turn.upstreamEvent)

	session.close()
	session.wg.Wait()
}

func TestResponsesWSUpstreamFailureDoesNotReplayWhenOptInDisabled(t *testing.T) {
	clientServer, clientPeer, closeClient := newResponsesWSTestPair(t)
	defer closeClient()
	upstreamServer, _, closeUpstream := newResponsesWSTestPair(t)
	defer closeUpstream()

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	upstream := newResponsesWSUpstreamConnection(upstreamServer)
	var reconnects atomic.Int32
	turn := &responsesWebSocketTurn{
		ctx:               ctx,
		info:              relaycommon.GenRelayInfoOpenAI(ctx, nil),
		accumulator:       openairelay.NewResponsesEventAccumulator(),
		outbound:          []byte(`{"type":"response.create","model":"gpt-5"}`),
		channel:           &model.Channel{Id: 2},
		requestDispatched: true,
		reconnect: func() (*websocket.Conn, *http.Response, error) {
			reconnects.Add(1)
			return nil, nil, errors.New("must not reconnect without opt-in")
		},
	}
	session := &responsesWebSocketSession{
		baseCtx:    ctx,
		client:     clientServer,
		upstream:   upstream,
		channel:    turn.channel,
		activeTurn: turn,
		closed:     make(chan struct{}),
	}

	session.handleUpstreamFailure(upstream, errors.New("broken pipe"))
	require.Zero(t, reconnects.Load())
	requireResponsesWSTurnFailureAndAlive(t, session, clientPeer, "")
	session.close()
}

func TestResponsesWSUpstreamFailureDoesNotReplayAfterUpstreamEvent(t *testing.T) {
	clientServer, clientPeer, closeClient := newResponsesWSTestPair(t)
	defer closeClient()
	upstreamServer, _, closeUpstream := newResponsesWSTestPair(t)
	defer closeUpstream()

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	upstream := newResponsesWSUpstreamConnection(upstreamServer)
	var reconnects atomic.Int32
	turn := &responsesWebSocketTurn{
		ctx:           ctx,
		info:          relaycommon.GenRelayInfoOpenAI(ctx, nil),
		accumulator:   openairelay.NewResponsesEventAccumulator(),
		outbound:      []byte(`{"type":"response.create","model":"gpt-5"}`),
		channel:       &model.Channel{Id: 2},
		upstreamEvent: true,
		reconnect: func() (*websocket.Conn, *http.Response, error) {
			reconnects.Add(1)
			return nil, nil, errors.New("must not reconnect")
		},
	}
	session := &responsesWebSocketSession{
		baseCtx:    ctx,
		client:     clientServer,
		upstream:   upstream,
		channel:    turn.channel,
		activeTurn: turn,
		closed:     make(chan struct{}),
	}

	session.handleUpstreamFailure(upstream, &websocket.CloseError{Code: websocket.CloseInternalServerErr, Text: "keepalive ping timeout"})
	require.Zero(t, reconnects.Load())
	require.Zero(t, turn.replayCount)
	require.Nil(t, session.getUpstream())
	require.Nil(t, session.getChannel())

	requireResponsesWSTurnFailureAndAlive(t, session, clientPeer, "")
	session.close()
}

func TestResponsesWSReplayFailureKeepsClientSessionAndFinishesTurn(t *testing.T) {
	clientServer, clientPeer, closeClient := newResponsesWSTestPair(t)
	defer closeClient()
	upstreamServer, _, closeUpstream := newResponsesWSTestPair(t)
	defer closeUpstream()

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	upstream := newResponsesWSUpstreamConnection(upstreamServer)
	var reconnects atomic.Int32
	var released atomic.Int32
	turn := &responsesWebSocketTurn{
		ctx:               ctx,
		info:              relaycommon.GenRelayInfoOpenAI(ctx, nil),
		accumulator:       openairelay.NewResponsesEventAccumulator(),
		outbound:          []byte(`{"type":"response.create","model":"gpt-5"}`),
		channel:           &model.Channel{Id: 2},
		replayEnabled:     true,
		requestDispatched: true,
		reconnect: func() (*websocket.Conn, *http.Response, error) {
			reconnects.Add(1)
			return nil, nil, errors.New("replacement dial failed")
		},
		rateLimitFinish: func(bool) { released.Add(1) },
		releaseUser:     func() { released.Add(1) },
		releaseBusiness: func() { released.Add(1) },
	}
	session := &responsesWebSocketSession{
		baseCtx:    ctx,
		client:     clientServer,
		upstream:   upstream,
		channel:    turn.channel,
		activeTurn: turn,
		closed:     make(chan struct{}),
	}

	session.handleUpstreamFailure(upstream, errors.New("broken pipe"))
	require.Equal(t, int32(1), reconnects.Load())
	require.Equal(t, int32(3), released.Load())
	require.Nil(t, session.getUpstream())
	require.Nil(t, session.getChannel())

	requireResponsesWSTurnFailureAndAlive(t, session, clientPeer, "")

	session.close()
	require.Equal(t, int32(3), released.Load())
}

func TestResponsesWSNonRecoverableUpstreamFailureReturnsFailedEvent(t *testing.T) {
	clientServer, clientPeer, closeClient := newResponsesWSTestPair(t)
	defer closeClient()
	upstreamServer, _, closeUpstream := newResponsesWSTestPair(t)
	defer closeUpstream()

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	upstream := newResponsesWSUpstreamConnection(upstreamServer)
	turn := &responsesWebSocketTurn{
		ctx:         ctx,
		info:        relaycommon.GenRelayInfoOpenAI(ctx, nil),
		accumulator: openairelay.NewResponsesEventAccumulator(),
	}
	session := &responsesWebSocketSession{
		baseCtx:    ctx,
		client:     clientServer,
		upstream:   upstream,
		activeTurn: turn,
		closed:     make(chan struct{}),
	}

	session.handleUpstreamFailure(upstream, &websocket.CloseError{
		Code: websocket.ClosePolicyViolation,
		Text: "upstream policy rejected the request",
	})

	require.NoError(t, clientPeer.SetReadDeadline(time.Now().Add(5*time.Second)))
	_, data, err := clientPeer.ReadMessage()
	require.NoError(t, err)
	var failedEvent struct {
		Type     string `json:"type"`
		Response struct {
			Status string `json:"status"`
		} `json:"response"`
	}
	require.NoError(t, common.Unmarshal(data, &failedEvent))
	require.Equal(t, "response.failed", failedEvent.Type)
	require.Equal(t, "failed", failedEvent.Response.Status)

	session.close()
}

func TestResponsesWSSSEFailureKeepsSessionAndAllowsNextTurn(t *testing.T) {
	clientServer, clientPeer, closeClient := newResponsesWSTestPair(t)
	defer closeClient()

	var requests atomic.Int32
	requestBodies := make(chan string, 2)
	requestAccepts := make(chan string, 2)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requestBodies <- string(body)
		requestAccepts <- r.Header.Get("Accept")
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		if requests.Add(1) == 1 {
			_, _ = w.Write([]byte("data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_first\"}}\n\n"))
			flusher.Flush()
			return
		}
		_, _ = w.Write([]byte("data: {\"type\":\"response.output_text.done\"}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"response.failed\",\"response\":{\"id\":\"resp_second\"}}\n\n"))
		flusher.Flush()
	}))
	defer upstream.Close()

	baseCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
	baseCtx.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	selectedChannel := &model.Channel{}
	var released atomic.Int32
	session := &responsesWebSocketSession{
		baseCtx: baseCtx,
		client:  clientServer,
		channel: selectedChannel,
		closed:  make(chan struct{}),
	}

	newTurn := func() (*responsesWebSocketTurn, *openairelay.Adaptor) {
		turnCtx, cancel := newResponsesWSTurnContext(baseCtx, "gpt-5")
		common.SetContextKey(turnCtx, constant.ContextKeyRequestStartTime, time.Now().Add(-time.Second))
		info := relaycommon.GenRelayInfoOpenAI(turnCtx, nil)
		info.IsStream = true
		info.RelayMode = relayconstant.RelayModeResponses
		info.RelayFormat = types.RelayFormatOpenAIResponses
		info.RequestURLPath = "/v1/responses"
		info.ChannelMeta = &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeOpenAI,
			ChannelBaseUrl:    upstream.URL,
			ApiKey:            "test-key",
			UpstreamModelName: "gpt-5",
		}
		adaptor := &openairelay.Adaptor{}
		adaptor.Init(info)
		return &responsesWebSocketTurn{
			ctx:             turnCtx,
			info:            info,
			accumulator:     openairelay.NewResponsesEventAccumulator(),
			channel:         selectedChannel,
			cancel:          cancel,
			rateLimitFinish: func(bool) { released.Add(1) },
			releaseUser:     func() { released.Add(1) },
			releaseBusiness: func() { released.Add(1) },
		}, adaptor
	}

	firstTurn, firstAdaptor := newTurn()
	require.True(t, session.activateSSETurn(firstTurn))
	firstTurn.setSSEReplayState([]byte(`{"model":"gpt-5","stream":true}`), firstAdaptor)
	session.runSSETurn(firstTurn)
	require.NoError(t, clientPeer.SetReadDeadline(time.Now().Add(5*time.Second)))
	_, data, err := clientPeer.ReadMessage()
	require.NoError(t, err)
	require.JSONEq(t, `{"type":"response.created","response":{"id":"resp_first"}}`, string(data))
	requireResponsesWSTurnFailureAndAlive(t, session, clientPeer, "resp_first")
	_, _, replayable := firstTurn.sseReplaySnapshot()
	require.False(t, replayable)
	require.Nil(t, session.getChannel())

	session.setChannel(selectedChannel)
	secondTurn, secondAdaptor := newTurn()
	require.True(t, session.activateSSETurn(secondTurn))
	secondTurn.setSSEReplayState([]byte(`{"model":"gpt-5","stream":true}`), secondAdaptor)
	session.runSSETurn(secondTurn)
	_, data, err = clientPeer.ReadMessage()
	require.NoError(t, err)
	require.JSONEq(t, `{"type":"response.output_text.done"}`, string(data))
	_, data, err = clientPeer.ReadMessage()
	require.NoError(t, err)
	require.JSONEq(t, `{"type":"response.failed","response":{"id":"resp_second"}}`, string(data))
	require.Truef(t, secondTurn.info.HasSendResponse(), "start=%s first=%s", secondTurn.info.StartTime, secondTurn.info.FirstResponseTime)
	require.False(t, session.isActiveTurn(secondTurn))
	select {
	case <-session.closed:
		t.Fatal("downstream websocket closed after the second SSE turn")
	default:
	}

	for i := 0; i < 2; i++ {
		var request map[string]any
		require.NoError(t, common.Unmarshal([]byte(<-requestBodies), &request))
		require.Equal(t, true, request["stream"])
		require.NotContains(t, request, "type")
		require.Equal(t, "text/event-stream", <-requestAccepts)
	}
	require.Equal(t, int32(6), released.Load())
	session.close()
	require.Equal(t, int32(6), released.Load())
}

func TestResponsesWSSSERetriesAnotherLocalAccountBeforeVisibleEvent(t *testing.T) {
	clientServer, clientPeer, closeClient := newResponsesWSTestPair(t)
	defer closeClient()

	var requests atomic.Int32
	authorizations := make(chan string, 2)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorizations <- r.Header.Get("Authorization")
		if requests.Add(1) == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"message":"rate limited","type":"rate_limit_error","code":"rate_limit"}}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"response.failed\",\"response\":{\"id\":\"resp_second_account\",\"status\":\"failed\",\"error\":{\"code\":\"upstream_failed\",\"message\":\"second account reached upstream\"}}}\n\n"))
	}))
	defer upstream.Close()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.Channel{}, &model.UpstreamAccountPool{}, &model.UpstreamAccount{},
		&model.UpstreamAccountPoolMember{}, &model.UpstreamProxy{}, &model.UpstreamAccountEvent{},
	))
	originalDB := model.DB
	model.DB = db
	originalRedisEnabled := common.RedisEnabled
	originalRDB := common.RDB
	common.RedisEnabled = false
	common.RDB = nil
	t.Cleanup(func() {
		model.DB = originalDB
		common.RedisEnabled = originalRedisEnabled
		common.RDB = originalRDB
	})
	t.Setenv("UPSTREAM_ACCOUNT_ALLOW_LOCAL_LEASES", "true")
	key := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	keyringJSON, err := common.Marshal(map[string]string{"1": key})
	require.NoError(t, err)
	t.Setenv("UPSTREAM_ACCOUNT_CREDENTIAL_KEYS", string(keyringJSON))
	t.Setenv("UPSTREAM_ACCOUNT_ACTIVE_KEY_VERSION", "1")

	pool := model.UpstreamAccountPool{
		Name: "openai-local", Platform: constant.UpstreamPlatformOpenAI,
		CredentialType: constant.UpstreamAccountTypeAPIKey, Status: constant.UpstreamStatusActive,
		SchedulerConfig: "{}",
	}
	require.NoError(t, service.CreateUpstreamAccountPool(&pool))
	accounts := []struct {
		name string
		key  string
	}{{name: "account-one", key: "local-key-one"}, {name: "account-two", key: "local-key-two"}}
	for _, candidate := range accounts {
		input := service.UpstreamAccountCreateInput{
			Account: model.UpstreamAccount{
				Name: candidate.name, Platform: constant.UpstreamPlatformOpenAI,
				Type: constant.UpstreamAccountTypeAPIKey, Extra: "{}", Concurrency: 1,
				Priority: 50, Weight: 1, Status: constant.UpstreamStatusActive, Schedulable: true,
			},
			Credentials: map[string]any{"api_key": candidate.key, "base_url": upstream.URL},
			PoolIds:     []int{pool.Id},
		}
		require.NoError(t, service.CreateUpstreamAccount(&input))
	}

	selectedChannel := &model.Channel{
		Id: 77, Type: constant.ChannelTypeOpenAI, Name: "local-pool",
		CredentialSource: constant.ChannelCredentialSourceAccountPool, UpstreamAccountPoolId: &pool.Id,
	}
	baseCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
	baseCtx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	turnCtx, cancel := newResponsesWSTurnContext(baseCtx, "gpt-5")
	require.Nil(t, middleware.SetupContextForSelectedChannel(turnCtx, selectedChannel, "gpt-5"))
	info := relaycommon.GenRelayInfoOpenAI(turnCtx, nil)
	info.IsStream = true
	info.RelayMode = relayconstant.RelayModeResponses
	info.RelayFormat = types.RelayFormatOpenAIResponses
	info.RequestURLPath = "/v1/responses"
	info.InitChannelMeta(turnCtx)
	adaptor := &openairelay.Adaptor{}
	adaptor.Init(info)
	turn := &responsesWebSocketTurn{
		ctx: turnCtx, info: info, accumulator: openairelay.NewResponsesEventAccumulator(),
		channel: selectedChannel, cancel: cancel,
	}
	session := &responsesWebSocketSession{
		baseCtx: baseCtx, client: clientServer, channel: selectedChannel, closed: make(chan struct{}),
	}
	require.True(t, session.activateSSETurn(turn))
	turn.setSSEReplayState([]byte(`{"model":"gpt-5","stream":true}`), adaptor)
	session.startSSETurn(turn)

	requireResponsesWSTurnFailureAndAlive(t, session, clientPeer, "resp_second_account")
	require.Equal(t, int32(2), requests.Load())
	firstAuthorization := <-authorizations
	secondAuthorization := <-authorizations
	require.NotEqual(t, firstAuthorization, secondAuthorization)
	require.Contains(t, []string{"Bearer local-key-one", "Bearer local-key-two"}, firstAuthorization)
	require.Contains(t, []string{"Bearer local-key-one", "Bearer local-key-two"}, secondAuthorization)
	session.close()
}

func TestRecordResponsesWSCapacityRetryDecisionPersistsReason(t *testing.T) {
	dsn := fmt.Sprintf("file:responses_ws_capacity_decision_%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.UpstreamAccountEvent{}))

	originalDB := model.DB
	model.DB = db
	t.Cleanup(func() {
		model.DB = originalDB
	})

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	common.SetContextKey(ctx, constant.ContextKeyUpstreamAccountId, 123)
	turn := &responsesWebSocketTurn{
		ctx:                    ctx,
		accumulator:            openairelay.NewResponsesEventAccumulator(),
		capacityRetryCount:     1,
		accountFailovers:       1,
		upstreamEventCount:     3,
		capacitySafeEventCount: 1,
		capacityRetryBlocked:   true,
	}
	recordResponsesWSCapacityRetryDecision(turn, responsesWSCapacityRetryDecision{
		Reason:       "semantic_output_started",
		TerminalType: "response.failed",
	})

	var event model.UpstreamAccountEvent
	require.NoError(t, db.Where("event_type = ?", "capacity_retry_decision").First(&event).Error)
	require.Equal(t, "skipped", event.Result)
	require.Equal(t, "semantic_output_started", event.Message)
	var metadata map[string]any
	require.NoError(t, common.UnmarshalJsonStr(event.Metadata, &metadata))
	require.Equal(t, "semantic_output_started", metadata["reason"])
	require.Equal(t, "response.failed", metadata["terminal_type"])
	require.Equal(t, true, metadata["semantic_output_started"])
}

func TestResponsesWSCapacityFailureRetriesAcrossLocalAccounts(t *testing.T) {
	// Exercise the full production retry budget: three capacity-rejected
	// accounts must be replaced before the fourth account succeeds.
	t.Setenv("UPSTREAM_ACCOUNT_MAX_FAILOVERS", "3")
	clientServer, clientPeer, closeClient := newResponsesWSTestPair(t)
	defer closeClient()

	var connections atomic.Int32
	authorizations := make(chan string, 4)
	requestBodies := make(chan []byte, 4)
	serverErrors := make(chan error, 4)
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connectionNumber := connections.Add(1)
		authorizations <- r.Header.Get("Authorization")
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			serverErrors <- err
			return
		}
		defer conn.Close()
		_, body, err := conn.ReadMessage()
		if err != nil {
			serverErrors <- err
			return
		}
		requestBodies <- body
		if connectionNumber <= 3 {
			if err = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.created","response":{"id":"resp_capacity"}}`)); err != nil {
				serverErrors <- err
				return
			}
			if err = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.in_progress","response":{"id":"resp_capacity"}}`)); err != nil {
				serverErrors <- err
				return
			}
			if err = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.output_item.added","item":{"type":"reasoning","summary":[],"encrypted_content":"opaque-capacity-prelude"}}`)); err != nil {
				serverErrors <- err
				return
			}
			if err = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.reasoning_summary_part.added","part":{"type":"summary_text","text":""}}`)); err != nil {
				serverErrors <- err
				return
			}
			if connectionNumber == 1 {
				// Production may terminate a capacity-rejected turn with only an
				// error envelope. The gateway must fail over without waiting for a
				// response.failed event that never arrives.
				if err = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"error","error":{"type":"service_unavailable_error","code":"server_is_overloaded","message":"Our servers are currently overloaded. Please try again later."}}`)); err != nil {
					serverErrors <- err
				}
				return
			}
			err = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.failed","response":{"id":"resp_capacity","status":"failed","error":{"code":"server_error","message":"Our servers are currently overloaded. Please try again later."}}}`))
			if err != nil {
				serverErrors <- err
			}
			return
		}
		if err = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.created","response":{"id":"resp_retried"}}`)); err != nil {
			serverErrors <- err
			return
		}
		for {
			if _, _, err = conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer upstream.Close()

	dsn := fmt.Sprintf("file:responses_ws_capacity_retry_%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.Channel{}, &model.UpstreamAccountPool{}, &model.UpstreamAccount{},
		&model.UpstreamAccountPoolMember{}, &model.UpstreamProxy{}, &model.UpstreamAccountEvent{},
	))
	originalDB := model.DB
	model.DB = db
	originalRedisEnabled := common.RedisEnabled
	originalRDB := common.RDB
	common.RedisEnabled = false
	common.RDB = nil
	t.Cleanup(func() {
		model.DB = originalDB
		common.RedisEnabled = originalRedisEnabled
		common.RDB = originalRDB
	})
	t.Setenv("UPSTREAM_ACCOUNT_ALLOW_LOCAL_LEASES", "true")
	key := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	keyringJSON, err := common.Marshal(map[string]string{"1": key})
	require.NoError(t, err)
	t.Setenv("UPSTREAM_ACCOUNT_CREDENTIAL_KEYS", string(keyringJSON))
	t.Setenv("UPSTREAM_ACCOUNT_ACTIVE_KEY_VERSION", "1")

	entityBaseID := int(responsesWSTestEntityID.Add(10)) + 1_000_000
	pool := model.UpstreamAccountPool{
		Id:   entityBaseID,
		Name: "openai-native-ws", Platform: constant.UpstreamPlatformOpenAI,
		CredentialType: constant.UpstreamAccountTypeAPIKey, Status: constant.UpstreamStatusActive,
		SchedulerConfig: "{}",
	}
	require.NoError(t, service.CreateUpstreamAccountPool(&pool))
	for index, candidate := range []struct {
		name string
		key  string
	}{{name: "native-account-one", key: "native-key-one"}, {name: "native-account-two", key: "native-key-two"}, {name: "native-account-three", key: "native-key-three"}, {name: "native-account-four", key: "native-key-four"}} {
		input := service.UpstreamAccountCreateInput{
			Account: model.UpstreamAccount{
				Id:   entityBaseID + index + 1,
				Name: candidate.name, Platform: constant.UpstreamPlatformOpenAI,
				Type:        constant.UpstreamAccountTypeAPIKey,
				Extra:       `{"openai_apikey_responses_websockets_v2_mode":"ctx_pool"}`,
				Concurrency: 1, Priority: 50, Weight: 1,
				Status: constant.UpstreamStatusActive, Schedulable: true,
			},
			Credentials: map[string]any{"api_key": candidate.key, "base_url": upstream.URL},
			PoolIds:     []int{pool.Id},
		}
		require.NoError(t, service.CreateUpstreamAccount(&input))
	}

	selectedChannel := &model.Channel{
		Id: entityBaseID + 5, Type: constant.ChannelTypeOpenAI, Name: "native-local-pool",
		CredentialSource: constant.ChannelCredentialSourceAccountPool, UpstreamAccountPoolId: &pool.Id,
	}
	baseCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
	baseCtx.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	turnCtx, cancel := newResponsesWSTurnContext(baseCtx, "gpt-5")
	require.Nil(t, middleware.SetupContextForSelectedChannel(turnCtx, selectedChannel, "gpt-5"))
	initialAccountID := common.GetContextKeyInt(turnCtx, constant.ContextKeyUpstreamAccountId)
	require.Positive(t, initialAccountID)

	request := &dto.OpenAIResponsesRequest{Model: "gpt-5"}
	info := relaycommon.GenRelayInfoOpenAI(turnCtx, nil)
	info.IsStream = true
	info.RelayMode = relayconstant.RelayModeResponses
	info.RelayFormat = types.RelayFormatOpenAIResponses
	info.RequestURLPath = "/v1/responses"
	prepared, adaptor, apiErr := relay.PrepareResponsesWebSocketRequest(turnCtx, info, request, false)
	require.Nil(t, apiErr)
	upstreamConn, resp, err := relaychannel.DoResponsesWssRequest(adaptor, turnCtx, info)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	require.NoError(t, err)

	turn := &responsesWebSocketTurn{
		ctx: turnCtx, info: info, accumulator: openairelay.NewResponsesEventAccumulator(),
		originalRequest: request, upstreamMode: model.UpstreamOpenAIWSModeContextPool,
		channel: selectedChannel, requestDispatched: true, accountID: initialAccountID,
		failoverStartedAt: time.Now(), cancel: cancel,
	}
	turn.setReplayState(prepared, func() (*websocket.Conn, *http.Response, error) {
		return relaychannel.DoResponsesWssRequest(adaptor, turnCtx, info)
	})
	upstreamConnection := newResponsesWSUpstreamConnection(upstreamConn)
	session := &responsesWebSocketSession{
		baseCtx: baseCtx, client: clientServer, upstream: upstreamConnection,
		channel: selectedChannel, activeTurn: turn, lockedAccountID: initialAccountID,
		lockedWSMode: model.UpstreamOpenAIWSModeContextPool, closed: make(chan struct{}),
	}
	go session.readUpstream(upstreamConnection)
	require.NoError(t, upstreamConn.WriteMessage(websocket.TextMessage, prepared))

	require.NoError(t, clientPeer.SetReadDeadline(time.Now().Add(5*time.Second)))
	messageType, downstream, err := clientPeer.ReadMessage()
	require.NoError(t, err)
	require.Equal(t, websocket.TextMessage, messageType)
	require.JSONEq(t, `{"type":"response.created","response":{"id":"resp_retried"}}`, string(downstream))
	require.Equal(t, int32(4), connections.Load())
	require.Equal(t, 3, turn.capacityRetryCount)
	require.Equal(t, 3, turn.replayCount)
	require.NotEqual(t, initialAccountID, turn.accountID)
	var decisionEvents int64
	require.NoError(t, db.Model(&model.UpstreamAccountEvent{}).
		Where("event_type = ? AND result = ?", "capacity_retry_decision", "allowed").
		Count(&decisionEvents).Error)
	require.EqualValues(t, 3, decisionEvents)
	var retryEvents int64
	require.NoError(t, db.Model(&model.UpstreamAccountEvent{}).
		Where("event_type = ? AND result = ?", "request_retry", "retry").
		Count(&retryEvents).Error)
	require.EqualValues(t, 3, retryEvents)

	seenAuthorizations := map[string]struct{}{}
	for range 4 {
		seenAuthorizations[<-authorizations] = struct{}{}
	}
	require.Equal(t, map[string]struct{}{
		"Bearer native-key-one": {}, "Bearer native-key-two": {}, "Bearer native-key-three": {}, "Bearer native-key-four": {},
	}, seenAuthorizations)
	firstBody := <-requestBodies
	secondBody := <-requestBodies
	thirdBody := <-requestBodies
	fourthBody := <-requestBodies
	require.JSONEq(t, string(firstBody), string(secondBody))
	require.JSONEq(t, string(firstBody), string(thirdBody))
	require.JSONEq(t, string(firstBody), string(fourthBody))
	select {
	case serverErr := <-serverErrors:
		require.NoError(t, serverErr)
	default:
	}

	session.mu.Lock()
	session.activeTurn = nil
	session.mu.Unlock()
	session.close()
	session.wg.Wait()
}
