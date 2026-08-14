package controller

import (
	"container/list"
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
	responsesWSContinuationTTL               = 2 * time.Hour
	responsesWSContinuationMaxEntries        = 65_536
	responsesWSContinuationDefaultMaxBytesMB = 256
	responsesWSContinuationEntryOverhead     = 256
	responsesWSContinuationRawItemOverhead   = 24
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
	mu             sync.Mutex
	entries        map[string]*responsesWSContinuationCacheEntry
	lru            *list.List
	expiry         *list.List
	ttl            time.Duration
	maxEntries     int
	maxBytes       int64
	estimatedBytes int64
	metrics        responsesWSContinuationCacheMetrics
}

type responsesWSContinuationCacheEntry struct {
	key            string
	state          responsesWSContinuationState
	estimatedBytes int64
	lruElement     *list.Element
	expiryElement  *list.Element
}

type responsesWSContinuationCacheMetrics struct {
	hits                 uint64
	misses               uint64
	puts                 uint64
	replacements         uint64
	expiredEvictions     uint64
	entryLimitEvictions  uint64
	byteLimitEvictions   uint64
	oversizedRejections  uint64
	disabledRejections   uint64
	cleanupRuns          uint64
	cleanupDurationTotal uint64
	cleanupDurationLast  uint64
	cleanupDurationMax   uint64
}

// ResponsesWSContinuationCacheStats is a point-in-time snapshot of the
// WebSocket continuation replay cache. EstimatedBytes includes the retained
// payload and a conservative fixed per-entry overhead, but excludes temporary
// copies returned to active requests.
type ResponsesWSContinuationCacheStats struct {
	Entries                  int     `json:"entries"`
	EstimatedBytes           int64   `json:"estimated_bytes"`
	MaxEntries               int     `json:"max_entries"`
	MaxBytes                 int64   `json:"max_bytes"`
	HitsTotal                uint64  `json:"hits_total"`
	MissesTotal              uint64  `json:"misses_total"`
	HitRatio                 float64 `json:"hit_ratio"`
	PutsTotal                uint64  `json:"puts_total"`
	ReplacementsTotal        uint64  `json:"replacements_total"`
	ExpiredEvictionsTotal    uint64  `json:"expired_evictions_total"`
	EntryLimitEvictionsTotal uint64  `json:"entry_limit_evictions_total"`
	ByteLimitEvictionsTotal  uint64  `json:"byte_limit_evictions_total"`
	OversizedRejectionsTotal uint64  `json:"oversized_rejections_total"`
	DisabledRejectionsTotal  uint64  `json:"disabled_rejections_total"`
	CleanupRunsTotal         uint64  `json:"cleanup_runs_total"`
	CleanupDurationNsTotal   uint64  `json:"cleanup_duration_ns_total"`
	CleanupDurationNsLast    uint64  `json:"cleanup_duration_ns_last"`
	CleanupDurationNsMax     uint64  `json:"cleanup_duration_ns_max"`
	OldestEntryAgeSeconds    int64   `json:"oldest_entry_age_seconds"`
}

var defaultResponsesWSContinuationStore = newResponsesWSContinuationStore(
	responsesWSContinuationMaxEntries,
	configuredResponsesWSContinuationMaxBytes(),
	responsesWSContinuationTTL,
)

func configuredResponsesWSContinuationMaxBytes() int64 {
	maxMB := common.GetEnvOrDefault("RESPONSES_WS_CONTINUATION_CACHE_MAX_MB", responsesWSContinuationDefaultMaxBytesMB)
	if maxMB <= 0 {
		return 0
	}
	return int64(maxMB) * 1024 * 1024
}

