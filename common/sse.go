package common

import (
	"bytes"
	"mime"
	"strings"
)

// IsEventStreamContentType reports whether a Content-Type header denotes SSE.
// It accepts media types such as "text/event-stream" and
// "text/event-stream; charset=utf-8", case-insensitively.
func IsEventStreamContentType(contentType string) bool {
	contentType = strings.TrimSpace(contentType)
	if contentType == "" {
		return false
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err == nil && mediaType != "" {
		return strings.EqualFold(mediaType, "text/event-stream")
	}
	return strings.Contains(strings.ToLower(contentType), "text/event-stream")
}

// LooksLikeSSEBody reports whether a response body appears to be SSE rather
// than JSON. Used when upstream omits or rewrites Content-Type but still
// returns an event-stream payload (e.g. starting with "event:" or "data:").
func LooksLikeSSEBody(body []byte) bool {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) >= 3 && trimmed[0] == 0xEF && trimmed[1] == 0xBB && trimmed[2] == 0xBF {
		trimmed = bytes.TrimSpace(trimmed[3:])
	}
	if len(trimmed) == 0 {
		return false
	}
	switch {
	case bytes.HasPrefix(trimmed, []byte("event:")),
		bytes.HasPrefix(trimmed, []byte("data:")),
		bytes.HasPrefix(trimmed, []byte("id:")),
		bytes.HasPrefix(trimmed, []byte("retry:")):
		return true
	default:
		return false
	}
}

// PreviewUpstreamBody returns a single-line, length-capped preview of an
// upstream body for operator logs.
func PreviewUpstreamBody(body []byte, max int) string {
	if max <= 0 {
		max = 256
	}
	preview := strings.ReplaceAll(truncateUTF8(string(body), max), "\n", `\n`)
	preview = strings.ReplaceAll(preview, "\r", `\r`)
	if len(body) > max {
		return preview + "...(truncated)"
	}
	return preview
}
