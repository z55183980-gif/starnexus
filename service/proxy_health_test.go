package service

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

func TestIsRealProxyFailureError(t *testing.T) {
	if IsRealProxyFailureError(context.Canceled) {
		t.Fatal("client cancellation must not be classified as a proxy failure")
	}
	if IsRealProxyFailureError(errors.New("invalid request body")) {
		t.Fatal("local request errors must not be classified as a proxy failure")
	}
	if !IsRealProxyFailureError(context.DeadlineExceeded) {
		t.Fatal("deadline exceeded must be classified as a proxy failure")
	}
	if !IsRealProxyFailureError(errors.New("socks connect tcp 127.0.0.1:1337: i/o timeout")) {
		t.Fatal("proxy timeout must be classified as a proxy failure")
	}
	var netErr net.Error = &proxyTimeoutError{}
	if !IsRealProxyFailureError(netErr) {
		t.Fatal("network errors must be classified as a proxy failure")
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

type proxyTimeoutError struct{}

func (*proxyTimeoutError) Error() string   { return "proxy timeout" }
func (*proxyTimeoutError) Timeout() bool   { return true }
func (*proxyTimeoutError) Temporary() bool { return true }
