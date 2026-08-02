package helper

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/dto"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type failingResponsesHTTPWriter struct {
	header http.Header
}

func (writer *failingResponsesHTTPWriter) Header() http.Header {
	return writer.header
}

func (writer *failingResponsesHTTPWriter) Write([]byte) (int, error) {
	return 0, errors.New("downstream write failed")
}

func (writer *failingResponsesHTTPWriter) WriteHeader(int) {}

func TestResponseChunkDataPropagatesDownstreamWriteFailure(t *testing.T) {
	writer := &failingResponsesHTTPWriter{header: make(http.Header)}
	ctx, _ := gin.CreateTestContext(writer)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	err := ResponseChunkData(ctx, dto.ResponsesStreamResponse{Type: "response.output_item.done"}, `{"type":"response.output_item.done"}`)
	require.Error(t, err)
	require.Contains(t, err.Error(), "downstream write failed")
}
