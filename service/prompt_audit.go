package service

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

const (
	promptAuditMaxPromptRunes       = 12000
	promptAuditPolicyCacheTTL       = 5 * time.Second
	promptAuditBehaviorSourceMarker = "audit-source:user-behavior"
)

var promptAuditSystemReminderBlock = regexp.MustCompile(`(?is)<system-reminder>.*?</system-reminder>`)

var promptAuditPolicyCache = struct {
	sync.RWMutex
	loadedAt time.Time
	policies map[int]model.PromptAuditPolicy
}{
	policies: make(map[int]model.PromptAuditPolicy),
}

var promptAuditPolicyRefresh sync.Mutex

func InvalidatePromptAuditPolicyCache() {
	promptAuditPolicyRefresh.Lock()
	defer promptAuditPolicyRefresh.Unlock()
	promptAuditPolicyCache.Lock()
	promptAuditPolicyCache.loadedAt = time.Time{}
	promptAuditPolicyCache.Unlock()
}

func getPromptAuditPolicy(userId int) (*model.PromptAuditPolicy, error) {
	now := time.Now()
	promptAuditPolicyCache.RLock()
	if now.Sub(promptAuditPolicyCache.loadedAt) < promptAuditPolicyCacheTTL {
		policy, ok := promptAuditPolicyCache.policies[userId]
		promptAuditPolicyCache.RUnlock()
		if !ok {
			return nil, nil
		}
		return &policy, nil
	}
	promptAuditPolicyCache.RUnlock()

	promptAuditPolicyRefresh.Lock()
	defer promptAuditPolicyRefresh.Unlock()

	promptAuditPolicyCache.RLock()
	if time.Since(promptAuditPolicyCache.loadedAt) < promptAuditPolicyCacheTTL {
		policy, ok := promptAuditPolicyCache.policies[userId]
		promptAuditPolicyCache.RUnlock()
		if !ok {
			return nil, nil
		}
		return &policy, nil
	}
	promptAuditPolicyCache.RUnlock()

	policies, err := model.ListPromptAuditPolicyModels()
	if err != nil {
		return nil, err
	}
	policyMap := make(map[int]model.PromptAuditPolicy, len(policies))
	for _, policy := range policies {
		policyMap[policy.UserId] = policy
	}

	promptAuditPolicyCache.Lock()
	promptAuditPolicyCache.policies = policyMap
	promptAuditPolicyCache.loadedAt = now
	policy, ok := promptAuditPolicyCache.policies[userId]
	promptAuditPolicyCache.Unlock()
	if !ok {
		return nil, nil
	}
	return &policy, nil
}

// ApplyPromptAudit applies local per-user behavior controls after the global
// upstream account-risk content audit and before billing or forwarding. It is
// not an OpenAI policy classifier; it only monitors, delays, or blocks local
// sensitive-word hits for selected users.
func ApplyPromptAudit(c *gin.Context, request dto.Request, relayFormat types.RelayFormat, modelName string) *types.NewAPIError {
	userId := common.GetContextKeyInt(c, constant.ContextKeyUserId)
	if userId <= 0 {
		return nil
	}

	policy, err := getPromptAuditPolicy(userId)
	if err != nil {
		common.SysError("failed to load prompt audit policies: " + err.Error())
		return nil
	}
	if policy == nil {
		return nil
	}

	prompt := ExtractPromptAuditUserText(request)
	if prompt == "" {
		return nil
	}
	if shouldSkipPromptAuditText(c, prompt) {
		return nil
	}

	contains, words := CheckSensitiveText(prompt)
	action := model.PromptAuditActionRecorded
	delayMs := 0
	if contains {
		action = model.PromptAuditActionHit
		if policy.BlockOnHit {
			action = model.PromptAuditActionBlocked
		} else if policy.DelayOnHit {
			action = model.PromptAuditActionDelayed
			delayMs = promptAuditDelayMilliseconds(policy.DelaySeconds)
		}
	}

	if policy.MonitorEnabled || contains {
		log := buildPromptAuditLog(c, userId, modelName, relayFormat, prompt, words, contains, action, delayMs)
		if err = model.CreatePromptAuditLog(log); err != nil {
			common.SysError("failed to create prompt audit log: " + err.Error())
		}
	}

	if action == model.PromptAuditActionBlocked {
		return types.NewErrorWithStatusCode(
			errors.New("prompt blocked by user behavior rule"),
			types.ErrorCodeUserBehaviorBlocked,
			http.StatusBadRequest,
			types.ErrOptionWithSkipRetry(),
		)
	}
	if action == model.PromptAuditActionDelayed {
		timer := time.NewTimer(time.Duration(delayMs) * time.Millisecond)
		defer timer.Stop()
		select {
		case <-timer.C:
			return nil
		case <-c.Request.Context().Done():
			return types.NewErrorWithStatusCode(
				fmt.Errorf("security audit delay canceled: %w", c.Request.Context().Err()),
				types.ErrorCodeAccessDenied,
				http.StatusRequestTimeout,
				types.ErrOptionWithSkipRetry(),
			)
		}
	}

	return nil
}

