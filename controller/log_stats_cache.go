package controller

import (
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ttlCache is intentionally process-local. These values are dashboard hints,
// so a short-lived per-process cache avoids adding another Redis dependency to
// the log/stat hot path (and still works while Redis is degraded).
type ttlCache[V any] struct {
	mu         sync.Mutex
	items      map[string]ttlCacheItem[V]
	ttl        time.Duration
	maxEntries int
}

type ttlCacheItem[V any] struct {
	value     V
	expiresAt time.Time
}

func newTTLCache[V any](ttl time.Duration, maxEntries int) *ttlCache[V] {
	return &ttlCache[V]{
		items:      make(map[string]ttlCacheItem[V]),
		ttl:        ttl,
		maxEntries: maxEntries,
	}
}

func (c *ttlCache[V]) Get(key string) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	item, ok := c.items[key]
	if !ok || time.Now().After(item.expiresAt) {
		if ok {
			delete(c.items, key)
		}
		var zero V
		return zero, false
	}
	return item.value, true
}

func (c *ttlCache[V]) Set(key string, value V) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.items) >= c.maxEntries {
		now := time.Now()
		for itemKey, item := range c.items {
			if now.After(item.expiresAt) {
				delete(c.items, itemKey)
			}
		}
		if len(c.items) >= c.maxEntries {
			// Dashboard filters are unbounded user input. If the cache is full,
			// evict one arbitrary entry rather than allowing unbounded growth.
			for itemKey := range c.items {
				delete(c.items, itemKey)
				break
			}
		}
	}
	c.items[key] = ttlCacheItem[V]{value: value, expiresAt: time.Now().Add(c.ttl)}
}

// dashboardCacheKey canonicalizes query parameters and buckets a moving end
// timestamp, allowing refreshes in the same short window to share a result.
func dashboardCacheKey(path string, query url.Values, scope string, identity string, bucket time.Duration) string {
	values := make(url.Values, len(query))
	for key, items := range query {
		values[key] = append([]string(nil), items...)
	}
	if raw := values.Get("end_timestamp"); raw != "" && bucket > 0 {
		if timestamp, err := strconv.ParseInt(raw, 10, 64); err == nil {
			seconds := int64(bucket / time.Second)
			if seconds > 0 {
				values.Set("end_timestamp", strconv.FormatInt(timestamp-timestamp%seconds, 10))
			}
		}
	}
	return strings.Join([]string{scope, identity, path, values.Encode()}, "|")
}
