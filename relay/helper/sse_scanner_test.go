package helper

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestScanSSEDataHandlesLargeEventAndDone(t *testing.T) {
	t.Parallel()

	payload := strings.Repeat("x", InitialScannerBufferSize+1024)
	stream := "event: response.output_text.delta\n" +
		"data: " + payload + "\n\n" +
		"data: [DONE]\n\n"
	var received string
	done, err := ScanSSEData(context.Background(), strings.NewReader(stream), func(data []byte) (bool, error) {
		received = string(data)
		return false, nil
	})
	require.NoError(t, err)
	require.True(t, done)
	require.Equal(t, payload, received)
}

func TestScanSSEDataStopsAtHandlerBoundary(t *testing.T) {
	t.Parallel()

	stream := "data: first\n\ndata: second\n\n"
	var received []string
	done, err := ScanSSEData(context.Background(), strings.NewReader(stream), func(data []byte) (bool, error) {
		received = append(received, string(data))
		return true, nil
	})
	require.NoError(t, err)
	require.False(t, done)
	require.Equal(t, []string{"first"}, received)
}

func TestScanSSEDataIdleTimeoutClosesReader(t *testing.T) {
	t.Parallel()

	reader, writer := io.Pipe()
	defer writer.Close()
	_, err := ScanSSEDataWithIdleTimeout(context.Background(), reader, 20*time.Millisecond, func([]byte) (bool, error) {
		return false, nil
	})
	require.ErrorIs(t, err, ErrSSEIdleTimeout)
	_, writeErr := writer.Write([]byte("data: late\n\n"))
	require.Error(t, writeErr)
}