func promptAuditDelayMilliseconds(seconds int) int {
	if !model.IsPromptAuditDelaySecondsAllowed(seconds) {
		seconds = model.PromptAuditDefaultDelaySeconds
	}
	return int((time.Duration(seconds) * time.Second) / time.Millisecond)
}

func buildPromptAuditLog(c *gin.Context, userId int, modelName string, relayFormat types.RelayFormat, prompt string, words []string, hit bool, action string, delayMs int) *model.PromptAuditLog {
	storedPrompt, truncated := truncatePromptAuditText(prompt)
	words = append([]string{promptAuditBehaviorSourceMarker}, words...)
	matchedWords, err := common.Marshal(words)
	if err != nil {
		matchedWords = []byte("[]")
	}
	username := c.GetString("username")
	endpoint := ""
	if c.Request != nil && c.Request.URL != nil {
		endpoint = c.Request.URL.Path
	}

	return &model.PromptAuditLog{
		UserId:       userId,
		Username:     username,
		TokenId:      c.GetInt("token_id"),
		TokenName:    c.GetString("token_name"),
		RequestId:    c.GetString(common.RequestIdKey),
		ModelName:    modelName,
		Protocol:     string(relayFormat),
		Endpoint:     endpoint,
		Prompt:       storedPrompt,
		PromptHash:   fmt.Sprintf("%x", sha256.Sum256([]byte(prompt))),
		Hit:          hit,
		MatchedWords: string(matchedWords),
		Action:       action,
		DelayMs:      delayMs,
		Truncated:    truncated,
		CreatedAt:    common.GetTimestamp(),
	}
}

func shouldSkipPromptAuditText(c *gin.Context, prompt string) bool {
	text := strings.ToLower(strings.TrimSpace(prompt))
	if text == "" {
		return true
	}
	// System-injected noise can appear for any client (Claude Code, Cursor, etc.),
	// not only Codex. Strip/skip these regardless of request headers.
	switch {
	case isPromptAuditSystemReminderText(text):
		return true
	case strings.HasPrefix(text, "[background task completed]") || strings.HasPrefix(text, "[all background tasks complete]"):
		return true
	}
	if !isCodexPromptAuditRequest(c) {
		return false
	}
	switch {
	case strings.HasPrefix(text, "calculate and respond with only the number, nothing else."):
		return true
	case strings.HasPrefix(text, "# overview") && strings.Contains(text, "hyperpersonalized suggestions"):
		return true
	case strings.HasPrefix(text, "you are a helpful assistant.") && strings.Contains(text, "short title for a task"):
		return true
	case strings.HasPrefix(text, "task:"):
		return true
	case hasPromptAuditTemporaryRepoPath(text) &&
		(strings.HasPrefix(text, "inspect the repository") ||
			strings.HasPrefix(text, "audit the repository") ||
			strings.HasPrefix(text, "in /var/") ||
			strings.HasPrefix(text, "in /tmp/") ||
			strings.HasPrefix(text, "in the checkout")):
		return true
	default:
		return false
	}
}

