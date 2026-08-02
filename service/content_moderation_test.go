package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

func TestEvaluateContentModerationScores(t *testing.T) {
	thresholds := setting.ContentModerationDefaultThresholds()
	scores := map[string]float64{
		"sexual":     0.9,
		"harassment": 0.1,
	}
	flagged, category, score, hits := evaluateContentModerationScores(scores, thresholds)
	if !flagged {
		t.Fatal("expected flagged")
	}
	if category != "sexual" {
		t.Fatalf("unexpected category: %s", category)
	}
	if score < 0.9 {
		t.Fatalf("unexpected score: %v", score)
	}
	if len(hits) == 0 || hits[0] != "moderation:sexual" {
		t.Fatalf("unexpected hits: %#v", hits)
	}
}

func TestEvaluateContentModerationScoresClean(t *testing.T) {
	thresholds := setting.ContentModerationDefaultThresholds()
	scores := map[string]float64{
		"sexual": 0.01,
		"hate":   0.01,
	}
	flagged, _, _, hits := evaluateContentModerationScores(scores, thresholds)
	if flagged {
		t.Fatal("expected clean")
	}
	if len(hits) != 0 {
		t.Fatalf("unexpected hits: %#v", hits)
	}
}

func TestApplyContentModerationDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	req := &dto.GeneralOpenAIRequest{
		Messages: []dto.Message{{Role: "user", Content: "hello"}},
	}
	if err := ApplyContentModeration(c, req, types.RelayFormatOpenAI, "gpt-test"); err != nil {
		t.Fatalf("expected nil when disabled, got %v", err)
	}
}

func TestCallContentModerationOnceFlaggedAndFailOpen(t *testing.T) {
	gin.SetMode(gin.TestMode)

	flaggedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/moderations" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{{
				"flagged": true,
				"category_scores": map[string]float64{
					"sexual": 0.99,
				},
			}},
		})
	}))
	defer flaggedServer.Close()

	cfg := setting.ContentModerationConfig{
		Enabled:    true,
		BaseURL:    flaggedServer.URL,
		Model:      "omni-moderation-latest",
		APIKeys:    []string{"sk-test"},
		TimeoutMS:  3000,
		AllGroups:  true,
		Thresholds: setting.ContentModerationDefaultThresholds(),
	}
	result, err := callContentModerationAPI(context.Background(), cfg, "bad prompt")
	if err != nil {
		t.Fatal(err)
	}
	flagged, category, _, _ := evaluateContentModerationScores(result.CategoryScores, cfg.Thresholds)
	if !flagged || category != "sexual" {
		t.Fatalf("unexpected evaluation: flagged=%v category=%s", flagged, category)
	}

	errorServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer errorServer.Close()

	cfg.BaseURL = errorServer.URL
	_, err = callContentModerationAPI(context.Background(), cfg, "prompt")
	if err == nil {
		t.Fatal("expected api error")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestApplyContentModerationBlocksWhenFlagged(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{{
				"flagged": true,
				"category_scores": map[string]float64{
					"sexual": 0.99,
				},
			}},
		})
	}))
	defer server.Close()

	original := setting.GetContentModerationConfig()
	defer func() {
		_ = setting.UpdateContentModerationConfigByJsonString(setting.ContentModerationConfigJSON(original))
	}()

	cfg := setting.ContentModerationConfig{
		Enabled:    true,
		Mode:       setting.ContentModerationModePreBlock,
		BaseURL:    server.URL,
		Model:      "omni-moderation-latest",
		APIKeys:    []string{"sk-test"},
		TimeoutMS:  3000,
		AllGroups:  true,
		Thresholds: setting.ContentModerationDefaultThresholds(),
	}
	if err := setting.UpdateContentModerationConfigByJsonString(setting.ContentModerationConfigJSON(cfg)); err != nil {
		t.Fatal(err)
	}

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req := &dto.GeneralOpenAIRequest{
		Messages: []dto.Message{{Role: "user", Content: "flag this"}},
	}
	apiErr := ApplyContentModeration(c, req, types.RelayFormatOpenAI, "gpt-test")
	if apiErr == nil {
		t.Fatal("expected block error")
	}
	if apiErr.GetErrorCode() != types.ErrorCodePromptBlocked {
		t.Fatalf("unexpected code: %v", apiErr.GetErrorCode())
	}
}

func TestApplyContentModerationSkipsOutOfScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{{
				"flagged": true,
				"category_scores": map[string]float64{
					"sexual": 0.99,
				},
			}},
		})
	}))
	defer server.Close()

	original := setting.GetContentModerationConfig()
	defer func() {
		_ = setting.UpdateContentModerationConfigByJsonString(setting.ContentModerationConfigJSON(original))
	}()

	cfg := setting.ContentModerationConfig{
		Enabled:   true,
		Mode:      setting.ContentModerationModePreBlock,
		BaseURL:   server.URL,
		Model:     "omni-moderation-latest",
		APIKeys:   []string{"sk-test"},
		TimeoutMS: 3000,
		AllGroups: false,
		Groups:    []string{"vip"},
		ModelFilter: setting.ContentModerationModelFilter{
			Type:   setting.ContentModerationModelFilterInclude,
			Models: []string{"gpt-4o"},
		},
		Thresholds: setting.ContentModerationDefaultThresholds(),
	}
	if err := setting.UpdateContentModerationConfigByJsonString(setting.ContentModerationConfigJSON(cfg)); err != nil {
		t.Fatal(err)
	}

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Set("group", "default")
	req := &dto.GeneralOpenAIRequest{
		Messages: []dto.Message{{Role: "user", Content: "flag this"}},
	}
	if apiErr := ApplyContentModeration(c, req, types.RelayFormatOpenAI, "gpt-4o"); apiErr != nil {
		t.Fatalf("out-of-group scope must allow, got %v", apiErr)
	}
	if called {
		t.Fatal("moderation api must not be called for out-of-group requests")
	}

	c.Set("group", "vip")
	if apiErr := ApplyContentModeration(c, req, types.RelayFormatOpenAI, "gpt-test"); apiErr != nil {
		t.Fatalf("out-of-model scope must allow, got %v", apiErr)
	}
	if called {
		t.Fatal("moderation api must not be called for out-of-model requests")
	}
}

func TestApplyContentModerationObserveDoesNotBlock(t *testing.T) {
	gin.SetMode(gin.TestMode)
	called := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called <- struct{}{}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{{
				"flagged": true,
				"category_scores": map[string]float64{
					"sexual": 0.99,
				},
			}},
		})
	}))
	defer server.Close()

	original := setting.GetContentModerationConfig()
	defer func() {
		_ = setting.UpdateContentModerationConfigByJsonString(setting.ContentModerationConfigJSON(original))
	}()

	cfg := setting.ContentModerationConfig{
		Enabled:    true,
		Mode:       setting.ContentModerationModeObserve,
		BaseURL:    server.URL,
		Model:      "omni-moderation-latest",
		APIKeys:    []string{"sk-test"},
		TimeoutMS:  3000,
		AllGroups:  true,
		Thresholds: setting.ContentModerationDefaultThresholds(),
	}
	if err := setting.UpdateContentModerationConfigByJsonString(setting.ContentModerationConfigJSON(cfg)); err != nil {
		t.Fatal(err)
	}

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req := &dto.GeneralOpenAIRequest{
		Messages: []dto.Message{{Role: "user", Content: "flag this"}},
	}
	if apiErr := ApplyContentModeration(c, req, types.RelayFormatOpenAI, "gpt-test"); apiErr != nil {
		t.Fatalf("observe mode must not block, got %v", apiErr)
	}

	select {
	case <-called:
	case <-time.After(2 * time.Second):
		t.Fatal("expected observe mode to call moderation api")
	}
}

func TestApplyContentModerationFailOpenOnAPIError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusBadGateway)
	}))
	defer server.Close()

	original := setting.GetContentModerationConfig()
	defer func() {
		_ = setting.UpdateContentModerationConfigByJsonString(setting.ContentModerationConfigJSON(original))
	}()

	cfg := setting.ContentModerationConfig{
		Enabled:    true,
		Mode:       setting.ContentModerationModePreBlock,
		BaseURL:    server.URL,
		Model:      "omni-moderation-latest",
		APIKeys:    []string{"sk-test"},
		TimeoutMS:  3000,
		AllGroups:  true,
		Thresholds: setting.ContentModerationDefaultThresholds(),
	}
	if err := setting.UpdateContentModerationConfigByJsonString(setting.ContentModerationConfigJSON(cfg)); err != nil {
		t.Fatal(err)
	}

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req := &dto.GeneralOpenAIRequest{
		Messages: []dto.Message{{Role: "user", Content: "hello"}},
	}
	if apiErr := ApplyContentModeration(c, req, types.RelayFormatOpenAI, "gpt-test"); apiErr != nil {
		t.Fatalf("expected fail-open allow, got %v", apiErr)
	}
}

