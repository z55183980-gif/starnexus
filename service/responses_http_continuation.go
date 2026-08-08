package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	appconstant "github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
)

const (
	responsesHTTPContinuationContextKey  = "responses_http_continuation"
	responsesHTTPDeliveredContextKey     = "responses_http_delivered_context"
	responsesHTTPStagedOutputContextKey  = "responses_http_staged_output"
	responsesHTTPStatusContextKey        = "responses_http_continuation_status"
	responsesHTTPPersistStatusContextKey = "responses_http_persist_status"
	responsesHTTPLocalCacheContextKey    = "responses_http_local_cache_status"
	responsesHTTPRedisStatusContextKey   = "responses_http_redis_status"
	responsesHTTPPersistTargetContextKey = "responses_http_persist_target"
	responsesHTTPPendingL1ContextKey     = "responses_http_pending_l1"
	responsesHTTPContinuationTTL         = time.Hour
	responsesHTTPRedisTimeout            = 500 * time.Millisecond
)

type responsesHTTPContinuationState struct {
	Model       string            `json:"model"`
	ReplayInput []json.RawMessage `json:"replay_input"`
	AccountID   int               `json:"account_id,omitempty"`
	Complete    bool              `json:"complete"`
}

type responsesHTTPContinuationPreparation struct {
	Model                string
	PreviousResponseID   string
	FullInput            []json.RawMessage
	FullInputExists      bool
	StateFound           bool
	ReplayComplete       bool
	PreferredAccountID   int
	MissingToolContext   bool
	ContextTooLarge      bool
	ContinuationConflict string
}

type responsesHTTPDeliveredContext struct {
	ResponseID        string
	Output            []json.RawMessage
	DeliveredToolCall bool
	TerminalCommitted bool
}

type responsesHTTPStagedOutput struct {
	ResponseID string
	Output     []json.RawMessage
}

// ResponsesHTTPPersistTarget fingerprints the Codex HTTP attempt that is
// allowed to create or update continuation state.
type ResponsesHTTPPersistTarget struct {
	ChannelID int
	PoolID    int
	AccountID int
}

// PrepareResponsesHTTPContinuation restores a best-effort, gateway-owned
// representation of a Responses continuation. It never mutates the client
// request: compatible upstreams may still consume previous_response_id
// natively, while Codex HTTP can opt into the expanded input later.
func PrepareResponsesHTTPContinuation(c *gin.Context, request *dto.OpenAIResponsesRequest) {
	if c == nil || request == nil {
		return
	}
	current, currentExists, err := normalizeResponsesHTTPInput(request.Input)
	if err != nil {
		return
	}
	preparation := &responsesHTTPContinuationPreparation{
		Model:              strings.TrimSpace(request.Model),
		PreviousResponseID: strings.TrimSpace(request.PreviousResponseID),
		FullInput:          cloneResponsesHTTPRawMessages(current),
		FullInputExists:    currentExists,
		ReplayComplete:     strings.TrimSpace(request.PreviousResponseID) == "",
	}
	if preparation.PreviousResponseID != "" {
		if state, found := getResponsesHTTPContinuation(c, preparation.PreviousResponseID, false); found {
			preparation.StateFound = true
			preparation.ReplayComplete = state.Complete
			preparation.PreferredAccountID = state.AccountID
			if preparation.Model != "" && state.Model != "" && preparation.Model != state.Model {
				preparation.ContinuationConflict = fmt.Sprintf("previous_response_id belongs to model %s, not %s", state.Model, preparation.Model)
			} else {
				preparation.FullInput, preparation.FullInputExists = mergeResponsesHTTPInput(
					state.ReplayInput, true, current, currentExists, true,
				)
				preparation.FullInput, _ = pruneUnansweredResponsesHTTPToolCalls(preparation.FullInput)
			}
		}
	}
	if responsesHTTPRawItemsHaveToolOutput(preparation.FullInput) &&
		!responsesHTTPRawItemsHaveToolCallContextForOutputs(preparation.FullInput) {
		preparation.MissingToolContext = true
	}
	if responsesHTTPRawMessagesSize(preparation.FullInput) > responsesHTTPReplayLimitBytes() {
		preparation.ContextTooLarge = true
	}
	c.Set(responsesHTTPContinuationContextKey, preparation)
	switch {
	case preparation.PreviousResponseID == "":
		setResponsesHTTPContinuationStatus(c, "root")
	case preparation.ContinuationConflict != "":
		setResponsesHTTPContinuationStatus(c, "model_conflict")
	case preparation.ContextTooLarge:
		setResponsesHTTPContinuationStatus(c, "too_large")
	case preparation.MissingToolContext:
		setResponsesHTTPContinuationStatus(c, "orphan_tool_output")
	case preparation.StateFound && preparation.ReplayComplete:
		setResponsesHTTPContinuationStatus(c, "restored")
	default:
		setResponsesHTTPContinuationStatus(c, "miss")
	}
}

