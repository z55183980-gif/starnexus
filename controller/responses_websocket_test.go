package controller

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	openairelay "github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

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

func TestParseResponsesWSClientEvent(t *testing.T) {
	t.Parallel()

	event, request, envelope, apiErr := parseResponsesWSClientEvent([]byte(`{
		"type":"response.create",
		"model":"gpt-5",
		"input":"hello",
		"previous_response_id":"resp_1",
		"generate":false
	}`))
	require.Nil(t, apiErr)
	require.Equal(t, "response.create", event.Type)
	require.Equal(t, "gpt-5", request.Model)
	require.Equal(t, "resp_1", request.PreviousResponseID)
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

	require.NoError(t, upstreamPeer.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.created","response":{"id":"resp_1"}}`)))
	require.NoError(t, clientPeer.SetReadDeadline(time.Now().Add(5*time.Second)))
	messageType, data, err := clientPeer.ReadMessage()
	require.NoError(t, err)
	require.Equal(t, websocket.TextMessage, messageType)
	require.JSONEq(t, `{"type":"response.created","response":{"id":"resp_1"}}`, string(data))
	session.mu.Lock()
	hasFirstResponse := info.HasSendResponse()
	session.mu.Unlock()
	require.True(t, hasFirstResponse)

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

func TestResponsesWSUpstreamFailureRetainsClientAndAllowsReplacement(t *testing.T) {
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
	select {
	case <-session.closed:
		t.Fatal("client session closed after upstream failure")
	default:
	}

	require.NoError(t, clientPeer.SetReadDeadline(time.Now().Add(5*time.Second)))
	_, data, err := clientPeer.ReadMessage()
	require.NoError(t, err)
	var errorEvent struct {
		Type   string `json:"type"`
		Status int    `json:"status"`
	}
	require.NoError(t, common.Unmarshal(data, &errorEvent))
	require.Equal(t, "error", errorEvent.Type)
	require.Equal(t, http.StatusBadGateway, errorEvent.Status)

	replacementServer, replacementPeer, closeReplacement := newResponsesWSTestPair(t)
	defer closeReplacement()
	replacement := session.attachUpstream(replacementServer)
	require.NotNil(t, replacement)
	require.Same(t, replacement, session.getUpstream())
	replacementChannel := &model.Channel{Id: 2}
	session.setChannel(replacementChannel)

	session.handleUpstreamFailure(upstream, errors.New("late stale reader failure"))
	require.Same(t, replacement, session.getUpstream())
	require.Same(t, replacementChannel, session.getChannel())
	require.Equal(t, int32(3), released.Load())

	replacementInfo := relaycommon.GenRelayInfoOpenAI(ctx, nil)
	replacementTurn := &responsesWebSocketTurn{
		ctx:         ctx,
		info:        replacementInfo,
		accumulator: openairelay.NewResponsesEventAccumulator(),
	}
	require.True(t, session.activateTurn(replacement, replacementTurn))
	require.NoError(t, replacementPeer.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.created","response":{"id":"resp_2"}}`)))
	_, data, err = clientPeer.ReadMessage()
	require.NoError(t, err)
	require.JSONEq(t, `{"type":"response.created","response":{"id":"resp_2"}}`, string(data))

	session.close()
	session.wg.Wait()
}
