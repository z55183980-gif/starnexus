package controller

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	appconstant "github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

const (
	responsesWSContinuationTTL        = 2 * time.Hour
	responsesWSContinuationMaxEntries = 65_536
)

type responsesWSContinuationState struct {
	accountID         int
	channelID         int
	upstreamMode      string
	model             string
	turnState         string
	replayInput       []json.RawMessage
	replayInputExists bool
	expiresAt         time.Time
	updatedAt         time.Time
}

type responsesWSContinuationStore struct {
	mu      sync.Mutex
	entries map[string]responsesWSContinuationState
}

var defaultResponsesWSContinuationStore = &responsesWSContinuationStore{
	entries: make(map[string]responsesWSContinuationState, 256),
}

func responsesWSContinuationScope(c *gin.Context) string {
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

func responsesWSContinuationKey(c *gin.Context, responseID string) string {
	scope := responsesWSContinuationScope(c)
	responseID = strings.TrimSpace(responseID)
	if scope == "" || responseID == "" {
		return ""
	}
	return scope + ":" + responseID
}

func (s *responsesWSContinuationStore) get(c *gin.Context, responseID string) (responsesWSContinuationState, bool) {
	if s == nil {
		return responsesWSContinuationState{}, false
	}
	key := responsesWSContinuationKey(c, responseID)
	if key == "" {
		return responsesWSContinuationState{}, false
	}
	now := time.Now()
	s.mu.Lock()
	state, ok := s.entries[key]
	if ok && !now.Before(state.expiresAt) {
		delete(s.entries, key)
		ok = false
	}
	s.mu.Unlock()
	if !ok {
		return responsesWSContinuationState{}, false
	}
	state.replayInput = cloneResponsesWSRawMessages(state.replayInput)
	return state, true
}

func (s *responsesWSContinuationStore) put(c *gin.Context, responseID string, state responsesWSContinuationState) {
	if s == nil {
		return
	}
	key := responsesWSContinuationKey(c, responseID)
	if key == "" {
		return
	}
	now := time.Now()
	state.replayInput = cloneResponsesWSRawMessages(state.replayInput)
	state.updatedAt = now
	state.expiresAt = now.Add(responsesWSContinuationTTL)

	s.mu.Lock()
	for entryKey, entry := range s.entries {
		if !now.Before(entry.expiresAt) {
			delete(s.entries, entryKey)
		}
	}
	if _, exists := s.entries[key]; !exists && len(s.entries) >= responsesWSContinuationMaxEntries {
		oldestKey := ""
		var oldest time.Time
		for entryKey, entry := range s.entries {
			if oldestKey == "" || entry.updatedAt.Before(oldest) {
				oldestKey = entryKey
				oldest = entry.updatedAt
			}
		}
		if oldestKey != "" {
			delete(s.entries, oldestKey)
		}
	}
	s.entries[key] = state
	s.mu.Unlock()
}

func (s *responsesWebSocketSession) restoreContinuation(request *dto.OpenAIResponsesRequest) *types.NewAPIError {
	if s == nil || request == nil {
		return nil
	}
	previousResponseID := strings.TrimSpace(request.PreviousResponseID)
	if previousResponseID == "" {
		return nil
	}
	state, ok := defaultResponsesWSContinuationStore.get(s.baseCtx, previousResponseID)
	if !ok {
		return nil
	}
	requestedModel := strings.TrimSpace(request.Model)
	if requestedModel != "" && state.model != "" && requestedModel != state.model {
		return types.NewErrorWithStatusCode(
			fmt.Errorf("previous_response_id belongs to model %s, not %s", state.model, requestedModel),
			types.ErrorCodeInvalidRequest,
			http.StatusConflict,
			types.ErrOptionWithSkipRetry(),
		)
	}
	s.mu.Lock()
	if s.lockedModel == "" {
		s.lockedModel = state.model
	}
	s.lockedAccountID = state.accountID
	s.lockedChannelID = state.channelID
	s.lockedWSMode = state.upstreamMode
	s.turnState = state.turnState
	s.replayInput = cloneResponsesWSRawMessages(state.replayInput)
	s.replayInputExists = state.replayInputExists
	s.lastResponseID = previousResponseID
	s.mu.Unlock()
	return nil
}

func (s *responsesWebSocketSession) inferToolContinuation(request *dto.OpenAIResponsesRequest, envelope map[string]json.RawMessage) error {
	if s == nil || request == nil || strings.TrimSpace(request.PreviousResponseID) != "" {
		return nil
	}
	input, inputExists, err := responsesWSNormalizedInput(request.Input)
	if err != nil || !inputExists || !responsesWSRawItemsHaveToolOutput(input) {
		return err
	}
	s.mu.Lock()
	previousResponseID := s.lastResponseID
	s.mu.Unlock()
	if previousResponseID == "" {
		return nil
	}
	request.PreviousResponseID = previousResponseID
	encoded, err := common.Marshal(previousResponseID)
	if err != nil {
		return err
	}
	envelope["previous_response_id"] = encoded
	return nil
}
