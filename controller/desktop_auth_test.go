package controller

import (
	"context"
	"errors"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type testDesktopStore struct {
	mu     sync.Mutex
	values map[string]string
}

func (s *testDesktopStore) Save(_ context.Context, k, v string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[k] = v
	return nil
}
func (s *testDesktopStore) Get(_ context.Context, k string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.values[k]
	if !ok {
		return "", errors.New("expired")
	}
	return v, nil
}
func (s *testDesktopStore) Consume(_ context.Context, k, v string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.values[k] != v {
		return false, nil
	}
	delete(s.values, k)
	return true, nil
}
func TestDesktopPKCEAndSingleUse(t *testing.T) {
	s := &testDesktopStore{values: map[string]string{}}
	ctx := context.Background()
	verifier := strings.Repeat("v", 64)
	code, err := issueDesktopGrant(ctx, s, 7, desktopHash(verifier))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = consumeDesktopGrant(ctx, s, code, strings.Repeat("x", 64)); err == nil {
		t.Fatal("wrong verifier accepted")
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	success := 0
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			uid, err := consumeDesktopGrant(ctx, s, code, verifier)
			if err == nil {
				if uid != 7 {
					t.Error("wrong user")
				}
				mu.Lock()
				success++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if success != 1 {
		t.Fatalf("expected exactly one exchange, got %d", success)
	}
	if _, err = consumeDesktopGrant(ctx, s, code, verifier); err == nil {
		t.Fatal("replayed code accepted")
	}
	code, _ = issueDesktopGrant(ctx, s, 7, desktopHash(verifier))
	s.values = map[string]string{}
	if _, err = consumeDesktopGrant(ctx, s, code, verifier); err == nil {
		t.Fatal("expired code accepted")
	}
	if _, err = issueDesktopGrant(ctx, s, 7, "invalid"); err == nil {
		t.Fatal("invalid challenge accepted")
	}
	if _, err = issueDesktopGrant(ctx, s, 0, desktopHash(verifier)); err == nil {
		t.Fatal("anonymous grant accepted")
	}
}
func TestDesktopRejectsCrossOrigin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, handler := range []gin.HandlerFunc{DesktopLoginAuthorize, DesktopLoginExchange} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{}`))
		c.Request.Header.Set("Origin", "https://evil.example")
		handler(c)
		if w.Code != http.StatusForbidden {
			t.Fatalf("cross-origin request returned %d", w.Code)
		}
	}
}
func TestDesktopLoginPageHasFixedCallbackAndNoCache(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/desktop-login?state=<script>", nil)
	DesktopLoginPage(c)
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Frame-Options") != "DENY" {
		t.Fatal("missing page protections")
	}
	if strings.Contains(w.Body.String(), "state=<script>") {
		t.Fatal("query reflected into HTML")
	}
	if desktopCallback != "com.dkby.starnexus.keytool://auth/callback" {
		t.Fatal("unexpected callback")
	}
}
