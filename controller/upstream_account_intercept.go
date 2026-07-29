package controller

import (
	"crypto/rand"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

type upstreamClaudeInterceptType int

const (
	upstreamClaudeInterceptNone upstreamClaudeInterceptType = iota
	upstreamClaudeInterceptWarmup
	upstreamClaudeInterceptSuggestion
	upstreamClaudeInterceptHaikuProbe
)

const upstreamClaudeTitlePrompt = "Please write a 5-10 word title for the following conversation:"

func tryInterceptUpstreamWarmup(c *gin.Context, relayFormat types.RelayFormat, request dto.Request) bool {
	if relayFormat != types.RelayFormatClaude || !common.GetContextKeyBool(c, constant.ContextKeyUpstreamInterceptWarmup) {
		return false
	}
	claudeRequest, ok := request.(*dto.ClaudeRequest)
	if !ok || claudeRequest == nil {
		return false
	}
	interceptType := detectUpstreamClaudeIntercept(c, claudeRequest)
	if interceptType == upstreamClaudeInterceptNone {
		return false
	}

	middleware.ReleaseUpstreamAccountSelection(c)
	if claudeRequest.IsStream(c) {
		sendUpstreamClaudeInterceptStream(c, claudeRequest.Model, interceptType)
	} else {
		sendUpstreamClaudeInterceptResponse(c, claudeRequest.Model, interceptType)
	}
	return true
}

func detectUpstreamClaudeIntercept(c *gin.Context, request *dto.ClaudeRequest) upstreamClaudeInterceptType {
	if request == nil {
		return upstreamClaudeInterceptNone
	}
	if request.MaxTokens != nil && *request.MaxTokens == 1 && strings.Contains(strings.ToLower(request.Model), "haiku") && isClaudeCodeRequest(c) {
		return upstreamClaudeInterceptHaikuProbe
	}
	if len(request.Messages) > 0 {
		lastMessage := request.Messages[len(request.Messages)-1]
		if lastMessage.Role == "user" && strings.HasPrefix(lastMessage.GetStringContent(), "[SUGGESTION MODE:") {
			return upstreamClaudeInterceptSuggestion
		}
	}
	for i := range request.Messages {
		content := request.Messages[i].GetStringContent()
		if strings.Contains(content, upstreamClaudeTitlePrompt) || strings.TrimSpace(content) == "Warmup" {
			return upstreamClaudeInterceptWarmup
		}
	}
	systemText := request.GetStringSystem()
	if systemText == "" {
		var builder strings.Builder
		for _, message := range request.ParseSystem() {
			builder.WriteString(message.GetText())
		}
		systemText = builder.String()
	}
	if strings.Contains(systemText, "nalyze if this message indicates a new conversation topic. If it does, extract a 2-3 word title") {
		return upstreamClaudeInterceptWarmup
	}
	return upstreamClaudeInterceptNone
}

func isClaudeCodeRequest(c *gin.Context) bool {
	if c == nil || c.Request == nil {
		return false
	}
	identity := strings.ToLower(c.GetHeader("User-Agent") + " " + c.GetHeader("X-App") + " " + c.GetHeader("anthropic-client"))
	return strings.Contains(identity, "claude-code") || strings.Contains(identity, "claude_cli") || strings.Contains(identity, "claude-cli")
}

func upstreamClaudeInterceptContent(interceptType upstreamClaudeInterceptType) (string, string, int) {
	switch interceptType {
	case upstreamClaudeInterceptSuggestion:
		return "", "end_turn", 1
	case upstreamClaudeInterceptHaikuProbe:
		return "#", "max_tokens", 1
	default:
		return "New Conversation", "end_turn", 2
	}
}

func sendUpstreamClaudeInterceptResponse(c *gin.Context, model string, interceptType upstreamClaudeInterceptType) {
	text, stopReason, outputTokens := upstreamClaudeInterceptContent(interceptType)
	c.JSON(200, gin.H{
		"model": model, "id": newUpstreamClaudeMessageID(), "type": "message", "role": "assistant",
		"content": []gin.H{{"type": "text", "text": text}}, "stop_reason": stopReason,
		"stop_sequence": nil, "stop_details": nil,
		"usage": gin.H{
			"input_tokens": 10, "cache_creation_input_tokens": 0, "cache_read_input_tokens": 0, "output_tokens": outputTokens,
			"cache_creation": gin.H{"ephemeral_5m_input_tokens": 0, "ephemeral_1h_input_tokens": 0},
		},
	})
}

func sendUpstreamClaudeInterceptStream(c *gin.Context, model string, interceptType upstreamClaudeInterceptType) {
	text, stopReason, outputTokens := upstreamClaudeInterceptContent(interceptType)
	messageID := newUpstreamClaudeMessageID()
	events := []struct {
		name string
		data any
	}{
		{name: "message_start", data: gin.H{"type": "message_start", "message": gin.H{
			"model": model, "id": messageID, "type": "message", "role": "assistant", "content": []any{},
			"stop_reason": nil, "stop_sequence": nil, "stop_details": nil,
			"usage": gin.H{"input_tokens": 10, "cache_creation_input_tokens": 0, "cache_read_input_tokens": 0, "output_tokens": 0},
		}}},
		{name: "content_block_start", data: gin.H{"type": "content_block_start", "index": 0, "content_block": gin.H{"type": "text", "text": ""}}},
		{name: "content_block_delta", data: gin.H{"type": "content_block_delta", "index": 0, "delta": gin.H{"type": "text_delta", "text": text}}},
		{name: "content_block_stop", data: gin.H{"type": "content_block_stop", "index": 0}},
		{name: "message_delta", data: gin.H{"type": "message_delta", "delta": gin.H{"stop_reason": stopReason, "stop_sequence": nil, "stop_details": nil}, "usage": gin.H{"output_tokens": outputTokens}}},
		{name: "message_stop", data: gin.H{"type": "message_stop"}},
	}

	helper.SetEventStreamHeaders(c)
	for _, event := range events {
		data, err := common.Marshal(event.data)
		if err != nil {
			return
		}
		_, _ = c.Writer.WriteString("event: " + event.name + "\n" + "data: " + string(data) + "\n\n")
		_ = helper.FlushWriter(c)
	}
}

func newUpstreamClaudeMessageID() string {
	const charset = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	randomBytes := make([]byte, 22)
	if _, err := rand.Read(randomBytes); err != nil {
		return fmt.Sprintf("msg_01%d", time.Now().UnixNano())
	}
	for i := range randomBytes {
		randomBytes[i] = charset[int(randomBytes[i])%len(charset)]
	}
	return "msg_01" + string(randomBytes)
}
