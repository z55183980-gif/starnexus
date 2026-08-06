package codex

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	appconstant "github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

type Adaptor struct {
}

// Default Codex CLI identity used when the inbound request has no User-Agent
// (for example channel / account-pool tests) or only a browser UA that would
// trigger Cloudflare challenges on chatgpt.com.
const defaultCodexUserAgent = "codex_cli_rs/0.144.1 (Ubuntu 22.4.0; x86_64) xterm-256color"

func (a *Adaptor) ConvertGeminiRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeminiChatRequest) (any, error) {
	return nil, errors.New("codex channel: endpoint not supported")
}

func (a *Adaptor) ConvertClaudeRequest(*gin.Context, *relaycommon.RelayInfo, *dto.ClaudeRequest) (any, error) {
	return nil, errors.New("codex channel: /v1/messages endpoint not supported")
}

func (a *Adaptor) ConvertAudioRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.AudioRequest) (io.Reader, error) {
	return nil, errors.New("codex channel: endpoint not supported")
}

func (a *Adaptor) ConvertImageRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.ImageRequest) (any, error) {
	return nil, errors.New("codex channel: endpoint not supported")
}

func (a *Adaptor) Init(info *relaycommon.RelayInfo) {
}

func (a *Adaptor) ConvertOpenAIRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest) (any, error) {
	return nil, errors.New("codex channel: /v1/chat/completions endpoint not supported")
}

func (a *Adaptor) ConvertRerankRequest(c *gin.Context, relayMode int, request dto.RerankRequest) (any, error) {
	return nil, errors.New("codex channel: /v1/rerank endpoint not supported")
}

func (a *Adaptor) ConvertEmbeddingRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.EmbeddingRequest) (any, error) {
	return nil, errors.New("codex channel: /v1/embeddings endpoint not supported")
}

func (a *Adaptor) ConvertOpenAIResponsesRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.OpenAIResponsesRequest) (any, error) {
	isCompact := info != nil && info.RelayMode == relayconstant.RelayModeResponsesCompact
	hadClientInstructions := len(request.Instructions) > 0

	if isCompact {
		// Compact has a deliberately narrower normalization path, but the same
		// ChatGPT Codex backend restriction on role:"system" still applies.
		if err := promoteCodexSystemMessages(&request); err != nil {
			return nil, err
		}
		if err := applyCodexChannelSystemPrompt(info, &request, hadClientInstructions); err != nil {
			return nil, err
		}
		ensureCodexInstructionsField(&request)
		return request, nil
	}
	// Ordinary Responses HTTP cannot forward previous_response_id to the Codex
	// backend. Restore the gateway-owned full input first when it is available;
	// orphan tool outputs fail closed instead of being sent without their call.
	if !common.GetContextKeyBool(c, appconstant.ContextKeyResponsesWebSocketIngress) {
		if apiErr := service.ApplyResponsesHTTPContinuationForCodex(c, &request); apiErr != nil {
			return nil, apiErr
		}
		service.MarkResponsesHTTPContinuationPersistTarget(c)
	}
	repairedInput, err := applyCodexInputRepair(c, request.Input)
	if err != nil {
		return nil, err
	}
	request.Input = repairedInput
	if err := normalizeCodexResponsesRequest(&request); err != nil {
		return nil, err
	}
	if err := applyCodexChannelSystemPrompt(info, &request, hadClientInstructions); err != nil {
		return nil, err
	}
	applyCodexStructuredOutputCompatibility(c, &request)
	ensureCodexInstructionsField(&request)
	// ChatGPT's Codex Responses endpoint only accepts streaming requests. Keep
	// the downstream stream preference in RelayInfo and force only the outbound
	// request here; non-stream clients are bridged back to JSON in DoResponse.
	stream := true
	request.Stream = &stream
	// ChatGPT's Codex HTTP endpoint does not accept previous_response_id. SUB2API
	// only forwards it when the chosen upstream transport is Responses WS v2;
	// HTTP (including client-WS/http_bridge) must use a full input payload.
	if !ResponsesUsesNativeWebSocket(c, info) {
		request.PreviousResponseID = ""
	}
	// codex: store must be false
	request.Store = json.RawMessage("false")
	// rm max_output_tokens
	request.MaxOutputTokens = nil
	request.Temperature = nil
	return request, nil
}

