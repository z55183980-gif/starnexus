package service

import (
	"context"
	"errors"
	"net"
	"net/url"
	"testing"
	"time"
)

func TestIsExplicitProxyFailureError(t *testing.T) {
	if IsExplicitProxyFailureError(context.Canceled) {
		t.Fatal("client cancellation must not be classified as a proxy failure")
	}
	if IsExplicitProxyFailureError(errors.New("invalid request body")) {
		t.Fatal("local request errors must not be classified as a proxy failure")
	}
	if IsExplicitProxyFailureError(context.DeadlineExceeded) {
		t.Fatal("generic deadline must not be classified as a proxy failure")
	}
	if IsExplicitProxyFailureError(errors.New("read tcp 127.0.0.1:1337: i/o timeout")) {
		t.Fatal("post-connect read timeout must not be classified as a proxy failure")
	}
	if !IsExplicitProxyFailureError(MarkExplicitSOCKSProxyFailure(&net.OpError{Op: "socks connect", Err: &net.OpError{Op: "dial", Err: errors.New("connection refused")}})) {
		t.Fatal("explicit proxy dial timeout must be classified as a proxy failure")
	}
}

func TestProxyFailureResponsibility(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		want bool
	}{
		{"proxy TCP timeout", MarkExplicitSOCKSProxyFailure(&net.OpError{Op: "socks connect", Err: &net.OpError{Op: "dial", Err: context.DeadlineExceeded}}), true},
		{"proxy authentication rejected", MarkExplicitSOCKSProxyFailure(&net.OpError{Op: "socks connect", Err: errors.New("username/password authentication failed")}), true},
		{"SOCKS target unreachable", MarkExplicitSOCKSProxyFailure(&net.OpError{Op: "socks connect", Err: errors.New("unknown error host unreachable")}), false},
		{"SOCKS handshake read timeout", MarkExplicitSOCKSProxyFailure(&net.OpError{Op: "socks connect", Err: &net.OpError{Op: "read", Err: context.DeadlineExceeded}}), false},
		{"HTTP proxy dial", &net.OpError{Op: "proxyconnect", Err: &net.OpError{Op: "dial", Err: errors.New("connection refused")}}, true},
		{"HTTP CONNECT read", &net.OpError{Op: "proxyconnect", Err: &net.OpError{Op: "read", Err: context.DeadlineExceeded}}, false},
		{"production TCP read timeout", &net.OpError{Op: "read", Err: errors.New("connection timed out")}, false},
		{"production HTTP2 error", errors.New("http2: stream ID 3; PROTOCOL_ERROR; received from peer"), false},
		{"text cannot establish ownership", errors.New("proxy dial failed: timeout"), false},
		{"canceled proxy dial", MarkExplicitSOCKSProxyFailure(&net.OpError{Op: "socks connect", Err: context.Canceled}), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := &url.Error{Op: "Post", URL: "https://upstream.example/responses", Err: test.err}
			if got := IsExplicitProxyFailureError(err); got != test.want {
				t.Fatalf("got %v, want %v: %v", got, test.want, err)
			}
		})
	}
}

func TestClassifyProxyFailureRedactsProxyCredentials(t *testing.T) {
	metadata := ClassifyProxyFailure(
		errors.New("socks connect tcp 10.0.0.1:1337: i/o timeout"),
		"socks5://user:secret@10.0.0.1:1337",
		10*time.Second,
		"gpt-test",
	)
	if metadata.ProxyErrorClass != "proxy_connect_timeout" || metadata.ProxyErrorStage != "socks5_dial" {
		t.Fatalf("unexpected classification: %+v", metadata)
	}
	if metadata.ProxyEndpoint != "10.0.0.1:1337" {
		t.Fatalf("unexpected endpoint: %q", metadata.ProxyEndpoint)
	}
}
