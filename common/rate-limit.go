package common

import (
	"sync"
	"time"
)

type InMemoryRateLimiter struct {
	store              map[string]*[]int64
	reservations       map[string]map[uint64]int64
	sequence           uint64
	mutex              sync.Mutex
	expirationDuration time.Duration
}

func (l *InMemoryRateLimiter) Init(expirationDuration time.Duration) {
	l.mutex.Lock()
	if l.store != nil {
		if l.reservations == nil {
			l.reservations = make(map[string]map[uint64]int64)
		}
		l.mutex.Unlock()
		return
	}
	l.store = make(map[string]*[]int64)
	l.reservations = make(map[string]map[uint64]int64)
	l.expirationDuration = expirationDuration
	l.mutex.Unlock()
	if expirationDuration > 0 {
		go l.clearExpiredItems()
	}
}

func (l *InMemoryRateLimiter) clearExpiredItems() {
	for {
		time.Sleep(l.expirationDuration)
		l.mutex.Lock()
		if l.expirationDuration <= 0 {
			l.mutex.Unlock()
			return
		}
		now := time.Now().Unix()
		for key := range l.store {
			queue := l.store[key]
			size := len(*queue)
			if size == 0 || now-(*queue)[size-1] > int64(l.expirationDuration.Seconds()) {
				delete(l.store, key)
			}
		}
		for key, pending := range l.reservations {
			for token, createdAt := range pending {
				if now-createdAt > int64(l.expirationDuration.Seconds()) {
					delete(pending, token)
				}
			}
			if len(pending) == 0 {
				delete(l.reservations, key)
			}
		}
		l.mutex.Unlock()
	}
}

// Request parameter duration's unit is seconds
func (l *InMemoryRateLimiter) Request(key string, maxRequestNum int, duration int64) bool {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	if maxRequestNum <= 0 {
		return true
	}
	if l.store == nil {
		l.store = make(map[string]*[]int64)
		l.reservations = make(map[string]map[uint64]int64)
	}
	// [old <-- new]
	queue, ok := l.store[key]
	now := time.Now().Unix()
	if ok {
		// Drop every entry outside the request window before evaluating the
		// limit.  The previous implementation removed at most one entry, which
		// could leave stale entries occupying the bucket after a quiet period.
		*queue = removeExpiredRateLimitItems(*queue, now, duration)
		if len(*queue) < maxRequestNum {
			*queue = append(*queue, now)
			return true
		} else {
			return false
		}
	} else {
		s := make([]int64, 0, maxRequestNum)
		l.store[key] = &s
		*(l.store[key]) = append(*(l.store[key]), now)
	}
	return true
}

// Reserve reserves one slot in a sliding window without counting it as a
// completed request. The caller must later call Finalize with the returned
// token. Pending reservations count toward the limit, which keeps concurrent
// admissions bounded without allowing failed requests to consume the success
// quota permanently.
func (l *InMemoryRateLimiter) Reserve(key string, maxRequestNum int, duration int64) (uint64, bool) {
	if maxRequestNum <= 0 || duration <= 0 {
		return 0, true
	}
	l.mutex.Lock()
	defer l.mutex.Unlock()
	if l.store == nil {
		l.store = make(map[string]*[]int64)
		l.reservations = make(map[string]map[uint64]int64)
	}
	now := time.Now().Unix()
	queue := l.store[key]
	if queue != nil {
		*queue = removeExpiredRateLimitItems(*queue, now, duration)
	}
	pending := l.reservations[key]
	for token, createdAt := range pending {
		if duration > 0 && now-createdAt >= duration {
			delete(pending, token)
		}
	}
	if len(pending) == 0 {
		delete(l.reservations, key)
		pending = nil
	}
	queueSize := 0
	if queue != nil {
		queueSize = len(*queue)
	}
	if queueSize+len(pending) >= maxRequestNum {
		return 0, false
	}
	l.sequence++
	if pending == nil {
		pending = make(map[uint64]int64)
		l.reservations[key] = pending
	}
	pending[l.sequence] = now
	return l.sequence, true
}

// Finalize converts a reservation into a completed request only when success
// is true. A failed request releases its reservation and therefore cannot
// trigger a later false success-limit 429.
func (l *InMemoryRateLimiter) Finalize(key string, token uint64, success bool) {
	if token == 0 {
		return
	}
	l.mutex.Lock()
	defer l.mutex.Unlock()
	pending := l.reservations[key]
	if _, exists := pending[token]; !exists {
		return
	}
	delete(pending, token)
	if len(pending) == 0 {
		delete(l.reservations, key)
	}
	if !success {
		return
	}
	queue := l.store[key]
	if queue == nil {
		items := make([]int64, 0, 1)
		queue = &items
		l.store[key] = queue
	}
	*queue = append(*queue, time.Now().Unix())
}

// Reset clears all state. It is primarily useful for tests and for callers
// that intentionally replace a limiter policy in-process.
func (l *InMemoryRateLimiter) Reset() {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	l.store = nil
	l.reservations = nil
	l.sequence = 0
	l.expirationDuration = 0
}

func removeExpiredRateLimitItems(queue []int64, now int64, duration int64) []int64 {
	if len(queue) == 0 {
		return queue
	}
	if duration <= 0 {
		return queue[:0]
	}
	firstValid := 0
	for firstValid < len(queue) && now-queue[firstValid] >= duration {
		firstValid++
	}
	if firstValid == 0 {
		return queue
	}
	if firstValid == len(queue) {
		return queue[:0]
	}
	return queue[firstValid:]
}
