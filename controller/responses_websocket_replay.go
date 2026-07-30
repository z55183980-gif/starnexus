package controller

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	appconstant "github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/types"
)

func (s *responsesWebSocketSession) prepareReplayPayload(turn *responsesWebSocketTurn, prepared json.RawMessage) (json.RawMessage, *types.NewAPIError) {
	if s == nil || turn == nil {
		return nil, types.NewError(fmt.Errorf("responses websocket replay state is unavailable"), types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}
	var payload map[string]json.RawMessage
	if err := common.Unmarshal(prepared, &payload); err != nil {
		return nil, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}
	currentInput, currentInputExists, err := responsesWSNormalizedInput(payload["input"])
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}

	s.mu.Lock()
	previousInput := cloneResponsesWSRawMessages(s.replayInput)
	previousInputExists := s.replayInputExists
	hasUpstream := s.upstream != nil
	s.mu.Unlock()

	previousResponseID := ""
	if turn.originalRequest != nil {
		previousResponseID = strings.TrimSpace(turn.originalRequest.PreviousResponseID)
	}
	fullInput, fullInputExists := mergeResponsesWSReplayInput(previousInput, previousInputExists, currentInput, currentInputExists, previousResponseID != "")
	turn.replayInput = cloneResponsesWSRawMessages(fullInput)
	turn.replayInputExists = fullInputExists

	statelessReplay := turn.upstreamMode == model.UpstreamOpenAIWSModeHTTPBridge ||
		(responsesWSModeUsesUpstreamWebSocket(turn.upstreamMode) && previousResponseID != "" && !hasUpstream && previousInputExists)
	if !statelessReplay {
		return prepared, nil
	}
	if responsesWSRawItemsHaveToolOutput(fullInput) && !responsesWSRawItemsHaveToolCallContextForOutputs(fullInput) {
		return nil, types.NewErrorWithStatusCode(
			fmt.Errorf("tool output continuation cannot be recovered without its matching tool call context"),
			types.ErrorCodeInvalidRequest,
			http.StatusConflict,
			types.ErrOptionWithSkipRetry(),
		)
	}
	if replayBytes := responsesWSRawMessagesSize(fullInput); replayBytes > responsesWSReplayLimitBytes() {
		return nil, types.NewErrorWithStatusCode(
			fmt.Errorf("Responses WebSocket replay context exceeds the configured request size limit"),
			types.ErrorCodeInvalidRequest,
			http.StatusRequestEntityTooLarge,
			types.ErrOptionWithSkipRetry(),
		)
	}
	delete(payload, "previous_response_id")
	if fullInputExists {
		encodedInput, marshalErr := common.Marshal(fullInput)
		if marshalErr != nil {
			return nil, types.NewError(marshalErr, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}
		payload["input"] = encodedInput
	}
	encoded, marshalErr := common.Marshal(payload)
	if marshalErr != nil {
		return nil, types.NewError(marshalErr, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}
	return encoded, nil
}

func responsesWSNormalizedInput(raw json.RawMessage) ([]json.RawMessage, bool, error) {
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
		return nil, false, fmt.Errorf("Responses input must be a string, object, or list")
	}
}

func mergeResponsesWSReplayInput(previous []json.RawMessage, previousExists bool, current []json.RawMessage, currentExists bool, hasPreviousResponseID bool) ([]json.RawMessage, bool) {
	if !hasPreviousResponseID || !previousExists {
		return cloneResponsesWSRawMessages(current), currentExists
	}
	if !currentExists || len(current) == 0 {
		return cloneResponsesWSRawMessages(previous), true
	}
	if responsesWSRawMessagesHavePrefix(current, previous) {
		return cloneResponsesWSRawMessages(current), true
	}
	merged := make([]json.RawMessage, 0, len(previous)+len(current))
	merged = append(merged, cloneResponsesWSRawMessages(previous)...)
	merged = append(merged, cloneResponsesWSRawMessages(current)...)
	return merged, true
}

func responsesWSRawMessagesHavePrefix(items, prefix []json.RawMessage) bool {
	if len(prefix) > len(items) {
		return false
	}
	for i := range prefix {
		if !bytes.Equal(responsesWSCanonicalJSON(items[i]), responsesWSCanonicalJSON(prefix[i])) {
			return false
		}
	}
	return true
}

func responsesWSCanonicalJSON(raw json.RawMessage) []byte {
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

func responsesWSRawMessagesSize(items []json.RawMessage) int64 {
	var size int64
	for _, item := range items {
		size += int64(len(item))
	}
	return size
}

func responsesWSReplayLimitBytes() int64 {
	limit := int64(appconstant.MaxRequestBodyMB) * 1024 * 1024
	if limit <= 0 {
		return 128 * 1024 * 1024
	}
	return limit
}

func prepareResponsesWSStatelessReplayOutbound(turn *responsesWebSocketTurn, outbound []byte) ([]byte, error) {
	if turn == nil || !turn.replayInputExists {
		return nil, errors.New("Responses WebSocket replay input is unavailable")
	}
	fullInput := cloneResponsesWSRawMessages(turn.replayInput)
	if responsesWSRawItemsHaveToolOutput(fullInput) && !responsesWSRawItemsHaveToolCallContextForOutputs(fullInput) {
		return nil, errors.New("tool output continuation cannot be recovered without its matching tool call context")
	}
	if responsesWSRawMessagesSize(fullInput) > responsesWSReplayLimitBytes() {
		return nil, errors.New("Responses WebSocket replay context exceeds the configured request size limit")
	}
	var payload map[string]json.RawMessage
	if err := common.Unmarshal(outbound, &payload); err != nil {
		return nil, err
	}
	delete(payload, "previous_response_id")
	encodedInput, err := common.Marshal(fullInput)
	if err != nil {
		return nil, err
	}
	payload["input"] = encodedInput
	return common.Marshal(payload)
}

func cloneResponsesWSRawMessages(items []json.RawMessage) []json.RawMessage {
	if items == nil {
		return nil
	}
	cloned := make([]json.RawMessage, len(items))
	for i := range items {
		cloned[i] = append(json.RawMessage(nil), items[i]...)
	}
	return cloned
}

func (t *responsesWebSocketTurn) collectReplayContext(data []byte) {
	if t == nil || len(data) == 0 {
		return
	}
	var event map[string]json.RawMessage
	if err := common.Unmarshal(data, &event); err != nil {
		return
	}
	var eventType string
	_ = common.Unmarshal(event["type"], &eventType)
	switch strings.TrimSpace(eventType) {
	case "response.output_item.done":
		t.addReplayToolContext(event["item"])
	case "response.completed", "response.done":
		var response struct {
			Output []json.RawMessage `json:"output"`
		}
		if common.Unmarshal(event["response"], &response) == nil {
			for _, item := range response.Output {
				t.addReplayToolContext(item)
			}
		}
	}
}

func (t *responsesWebSocketTurn) addReplayToolContext(item json.RawMessage) {
	if t == nil || len(bytes.TrimSpace(item)) == 0 {
		return
	}
	var envelope struct {
		Type   string `json:"type"`
		ID     string `json:"id"`
		CallID string `json:"call_id"`
	}
	if common.Unmarshal(item, &envelope) != nil || !isResponsesWSToolCallContextType(envelope.Type) {
		return
	}
	key := strings.TrimSpace(envelope.ID)
	if key == "" {
		key = strings.TrimSpace(envelope.CallID)
	}
	if key == "" {
		key = string(bytes.TrimSpace(item))
	}
	if t.replayToolContextSeen == nil {
		t.replayToolContextSeen = make(map[string]struct{})
	}
	if _, exists := t.replayToolContextSeen[key]; exists {
		return
	}
	t.replayToolContextSeen[key] = struct{}{}
	t.replayToolContext = append(t.replayToolContext, append(json.RawMessage(nil), item...))
}

func isResponsesWSToolCallContextType(itemType string) bool {
	switch strings.TrimSpace(itemType) {
	case "tool_call", "function_call", "local_shell_call", "tool_search_call", "custom_tool_call", "mcp_tool_call":
		return true
	default:
		return false
	}
}

func isResponsesWSToolCallOutputType(itemType string) bool {
	switch strings.TrimSpace(itemType) {
	case "function_call_output", "tool_search_output", "custom_tool_call_output", "mcp_tool_call_output":
		return true
	default:
		return false
	}
}

func responsesWSRawItemsHaveToolOutput(items []json.RawMessage) bool {
	for _, item := range items {
		var envelope struct {
			Type string `json:"type"`
		}
		if common.Unmarshal(item, &envelope) == nil && isResponsesWSToolCallOutputType(envelope.Type) {
			return true
		}
	}
	return false
}

func responsesWSRawItemsHaveToolCallContextForOutputs(items []json.RawMessage) bool {
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
		case isResponsesWSToolCallContextType(envelope.Type):
			if callID != "" {
				contextCallIDs[callID] = struct{}{}
			}
		case isResponsesWSToolCallOutputType(envelope.Type):
			if callID == "" {
				return false
			}
			outputCallIDs[callID] = struct{}{}
		}
	}
	if len(outputCallIDs) == 0 {
		return true
	}
	for callID := range outputCallIDs {
		if _, ok := contextCallIDs[callID]; !ok {
			return false
		}
	}
	return true
}