// ResponsesHTTPContinuationBillingRequest returns a copy whose input reflects
// the recoverable full context. Callers use it for token estimation only; the
// original request remains untouched for upstreams with native continuation.
func ResponsesHTTPContinuationBillingRequest(c *gin.Context, request *dto.OpenAIResponsesRequest) (*dto.OpenAIResponsesRequest, bool) {
	preparation := getResponsesHTTPPreparation(c)
	if request == nil || preparation == nil || !preparation.StateFound || !preparation.ReplayComplete || preparation.ContinuationConflict != "" ||
		preparation.ContextTooLarge || !preparation.FullInputExists {
		return request, false
	}
	copyRequest, err := common.DeepCopy(request)
	if err != nil {
		return request, false
	}
	encoded, err := common.Marshal(preparation.FullInput)
	if err != nil {
		return request, false
	}
	copyRequest.Input = encoded
	return copyRequest, true
}

// ResponsesHTTPContinuationTokenCountMeta counts the complete replay JSON,
// including assistant output, reasoning and tool-call arguments that the
// generic Responses input parser intentionally ignores.
func ResponsesHTTPContinuationTokenCountMeta(c *gin.Context, request *dto.OpenAIResponsesRequest) (*types.TokenCountMeta, bool) {
	expanded, ok := ResponsesHTTPContinuationBillingRequest(c, request)
	if !ok || expanded == nil {
		return nil, false
	}
	meta := expanded.GetTokenCountMeta()
	inputJSON := strings.TrimSpace(string(expanded.Input))
	withoutInput, err := common.DeepCopy(expanded)
	if err != nil {
		return nil, false
	}
	withoutInput.Input = nil
	otherMeta := withoutInput.GetTokenCountMeta()
	meta.CombineText = inputJSON
	if strings.TrimSpace(otherMeta.CombineText) != "" {
		if meta.CombineText != "" {
			meta.CombineText += "\n"
		}
		meta.CombineText += otherMeta.CombineText
	}
	return meta, true
}

// ResponsesHTTPContinuationPreferredAccountID exposes the account that created
// the restored state. Callers may use it as observability or a future soft
// affinity hint; it must not be treated as a hard availability requirement.
func ResponsesHTTPContinuationPreferredAccountID(c *gin.Context) int {
	preparation := getResponsesHTTPPreparation(c)
	if preparation == nil || !preparation.StateFound || !preparation.ReplayComplete {
		return 0
	}
	return preparation.PreferredAccountID
}

// ClearResponsesHTTPContinuationPersistTarget drops any sticky persist
// fingerprint before channel or account retries.
func ClearResponsesHTTPContinuationPersistTarget(c *gin.Context) {
	if c == nil {
		return
	}
	c.Set(responsesHTTPPersistTargetContextKey, (*ResponsesHTTPPersistTarget)(nil))
}

// MarkResponsesHTTPContinuationPersistTarget records that this attempt is the
// Codex HTTP path that may create continuation state, and promotes any
// request-local Redis payloads into the process L1 cache.
func MarkResponsesHTTPContinuationPersistTarget(c *gin.Context) {
	if c == nil || !responsesHTTPContinuationPersistEligible(c) {
		return
	}
	target := &ResponsesHTTPPersistTarget{
		ChannelID: common.GetContextKeyInt(c, appconstant.ContextKeyChannelId),
		PoolID:    common.GetContextKeyInt(c, appconstant.ContextKeyUpstreamAccountPoolId),
		AccountID: common.GetContextKeyInt(c, appconstant.ContextKeyUpstreamAccountId),
	}
	if target.ChannelID <= 0 || target.PoolID <= 0 || target.AccountID <= 0 {
		return
	}
	c.Set(responsesHTTPPersistTargetContextKey, target)
	promotePendingResponsesHTTPL1(c)
}

