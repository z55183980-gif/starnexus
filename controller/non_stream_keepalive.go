package controller

import (
	"net/http"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

const nonStreamKeepAliveHeader = "X-New-Api-Non-Stream-Keepalive"

// nonStreamKeepAliveWriter serializes heartbeat and application writes. Once
// the application starts its body, no later heartbeat can split the JSON.
type nonStreamKeepAliveWriter struct {
	gin.ResponseWriter

	mu               sync.Mutex
	header           http.Header
	applicationBody  bool
	keepAliveStarted bool
}

func (w *nonStreamKeepAliveWriter) Header() http.Header {
	return w.header
}

func replaceHeader(destination, source http.Header) {
	for key := range destination {
		destination.Del(key)
	}
	for key, values := range source {
		destination[key] = append([]string(nil), values...)
	}
}

func (w *nonStreamKeepAliveWriter) commitApplicationHeader() {
	if !w.keepAliveStarted {
		replaceHeader(w.ResponseWriter.Header(), w.header)
	}
}

func (w *nonStreamKeepAliveWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.applicationBody = true
	w.commitApplicationHeader()
	return w.ResponseWriter.Write(data)
}

func (w *nonStreamKeepAliveWriter) WriteString(data string) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.applicationBody = true
	w.commitApplicationHeader()
	return w.ResponseWriter.WriteString(data)
}

func (w *nonStreamKeepAliveWriter) WriteHeader(code int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.keepAliveStarted {
		// The first heartbeat necessarily commits HTTP 200. Preserve the JSON
		// error body if a later upstream failure can no longer change its status.
		return
	}
	w.applicationBody = true
	w.commitApplicationHeader()
	w.ResponseWriter.WriteHeader(code)
}

func (w *nonStreamKeepAliveWriter) WriteHeaderNow() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.applicationBody = true
	w.commitApplicationHeader()
	w.ResponseWriter.WriteHeaderNow()
}

func (w *nonStreamKeepAliveWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.applicationBody = true
	w.commitApplicationHeader()
	w.ResponseWriter.Flush()
}

func (w *nonStreamKeepAliveWriter) writeKeepAlive() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.applicationBody {
		return nil
	}
	if !w.keepAliveStarted {
		header := w.ResponseWriter.Header()
		header.Set("Content-Type", "application/json")
		header.Set("Cache-Control", "no-cache, no-transform")
		header.Set("X-Accel-Buffering", "no")
		header.Set(nonStreamKeepAliveHeader, "active")
		header.Del("Content-Length")
		w.keepAliveStarted = true
	}
	if _, err := w.ResponseWriter.WriteString("\n"); err != nil {
		return err
	}
	w.ResponseWriter.Flush()
	return nil
}

func supportsNonStreamJSONKeepAlive(format types.RelayFormat) bool {
	switch format {
	case types.RelayFormatOpenAI,
		types.RelayFormatOpenAIResponses,
		types.RelayFormatOpenAIResponsesCompaction,
		types.RelayFormatOpenAIAlphaSearch,
		types.RelayFormatOpenAIImage:
		return true
	default:
		return false
	}
}

func startNonStreamJSONKeepAlive(c *gin.Context, format types.RelayFormat, request dto.Request) func() {
	if !constant.NonStreamKeepAliveEnabled || c == nil || c.Writer == nil || request == nil ||
		request.IsStream(c) || !supportsNonStreamJSONKeepAlive(format) {
		return func() {}
	}
	delay := time.Duration(constant.NonStreamKeepAliveDelaySeconds) * time.Second
	interval := time.Duration(constant.NonStreamKeepAliveIntervalSeconds) * time.Second
	if delay <= 0 || interval <= 0 {
		return func() {}
	}
	return startNonStreamJSONKeepAliveWithIntervals(c, delay, interval)
}

func startNonStreamJSONKeepAliveWithIntervals(c *gin.Context, delay, interval time.Duration) func() {
	originalWriter := c.Writer
	writer := &nonStreamKeepAliveWriter{
		ResponseWriter: originalWriter,
		header:         originalWriter.Header().Clone(),
	}
	c.Writer = writer

	stop := make(chan struct{})
	done := make(chan struct{})
	var stopOnce sync.Once
	go func() {
		defer close(done)
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-stop:
			return
		case <-c.Request.Context().Done():
			return
		}

		if err := writer.writeKeepAlive(); err != nil {
			return
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := writer.writeKeepAlive(); err != nil {
					return
				}
			case <-stop:
				return
			case <-c.Request.Context().Done():
				return
			}
		}
	}()

	return func() {
		stopOnce.Do(func() { close(stop) })
		<-done
	}
}

var _ http.Flusher = (*nonStreamKeepAliveWriter)(nil)