func (s *responsesWebSocketSession) commitReplayState(turn *responsesWebSocketTurn) {
	if s == nil || turn == nil {
		return
	}
	if turn.replayInputExists {
		committed := cloneResponsesWSRawMessages(turn.replayInput)
		committed = append(committed, cloneResponsesWSRawMessages(turn.replayToolContext)...)
		s.replayInput = committed
		s.replayInputExists = true
	}
	if turn.turnState != "" {
		s.turnState = turn.turnState
	}
	if turn.accountID > 0 && s.lockedAccountID == 0 {
		s.lockedAccountID = turn.accountID
		s.lockedWSMode = turn.upstreamMode
	}
	if turn.channel != nil && s.lockedChannelID == 0 {
		s.lockedChannelID = turn.channel.Id
	}
	if turn.accumulator == nil {
		return
	}
	responseID := strings.TrimSpace(turn.accumulator.ResponseID())
	if responseID == "" {
		return
	}
	s.lastResponseID = responseID
	channelID := 0
	if turn.channel != nil {
		channelID = turn.channel.Id
	}
	modelName := ""
	if turn.info != nil {
		modelName = turn.info.OriginModelName
	}
	defaultResponsesWSContinuationStore.put(s.baseCtx, responseID, responsesWSContinuationState{
		accountID:         turn.accountID,
		channelID:         channelID,
		upstreamMode:      turn.upstreamMode,
		model:             modelName,
		turnState:         s.turnState,
		replayInput:       s.replayInput,
		replayInputExists: s.replayInputExists,
	})
}
