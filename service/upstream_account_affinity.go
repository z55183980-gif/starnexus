package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/samber/hot"
)

const upstreamAccountAffinityCacheNamespace = "new-api:upstream_account_affinity:v1"

const ginKeyUpstreamAccountAffinityLogInfo = "upstream_account_affinity_log_info"

type upstreamAccountAffinityStore interface {
	Get(ctx context.Context, key string) (int, bool, error)
	BindIfAbsent(ctx context.Context, key string, accountID int, ttl time.Duration) (int, bool, error)
	Replace(ctx context.Context, key string, expectedAccountID int, accountID int, ttl time.Duration) (bool, error)
}

type defaultUpstreamAccountAffinityStore struct {
	memOnce sync.Once
	mem     *hot.HotCache[string, int]
	memMu   sync.Mutex
}

func newDefaultUpstreamAccountAffinityStore() upstreamAccountAffinityStore {
	return &defaultUpstreamAccountAffinityStore{}
}

func (s *defaultUpstreamAccountAffinityStore) memory() *hot.HotCache[string, int] {
	s.memOnce.Do(func() {
		s.mem = hot.NewHotCache[string, int](hot.LRU, 100_000).WithTTL(time.Hour).WithJanitor().Build()
	})
	return s.mem
}

func upstreamAccountAffinityFullKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	return upstreamAccountAffinityCacheNamespace + ":" + key
}

func (s *defaultUpstreamAccountAffinityStore) Get(ctx context.Context, key string) (int, bool, error) {
	fullKey := upstreamAccountAffinityFullKey(key)
	if fullKey == "" {
		return 0, false, nil
	}
	if common.RedisEnabled && common.RDB != nil {
		value, err := common.RDB.Get(ctx, fullKey).Result()
		if err != nil {
			if errors.Is(err, redis.Nil) {
				return 0, false, nil
			}
			return 0, false, err
		}
		accountID, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || accountID <= 0 {
			return 0, false, fmt.Errorf("invalid upstream account affinity value")
		}
		return accountID, true, nil
	}
	return s.memory().Get(fullKey)
}

func (s *defaultUpstreamAccountAffinityStore) BindIfAbsent(ctx context.Context, key string, accountID int, ttl time.Duration) (int, bool, error) {
	fullKey := upstreamAccountAffinityFullKey(key)
	if fullKey == "" || accountID <= 0 {
		return 0, false, nil
	}
	if common.RedisEnabled && common.RDB != nil {
		installed, err := common.RDB.SetNX(ctx, fullKey, accountID, ttl).Result()
		if err != nil {
			return 0, false, err
		}
		if installed {
			return accountID, true, nil
		}
		winner, found, err := s.Get(ctx, key)
		return winner, found && winner == accountID, err
	}

	s.memMu.Lock()
	defer s.memMu.Unlock()
	if current, found, _ := s.memory().Get(fullKey); found {
		return current, current == accountID, nil
	}
	s.memory().SetWithTTL(fullKey, accountID, ttl)
	return accountID, true, nil
}

func (s *defaultUpstreamAccountAffinityStore) Replace(ctx context.Context, key string, expectedAccountID int, accountID int, ttl time.Duration) (bool, error) {
	fullKey := upstreamAccountAffinityFullKey(key)
	if fullKey == "" || expectedAccountID <= 0 || accountID <= 0 {
		return false, nil
	}
	if common.RedisEnabled && common.RDB != nil {
		const compareAndSet = `
if redis.call("GET", KEYS[1]) == ARGV[1] then
  redis.call("SET", KEYS[1], ARGV[2], "PX", ARGV[3])
  return 1
end
return 0`
		result, err := common.RDB.Eval(ctx, compareAndSet, []string{fullKey}, expectedAccountID, accountID, ttl.Milliseconds()).Int()
		return result == 1, err
	}

	s.memMu.Lock()
	defer s.memMu.Unlock()
	current, found, _ := s.memory().Get(fullKey)
	if !found || current != expectedAccountID {
		return false, nil
	}
	s.memory().SetWithTTL(fullKey, accountID, ttl)
	return true, nil
}

func buildUpstreamAccountAffinityKey(poolID int, modelName string, seed string) string {
	if poolID <= 0 || strings.TrimSpace(seed) == "" {
		return ""
	}
	return fmt.Sprintf("%d:%s:%s", poolID, strings.ToLower(strings.TrimSpace(modelName)), strings.TrimSpace(seed))
}

func MarkUpstreamAccountAffinitySelection(c *gin.Context, info *UpstreamAccountAffinitySelectionInfo) {
	if c == nil || info == nil || !info.Enabled {
		return
	}
	c.Set(ginKeyUpstreamAccountAffinityLogInfo, map[string]interface{}{
		"enabled":              true,
		"key_fp":               info.KeyFingerprint,
		"cache_hit":            info.CacheHit,
		"preferred_account_id": info.PreferredAccountID,
		"selected_account_id":  info.SelectedAccountID,
		"outcome":              info.Outcome,
	})
}

func AppendUpstreamAccountAffinityAdminInfo(c *gin.Context, adminInfo map[string]interface{}) {
	if c == nil || adminInfo == nil {
		return
	}
	value, ok := c.Get(ginKeyUpstreamAccountAffinityLogInfo)
	if !ok {
		return
	}
	info, ok := value.(map[string]interface{})
	if !ok || len(info) == 0 {
		return
	}
	adminInfo["upstream_account_affinity"] = info
}
