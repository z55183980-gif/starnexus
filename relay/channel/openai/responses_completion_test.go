package openai

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type responsesCancelOnFlush struct {
	*httptest.ResponseRecorder
	cancel context.CancelFunc
}

func (w *responsesCancelOnFlush) Flush() {
	w.ResponseRecorder.Flush()
	if strings.Contains(w.Body.String(), "response.completed") {
		w.cancel()
		time.Sleep(10 * time.Millisecond)
	}
}

func TestResponsesCompletionBeforeUpstreamEOF(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := &responsesCancelOnFlush{httptest.NewRecorder(), cancel}
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil).WithContext(ctx)
	info := relaycommon.GenRelayInfoOpenAI(c, nil)
	info.ChannelMeta = &relaycommon.ChannelMeta{}
	info.RelayFormat = types.RelayFormatOpenAIResponses
	info.DisablePing = true
	r, writer := io.Pipe()
	defer writer.Close()
	go func() {
		fmt.Fprint(writer, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_complete\",\"status\":\"completed\",\"output\":[],\"usage\":{\"input_tokens\":2,\"output_tokens\":3,\"total_tokens\":5}}}\n\n")
	}()
	start := time.Now()
	usage, apiErr := OaiResponsesStreamHandler(c, info, &http.Response{Body: r})
	require.Nil(t, apiErr)
	require.Equal(t, 3, usage.CompletionTokens)
	require.Equal(t, relaycommon.StreamEndReasonDone, info.StreamStatus.EndReason)
	require.Less(t, time.Since(start), time.Second)
}