func TestScheduleContentModerationObserveDropsWhenSaturated(t *testing.T) {
	gin.SetMode(gin.TestMode)

	block := make(chan struct{})
	started := make(chan struct{}, maxContentModerationObserveConcurrency)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		<-block
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{{
				"flagged":         false,
				"category_scores": map[string]float64{},
			}},
		})
	}))
	defer server.Close()
	defer close(block)

	original := setting.GetContentModerationConfig()
	defer func() {
		_ = setting.UpdateContentModerationConfigByJsonString(setting.ContentModerationConfigJSON(original))
	}()
	beforeSkipped := atomic.LoadUint64(&contentModerationObserveSkipped)

	cfg := setting.ContentModerationConfig{
		Enabled:    true,
		Mode:       setting.ContentModerationModeObserve,
		BaseURL:    server.URL,
		Model:      "omni-moderation-latest",
		APIKeys:    []string{"sk-test"},
		TimeoutMS:  3000,
		AllGroups:  true,
		Thresholds: setting.ContentModerationDefaultThresholds(),
	}
	if err := setting.UpdateContentModerationConfigByJsonString(setting.ContentModerationConfigJSON(cfg)); err != nil {
		t.Fatal(err)
	}

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req := &dto.GeneralOpenAIRequest{
		Messages: []dto.Message{{Role: "user", Content: "observe me"}},
	}

	for i := 0; i < maxContentModerationObserveConcurrency; i++ {
		if apiErr := ApplyContentModeration(c, req, types.RelayFormatOpenAI, "gpt-test"); apiErr != nil {
			t.Fatalf("observe must not block: %v", apiErr)
		}
	}
	deadline := time.After(2 * time.Second)
	for i := 0; i < maxContentModerationObserveConcurrency; i++ {
		select {
		case <-started:
		case <-deadline:
			t.Fatalf("timed out waiting for observe workers to start (%d/%d)", i, maxContentModerationObserveConcurrency)
		}
	}

	if apiErr := ApplyContentModeration(c, req, types.RelayFormatOpenAI, "gpt-test"); apiErr != nil {
		t.Fatalf("saturated observe must still allow request: %v", apiErr)
	}
	if got := atomic.LoadUint64(&contentModerationObserveSkipped); got <= beforeSkipped {
		t.Fatalf("expected skip counter to increase, before=%d after=%d", beforeSkipped, got)
	}

	select {
	case <-started:
		t.Fatal("saturated observe must not start another moderation call")
	case <-time.After(200 * time.Millisecond):
	}
}

func TestTrimContentModerationRunes(t *testing.T) {
	text := strings.Repeat("测", maxContentModerationInputRunes+10)
	got := trimContentModerationRunes(text)
	if len([]rune(got)) != maxContentModerationInputRunes {
		t.Fatalf("unexpected length: %d", len([]rune(got)))
	}
}

func TestContentModerationAPIKeyProbe(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/moderations" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk-draft-key" {
			t.Fatalf("unexpected auth: %q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["input"] != "hello" {
			t.Fatalf("expected benign hello prompt, got %#v", body["input"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"flagged":false,"category_scores":{"harassment":0.01}}]}`))
	}))
	defer server.Close()

	result, err := TestContentModerationAPIKey(context.Background(), server.URL, "omni-moderation-latest", "sk-draft-key", 3000)
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || !result.OK {
		t.Fatalf("expected ok result, got %#v", result)
	}
	if result.HTTPStatus != http.StatusOK {
		t.Fatalf("unexpected status: %d", result.HTTPStatus)
	}
	if !strings.HasPrefix(result.KeyMask, "****") {
		t.Fatalf("expected masked key, got %q", result.KeyMask)
	}
}

func TestContentModerationAPIKeyProbeUsesStoredMasked(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer sk-stored-key-zzzz" {
			t.Fatalf("unexpected auth: %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"flagged":false,"category_scores":{}}]}`))
	}))
	defer server.Close()

	original := setting.GetContentModerationConfig()
	t.Cleanup(func() {
		_ = setting.UpdateContentModerationConfigByJsonString(setting.ContentModerationConfigJSON(original))
	})
	cfg := setting.ContentModerationConfig{
		BaseURL:   server.URL,
		Model:     "omni-moderation-latest",
		APIKeys:   []string{"sk-stored-key-zzzz"},
		TimeoutMS: 3000,
	}
	if err := setting.UpdateContentModerationConfigByJsonString(setting.ContentModerationConfigJSON(cfg)); err != nil {
		t.Fatal(err)
	}

	result, err := TestContentModerationAPIKey(context.Background(), server.URL, "omni-moderation-latest", "****zzzz", 3000)
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || !result.OK {
		t.Fatalf("expected ok using stored key, got %#v", result)
	}
}
