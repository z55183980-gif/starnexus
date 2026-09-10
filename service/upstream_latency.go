package service

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
)

type latencyMetadataKey struct{}

// WithUpstreamLatencyMetadata associates attempts without changing cancellation semantics.
func WithUpstreamLatencyMetadata(req *http.Request, requestID string, channel, user, proxy int, model string, start time.Time) *http.Request {
	if os.Getenv("UPSTREAM_LATENCY_TRACE") != "1" {
		return req
	}
	ctx := context.WithValue(req.Context(), latencyMetadataKey{}, map[string]any{"request_id": requestID, "channel_id": channel, "user_id": user, "proxy_id": proxy, "model": model, "pre_upstream_ms": time.Since(start).Milliseconds()})
	return req.WithContext(ctx)
}

type latencyTraceTransport struct {
	base     http.RoundTripper
	proxyURL string
}

func wrapLatencyTransport(base http.RoundTripper, proxyURL string) http.RoundTripper {
	if os.Getenv("UPSTREAM_LATENCY_TRACE") != "1" {
		return base
	}
	return &latencyTraceTransport{base: base, proxyURL: proxyURL}
}
func (t *latencyTraceTransport) CloseIdleConnections() {
	if c, ok := t.base.(interface{ CloseIdleConnections() }); ok {
		c.CloseIdleConnections()
	}
}
func (t *latencyTraceTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if until, err := time.Parse(time.RFC3339, os.Getenv("UPSTREAM_LATENCY_TRACE_UNTIL")); err == nil && time.Now().After(until) {
		return t.base.RoundTrip(req)
	}
	start := time.Now()
	fields := map[string]any{"schema": 2, "started_at": start.UTC().Format(time.RFC3339Nano), "target": req.URL.Host, "node": os.Getenv("NODE_NAME")}
	if meta, ok := req.Context().Value(latencyMetadataKey{}).(map[string]any); ok {
		for k, v := range meta {
			fields[k] = v
		}
	}
	proxyURL := t.proxyURL
	if proxyURL == "" {
		if tr, ok := t.base.(*http.Transport); ok && tr.Proxy != nil {
			if u, e := tr.Proxy(req); e == nil && u != nil {
				proxyURL = u.String()
			}
		}
	}
	if u, e := url.Parse(proxyURL); e == nil && u != nil {
		fields["proxy_host"] = u.Host
		fields["proxy_scheme"] = u.Scheme
	}
	var mu sync.Mutex
	events := []map[string]any{}
	event := func(name string, extra map[string]any) {
		mu.Lock()
		defer mu.Unlock()
		row := map[string]any{"name": name, "at_ms": float64(time.Since(start).Microseconds()) / 1000}
		for k, v := range extra {
			row[k] = v
		}
		events = append(events, row)
	}
	trace := &httptrace.ClientTrace{
		DNSStart:     func(httptrace.DNSStartInfo) { event("dns_start", nil) },
		DNSDone:      func(i httptrace.DNSDoneInfo) { event("dns_done", map[string]any{"failed": i.Err != nil}) },
		ConnectStart: func(n, a string) { event("tcp_start", map[string]any{"network": n, "address": a}) },
		ConnectDone: func(n, a string, e error) {
			event("tcp_done", map[string]any{"network": n, "address": a, "failed": e != nil})
		},
		TLSHandshakeStart: func() { event("tls_start", nil) },
		TLSHandshakeDone:  func(_ tls.ConnectionState, e error) { event("tls_done", map[string]any{"failed": e != nil}) },
		GotConn: func(i httptrace.GotConnInfo) {
			event("got_conn", map[string]any{"reused": i.Reused, "idle_ms": i.IdleTime.Milliseconds()})
		},
		WroteRequest:         func(i httptrace.WroteRequestInfo) { event("wrote_request", map[string]any{"failed": i.Err != nil}) },
		GotFirstResponseByte: func() { event("first_response_byte", nil) },
	}
	emit := func(phase string, e error) {
		mu.Lock()
		defer mu.Unlock()
		record := map[string]any{}
		for k, v := range fields {
			record[k] = v
		}
		record["phase"] = phase
		record["elapsed_ms"] = float64(time.Since(start).Microseconds()) / 1000
		record["events"] = events
		if e != nil {
			record["error_type"] = fmt.Sprintf("%T", e)
		}
		if b, err := common.Marshal(record); err == nil {
			logger.LogInfo(req.Context(), "upstream_latency "+string(b))
		}
	}
	resp, err := t.base.RoundTrip(req.WithContext(httptrace.WithClientTrace(req.Context(), trace)))
	if resp != nil {
		fields["status"] = resp.StatusCode
		fields["protocol"] = resp.Proto
	}
	emit("headers", err)
	if err == nil && resp != nil && resp.Body != nil {
		resp.Body = &latencyResponseBody{ReadCloser: resp.Body, event: event, emit: emit}
	}
	return resp, err
}

type latencyResponseBody struct {
	io.ReadCloser
	once  sync.Once
	first sync.Once
	event func(string, map[string]any)
	emit  func(string, error)
}

func (b *latencyResponseBody) Read(p []byte) (int, error) {
	n, e := b.ReadCloser.Read(p)
	if n > 0 {
		b.first.Do(func() { b.event("first_body_read", nil) })
	}
	if e != nil {
		b.once.Do(func() {
			if e == io.EOF {
				b.emit("body_eof", nil)
			} else {
				b.emit("body_error", e)
			}
		})
	}
	return n, e
}
func (b *latencyResponseBody) Close() error {
	e := b.ReadCloser.Close()
	b.once.Do(func() { b.emit("body_closed", e) })
	return e
}
