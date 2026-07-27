package service

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
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
	promptAuditMaxPromptRunes = 12000
	promptAuditPolicyCacheTTL = 5 * time.Second
)

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

// ApplyPromptAudit applies the per-user security audit policy before token
// counting, billing, or upstream forwarding. Global sensitive-word enforcement
// remains independent and must still run after this audit step.
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
			errors.New("prompt blocked by security audit"),
			types.ErrorCodePromptBlocked,
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

func truncatePromptAuditText(text string) (string, bool) {
	if utf8.RuneCountInString(text) <= promptAuditMaxPromptRunes {
		return text, false
	}
	return string([]rune(text)[:promptAuditMaxPromptRunes]), true
}

// ExtractPromptAuditUserText returns only the latest user-provided text. It
// intentionally excludes system/developer prompts, tools, assistant messages,
// and earlier conversation history.
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
	for i := len(req.Messages) - 1; i >= 0; i-- {
		message := &req.Messages[i]
		if message.Role != "user" {
			continue
		}
		var parts []string
		for _, content := range message.ParseContent() {
			if content.Type == dto.ContentTypeText && content.Text != "" {
				parts = append(parts, content.Text)
			}
		}
		return strings.Join(parts, "\n")
	}
	if prompt := stringValue(req.Prompt); prompt != "" {
		return prompt
	}
	if input := stringValue(req.Input); input != "" {
		return input
	}
	return req.Instruction
}

func extractResponsesUserText(req *dto.OpenAIResponsesRequest) string {
	if common.GetJsonType(req.Input) == "string" {
		var value string
		_ = common.Unmarshal(req.Input, &value)
		return value
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
	for i := len(inputs) - 1; i >= 0; i-- {
		input := inputs[i]
		if input.Type == "input_text" && input.Text != "" {
			return input.Text
		}
		if input.Role != "" && input.Role != "user" {
			continue
		}
		if common.GetJsonType(input.Content) == "string" {
			var value string
			_ = common.Unmarshal(input.Content, &value)
			return value
		}
		if common.GetJsonType(input.Content) == "array" {
			var content []dto.MediaInput
			if err := common.Unmarshal(input.Content, &content); err != nil {
				continue
			}
			var parts []string
			for _, item := range content {
				if item.Type == "input_text" && item.Text != "" {
					parts = append(parts, item.Text)
				}
			}
			return strings.Join(parts, "\n")
		}
	}
	return ""
}

func extractClaudeUserText(req *dto.ClaudeRequest) string {
	for i := len(req.Messages) - 1; i >= 0; i-- {
		message := &req.Messages[i]
		if message.Role != "user" {
			continue
		}
		if message.IsStringContent() {
			return message.GetStringContent()
		}
		content, err := message.ParseContent()
		if err != nil {
			return ""
		}
		var parts []string
		for _, item := range content {
			if item.Type == "text" && item.GetText() != "" {
				parts = append(parts, item.GetText())
			}
		}
		return strings.Join(parts, "\n")
	}
	return req.Prompt
}

func extractGeminiUserText(req *dto.GeminiChatRequest) string {
	for i := len(req.Contents) - 1; i >= 0; i-- {
		content := req.Contents[i]
		if content.Role != "" && content.Role != "user" {
			continue
		}
		var parts []string
		for _, part := range content.Parts {
			if part.Text != "" && !part.Thought {
				parts = append(parts, part.Text)
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
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