func responsesHTTPContinuationPersistEligible(c *gin.Context) bool {
	if c == nil {
		return false
	}
	if common.GetContextKeyBool(c, appconstant.ContextKeyResponsesWebSocketIngress) {
		return false
	}
	if common.GetContextKeyInt(c, appconstant.ContextKeyChannelType) != appconstant.ChannelTypeCodex {
		return false
	}
	if common.GetContextKeyInt(c, appconstant.ContextKeyUpstreamAccountPoolId) <= 0 {
		return false
	}
	if common.GetContextKeyInt(c, appconstant.ContextKeyUpstreamAccountId) <= 0 {
		return false
	}
	platform := strings.TrimSpace(common.GetContextKeyString(c, appconstant.ContextKeyUpstreamAccountPlatform))
	accountType := strings.TrimSpace(common.GetContextKeyString(c, appconstant.ContextKeyUpstreamAccountType))
	return platform == appconstant.UpstreamPlatformOpenAI && accountType == appconstant.UpstreamAccountTypeOAuth
}

func getResponsesHTTPPersistTarget(c *gin.Context) *ResponsesHTTPPersistTarget {
	if c == nil {
		return nil
	}
	value, ok := c.Get(responsesHTTPPersistTargetContextKey)
	if !ok {
		return nil
	}
	target, _ := value.(*ResponsesHTTPPersistTarget)
	return target
}

func responsesHTTPPersistTargetMatches(c *gin.Context) bool {
	target := getResponsesHTTPPersistTarget(c)
	if target == nil {
		return false
	}
	return target.ChannelID == common.GetContextKeyInt(c, appconstant.ContextKeyChannelId) &&
		target.PoolID == common.GetContextKeyInt(c, appconstant.ContextKeyUpstreamAccountPoolId) &&
		target.AccountID == common.GetContextKeyInt(c, appconstant.ContextKeyUpstreamAccountId) &&
		responsesHTTPContinuationPersistEligible(c)
}

