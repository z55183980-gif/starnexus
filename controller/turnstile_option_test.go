package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

func TestTurnstileOptionVisibilityAndValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)

	common.OptionMapRWMutex.Lock()
	origMap := common.OptionMap
	common.OptionMap = map[string]string{
		"TurnstileCheckEnabled":        "false",
		"TurnstileSiteKey":             "0x4AAAAAATestSiteKey",
		"TurnstileSecretKey":           "0x4AAAAAATestSecretKey",
		"TurnstileSecretKeyConfigured": "true",
	}
	common.OptionMapRWMutex.Unlock()

	origSite := common.TurnstileSiteKey
	origSecret := common.TurnstileSecretKey
	origEnabled := common.TurnstileCheckEnabled
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = origMap
		common.OptionMapRWMutex.Unlock()
		common.TurnstileSiteKey = origSite
		common.TurnstileSecretKey = origSecret
		common.TurnstileCheckEnabled = origEnabled
	})

	r := gin.New()
	r.GET("/api/option/", GetOptions)
	r.PUT("/api/option/", UpdateOption)

	// 1. GetOptions exposes TurnstileSiteKey and TurnstileSecretKeyConfigured, but hides TurnstileSecretKey
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/option/", nil)
	r.ServeHTTP(w, req)

	var resp struct {
		Success bool `json:"success"`
		Data    []struct {
			Key   string `json:"key"`
			Value string `json:"value"`
		} `json:"data"`
	}
	if err := common.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal GetOptions: %v", err)
	}
	got := map[string]string{}
	for _, item := range resp.Data {
		got[item.Key] = item.Value
	}
	if got["TurnstileSiteKey"] != "0x4AAAAAATestSiteKey" {
		t.Fatalf("expected TurnstileSiteKey to be returned, got %q", got["TurnstileSiteKey"])
	}
	if _, leaked := got["TurnstileSecretKey"]; leaked {
		t.Fatal("TurnstileSecretKey must not be returned by GetOptions")
	}
	if got["TurnstileSecretKeyConfigured"] != "true" {
		t.Fatalf("expected TurnstileSecretKeyConfigured=true, got %q", got["TurnstileSecretKeyConfigured"])
	}

	// 2. Enabling TurnstileCheckEnabled without TurnstileSecretKey must be rejected
	common.TurnstileSiteKey = "0x4AAAAAATestSiteKey"
	common.TurnstileSecretKey = ""
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest(
		http.MethodPut,
		"/api/option/",
		bytes.NewBufferString(`{"key":"TurnstileCheckEnabled","value":true}`),
	)
	req2.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w2, req2)

	var resp2 map[string]any
	if err := common.Unmarshal(w2.Body.Bytes(), &resp2); err != nil {
		t.Fatalf("unmarshal UpdateOption: %v", err)
	}
	if resp2["success"] != false {
		t.Fatalf("expected TurnstileCheckEnabled=true with empty SecretKey to fail, got %v", resp2)
	}
}
