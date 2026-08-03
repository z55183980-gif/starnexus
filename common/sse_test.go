package common

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsEventStreamContentType(t *testing.T) {
	t.Parallel()
	require.True(t, IsEventStreamContentType("text/event-stream"))
	require.True(t, IsEventStreamContentType("text/event-stream; charset=utf-8"))
	require.True(t, IsEventStreamContentType("Text/Event-Stream"))
	require.False(t, IsEventStreamContentType("application/json"))
	require.False(t, IsEventStreamContentType(""))
}

func TestLooksLikeSSEBody(t *testing.T) {
	t.Parallel()
	require.True(t, LooksLikeSSEBody([]byte("event: response.created\ndata: {}\n\n")))
	require.True(t, LooksLikeSSEBody([]byte("data: {\"type\":\"response.completed\"}\n\n")))
	require.True(t, LooksLikeSSEBody([]byte("\xef\xbb\xbfevent: message\n\n")))
	require.False(t, LooksLikeSSEBody([]byte(`{"id":"resp_1","object":"response"}`)))
	require.False(t, LooksLikeSSEBody([]byte("error: upstream failed")))
	require.False(t, LooksLikeSSEBody(nil))
}

func TestPreviewUpstreamBody(t *testing.T) {
	t.Parallel()
	require.Equal(t, `event: x\ndata: y`, PreviewUpstreamBody([]byte("event: x\ndata: y"), 256))
	require.Equal(t, "abc...(truncated)", PreviewUpstreamBody([]byte("abcdef"), 3))
}
