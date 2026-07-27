package helper

import (
	"bufio"
	"context"
	"errors"
	"io"
	"strings"
	"time"
)

var ErrSSEIdleTimeout = errors.New("SSE stream idle timeout")

// ScanSSEData reads SSE data fields without writing HTTP response headers or
// keepalive frames. It is intended for protocol bridges whose downstream
// transport is not SSE, such as a persistent WebSocket client connection.
func ScanSSEData(ctx context.Context, reader io.Reader, handler func(data []byte) (bool, error)) (bool, error) {
	return scanSSEData(ctx, reader, 0, handler)
}

func ScanSSEDataWithIdleTimeout(ctx context.Context, reader io.Reader, idleTimeout time.Duration, handler func(data []byte) (bool, error)) (bool, error) {
	return scanSSEData(ctx, reader, idleTimeout, handler)
}

type sseScanResult struct {
	line string
	err  error
	done bool
}

func scanSSEData(ctx context.Context, reader io.Reader, idleTimeout time.Duration, handler func(data []byte) (bool, error)) (bool, error) {
	if reader == nil || handler == nil {
		return false, nil
	}

	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, InitialScannerBufferSize), getScannerBufferSize())
	results := make(chan sseScanResult)
	scanCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		for scanner.Scan() {
			select {
			case results <- sseScanResult{line: scanner.Text()}:
			case <-scanCtx.Done():
				return
			}
		}
		select {
		case results <- sseScanResult{err: scanner.Err(), done: true}:
		case <-scanCtx.Done():
		}
	}()

	var idleTimer *time.Timer
	var idleC <-chan time.Time
	if idleTimeout > 0 {
		idleTimer = time.NewTimer(idleTimeout)
		idleC = idleTimer.C
		defer idleTimer.Stop()
	}

	for {
		select {
		case <-ctx.Done():
			if closer, ok := reader.(io.Closer); ok {
				_ = closer.Close()
			}
			return false, ctx.Err()
		case <-idleC:
			if closer, ok := reader.(io.Closer); ok {
				_ = closer.Close()
			}
			return false, ErrSSEIdleTimeout
		case result := <-results:
			if result.done {
				return false, result.err
			}
			if idleTimer != nil {
				if !idleTimer.Stop() {
					select {
					case <-idleTimer.C:
					default:
					}
				}
				idleTimer.Reset(idleTimeout)
			}
			line := result.line
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "" {
				continue
			}
			if data == "[DONE]" {
				return true, nil
			}
			stop, err := handler([]byte(data))
			if err != nil || stop {
				return false, err
			}
		}
	}
}
