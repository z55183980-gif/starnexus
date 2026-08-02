package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

const (
	contentModerationSourcePrefix          = "moderation:"
	maxContentModerationInputRunes         = 12000
	maxContentModerationObserveConcurrency = 32
)

var (
	contentModerationKeyIndex       uint64
	contentModerationObserveSkipped uint64
	contentModerationObserveSem     = make(chan struct{}, maxContentModerationObserveConcurrency)
)

type moderationAPIRequest struct {
	Model string `json:"model"`
	Input any    `json:"input"`
}

type moderationAPIResponse struct {
	Results []moderationAPIResult `json:"results"`
}

type moderationAPIResult struct {
	Flagged        bool               `json:"flagged"`
	CategoryScores map[string]float64 `json:"category_scores"`
}

type contentModerationLogContext struct {
	UserId    int
	Username  string
	TokenId   int
	TokenName string
	RequestId string
	Endpoint  string
	ModelName string
	Protocol  string
}

// ApplyContentModeration runs the global OpenAI Moderations gate before billing
// and upstream forwarding. It is independent of per-user prompt audit policies.
//
// Modes:
//   - pre_block: sync call; flagged prompts are blocked
//   - observe: async call; always allows the request, logs hits in background
//
// Audit scope (groups + model filter) skips Moderations entirely when out of scope.
// API failures are fail-open.
func ApplyContentModeration(c *gin.Context, request dto.Request, relayFormat types.RelayFormat, modelName string) *types.NewAPIError {
	cfg := setting.GetContentModerationConfig()
	if !cfg.Enabled {
		return nil
	}
	if len(cfg.APIKeys) == 0 {
		return nil
	}

	usingGroup := ""
	if c != nil {
		usingGroup = common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
	}
	if !cfg.IncludesGroup(usingGroup) {
		return nil
	}
	if !cfg.IncludesModel(modelName) {
		return nil
	}

	prompt := ExtractPromptAuditUserText(request)
	if prompt == "" {
		return nil
	}
	prompt = trimContentModerationRunes(prompt)
	logCtx := captureContentModerationLogContext(c, modelName, relayFormat)

	if cfg.Mode == setting.ContentModerationModeObserve {
		scheduleContentModerationObserve(cfg, logCtx, prompt)
		return nil
	}

	result, err := callContentModerationAPI(c.Request.Context(), cfg, prompt)
	if err != nil {
		common.SysError("content moderation api failed: " + err.Error())
		return nil
	}

	flagged, highestCategory, highestScore, hitCategories := evaluateContentModerationScores(result.CategoryScores, cfg.Thresholds)
	if !flagged {
		return nil
	}

	logContentModerationResult(logCtx, prompt, hitCategories, highestCategory, highestScore, true, model.PromptAuditActionBlocked)
	return types.NewErrorWithStatusCode(
		errors.New("prompt blocked by content moderation"),
		types.ErrorCodePromptBlocked,
		http.StatusBadRequest,
		types.ErrOptionWithSkipRetry(),
	)
}

func scheduleContentModerationObserve(cfg setting.ContentModerationConfig, logCtx contentModerationLogContext, prompt string) {
	select {
	case contentModerationObserveSem <- struct{}{}:
		go func() {
			defer func() { <-contentModerationObserveSem }()
			runContentModerationObserve(cfg, logCtx, prompt)
		}()
	default:
		skipped := atomic.AddUint64(&contentModerationObserveSkipped, 1)
		if skipped == 1 || skipped%100 == 0 {
			common.SysError(fmt.Sprintf(
				"content moderation observe skipped: concurrency limit reached (limit=%d skipped=%d)",
				maxContentModerationObserveConcurrency, skipped,
			))
		}
	}
}

func runContentModerationObserve(cfg setting.ContentModerationConfig, logCtx contentModerationLogContext, prompt string) {
	defer func() {
		if recovered := recover(); recovered != nil {
			common.SysError(fmt.Sprintf("content moderation observe panic: %v", recovered))
		}
	}()

	timeout := time.Duration(cfg.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout+time.Second)
	defer cancel()

	result, err := callContentModerationAPI(ctx, cfg, prompt)
	if err != nil {
		common.SysError("content moderation observe api failed: " + err.Error())
		return
	}

	flagged, highestCategory, highestScore, hitCategories := evaluateContentModerationScores(result.CategoryScores, cfg.Thresholds)
	if !flagged {
		return
	}
	logContentModerationResult(logCtx, prompt, hitCategories, highestCategory, highestScore, true, model.PromptAuditActionHit)
}