func applyCodexInputRepair(c *gin.Context, input json.RawMessage) (json.RawMessage, error) {
	repairedInput, repairResult, err := repairCodexInvalidLocalItemIDs(input)
	if err != nil || repairResult.DroppedItems() == 0 {
		return repairedInput, err
	}
	if c != nil {
		c.Set("codex_input_repair_admin_info", map[string]interface{}{
			"dropped_reasoning_items": repairResult.DroppedReasoningItems,
			"dropped_item_references": repairResult.DroppedItemReferences,
			"first_dropped_index":     repairResult.FirstDroppedIndex,
		})
	}
	if repairResult.RemainingItems == 0 {
		return nil, types.NewErrorWithStatusCode(
			errors.New("Codex input contains only invalid client-local reasoning items; start a new session"),
			types.ErrorCodeInvalidRequest,
			http.StatusBadRequest,
			types.ErrOptionWithSkipRetry(),
		)
	}
	if repairResult.HasOrphanToolOutput {
		return nil, types.NewErrorWithStatusCode(
			errors.New("Codex input repair would leave a tool output without its matching tool call; resend full history or start a new session"),
			types.ErrorCodeInvalidRequest,
			http.StatusConflict,
			types.ErrOptionWithSkipRetry(),
		)
	}
	return repairedInput, nil
}

func applyCodexChannelSystemPrompt(info *relaycommon.RelayInfo, request *dto.OpenAIResponsesRequest, hadClientInstructions bool) error {
	if info == nil || request == nil || info.ChannelSetting.SystemPrompt == "" {
		return nil
	}

	systemPrompt := info.ChannelSetting.SystemPrompt
	var existing string
	existingValid := len(request.Instructions) > 0 && common.Unmarshal(request.Instructions, &existing) == nil
	existing = strings.TrimSpace(existing)

	// Preserve the previous non-override behavior: a channel prompt is injected
	// when the client omitted the top-level instructions field. A promoted
	// system message is retained after that channel-owned prefix.
	if !hadClientInstructions {
		combined := systemPrompt
		if existingValid && existing != "" {
			combined += "\n" + existing
		}
		encoded, err := common.Marshal(combined)
		if err != nil {
			return err
		}
		request.Instructions = encoded
		return nil
	}

	if !info.ChannelSetting.SystemPromptOverride {
		return nil
	}
	combined := systemPrompt
	if existingValid && existing != "" {
		combined += "\n" + existing
	}
	encoded, err := common.Marshal(combined)
	if err != nil {
		return err
	}
	request.Instructions = encoded
	return nil
}

func ensureCodexInstructionsField(request *dto.OpenAIResponsesRequest) {
	if request != nil && len(request.Instructions) == 0 {
		// Codex backend requires the field even when there is no guidance.
		request.Instructions = json.RawMessage(`""`)
	}
}

// ResponsesUsesNativeWebSocket reports whether a client Responses WebSocket
// turn is forwarded over an upstream WebSocket instead of the HTTP bridge.
func ResponsesUsesNativeWebSocket(c *gin.Context, info *relaycommon.RelayInfo) bool {
	if c == nil || info == nil || !common.GetContextKeyBool(c, appconstant.ContextKeyResponsesWebSocketIngress) {
		return false
	}
	mode := strings.TrimSpace(info.ChannelOtherSettings.ResponsesWebSocketV2Mode)
	return mode == model.UpstreamOpenAIWSModeContextPool || mode == model.UpstreamOpenAIWSModePassthrough ||
		(mode == "" && info.ChannelOtherSettings.ResponsesWebSocketV2Enabled)
}