// isPromptAuditSystemReminderText reports Claude/Cursor style system reminder
// blocks that are often embedded inside role=user content.
func isPromptAuditSystemReminderText(text string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(text)), "<system-reminder>")
}

// sanitizePromptAuditTextPart removes embedded system-reminder blocks from a
// single content part while keeping any remaining user text.
func sanitizePromptAuditTextPart(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	text = promptAuditSystemReminderBlock.ReplaceAllString(text, "")
	if idx := strings.Index(strings.ToLower(text), "<system-reminder>"); idx >= 0 {
		text = text[:idx]
	}
	return strings.TrimSpace(text)
}

func appendPromptAuditTextPart(parts *[]string, text string) {
	text = sanitizePromptAuditTextPart(text)
	if text == "" {
		return
	}
	*parts = append(*parts, text)
}

func joinPromptAuditTextParts(parts []string) string {
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func isCodexPromptAuditRequest(c *gin.Context) bool {
	if c == nil || c.Request == nil {
		return false
	}
	if strings.Contains(strings.ToLower(c.GetHeader("originator")), "codex") {
		return true
	}
	if strings.Contains(strings.ToLower(c.GetHeader("User-Agent")), "codex") {
		return true
	}
	for _, header := range []string{"x-codex-window-id", "x-codex-installation-id", "x-codex-turn-metadata"} {
		if strings.TrimSpace(c.GetHeader(header)) != "" {
			return true
		}
	}
	return false
}

func hasPromptAuditTemporaryRepoPath(text string) bool {
	return strings.Contains(text, "/var/folders/") ||
		strings.Contains(text, "/tmp/") ||
		strings.Contains(text, "\\appdata\\local\\temp\\")
}

func truncatePromptAuditText(text string) (string, bool) {
	if utf8.RuneCountInString(text) <= promptAuditMaxPromptRunes {
		return text, false
	}
	return string([]rune(text)[:promptAuditMaxPromptRunes]), true
}

// ExtractPromptAuditUserText returns only user text introduced by the current
// request turn. It intentionally excludes system/developer prompts, tools,
// assistant messages, and user text repeated as earlier conversation history.
func ExtractPromptAuditUserText(request dto.Request) string {
	var text string
	switch req := request.(type) {
	case *dto.GeneralOpenAIRequest:
		text = extractOpenAIUserText(req)
	case *dto.OpenAIResponsesRequest:
		text = extractResponsesUserText(req)
	case *dto.AlphaSearchRequest:
		text = extractAlphaSearchUserText(req)
	case *dto.ClaudeRequest:
		text = extractClaudeUserText(req)
	case *dto.GeminiChatRequest:
		text = extractGeminiUserText(req)
	case *dto.ImageRequest:
		text = req.Prompt
	}
	return strings.TrimSpace(text)
}

func extractAlphaSearchUserText(req *dto.AlphaSearchRequest) string {
	if req == nil || len(req.RawBody) == 0 {
		return ""
	}
	type queryCommand struct {
		Query string `json:"q"`
	}
	var payload struct {
		Commands map[string][]queryCommand `json:"commands"`
	}
	if err := common.Unmarshal(req.RawBody, &payload); err != nil {
		return ""
	}
	var queries []string
	for _, commandName := range []string{"search_query", "image_query"} {
		for _, command := range payload.Commands[commandName] {
			if query := strings.TrimSpace(command.Query); query != "" {
				queries = append(queries, query)
			}
		}
	}
	return strings.Join(queries, "\n")
}

func extractOpenAIUserText(req *dto.GeneralOpenAIRequest) string {
	if len(req.Messages) > 0 {
		message := &req.Messages[len(req.Messages)-1]
		if message.Role != "user" {
			return ""
		}
		var parts []string
		for _, content := range message.ParseContent() {
			if content.Type == dto.ContentTypeText {
				appendPromptAuditTextPart(&parts, content.Text)
			}
		}
		return joinPromptAuditTextParts(parts)
	}
	if prompt := stringValue(req.Prompt); prompt != "" {
		var parts []string
		appendPromptAuditTextPart(&parts, prompt)
		return joinPromptAuditTextParts(parts)
	}
	if input := stringValue(req.Input); input != "" {
		var parts []string
		appendPromptAuditTextPart(&parts, input)
		return joinPromptAuditTextParts(parts)
	}
	// Instruction is typically a system/developer-style field, not current-turn
	// user input, so it is intentionally excluded from prompt audit text.
	return ""
}

func extractResponsesUserText(req *dto.OpenAIResponsesRequest) string {
	if common.GetJsonType(req.Input) == "string" {
		var value string
		_ = common.Unmarshal(req.Input, &value)
		var parts []string
		appendPromptAuditTextPart(&parts, value)
		return joinPromptAuditTextParts(parts)
	}
	if common.GetJsonType(req.Input) != "array" {
		return ""
	}

	type responseAuditInput struct {
		Type    string          `json:"type,omitempty"`
		Role    string          `json:"role,omitempty"`
		Text    string          `json:"text,omitempty"`
		Content json.RawMessage `json:"content,omitempty"`
	}
	var inputs []responseAuditInput
	if err := common.Unmarshal(req.Input, &inputs); err != nil {
		return ""
	}
	if len(inputs) == 0 {
		return ""
	}
	input := inputs[len(inputs)-1]
	if input.Type == "input_text" && input.Text != "" {
		var parts []string
		appendPromptAuditTextPart(&parts, input.Text)
		return joinPromptAuditTextParts(parts)
	}
	if input.Role != "user" {
		return ""
	}
	if common.GetJsonType(input.Content) == "string" {
		var value string
		_ = common.Unmarshal(input.Content, &value)
		var parts []string
		appendPromptAuditTextPart(&parts, value)
		return joinPromptAuditTextParts(parts)
	}
	if common.GetJsonType(input.Content) == "array" {
		var content []dto.MediaInput
		if err := common.Unmarshal(input.Content, &content); err != nil {
			return ""
		}
		var parts []string
		for _, item := range content {
			if item.Type == "input_text" {
				appendPromptAuditTextPart(&parts, item.Text)
			}
		}
		return joinPromptAuditTextParts(parts)
	}
	return ""
}

func extractClaudeUserText(req *dto.ClaudeRequest) string {
	if len(req.Messages) > 0 {
		message := &req.Messages[len(req.Messages)-1]
		if message.Role != "user" {
			return ""
		}
		if message.IsStringContent() {
			var parts []string
			appendPromptAuditTextPart(&parts, message.GetStringContent())
			return joinPromptAuditTextParts(parts)
		}
		content, err := message.ParseContent()
		if err != nil {
			return ""
		}
		var parts []string
		for _, item := range content {
			if item.Type == "text" {
				appendPromptAuditTextPart(&parts, item.GetText())
			}
		}
		return joinPromptAuditTextParts(parts)
	}
	var parts []string
	appendPromptAuditTextPart(&parts, req.Prompt)
	return joinPromptAuditTextParts(parts)
}

func extractGeminiUserText(req *dto.GeminiChatRequest) string {
	if len(req.Contents) == 0 {
		return ""
	}
	content := req.Contents[len(req.Contents)-1]
	if content.Role != "" && content.Role != "user" {
		return ""
	}
	var parts []string
	for _, part := range content.Parts {
		if part.Text != "" && !part.Thought {
			appendPromptAuditTextPart(&parts, part.Text)
		}
	}
	return joinPromptAuditTextParts(parts)
}

func stringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []string:
		return strings.Join(typed, "\n")
	case []any:
		var values []string
		for _, item := range typed {
			if text, ok := item.(string); ok {
				values = append(values, text)
			}
		}
		return strings.Join(values, "\n")
	default:
		return ""
	}
}
