package service

import (
	"container/list"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	appconstant "github.com/QuantumNous/new-api/constant"
)

const (
	responsesHTTPContinuationDefaultLocalBudgetMB   = 256
	responsesHTTPContinuationDefaultLocalMaxEntryMB = 64
	responsesHTTPContinuationDefaultMaxEntries      = 4096
	responsesHTTPContinuationEntryOverheadBytes     = 128
	responsesHTTPContinuationRedisOOMCooldown       = 30 * time.Second
	responsesHTTPContinuationRedisOOMJitter         = 15 * time.Second
)

type responsesHTTPContinuationLimits struct {
	LocalBudgetBytes   int64
	LocalMaxEntryBytes int64
	RedisMaxEntryBytes int64
	MaxEntries         int
	TTL                time.Duration
	RedisOOMCooldown   time.Duration
	RedisOOMJitter     time.Duration
}

func loadResponsesHTTPContinuationLimits() responsesHTTPContinuationLimits {
	replayLimit := responsesHTTPReplayLimitBytes()
	limits := responsesHTTPContinuationLimits{
		LocalBudgetBytes:   int64(responsesHTTPContinuationDefaultLocalBudgetMB) << 20,
		LocalMaxEntryBytes: int64(responsesHTTPContinuationDefaultLocalMaxEntryMB) << 20,
		RedisMaxEntryBytes: replayLimit,
		MaxEntries:         responsesHTTPContinuationDefaultMaxEntries,
		TTL:                responsesHTTPContinuationTTL,
		RedisOOMCooldown:   responsesHTTPContinuationRedisOOMCooldown,
		RedisOOMJitter:     responsesHTTPContinuationRedisOOMJitter,
	}
	if v := envInt64("RESPONSES_HTTP_CONT_LOCAL_BUDGET_MB"); v > 0 {
		limits.LocalBudgetBytes = v << 20
	}
	if v := envInt64("RESPONSES_HTTP_CONT_LOCAL_MAX_ENTRY_MB"); v > 0 {
		limits.LocalMaxEntryBytes = v << 20
	}
	if v := envInt64("RESPONSES_HTTP_CONT_REDIS_MAX_ENTRY_MB"); v > 0 {
		limits.RedisMaxEntryBytes = v << 20
	} else if appconstant.MaxRequestBodyMB > 0 {
		limits.RedisMaxEntryBytes = int64(appconstant.MaxRequestBodyMB) << 20
	}
	if v := envInt("RESPONSES_HTTP_CONT_TTL_MINUTES"); v > 0 {
		limits.TTL = time.Duration(v) * time.Minute
	}
	if v := envInt("RESPONSES_HTTP_CONT_MAX_ENTRIES"); v > 0 {
		limits.MaxEntries = v
	}
	if limits.LocalMaxEntryBytes > limits.LocalBudgetBytes {
		limits.LocalMaxEntryBytes = limits.LocalBudgetBytes
	}
	if limits.RedisMaxEntryBytes <= 0 {
		limits.RedisMaxEntryBytes = replayLimit
	}
	return limits
}

func envInt(name string) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return 0
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0
	}
	return v
}

func envInt64(name string) int64 {
	return int64(envInt(name))
}

type responsesHTTPCacheEntry struct {
	key         string
	encoded     []byte
	payloadSize int64
	expiresAt   time.Time
	element     *list.Element
}

type responsesHTTPContinuationCache struct {
	mu           sync.Mutex
	entries      map[string]*responsesHTTPCacheEntry
	lru          *list.List
	currentBytes int64
	limits       responsesHTTPContinuationLimits
}

func newResponsesHTTPContinuationCache(limits responsesHTTPContinuationLimits) *responsesHTTPContinuationCache {
	return &responsesHTTPContinuationCache{
		entries: make(map[string]*responsesHTTPCacheEntry, 256),
		lru:     list.New(),
		limits:  limits,
	}
}