func (a *Adaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (any, error) {
	return channel.DoApiRequest(a, c, info, requestBody)
}

func (a *Adaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (usage any, err *types.NewAPIError) {
	switch info.RelayMode {
	case relayconstant.RelayModeAlphaSearch:
		return nil, types.NewError(errors.New("codex channel: alpha search response should be handled by AlphaSearchHelper"), types.ErrorCodeInvalidRequest)
	case relayconstant.RelayModeResponsesCompact:
		return openai.OaiResponsesCompactionHandler(c, resp)
	case relayconstant.RelayModeResponses:
		if info.IsStream {
			return openai.OaiResponsesStreamHandler(c, info, resp)
		}
		return openai.OaiResponsesSSEToNonStreamHandler(c, info, resp)
	default:
		return nil, types.NewError(errors.New("codex channel: endpoint not supported"), types.ErrorCodeInvalidRequest)
	}
}

func (a *Adaptor) GetModelList() []string {
	return ModelList
}

func (a *Adaptor) GetChannelName() string {
	return ChannelName
}

func (a *Adaptor) GetRequestURL(info *relaycommon.RelayInfo) (string, error) {
	var path string
	switch info.RelayMode {
	case relayconstant.RelayModeResponses:
		path = "/backend-api/codex/responses"
	case relayconstant.RelayModeResponsesCompact:
		path = "/backend-api/codex/responses/compact"
	case relayconstant.RelayModeAlphaSearch:
		path = "/backend-api/codex/alpha/search"
	default:
		return "", errors.New("codex channel: only /v1/responses, /v1/responses/compact and /v1/alpha/search are supported")
	}
	return relaycommon.GetFullRequestURL(info.ChannelBaseUrl, path, info.ChannelType), nil
}

func (a *Adaptor) SetupRequestHeader(c *gin.Context, req *http.Header, info *relaycommon.RelayInfo) error {
	channel.SetupApiRequestHeader(info, c, req)
	for _, name := range []string{
		"session_id", "conversation_id", "x-codex-turn-state", "x-codex-turn-metadata",
		"x-codex-beta-features", "x-codex-window-id", "x-codex-installation-id", "User-Agent",
	} {
		if value := strings.TrimSpace(c.GetHeader(name)); value != "" && req.Get(name) == "" {
			if name == "session_id" || name == "conversation_id" {
				value = isolateCodexSessionHeader(c, value)
			}
			req.Set(name, value)
		}
	}
	// HTTP and the client-WS/SSE bridge need a stable upstream identity even
	// when the client only supplies prompt_cache_key in the request body.
	// Native upstream WebSocket connections establish identity at handshake
	// time and must retain their existing header behavior.
	if info != nil && !ResponsesUsesNativeWebSocket(c, info) {
		if promptCacheKey := parsePromptCacheKey(info.Request); promptCacheKey != "" {
			isolated := isolateCodexSessionHeader(c, promptCacheKey)
			req.Set("session_id", isolated)
			req.Set("conversation_id", isolated)
		}
	}
	// Native Responses WS conveys Lite per turn through client_metadata. HTTP
	// and the client-WS/SSE bridge need the equivalent real request header.
	if info != nil && info.RelayMode == relayconstant.RelayModeResponses &&
		!ResponsesUsesNativeWebSocket(c, info) && IsResponsesLiteHeader(c.GetHeader(ResponsesLiteHeader)) {
		req.Set(ResponsesLiteHeader, "true")
	}

	key := strings.TrimSpace(info.ApiKey)
	if !strings.HasPrefix(key, "{") {
		return errors.New("codex channel: key must be a JSON object")
	}

	oauthKey, err := ParseOAuthKey(key)
	if err != nil {
		return err
	}

	accessToken := strings.TrimSpace(oauthKey.AccessToken)
	accountID := strings.TrimSpace(oauthKey.AccountID)

	if accessToken == "" {
		return errors.New("codex channel: access_token is required")
	}
	if accountID == "" {
		return errors.New("codex channel: account_id is required")
	}

	req.Set("Authorization", "Bearer "+accessToken)
	req.Set("chatgpt-account-id", accountID)

	if req.Get("OpenAI-Beta") == "" {
		req.Set("OpenAI-Beta", "responses=experimental")
	}
	if req.Get("originator") == "" {
		req.Set("originator", "codex_cli_rs")
	}
	applyCodexUserAgent(req)

	// chatgpt.com/backend-api/codex/responses is strict about Content-Type.
	// Clients may omit it or include parameters like `application/json; charset=utf-8`,
	// which can be rejected by the upstream. Force the exact media type.
	req.Set("Content-Type", "application/json")
	if info.RelayMode == relayconstant.RelayModeResponses {
		req.Set("Accept", "text/event-stream")
	} else if req.Get("Accept") == "" {
		req.Set("Accept", "application/json")
	}

	return nil
}

func parsePromptCacheKey(request dto.Request) string {
	var raw json.RawMessage
	switch value := request.(type) {
	case *dto.OpenAIResponsesRequest:
		if value != nil {
			raw = value.PromptCacheKey
		}
	}
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var key string
	if err := common.Unmarshal(raw, &key); err != nil {
		return ""
	}
	key = strings.TrimSpace(key)
	if strings.ContainsAny(key, "\r\n") {
		return ""
	}
	return key
}

func applyCodexUserAgent(req *http.Header) {
	if req == nil {
		return
	}
	currentUA := strings.TrimSpace(req.Get("User-Agent"))
	if currentUA == "" || isBrowserUserAgent(currentUA) {
		req.Set("User-Agent", defaultCodexUserAgent)
	}
}

func isBrowserUserAgent(userAgent string) bool {
	lower := strings.ToLower(strings.TrimSpace(userAgent))
	if !strings.HasPrefix(lower, "mozilla/") {
		return false
	}
	return strings.Contains(lower, "chrome/") ||
		strings.Contains(lower, "firefox/") ||
		strings.Contains(lower, "safari/") ||
		strings.Contains(lower, "edg/") ||
		strings.Contains(lower, "opr/")
}

func isolateCodexSessionHeader(c *gin.Context, value string) string {
	value = strings.TrimSpace(value)
	if c == nil || value == "" {
		return value
	}
	tokenID := common.GetContextKeyInt(c, appconstant.ContextKeyTokenId)
	userID := common.GetContextKeyInt(c, appconstant.ContextKeyUserId)
	if tokenID <= 0 && userID <= 0 {
		return value
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d:%d:%s", userID, tokenID, value)))
	return fmt.Sprintf("%x", sum[:])
}
