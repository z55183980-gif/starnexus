package openai

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type streamFlushRecorder struct {
	header  http.Header
	body    bytes.Buffer
	mu      sync.Mutex
	flushCh chan struct{}
}

func newStreamFlushRecorder() *streamFlushRecorder {
	return &streamFlushRecorder{
		header:  make(http.Header),
		flushCh: make(chan struct{}, 8),
	}
}

func (r *streamFlushRecorder) Header() http.Header {
	return r.header
}

func (r *streamFlushRecorder) WriteHeader(_ int) {}

func (r *streamFlushRecorder) Write(data []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.body.Write(data)
}

func (r *streamFlushRecorder) Flush() {
	select {
	case r.flushCh <- struct{}{}:
	default:
	}
}

func (r *streamFlushRecorder) BodyString() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.body.String()
}

func newOpenAIStreamTestContext(recorder http.ResponseWriter) *gin.Context {
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	return c
}

func setStreamTestTimeout(t *testing.T) {
	t.Helper()
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() {
		constant.StreamingTimeout = oldTimeout
	})
}

func TestResponsesCreatedFlushesChatStartBeforeNextUpstreamEvent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setStreamTestTimeout(t)

	recorder := newStreamFlushRecorder()
	c := newOpenAIStreamTestContext(recorder)
	reader, writer := io.Pipe()
	resp := &http.Response{Body: reader}
	info := &relaycommon.RelayInfo{
		RelayMode:   relayconstant.RelayModeChatCompletions,
		RelayFormat: types.RelayFormatOpenAI,
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5.6-sol"},
	}

	done := make(chan *types.NewAPIError, 1)
	go func() {
		_, apiErr := OaiResponsesToChatStreamHandler(c, info, resp)
		done <- apiErr
	}()

	_, err := io.WriteString(writer, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"model\":\"gpt-5.6-sol\",\"created_at\":123}}\n\n")
	require.NoError(t, err)

	flushedBeforeNextEvent := false
	select {
	case <-recorder.flushCh:
		flushedBeforeNextEvent = true
	case <-time.After(time.Second):
	}

	_, err = io.WriteString(writer, "data: {\"type\":\"response.completed\",\"response\":{\"model\":\"gpt-5.6-sol\",\"created_at\":123,\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}\n\ndata: [DONE]\n\n")
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	select {
	case apiErr := <-done:
		require.Nil(t, apiErr)
	case <-time.After(2 * time.Second):
		t.Fatal("responses-to-chat stream handler did not finish")
	}

	require.True(t, flushedBeforeNextEvent, "response.created must flush the Chat start chunk without waiting for another upstream event")
	require.Contains(t, recorder.BodyString(), `"role":"assistant"`)
}

func TestOpenAIStreamFlushesCurrentFrameWithoutWaitingForNextFrame(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setStreamTestTimeout(t)

	recorder := newStreamFlushRecorder()
	c := newOpenAIStreamTestContext(recorder)
	reader, writer := io.Pipe()
	resp := &http.Response{Body: reader}
	info := &relaycommon.RelayInfo{
		RelayMode:   relayconstant.RelayModeChatCompletions,
		RelayFormat: types.RelayFormatOpenAI,
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5.6-sol"},
	}

	done := make(chan *types.NewAPIError, 1)
	go func() {
		_, apiErr := OaiStreamHandler(c, info, resp)
		done <- apiErr
	}()

	firstFrame := `{"id":"chatcmpl-upstream","object":"chat.completion.chunk","created":123,"model":"gpt-5.6-sol","choices":[{"index":0,"delta":{"role":"assistant","content":""}}]}`
	_, err := io.WriteString(writer, "data: "+firstFrame+"\n\n")
	require.NoError(t, err)

	flushedBeforeNextFrame := false
	select {
	case <-recorder.flushCh:
		flushedBeforeNextFrame = true
	case <-time.After(time.Second):
	}

	usageOnlyFrame := `{"id":"chatcmpl-upstream","object":"chat.completion.chunk","created":123,"model":"gpt-5.6-sol","choices":[],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`
	_, err = io.WriteString(writer, "data: "+usageOnlyFrame+"\n\ndata: [DONE]\n\n")
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	select {
	case apiErr := <-done:
		require.Nil(t, apiErr)
	case <-time.After(2 * time.Second):
		t.Fatal("OpenAI stream handler did not finish")
	}

	body := recorder.BodyString()
	require.True(t, flushedBeforeNextFrame, "the current OpenAI frame must be flushed without waiting for its successor")
	require.Contains(t, body, firstFrame)
	require.NotContains(t, body, usageOnlyFrame)
	require.True(t, strings.HasSuffix(body, "data: [DONE]\n\n"))
}

func TestShouldSendOpenAIStreamData(t *testing.T) {
	usageOnly := `{"choices":[],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`
	usageWithContent := `{"choices":[{"delta":{"content":"x"}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`
	normalFrame := `{"choices":[{"delta":{"content":"x"}}]}`

	baseInfo := &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeChatCompletions}
	require.False(t, shouldSendOpenAIStreamData(usageOnly, baseInfo))
	require.True(t, shouldSendOpenAIStreamData(usageWithContent, baseInfo))
	require.True(t, shouldSendOpenAIStreamData(normalFrame, baseInfo))
	require.True(t, shouldSendOpenAIStreamData(usageOnly, &relaycommon.RelayInfo{
		RelayMode:          relayconstant.RelayModeChatCompletions,
		ShouldIncludeUsage: true,
	}))
	require.False(t, shouldSendOpenAIStreamData(usageOnly, &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeCompletions,
	}))
}