// ApplyResponsesHTTPContinuationForCodex expands the input required by the
// stateless Codex HTTP endpoint. Plain messages fail open when history is not
// available; tool outputs fail closed because sending an orphan output is not a
// valid Responses request.
func ApplyResponsesHTTPContinuationForCodex(c *gin.Context, request *dto.OpenAIResponsesRequest) *types.NewAPIError {
	if request == nil || strings.TrimSpace(request.PreviousResponseID) == "" {
		return nil
	}
	preparation := getResponsesHTTPPreparation(c)
	if preparation == nil {
		current, currentExists, err := normalizeResponsesHTTPInput(request.Input)
		if err != nil {
			return types.NewErrorWithStatusCode(err, types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
		if currentExists && responsesHTTPRawItemsHaveToolOutput(current) &&
			!responsesHTTPRawItemsHaveToolCallContextForOutputs(current) {
			return orphanResponsesHTTPToolOutputError()
		}
		return nil
	}
	if preparation.ContinuationConflict != "" {
		return types.NewErrorWithStatusCode(errors.New(preparation.ContinuationConflict), types.ErrorCodeInvalidRequest, http.StatusConflict, types.ErrOptionWithSkipRetry())
	}
	if preparation.ContextTooLarge {
		return types.NewErrorWithStatusCode(errors.New("Responses HTTP replay context exceeds the configured request size limit"), types.ErrorCodeInvalidRequest, http.StatusRequestEntityTooLarge, types.ErrOptionWithSkipRetry())
	}
	if preparation.MissingToolContext {
		return orphanResponsesHTTPToolOutputError()
	}
	if !preparation.StateFound || !preparation.ReplayComplete || !preparation.FullInputExists {
		setResponsesHTTPContinuationStatus(c, "fallback_short_input")
		logger.LogWarn(c, "Responses HTTP continuation unavailable; forwarding current input only")
		return nil
	}
	encoded, err := common.Marshal(preparation.FullInput)
	if err != nil {
		return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}
	request.Input = encoded
	setResponsesHTTPContinuationStatus(c, "expanded")
	return nil
}

func setResponsesHTTPContinuationStatus(c *gin.Context, status string) {
	if c != nil {
		c.Set(responsesHTTPStatusContextKey, status)
	}
}

func setResponsesHTTPPersistStatus(c *gin.Context, status string) {
	if c != nil && status != "" {
		c.Set(responsesHTTPPersistStatusContextKey, status)
	}
}

func setResponsesHTTPLocalCacheStatus(c *gin.Context, status string) {
	if c != nil && status != "" {
		c.Set(responsesHTTPLocalCacheContextKey, status)
	}
}

func setResponsesHTTPRedisStatus(c *gin.Context, status string) {
	if c != nil && status != "" {
		c.Set(responsesHTTPRedisStatusContextKey, status)
	}
}

func orphanResponsesHTTPToolOutputError() *types.NewAPIError {
	return types.NewErrorWithStatusCode(
		errors.New("tool output continuation cannot be recovered without its matching tool call context"),
		types.ErrorCodeInvalidRequest,
		http.StatusConflict,
		types.ErrOptionWithSkipRetry(),
	)
}

// RecordDeliveredResponsesHTTPEvent advances continuation state only after an
// SSE frame has been written and flushed to the downstream client.
func RecordDeliveredResponsesHTTPEvent(c *gin.Context, data []byte) {
	if c == nil || len(data) == 0 || getResponsesHTTPPreparation(c) == nil {
		return
	}
	var event map[string]json.RawMessage
	if common.Unmarshal(data, &event) != nil {
		return
	}
	var eventType string
	_ = common.Unmarshal(event["type"], &eventType)
	delivered := getResponsesHTTPDeliveredContext(c)
	switch strings.TrimSpace(eventType) {
	case "response.created", "response.in_progress":
		if responseID, _ := responsesHTTPResponseEnvelope(event["response"]); responseID != "" {
			delivered.ResponseID = responseID
		}
	case "response.output_item.done":
		item := bytes.TrimSpace(event["item"])
		if len(item) != 0 && !bytes.Equal(item, []byte("null")) {
			delivered.Output = appendResponsesHTTPRawMessageOnce(delivered.Output, item)
			var envelope struct {
				Type string `json:"type"`
			}
			if common.Unmarshal(item, &envelope) == nil && isResponsesHTTPToolCallContextType(envelope.Type) {
				delivered.DeliveredToolCall = true
			}
		}
		// Deferred: do not commit on intermediate tool items.
	case "response.completed", "response.done", "response.incomplete":
		responseID, output := responsesHTTPResponseEnvelope(event["response"])
		if responseID != "" {
			delivered.ResponseID = responseID
		}
		if output != nil {
			delivered.Output = output
		}
		if delivered.ResponseID != "" {
			CommitResponsesHTTPContinuation(c, delivered.ResponseID, delivered.Output)
			delivered.TerminalCommitted = true
		}
	}
	c.Set(responsesHTTPDeliveredContextKey, delivered)
}

// FinalizeResponsesHTTPContinuationStream commits once when a stream ends
// after tool calls were delivered without a terminal Responses event.
func FinalizeResponsesHTTPContinuationStream(c *gin.Context) {
	if c == nil || getResponsesHTTPPreparation(c) == nil {
		return
	}
	delivered := getResponsesHTTPDeliveredContext(c)
	if delivered.TerminalCommitted || !delivered.DeliveredToolCall || strings.TrimSpace(delivered.ResponseID) == "" {
		return
	}
	CommitResponsesHTTPContinuation(c, delivered.ResponseID, delivered.Output)
	delivered.TerminalCommitted = true
	c.Set(responsesHTTPDeliveredContextKey, delivered)
}

// CommitResponsesHTTPContinuation stores the exact input sent for this turn
// plus downstream-visible response output under the new response ID.
func CommitResponsesHTTPContinuation(c *gin.Context, responseID string, output []json.RawMessage) {
	preparation := getResponsesHTTPPreparation(c)
	responseID = strings.TrimSpace(responseID)
	if preparation == nil || responseID == "" || !preparation.FullInputExists || !preparation.ReplayComplete || preparation.ContinuationConflict != "" || preparation.ContextTooLarge {
		return
	}
	if !responsesHTTPPersistTargetMatches(c) {
		setResponsesHTTPPersistStatus(c, "skipped_scope")
		return
	}
	replayInput := cloneResponsesHTTPRawMessages(preparation.FullInput)
	replayInput = append(replayInput, cloneResponsesHTTPRawMessages(output)...)
	if responsesHTTPRawMessagesSize(replayInput) > responsesHTTPReplayLimitBytes() {
		setResponsesHTTPPersistStatus(c, "skipped_replay_limit")
		return
	}
	putResponsesHTTPContinuation(c, responseID, responsesHTTPContinuationState{
		Model:       preparation.Model,
		ReplayInput: replayInput,
		AccountID:   common.GetContextKeyInt(c, appconstant.ContextKeyUpstreamAccountId),
		Complete:    true,
	})
}

// StageResponsesHTTPResponseOutput preserves raw output fields while a forced
// upstream SSE response is buffered for a non-stream downstream. The output is
// committed only if the later downstream body write succeeds.
func StageResponsesHTTPResponseOutput(c *gin.Context, responseID string, output []json.RawMessage) {
	if c == nil || strings.TrimSpace(responseID) == "" {
		return
	}
	c.Set(responsesHTTPStagedOutputContextKey, &responsesHTTPStagedOutput{
		ResponseID: strings.TrimSpace(responseID),
		Output:     cloneResponsesHTTPRawMessages(output),
	})
}

// PreferStagedResponsesHTTPOutput returns lossless output captured from the
// original SSE terminal event when the buffered DTO has omitted unknown fields.
func PreferStagedResponsesHTTPOutput(c *gin.Context, responseID string, fallback []json.RawMessage) []json.RawMessage {
	if c == nil {
		return fallback
	}
	value, ok := c.Get(responsesHTTPStagedOutputContextKey)
	if !ok {
		return fallback
	}
	staged, _ := value.(*responsesHTTPStagedOutput)
	if staged == nil || staged.ResponseID != strings.TrimSpace(responseID) {
		return fallback
	}
	return cloneResponsesHTTPRawMessages(staged.Output)
}

// ResponsesHTTPResponseEnvelope extracts lossless output items from a raw
// Responses response object.
func ResponsesHTTPResponseEnvelope(raw json.RawMessage) (string, []json.RawMessage) {
	return responsesHTTPResponseEnvelope(raw)
}

func responsesHTTPResponseEnvelope(raw json.RawMessage) (string, []json.RawMessage) {
	var response struct {
		ID     string            `json:"id"`
		Output []json.RawMessage `json:"output"`
	}
	if len(bytes.TrimSpace(raw)) == 0 || common.Unmarshal(raw, &response) != nil {
		return "", nil
	}
	return strings.TrimSpace(response.ID), cloneResponsesHTTPRawMessages(response.Output)
}

func getResponsesHTTPPreparation(c *gin.Context) *responsesHTTPContinuationPreparation {
	if c == nil {
		return nil
	}
	value, ok := c.Get(responsesHTTPContinuationContextKey)
	if !ok {
		return nil
	}
	preparation, _ := value.(*responsesHTTPContinuationPreparation)
	return preparation
}

func getResponsesHTTPDeliveredContext(c *gin.Context) *responsesHTTPDeliveredContext {
	if c != nil {
		if value, ok := c.Get(responsesHTTPDeliveredContextKey); ok {
			if delivered, ok := value.(*responsesHTTPDeliveredContext); ok && delivered != nil {
				return delivered
			}
		}
	}
	return &responsesHTTPDeliveredContext{}
}

func responsesHTTPContinuationScope(c *gin.Context) string {
	if c == nil {
		return ""
	}
	userID := common.GetContextKeyInt(c, appconstant.ContextKeyUserId)
	tokenID := common.GetContextKeyInt(c, appconstant.ContextKeyTokenId)
	if userID <= 0 && tokenID <= 0 {
		return ""
	}
	return fmt.Sprintf("%d:%d", userID, tokenID)
}

func responsesHTTPContinuationCacheKey(c *gin.Context, responseID string) string {
	scope := responsesHTTPContinuationScope(c)
	responseID = strings.TrimSpace(responseID)
	if scope == "" || responseID == "" {
		return ""
	}
	digest := sha256.Sum256([]byte(scope + ":" + responseID))
	return fmt.Sprintf("responses:http:continuation:%x", digest)
}

func getResponsesHTTPContinuation(c *gin.Context, responseID string, promoteToL1 bool) (responsesHTTPContinuationState, bool) {
	key := responsesHTTPContinuationCacheKey(c, responseID)
	if key == "" {
		return responsesHTTPContinuationState{}, false
	}
	if encoded, ok := defaultResponsesHTTPContinuationCache.getEncoded(key); ok {
		state, err := decodeResponsesHTTPContinuationState(encoded)
		if err != nil {
			return responsesHTTPContinuationState{}, false
		}
		return state, true
	}
	if !common.RedisEnabled || common.RDB == nil {
		return responsesHTTPContinuationState{}, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), responsesHTTPRedisTimeout)
	if c != nil && c.Request != nil {
		ctx, cancel = context.WithTimeout(c.Request.Context(), responsesHTTPRedisTimeout)
	}
	defer cancel()
	encoded, err := common.RDB.Get(ctx, key).Bytes()
	if err != nil {
		if !errors.Is(err, redis.Nil) {
			logger.LogWarn(c, "Responses HTTP continuation Redis read failed: "+err.Error())
		}
		return responsesHTTPContinuationState{}, false
	}
	state, decodeErr := decodeResponsesHTTPContinuationState(encoded)
	if decodeErr != nil {
		return responsesHTTPContinuationState{}, false
	}
	if promoteToL1 {
		status := defaultResponsesHTTPContinuationCache.putEncoded(key, encoded)
		setResponsesHTTPLocalCacheStatus(c, status)
	} else {
		stashPendingResponsesHTTPL1(c, key, encoded)
	}
	return state, true
}

func responsesHTTPRedisWriteContext(c *gin.Context) (context.Context, context.CancelFunc) {
	baseCtx := context.Background()
	if c != nil && c.Request != nil {
		baseCtx = context.WithoutCancel(c.Request.Context())
	}
	return context.WithTimeout(baseCtx, responsesHTTPRedisTimeout)
}

func stashPendingResponsesHTTPL1(c *gin.Context, key string, encoded []byte) {
	if c == nil || key == "" || len(encoded) == 0 {
		return
	}
	pending := getPendingResponsesHTTPL1(c)
	pending[key] = append([]byte(nil), encoded...)
	c.Set(responsesHTTPPendingL1ContextKey, pending)
}

func getPendingResponsesHTTPL1(c *gin.Context) map[string][]byte {
	if c == nil {
		return map[string][]byte{}
	}
	if value, ok := c.Get(responsesHTTPPendingL1ContextKey); ok {
		if pending, ok := value.(map[string][]byte); ok && pending != nil {
			return pending
		}
	}
	return map[string][]byte{}
}

func promotePendingResponsesHTTPL1(c *gin.Context) {
	pending := getPendingResponsesHTTPL1(c)
	if len(pending) == 0 {
		return
	}
	for key, encoded := range pending {
		status := defaultResponsesHTTPContinuationCache.putEncoded(key, encoded)
		setResponsesHTTPLocalCacheStatus(c, status)
	}
	c.Set(responsesHTTPPendingL1ContextKey, map[string][]byte{})
}

func putResponsesHTTPContinuation(c *gin.Context, responseID string, state responsesHTTPContinuationState) {
	key := responsesHTTPContinuationCacheKey(c, responseID)
	if key == "" {
		return
	}
	state.ReplayInput = cloneResponsesHTTPRawMessages(state.ReplayInput)
	encoded, err := common.Marshal(state)
	if err != nil {
		return
	}
	limits := defaultResponsesHTTPContinuationCache.limits
	payloadSize := int64(len(encoded))
	if payloadSize > limits.RedisMaxEntryBytes {
		setResponsesHTTPPersistStatus(c, "skipped_redis_entry_limit")
		setResponsesHTTPRedisStatus(c, "rejected_too_large")
		return
	}

	localStatus := defaultResponsesHTTPContinuationCache.putEncoded(key, encoded)
	setResponsesHTTPLocalCacheStatus(c, localStatus)
	setResponsesHTTPPersistStatus(c, "stored")

	if !common.RedisEnabled || common.RDB == nil {
		setResponsesHTTPRedisStatus(c, "disabled")
		return
	}
	allowed, isProbe := defaultResponsesHTTPRedisCircuit.allow()
	if !allowed {
		setResponsesHTTPRedisStatus(c, "oom_circuit")
		return
	}
	ctx, cancel := responsesHTTPRedisWriteContext(c)
	defer cancel()
	if err := common.RDB.Set(ctx, key, encoded, limits.TTL).Err(); err != nil {
		isOOM := isResponsesHTTPRedisOOMError(err)
		defaultResponsesHTTPRedisCircuit.fail(isOOM)
		if isOOM {
			setResponsesHTTPRedisStatus(c, "oom_circuit")
			if defaultResponsesHTTPRedisCircuit.shouldLog() {
				logger.LogWarn(c, "Responses HTTP continuation Redis write failed: "+err.Error())
			}
			return
		}
		setResponsesHTTPRedisStatus(c, "error")
		if isProbe {
			// Non-OOM probe failure keeps circuit closed via fail(false).
		}
		if defaultResponsesHTTPRedisCircuit.shouldLog() {
			logger.LogWarn(c, "Responses HTTP continuation Redis write failed: "+err.Error())
		}
		return
	}
	defaultResponsesHTTPRedisCircuit.success()
	if isProbe {
		setResponsesHTTPRedisStatus(c, "recovered")
	} else {
		setResponsesHTTPRedisStatus(c, "ok")
	}
}

func decodeResponsesHTTPContinuationState(encoded []byte) (responsesHTTPContinuationState, error) {
	var state responsesHTTPContinuationState
	if err := common.Unmarshal(encoded, &state); err != nil {
		return responsesHTTPContinuationState{}, err
	}
	return cloneResponsesHTTPContinuationState(state), nil
}

func cloneResponsesHTTPContinuationState(state responsesHTTPContinuationState) responsesHTTPContinuationState {
	state.ReplayInput = cloneResponsesHTTPRawMessages(state.ReplayInput)
	return state
}

func normalizeResponsesHTTPInput(raw json.RawMessage) ([]json.RawMessage, bool, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, false, nil
	}
	if bytes.Equal(trimmed, []byte("null")) {
		return []json.RawMessage{}, true, nil
	}
	switch trimmed[0] {
	case '[':
		var items []json.RawMessage
		if err := common.Unmarshal(trimmed, &items); err != nil {
			return nil, false, err
		}
		return items, true, nil
	case '{':
		return []json.RawMessage{append(json.RawMessage(nil), trimmed...)}, true, nil
	case '"':
		var text string
		if err := common.Unmarshal(trimmed, &text); err != nil {
			return nil, false, err
		}
		if strings.TrimSpace(text) == "" {
			return []json.RawMessage{}, true, nil
		}
		message, err := common.Marshal(map[string]any{"type": "message", "role": "user", "content": text})
		if err != nil {
			return nil, false, err
		}
		return []json.RawMessage{message}, true, nil
	default:
		return nil, false, errors.New("Responses input must be a string, object, or list")
	}
}