func captureContentModerationLogContext(c *gin.Context, modelName string, relayFormat types.RelayFormat) contentModerationLogContext {
	endpoint := ""
	if c != nil && c.Request != nil && c.Request.URL != nil {
		endpoint = c.Request.URL.Path
	}
	userId := 0
	username := ""
	tokenId := 0
	tokenName := ""
	requestId := ""
	if c != nil {
		userId = common.GetContextKeyInt(c, constant.ContextKeyUserId)
		username = c.GetString("username")
		tokenId = c.GetInt("token_id")
		tokenName = c.GetString("token_name")
		requestId = c.GetString(common.RequestIdKey)
	}
	return contentModerationLogContext{
		UserId:    userId,
		Username:  username,
		TokenId:   tokenId,
		TokenName: tokenName,
		RequestId: requestId,
		Endpoint:  endpoint,
		ModelName: modelName,
		Protocol:  string(relayFormat),
	}
}

func callContentModerationAPI(ctx context.Context, cfg setting.ContentModerationConfig, prompt string) (*moderationAPIResult, error) {
	if len(cfg.APIKeys) == 0 {
		return nil, errors.New("no content moderation api keys configured")
	}
	timeout := time.Duration(cfg.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	var lastErr error
	start := int(atomic.AddUint64(&contentModerationKeyIndex, 1)-1) % len(cfg.APIKeys)
	for attempt := 0; attempt < len(cfg.APIKeys); attempt++ {
		key := cfg.APIKeys[(start+attempt)%len(cfg.APIKeys)]
		result, err := callContentModerationOnce(ctx, cfg, key, prompt, timeout)
		if err == nil {
			return result, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = errors.New("content moderation api unavailable")
	}
	return nil, lastErr
}

// ContentModerationAPIKeyTestResult is the admin Test button response for a Moderations key.
// Mirrors sub2api risk-control TestAPIKeys: POST /v1/moderations with a benign prompt.
type ContentModerationAPIKeyTestResult struct {
	OK              bool    `json:"ok"`
	LatencyMS       int     `json:"latency_ms"`
	HTTPStatus      int     `json:"http_status"`
	Flagged         bool    `json:"flagged"`
	Error           string  `json:"error,omitempty"`
	KeyMask         string  `json:"key_mask,omitempty"`
	HighestScore    float64 `json:"highest_score,omitempty"`
	HighestCategory string  `json:"highest_category,omitempty"`
}

// TestContentModerationAPIKey probes OpenAI Moderations with draft or stored credentials.
// Empty or masked apiKey uses the first configured key (same as sub2api stored-key test).
func TestContentModerationAPIKey(ctx context.Context, baseURL, model, apiKey string, timeoutMS int) (*ContentModerationAPIKeyTestResult, error) {
	cfg := setting.GetContentModerationConfig()
	if strings.TrimSpace(baseURL) != "" {
		cfg.BaseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.openai.com"
	}
	if strings.TrimSpace(model) != "" {
		cfg.Model = strings.TrimSpace(model)
	}
	if cfg.Model == "" {
		cfg.Model = "omni-moderation-latest"
	}
	if timeoutMS > 0 {
		cfg.TimeoutMS = timeoutMS
	}
	if cfg.TimeoutMS <= 0 {
		cfg.TimeoutMS = 3000
	}
	if cfg.TimeoutMS > 30000 {
		cfg.TimeoutMS = 30000
	}

	resolvedKey := strings.TrimSpace(apiKey)
	if resolvedKey == "" || setting.IsMaskedAPIKeyPlaceholder(resolvedKey) {
		if len(cfg.APIKeys) == 0 {
			return nil, errors.New("no content moderation api key configured")
		}
		resolvedKey = cfg.APIKeys[0]
	}

	timeout := time.Duration(cfg.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 3 * time.Second
	}

	start := time.Now()
	result, err := callContentModerationOnce(ctx, cfg, resolvedKey, "hello", timeout)
	latency := int(time.Since(start).Milliseconds())
	out := &ContentModerationAPIKeyTestResult{
		LatencyMS: latency,
		KeyMask:   setting.MaskSecretTail(resolvedKey),
	}
	if err != nil {
		out.OK = false
		out.Error = sanitizeModerationTestError(err.Error())
		if status, ok := parseModerationHTTPStatus(err.Error()); ok {
			out.HTTPStatus = status
		}
		return out, nil
	}
	out.OK = true
	out.HTTPStatus = http.StatusOK
	if result != nil {
		_, highestCategory, highestScore, _ := evaluateContentModerationScores(result.CategoryScores, cfg.Thresholds)
		out.Flagged = result.Flagged
		out.HighestCategory = highestCategory
		out.HighestScore = highestScore
	}
	return out, nil
}

func sanitizeModerationTestError(msg string) string {
	msg = strings.TrimSpace(msg)
	if len(msg) > 500 {
		msg = msg[:500] + "…"
	}
	return msg
}

func parseModerationHTTPStatus(msg string) (int, bool) {
	const prefix = "moderation api status "
	if !strings.HasPrefix(msg, prefix) {
		return 0, false
	}
	rest := strings.TrimPrefix(msg, prefix)
	space := strings.IndexByte(rest, ':')
	if space <= 0 {
		space = strings.IndexByte(rest, ' ')
	}
	if space > 0 {
		rest = rest[:space]
	}
	var status int
	if _, err := fmt.Sscanf(rest, "%d", &status); err != nil || status <= 0 {
		return 0, false
	}
	return status, true
}

func callContentModerationOnce(ctx context.Context, cfg setting.ContentModerationConfig, apiKey, prompt string, timeout time.Duration) (*moderationAPIResult, error) {
	endpoint, err := url.JoinPath(strings.TrimRight(cfg.BaseURL, "/"), "/v1/moderations")
	if err != nil {
		return nil, err
	}
	payload, err := common.Marshal(moderationAPIRequest{
		Model: cfg.Model,
		Input: prompt,
	})
	if err != nil {
		return nil, err
	}

	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("moderation api status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var out moderationAPIResponse
	if err := common.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	if len(out.Results) == 0 {
		return nil, errors.New("moderation api returned empty results")
	}
	return &out.Results[0], nil
}

func evaluateContentModerationScores(scores, thresholds map[string]float64) (bool, string, float64, []string) {
	flagged := false
	highestCategory := ""
	highestScore := 0.0
	var hitCategories []string
	for _, category := range setting.ContentModerationCategories() {
		score := 0.0
		if scores != nil {
			score = scores[category]
		}
		if highestCategory == "" || score > highestScore {
			highestScore = score
			highestCategory = category
		}
		threshold := 1.0
		if thresholds != nil {
			if value, ok := thresholds[category]; ok {
				threshold = value
			}
		}
		if score >= threshold {
			flagged = true
			hitCategories = append(hitCategories, contentModerationSourcePrefix+category)
		}
	}
	return flagged, highestCategory, highestScore, hitCategories
}

func logContentModerationResult(
	logCtx contentModerationLogContext,
	prompt string,
	matched []string,
	highestCategory string,
	highestScore float64,
	hit bool,
	action string,
) {
	storedPrompt, truncated := truncatePromptAuditText(prompt)
	if matched == nil {
		matched = []string{}
	}
	if highestCategory != "" && hit {
		label := contentModerationSourcePrefix + highestCategory
		found := false
		for _, item := range matched {
			if item == label {
				found = true
				break
			}
		}
		if !found {
			matched = append(matched, label)
		}
	}
	matchedWords, err := common.Marshal(matched)
	if err != nil {
		matchedWords = []byte("[]")
	}
	log := &model.PromptAuditLog{
		UserId:       logCtx.UserId,
		Username:     logCtx.Username,
		TokenId:      logCtx.TokenId,
		TokenName:    logCtx.TokenName,
		RequestId:    logCtx.RequestId,
		ModelName:    logCtx.ModelName,
		Protocol:     logCtx.Protocol,
		Endpoint:     logCtx.Endpoint,
		Prompt:       storedPrompt,
		PromptHash:   fmt.Sprintf("%x", sha256.Sum256([]byte(prompt))),
		Hit:          hit,
		MatchedWords: string(matchedWords),
		Action:       action,
		DelayMs:      0,
		Truncated:    truncated,
		CreatedAt:    common.GetTimestamp(),
		Score:        highestScore,
	}
	if err := model.CreatePromptAuditLog(log); err != nil {
		common.SysError("failed to create content moderation audit log: " + err.Error())
	}
}

func trimContentModerationRunes(text string) string {
	runes := []rune(text)
	if len(runes) <= maxContentModerationInputRunes {
		return text
	}
	return string(runes[:maxContentModerationInputRunes])
}