var defaultResponsesHTTPContinuationCache = newResponsesHTTPContinuationCache(loadResponsesHTTPContinuationLimits())

func resetResponsesHTTPContinuationCacheForTest(limits responsesHTTPContinuationLimits) {
	defaultResponsesHTTPContinuationCache = newResponsesHTTPContinuationCache(limits)
}

func (cache *responsesHTTPContinuationCache) currentPayloadBytes() int64 {
	if cache == nil {
		return 0
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	return cache.currentBytes
}

func (cache *responsesHTTPContinuationCache) entryCount() int {
	if cache == nil {
		return 0
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	return len(cache.entries)
}

func responsesHTTPCacheBillableBytes(key string, payloadSize int64) int64 {
	return int64(len(key)) + payloadSize + responsesHTTPContinuationEntryOverheadBytes
}

func (cache *responsesHTTPContinuationCache) getEncoded(key string) ([]byte, bool) {
	if cache == nil || key == "" {
		return nil, false
	}
	now := time.Now()
	cache.mu.Lock()
	defer cache.mu.Unlock()
	entry, ok := cache.entries[key]
	if !ok {
		return nil, false
	}
	if !now.Before(entry.expiresAt) {
		cache.removeLocked(entry)
		return nil, false
	}
	cache.lru.MoveToFront(entry.element)
	cloned := append([]byte(nil), entry.encoded...)
	return cloned, true
}

// putEncoded stores an immutable payload. Returns local cache status:
// stored | replaced | skipped_too_large | evicted_stored.
func (cache *responsesHTTPContinuationCache) putEncoded(key string, encoded []byte) string {
	if cache == nil || key == "" || len(encoded) == 0 {
		return "skipped"
	}
	payloadSize := int64(len(encoded))
	billable := responsesHTTPCacheBillableBytes(key, payloadSize)
	if payloadSize > cache.limits.LocalMaxEntryBytes || billable > cache.limits.LocalBudgetBytes {
		return "skipped_too_large"
	}

	now := time.Now()
	expiresAt := now.Add(cache.limits.TTL)
	cloned := append([]byte(nil), encoded...)

	cache.mu.Lock()
	defer cache.mu.Unlock()
	cache.expireLocked(now)

	evicted := false
	if existing, ok := cache.entries[key]; ok {
		cache.currentBytes -= responsesHTTPCacheBillableBytes(existing.key, existing.payloadSize)
		existing.encoded = cloned
		existing.payloadSize = payloadSize
		existing.expiresAt = expiresAt
		cache.currentBytes += billable
		cache.lru.MoveToFront(existing.element)
		cache.evictLocked()
		if cache.currentBytes > cache.limits.LocalBudgetBytes || len(cache.entries) > cache.limits.MaxEntries {
			// Should not happen after evict; drop this entry if still over.
			cache.removeLocked(existing)
			return "skipped_too_large"
		}
		return "replaced"
	}

	for cache.currentBytes+billable > cache.limits.LocalBudgetBytes || len(cache.entries) >= cache.limits.MaxEntries {
		if cache.lru.Len() == 0 {
			return "skipped_too_large"
		}
		cache.evictOldestLocked()
		evicted = true
	}

	entry := &responsesHTTPCacheEntry{
		key:         key,
		encoded:     cloned,
		payloadSize: payloadSize,
		expiresAt:   expiresAt,
	}
	entry.element = cache.lru.PushFront(entry)
	cache.entries[key] = entry
	cache.currentBytes += billable
	if evicted {
		return "evicted_stored"
	}
	return "stored"
}

func (cache *responsesHTTPContinuationCache) expireLocked(now time.Time) {
	for key, entry := range cache.entries {
		if !now.Before(entry.expiresAt) {
			_ = key
			cache.removeLocked(entry)
		}
	}
}

func (cache *responsesHTTPContinuationCache) evictLocked() {
	for cache.currentBytes > cache.limits.LocalBudgetBytes || len(cache.entries) > cache.limits.MaxEntries {
		if cache.lru.Len() == 0 {
			return
		}
		cache.evictOldestLocked()
	}
}

func (cache *responsesHTTPContinuationCache) evictOldestLocked() {
	element := cache.lru.Back()
	if element == nil {
		return
	}
	entry, _ := element.Value.(*responsesHTTPCacheEntry)
	if entry == nil {
		cache.lru.Remove(element)
		return
	}
	cache.removeLocked(entry)
}

func (cache *responsesHTTPContinuationCache) removeLocked(entry *responsesHTTPCacheEntry) {
	if entry == nil {
		return
	}
	if current, ok := cache.entries[entry.key]; !ok || current != entry {
		return
	}
	delete(cache.entries, entry.key)
	if entry.element != nil {
		cache.lru.Remove(entry.element)
		entry.element = nil
	}
	cache.currentBytes -= responsesHTTPCacheBillableBytes(entry.key, entry.payloadSize)
	if cache.currentBytes < 0 {
		cache.currentBytes = 0
	}
}

type responsesHTTPRedisCircuit struct {
	mu            sync.Mutex
	openUntil     time.Time
	probeInFlight bool
	lastLogAt     time.Time
	tripCount     int64
	cooldown      time.Duration
	jitter        time.Duration
}

var defaultResponsesHTTPRedisCircuit = &responsesHTTPRedisCircuit{
	cooldown: responsesHTTPContinuationRedisOOMCooldown,
	jitter:   responsesHTTPContinuationRedisOOMJitter,
}

func resetResponsesHTTPRedisCircuitForTest() {
	defaultResponsesHTTPRedisCircuit = &responsesHTTPRedisCircuit{
		cooldown: responsesHTTPContinuationRedisOOMCooldown,
		jitter:   responsesHTTPContinuationRedisOOMJitter,
	}
}

func (circuit *responsesHTTPRedisCircuit) allow() (allowed bool, isProbe bool) {
	if circuit == nil {
		return true, false
	}
	now := time.Now()
	circuit.mu.Lock()
	defer circuit.mu.Unlock()
	if circuit.openUntil.IsZero() || now.After(circuit.openUntil) || now.Equal(circuit.openUntil) {
		if !circuit.openUntil.IsZero() && (now.After(circuit.openUntil) || now.Equal(circuit.openUntil)) {
			if circuit.probeInFlight {
				return false, false
			}
			circuit.probeInFlight = true
			return true, true
		}
		return true, false
	}
	return false, false
}

func (circuit *responsesHTTPRedisCircuit) success() {
	if circuit == nil {
		return
	}
	circuit.mu.Lock()
	defer circuit.mu.Unlock()
	circuit.openUntil = time.Time{}
	circuit.probeInFlight = false
}

func (circuit *responsesHTTPRedisCircuit) fail(isOOM bool) {
	if circuit == nil {
		return
	}
	circuit.mu.Lock()
	defer circuit.mu.Unlock()
	circuit.probeInFlight = false
	if !isOOM {
		return
	}
	circuit.tripCount++
	jitter := time.Duration(0)
	if circuit.jitter > 0 {
		jitter = time.Duration(rand.Int63n(int64(circuit.jitter)))
	}
	cooldown := circuit.cooldown
	if cooldown <= 0 {
		cooldown = responsesHTTPContinuationRedisOOMCooldown
	}
	circuit.openUntil = time.Now().Add(cooldown + jitter)
}

func (circuit *responsesHTTPRedisCircuit) shouldLog() bool {
	if circuit == nil {
		return true
	}
	circuit.mu.Lock()
	defer circuit.mu.Unlock()
	now := time.Now()
	if circuit.lastLogAt.IsZero() || now.Sub(circuit.lastLogAt) >= time.Minute {
		circuit.lastLogAt = now
		return true
	}
	return false
}

func isResponsesHTTPRedisOOMError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "oom") || strings.Contains(msg, "maxmemory")
}