func mergeResponsesHTTPInput(previous []json.RawMessage, previousExists bool, current []json.RawMessage, currentExists bool, hasPreviousResponseID bool) ([]json.RawMessage, bool) {
	if !hasPreviousResponseID || !previousExists {
		return cloneResponsesHTTPRawMessages(current), currentExists
	}
	if !currentExists || len(current) == 0 {
		return cloneResponsesHTTPRawMessages(previous), true
	}
	if responsesHTTPRawMessagesHavePrefix(current, previous) {
		return cloneResponsesHTTPRawMessages(current), true
	}
	merged := make([]json.RawMessage, 0, len(previous)+len(current))
	merged = append(merged, cloneResponsesHTTPRawMessages(previous)...)
	merged = append(merged, cloneResponsesHTTPRawMessages(current)...)
	return merged, true
}

func responsesHTTPRawMessagesHavePrefix(items, prefix []json.RawMessage) bool {
	if len(prefix) > len(items) {
		return false
	}
	for i := range prefix {
		if !bytes.Equal(responsesHTTPCanonicalJSON(items[i]), responsesHTTPCanonicalJSON(prefix[i])) {
			return false
		}
	}
	return true
}

func responsesHTTPCanonicalJSON(raw json.RawMessage) []byte {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil
	}
	var value any
	if common.Unmarshal(trimmed, &value) != nil {
		return trimmed
	}
	canonical, err := common.Marshal(value)
	if err != nil {
		return trimmed
	}
	return canonical
}