func newResponsesWSContinuationStore(maxEntries int, maxBytes int64, ttl time.Duration) *responsesWSContinuationStore {
	if maxEntries < 0 {
		maxEntries = 0
	}
	if maxBytes < 0 {
		maxBytes = 0
	}
	if ttl <= 0 {
		ttl = responsesWSContinuationTTL
	}
	return &responsesWSContinuationStore{
		entries:    make(map[string]*responsesWSContinuationCacheEntry, 256),
		lru:        list.New(),
		expiry:     list.New(),
		ttl:        ttl,
		maxEntries: maxEntries,
		maxBytes:   maxBytes,
	}
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
	entry, ok := s.entries[key]
	if ok && !now.Before(entry.state.expiresAt) {
		s.removeEntryLocked(entry, responsesWSContinuationEvictionExpired)
		ok = false
	}
	if ok {
		s.metrics.hits++
		s.lru.MoveToFront(entry.lruElement)
	} else {
		s.metrics.misses++
	}
	var state responsesWSContinuationState
	if ok {
		state = entry.state
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
	state.expiresAt = now.Add(s.ttl)
	estimatedBytes := estimateResponsesWSContinuationEntryBytes(key, state)

	s.mu.Lock()
	s.metrics.puts++
	s.cleanupExpiredLocked(now)
	if existing := s.entries[key]; existing != nil {
		s.removeEntryLocked(existing, responsesWSContinuationEvictionNone)
		s.metrics.replacements++
	}
	if s.maxEntries == 0 || s.maxBytes == 0 {
		s.metrics.disabledRejections++
		s.mu.Unlock()
		return
	}
	if estimatedBytes > s.maxBytes {
		s.metrics.oversizedRejections++
		s.mu.Unlock()
		return
	}
	for len(s.entries) >= s.maxEntries {
		if !s.removeLRULocked(responsesWSContinuationEvictionEntryLimit) {
			break
		}
	}
	for s.estimatedBytes+estimatedBytes > s.maxBytes {
		if !s.removeLRULocked(responsesWSContinuationEvictionByteLimit) {
			break
		}
	}
	entry := &responsesWSContinuationCacheEntry{
		key:            key,
		state:          state,
		estimatedBytes: estimatedBytes,
	}
	entry.lruElement = s.lru.PushFront(entry)
	entry.expiryElement = s.expiry.PushBack(entry)
	s.entries[key] = entry
	s.estimatedBytes += estimatedBytes
	s.mu.Unlock()
}

type responsesWSContinuationEvictionReason uint8

const (
	responsesWSContinuationEvictionNone responsesWSContinuationEvictionReason = iota
	responsesWSContinuationEvictionExpired
	responsesWSContinuationEvictionEntryLimit
	responsesWSContinuationEvictionByteLimit
)

func estimateResponsesWSContinuationEntryBytes(key string, state responsesWSContinuationState) int64 {
	size := int64(responsesWSContinuationEntryOverhead + len(key) + len(state.upstreamMode) + len(state.model) + len(state.turnState) + len(state.replayInput)*responsesWSContinuationRawItemOverhead)
	for _, item := range state.replayInput {
		size += int64(len(item))
	}
	return size
}

func (s *responsesWSContinuationStore) cleanupExpiredLocked(now time.Time) {
	startedAt := time.Now()
	for element := s.expiry.Front(); element != nil; {
		entry := element.Value.(*responsesWSContinuationCacheEntry)
		if now.Before(entry.state.expiresAt) {
			break
		}
		next := element.Next()
		s.removeEntryLocked(entry, responsesWSContinuationEvictionExpired)
		element = next
	}
	duration := uint64(time.Since(startedAt).Nanoseconds())
	s.metrics.cleanupRuns++
	s.metrics.cleanupDurationTotal += duration
	s.metrics.cleanupDurationLast = duration
	if duration > s.metrics.cleanupDurationMax {
		s.metrics.cleanupDurationMax = duration
	}
}

func (s *responsesWSContinuationStore) removeLRULocked(reason responsesWSContinuationEvictionReason) bool {
	element := s.lru.Back()
	if element == nil {
		return false
	}
	s.removeEntryLocked(element.Value.(*responsesWSContinuationCacheEntry), reason)
	return true
}

func (s *responsesWSContinuationStore) removeEntryLocked(entry *responsesWSContinuationCacheEntry, reason responsesWSContinuationEvictionReason) {
	if entry == nil || s.entries[entry.key] != entry {
		return
	}
	delete(s.entries, entry.key)
	s.lru.Remove(entry.lruElement)
	s.expiry.Remove(entry.expiryElement)
	s.estimatedBytes -= entry.estimatedBytes
	if s.estimatedBytes < 0 {
		s.estimatedBytes = 0
	}
	switch reason {
	case responsesWSContinuationEvictionExpired:
		s.metrics.expiredEvictions++
	case responsesWSContinuationEvictionEntryLimit:
		s.metrics.entryLimitEvictions++
	case responsesWSContinuationEvictionByteLimit:
		s.metrics.byteLimitEvictions++
	}
}

func (s *responsesWSContinuationStore) snapshotStats() ResponsesWSContinuationCacheStats {
	if s == nil {
		return ResponsesWSContinuationCacheStats{}
	}
	now := time.Now()
	s.mu.Lock()
	s.cleanupExpiredLocked(now)
	metrics := s.metrics
	stats := ResponsesWSContinuationCacheStats{
		Entries:                  len(s.entries),
		EstimatedBytes:           s.estimatedBytes,
		MaxEntries:               s.maxEntries,
		MaxBytes:                 s.maxBytes,
		HitsTotal:                metrics.hits,
		MissesTotal:              metrics.misses,
		PutsTotal:                metrics.puts,
		ReplacementsTotal:        metrics.replacements,
		ExpiredEvictionsTotal:    metrics.expiredEvictions,
		EntryLimitEvictionsTotal: metrics.entryLimitEvictions,
		ByteLimitEvictionsTotal:  metrics.byteLimitEvictions,
		OversizedRejectionsTotal: metrics.oversizedRejections,
		DisabledRejectionsTotal:  metrics.disabledRejections,
		CleanupRunsTotal:         metrics.cleanupRuns,
		CleanupDurationNsTotal:   metrics.cleanupDurationTotal,
		CleanupDurationNsLast:    metrics.cleanupDurationLast,
		CleanupDurationNsMax:     metrics.cleanupDurationMax,
	}
	if totalGets := metrics.hits + metrics.misses; totalGets > 0 {
		stats.HitRatio = float64(metrics.hits) / float64(totalGets)
	}
	if oldest := s.expiry.Front(); oldest != nil {
		age := now.Sub(oldest.Value.(*responsesWSContinuationCacheEntry).state.updatedAt)
		if age > 0 {
			stats.OldestEntryAgeSeconds = int64(age / time.Second)
		}
	}
	s.mu.Unlock()
	return stats
}

func (s *responsesWSContinuationStore) resetStats() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.metrics = responsesWSContinuationCacheMetrics{}
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
