package service

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
)

const (
	upstreamModelTransientTTL           = 30 * time.Minute
	upstreamModelTransientSecondBackoff = 10 * time.Second
	upstreamModelTransientLaterBackoff  = 45 * time.Second
	upstreamCapacityRetryBaseDelay      = 500 * time.Millisecond
	upstreamCapacityRetryMaximumDelay   = 8 * time.Second
	upstreamCapacityRetryDefaultCount   = 2
	upstreamModelTransientMaxEntries    = 100_000
)

type upstreamModelTransientState struct {
	Failures     int
	BlockedUntil time.Time
	ExpiresAt    time.Time
}

var upstreamModelTransientRegistry = struct {
	sync.Mutex
	entries map[string]upstreamModelTransientState
}{entries: make(map[string]upstreamModelTransientState)}

func upstreamModelTransientKey(accountID int, modelName string) string {
	return strconv.Itoa(accountID) + ":" + strings.ToLower(strings.TrimSpace(modelName))
}

// RecordUpstreamAccountModelTransientFailure records only a model-scoped
// capacity signal. The first exhausted request remains immediately eligible;
// repeated failures add short scheduler backoffs without mutating account rows.
func RecordUpstreamAccountModelTransientFailure(accountID int, modelName string) time.Duration {
	key := upstreamModelTransientKey(accountID, modelName)
	if accountID <= 0 || strings.TrimSpace(modelName) == "" || key == "" {
		return 0
	}
	now := time.Now()
	upstreamModelTransientRegistry.Lock()
	defer upstreamModelTransientRegistry.Unlock()
	for existingKey, existingState := range upstreamModelTransientRegistry.entries {
		if now.After(existingState.ExpiresAt) {
			delete(upstreamModelTransientRegistry.entries, existingKey)
		}
	}
	if _, exists := upstreamModelTransientRegistry.entries[key]; !exists &&
		len(upstreamModelTransientRegistry.entries) >= upstreamModelTransientMaxEntries {
		var oldestKey string
		var oldestExpiry time.Time
		for existingKey, existingState := range upstreamModelTransientRegistry.entries {
			if oldestKey == "" || existingState.ExpiresAt.Before(oldestExpiry) {
				oldestKey = existingKey
				oldestExpiry = existingState.ExpiresAt
			}
		}
		if oldestKey != "" {
			delete(upstreamModelTransientRegistry.entries, oldestKey)
		}
	}
	state := upstreamModelTransientRegistry.entries[key]
	if !state.ExpiresAt.IsZero() && now.After(state.ExpiresAt) {
		state = upstreamModelTransientState{}
	}
	state.Failures++
	state.ExpiresAt = now.Add(upstreamModelTransientTTL)
	var backoff time.Duration
	switch state.Failures {
	case 1:
		state.BlockedUntil = time.Time{}
	case 2:
		backoff = upstreamModelTransientSecondBackoff
		state.BlockedUntil = now.Add(backoff)
	default:
		backoff = upstreamModelTransientLaterBackoff
		state.BlockedUntil = now.Add(backoff)
	}
	upstreamModelTransientRegistry.entries[key] = state
	return backoff
}

func IsUpstreamAccountModelTransientBlocked(accountID int, modelName string) bool {
	key := upstreamModelTransientKey(accountID, modelName)
	if accountID <= 0 || strings.TrimSpace(modelName) == "" || key == "" {
		return false
	}
	now := time.Now()
	upstreamModelTransientRegistry.Lock()
	defer upstreamModelTransientRegistry.Unlock()
	state, ok := upstreamModelTransientRegistry.entries[key]
	if !ok {
		return false
	}
	if now.After(state.ExpiresAt) {
		delete(upstreamModelTransientRegistry.entries, key)
		return false
	}
	return now.Before(state.BlockedUntil)
}

func ClearUpstreamAccountModelTransient(accountID int, modelName string) {
	key := upstreamModelTransientKey(accountID, modelName)
	if accountID <= 0 || strings.TrimSpace(modelName) == "" || key == "" {
		return
	}
	upstreamModelTransientRegistry.Lock()
	delete(upstreamModelTransientRegistry.entries, key)
	upstreamModelTransientRegistry.Unlock()
}

// UpstreamAccountCapacityRetryDelay returns a context-friendly same-account
// retry delay. retryCount is zero-based (0 => 500ms, 1 => 1s).
func UpstreamAccountCapacityRetryDelay(retryCount int) (time.Duration, bool) {
	maxRetries := common.GetEnvOrDefault("UPSTREAM_ACCOUNT_CAPACITY_SAME_ACCOUNT_RETRIES", upstreamCapacityRetryDefaultCount)
	if retryCount < 0 || retryCount >= maxRetries {
		return 0, false
	}
	delay := upstreamCapacityRetryBaseDelay << retryCount
	if delay > upstreamCapacityRetryMaximumDelay {
		delay = upstreamCapacityRetryMaximumDelay
	}
	return delay, true
}

func WaitForUpstreamAccountRetry(ctx context.Context, delay time.Duration) bool {
	if delay <= 0 {
		return true
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}