func cloneResponsesHTTPRawMessages(items []json.RawMessage) []json.RawMessage {
	if items == nil {
		return nil
	}
	cloned := make([]json.RawMessage, len(items))
	for i := range items {
		cloned[i] = append(json.RawMessage(nil), items[i]...)
	}
	return cloned
}

func appendResponsesHTTPRawMessageOnce(items []json.RawMessage, item json.RawMessage) []json.RawMessage {
	canonical := responsesHTTPCanonicalJSON(item)
	for _, existing := range items {
		if bytes.Equal(responsesHTTPCanonicalJSON(existing), canonical) {
			return items
		}
	}
	return append(items, append(json.RawMessage(nil), item...))
}

func responsesHTTPRawMessagesSize(items []json.RawMessage) int64 {
	var size int64
	for _, item := range items {
		size += int64(len(item))
	}
	return size
}

func responsesHTTPReplayLimitBytes() int64 {
	limit := int64(appconstant.MaxRequestBodyMB) * 1024 * 1024
	if limit <= 0 {
		return 128 * 1024 * 1024
	}
	return limit
}

func isResponsesHTTPToolCallContextType(itemType string) bool {
	switch strings.TrimSpace(itemType) {
	case "tool_call", "function_call", "local_shell_call", "tool_search_call", "custom_tool_call", "mcp_tool_call", "computer_call":
		return true
	default:
		return false
	}
}

