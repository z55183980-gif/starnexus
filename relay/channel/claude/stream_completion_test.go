package claude

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type terminalCancelWriter struct {
	*httptest.ResponseRecorder
	cancel context.CancelFunc
	once   sync.Once
}

func (w *terminalCancelWriter) Flush() {
	w.ResponseRecorder.Flush()
	if strings.Contains(w.Body.String(), `"type":"message_stop"`) {
		w.once.Do(w.cancel)
		// Let cancellation race the handler's terminal status update.
		time.Sleep(10 * time.Millisecond)
	}
}

// Match the two incident stop reasons while keeping the upstream HTTP body
// open after its last SSE event, as can happen behind a proxy.
func TestClaudeIncidentCompletion(t *testing.T) {
	for _, reason := range []string{"tool_use", "end_turn"} {
		for _, terminal := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/terminal=%t", reason, terminal), func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				w := &terminalCancelWriter{ResponseRecorder: httptest.NewRecorder(), cancel: cancel}
				c, _ := gin.CreateTestContext(w)
				c.Request = httptest.NewRequest("POST", "/v1/messages", nil).WithContext(ctx)
				info := relaycommon.GenRelayInfoOpenAI(c, nil)
				info.ChannelMeta = &relaycommon.ChannelMeta{}
				info.RelayFormat = types.RelayFormatClaude
				info.DisablePing = true
				reader, writer := io.Pipe()
				defer writer.Close()
				go func() {
					fmt.Fprint(writer, "data: {\"type\":\"message_start\",\"message\":{\"model\":\"claude-opus-5-5\",\"usage\":{\"input_tokens\":2,\"output_tokens\":0}}}\n\n")
					fmt.Fprintf(writer, "data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":%q},\"usage\":{\"output_tokens\":3}}\n\n", reason)
					if terminal {
						fmt.Fprint(writer, "data: {\"type\":\"message_stop\"}\n\n")
					} else {
						time.Sleep(30 * time.Millisecond)
						cancel()
					}
				}()
				start := time.Now()
				_, _ = ClaudeStreamHandler(c, &http.Response{Body: reader}, info)
				want := relaycommon.StreamEndReasonClientGone
				if terminal {
					want = relaycommon.StreamEndReasonDone
				}
				require.Equal(t, want, info.StreamStatus.EndReason, info.StreamStatus.Summary())
				require.Less(t, time.Since(start), time.Second, "must close upstream before waiting for scanner")
			})
		}
	}
}

type failedClaudeWriter struct{ *httptest.ResponseRecorder }

func (w *failedClaudeWriter) Write(p []byte) (int, error) { return 0, errors.New("broken downstream") }
func (w *failedClaudeWriter) WriteString(s string) (int, error) {
	return 0, errors.New("broken downstream")
}

func TestClaudeTerminalWriteFailure(t *testing.T) {
	c, _ := gin.CreateTestContext(&failedClaudeWriter{httptest.NewRecorder()})
	c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
	info := relaycommon.GenRelayInfoOpenAI(c, nil)
	info.ChannelMeta = &relaycommon.ChannelMeta{}
	info.RelayFormat = types.RelayFormatClaude
	err := HandleStreamResponseData(c, info, &ClaudeResponseInfo{Usage: &dto.Usage{}}, `{"type":"message_stop"}`)
	require.NotNil(t, err, "a failed terminal write must not count as completed")
}

func TestClaudeMissingTerminalRemainsIncomplete(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
	info := relaycommon.GenRelayInfoOpenAI(c, nil)
	info.ChannelMeta = &relaycommon.ChannelMeta{}
	info.RelayFormat = types.RelayFormatClaude
	info.DisablePing = true
	body := "data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":3}}\n\n"
	usage, err := ClaudeStreamHandler(c, &http.Response{Body: io.NopCloser(strings.NewReader(body))}, info)
	require.Nil(t, err)
	require.Equal(t, 3, usage.CompletionTokens, "confirmed usage must be retained")
	require.Equal(t, relaycommon.StreamEndReasonIncomplete, info.StreamStatus.EndReason)
}
