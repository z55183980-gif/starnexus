package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestTurnstileCheck(t *testing.T) {
	gin.SetMode(gin.TestMode)

	origEnabled := common.TurnstileCheckEnabled
	origSecret := common.TurnstileSecretKey
	origClient := turnstileHTTPClient
	t.Cleanup(func() {
		common.TurnstileCheckEnabled = origEnabled
		common.TurnstileSecretKey = origSecret
		turnstileHTTPClient = origClient
	})

	var (
		mu          sync.Mutex
		spentTokens = map[string]bool{}
		lastForm    url.Values
	)

	turnstileHTTPClient = &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if err := r.ParseForm(); err != nil {
				return &http.Response{
					StatusCode: http.StatusBadRequest,
					Body:       io.NopCloser(strings.NewReader("")),
					Header:     make(http.Header),
				}, nil
			}
			mu.Lock()
			lastForm = r.PostForm
			secret := r.PostFormValue("secret")
			token := r.PostFormValue("response")
			alreadySpent := spentTokens[token]
			if secret == "valid-secret" && token != "" && token != "bad-token" && !alreadySpent {
				spentTokens[token] = true
				mu.Unlock()
				h := make(http.Header)
				h.Set("Content-Type", "application/json")
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     h,
					Body:       io.NopCloser(strings.NewReader(`{"success":true}`)),
				}, nil
			}
			mu.Unlock()
			h := make(http.Header)
			h.Set("Content-Type", "application/json")
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     h,
				Body:       io.NopCloser(strings.NewReader(`{"success":false,"error-codes":["timeout-or-duplicate"]}`)),
			}, nil
		}),
	}

	common.TurnstileCheckEnabled = true
	common.TurnstileSecretKey = "valid-secret"

	r := gin.New()
	r.POST("/protected", TurnstileCheck(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"success": true, "passed": true})
	})

	// 1. Missing token is rejected
	w1 := httptest.NewRecorder()
	req1, _ := http.NewRequest(http.MethodPost, "/protected", nil)
	r.ServeHTTP(w1, req1)
	var body1 map[string]any
	if err := common.Unmarshal(w1.Body.Bytes(), &body1); err != nil {
		t.Fatalf("unmarshal w1: %v", err)
	}
	if body1["success"] != false {
		t.Fatalf("expected missing token to fail, got %v", body1)
	}

	// 2. Valid token passes and does NOT send remoteip
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest(http.MethodPost, "/protected?turnstile=tok-1", nil)
	req2.RemoteAddr = "203.0.113.9:12345"
	r.ServeHTTP(w2, req2)
	var body2 map[string]any
	if err := common.Unmarshal(w2.Body.Bytes(), &body2); err != nil {
		t.Fatalf("unmarshal w2: %v", err)
	}
	if body2["passed"] != true {
		t.Fatalf("expected valid token to pass, got %v", body2)
	}
	mu.Lock()
	if _, hasRemoteIP := lastForm["remoteip"]; hasRemoteIP {
		mu.Unlock()
		t.Fatalf("expected remoteip to be omitted for split-tunnel compatibility, got %v", lastForm)
	}
	mu.Unlock()

	// 3. Replaying the same spent token fails (single-use enforcement, no session bypass)
	w3 := httptest.NewRecorder()
	req3, _ := http.NewRequest(http.MethodPost, "/protected?turnstile=tok-1", nil)
	r.ServeHTTP(w3, req3)
	var body3 map[string]any
	if err := common.Unmarshal(w3.Body.Bytes(), &body3); err != nil {
		t.Fatalf("unmarshal w3: %v", err)
	}
	if body3["success"] != false {
		t.Fatalf("expected replayed token to fail, got %v", body3)
	}

	// 4. Fresh second token (e.g. after frontend resetTurnstile) succeeds
	w4 := httptest.NewRecorder()
	req4, _ := http.NewRequest(http.MethodPost, "/protected?turnstile=tok-2", nil)
	r.ServeHTTP(w4, req4)
	var body4 map[string]any
	if err := common.Unmarshal(w4.Body.Bytes(), &body4); err != nil {
		t.Fatalf("unmarshal w4: %v", err)
	}
	if body4["passed"] != true {
		t.Fatalf("expected fresh second token to pass, got %v", body4)
	}
}