func isResponsesHTTPToolCallOutputType(itemType string) bool {
	switch strings.TrimSpace(itemType) {
	case "function_call_output", "local_shell_call_output", "tool_search_output", "custom_tool_call_output", "mcp_tool_call_output", "computer_call_output":
		return true
	default:
		return false
	}
}

func responsesHTTPRawItemsHaveToolOutput(items []json.RawMessage) bool {
	for _, item := range items {
		var envelope struct {
			Type string `json:"type"`
		}
		if common.Unmarshal(item, &envelope) == nil && isResponsesHTTPToolCallOutputType(envelope.Type) {
			return true
		}
	}
	return false
}

func responsesHTTPRawItemsHaveToolCallContextForOutputs(items []json.RawMessage) bool {
	contextCallIDs := make(map[string]struct{})
	outputCallIDs := make(map[string]struct{})
	for _, item := range items {
		var envelope struct {
			Type   string `json:"type"`
			CallID string `json:"call_id"`
		}
		if common.Unmarshal(item, &envelope) != nil {
			continue
		}
		callID := strings.TrimSpace(envelope.CallID)
		switch {
		case isResponsesHTTPToolCallContextType(envelope.Type):
			if callID != "" {
				contextCallIDs[callID] = struct{}{}
			}
		case isResponsesHTTPToolCallOutputType(envelope.Type):
			if callID == "" {
				return false
			}
			outputCallIDs[callID] = struct{}{}
		}
	}
	for callID := range outputCallIDs {
		if _, ok := contextCallIDs[callID]; !ok {
			return false
		}
	}
	return true
}

func pruneUnansweredResponsesHTTPToolCalls(items []json.RawMessage) ([]json.RawMessage, bool) {
	outputCallIDs := make(map[string]struct{})
	for _, item := range items {
		var envelope struct {
			Type   string `json:"type"`
			CallID string `json:"call_id"`
		}
		if common.Unmarshal(item, &envelope) == nil && isResponsesHTTPToolCallOutputType(envelope.Type) {
			if callID := strings.TrimSpace(envelope.CallID); callID != "" {
				outputCallIDs[callID] = struct{}{}
			}
		}
	}
	filtered := make([]json.RawMessage, 0, len(items))
	pruned := false
	for _, item := range items {
		var envelope struct {
			Type   string `json:"type"`
			CallID string `json:"call_id"`
		}
		if common.Unmarshal(item, &envelope) == nil && isResponsesHTTPToolCallContextType(envelope.Type) {
			callID := strings.TrimSpace(envelope.CallID)
			if _, answered := outputCallIDs[callID]; callID == "" || !answered {
				pruned = true
				continue
			}
		}
		filtered = append(filtered, append(json.RawMessage(nil), item...))
	}
	return filtered, pruned
}
