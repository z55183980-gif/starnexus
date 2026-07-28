package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
)

const upstreamAccountLeaseKeyPrefix = "new-api:upstream-account:leases:"

var ErrUpstreamAccountConcurrencyFull = errors.New("upstream account concurrency is full")

type UpstreamAccountLeaseManager interface {
	Acquire(ctx context.Context, accountId int, limit int, ttl time.Duration) (*UpstreamAccountLease, error)
	Count(ctx context.Context, accountId int) (int, error)
	CountBatch(ctx context.Context, accountIds []int) (map[int]int, error)
}

type upstreamAccountLeaseBackend interface {
	refresh(ctx context.Context, lease *UpstreamAccountLease, ttl time.Duration) error
	release(ctx context.Context, lease *UpstreamAccountLease) error
}

type UpstreamAccountLease struct {
	Id        string    `json:"lease_id"`
	AccountId int       `json:"account_id"`
	ExpiresAt time.Time `json:"expires_at"`

	backend upstreamAccountLeaseBackend
	once    sync.Once
	err     error
}

func (lease *UpstreamAccountLease) Refresh(ctx context.Context, ttl time.Duration) error {
	if lease == nil || lease.backend == nil {
		return errors.New("invalid upstream account lease")
	}
	if ttl <= 0 {
		return errors.New("upstream account lease TTL must be positive")
	}
	if err := lease.backend.refresh(ctx, lease, ttl); err != nil {
		return err
	}
	lease.ExpiresAt = time.Now().Add(ttl)
	return nil
}

func (lease *UpstreamAccountLease) Release(ctx context.Context) error {
	if lease == nil || lease.backend == nil {
		return nil
	}
	lease.once.Do(func() {
		lease.err = lease.backend.release(ctx, lease)
	})
	return lease.err
}

func NewUpstreamAccountLeaseManager() (UpstreamAccountLeaseManager, error) {
	if common.RedisEnabled && common.RDB != nil {
		return &redisUpstreamAccountLeaseManager{client: common.RDB}, nil
	}
	if !common.GetEnvOrDefaultBool("UPSTREAM_ACCOUNT_ALLOW_LOCAL_LEASES", false) {
		return nil, errors.New("Redis is required for upstream account leases; enable UPSTREAM_ACCOUNT_ALLOW_LOCAL_LEASES only for a single-instance development environment")
	}
	return NewLocalUpstreamAccountLeaseManager(), nil
}

var (
	upstreamAccountLeaseAcquireScript = redis.NewScript(`
local key = KEYS[1]
local leaseID = ARGV[1]
local limit = tonumber(ARGV[2])
local ttlMs = tonumber(ARGV[3])
local nowParts = redis.call('TIME')
local nowMs = tonumber(nowParts[1]) * 1000 + math.floor(tonumber(nowParts[2]) / 1000)
redis.call('ZREMRANGEBYSCORE', key, '-inf', nowMs)
local count = redis.call('ZCARD', key)
if count >= limit then
  return {0, count}
end
redis.call('ZADD', key, nowMs + ttlMs, leaseID)
redis.call('PEXPIRE', key, ttlMs)
return {1, count + 1}
`)
	upstreamAccountLeaseRefreshScript = redis.NewScript(`
local key = KEYS[1]
local leaseID = ARGV[1]
local ttlMs = tonumber(ARGV[2])
if not redis.call('ZSCORE', key, leaseID) then
  return 0
end
local nowParts = redis.call('TIME')
local nowMs = tonumber(nowParts[1]) * 1000 + math.floor(tonumber(nowParts[2]) / 1000)
redis.call('ZADD', key, nowMs + ttlMs, leaseID)
redis.call('PEXPIRE', key, ttlMs)
return 1
`)
	upstreamAccountLeaseReleaseScript = redis.NewScript(`
return redis.call('ZREM', KEYS[1], ARGV[1])
`)
	upstreamAccountLeaseCountScript = redis.NewScript(`
local key = KEYS[1]
local nowParts = redis.call('TIME')
local nowMs = tonumber(nowParts[1]) * 1000 + math.floor(tonumber(nowParts[2]) / 1000)
redis.call('ZREMRANGEBYSCORE', key, '-inf', nowMs)
return redis.call('ZCARD', key)
`)
)

type redisUpstreamAccountLeaseManager struct {
	client *redis.Client
}

func (manager *redisUpstreamAccountLeaseManager) Acquire(ctx context.Context, accountId int, limit int, ttl time.Duration) (*UpstreamAccountLease, error) {
	if manager == nil || manager.client == nil || accountId <= 0 || limit <= 0 || ttl <= 0 {
		return nil, errors.New("invalid upstream account lease request")
	}
	leaseId := uuid.NewString()
	result, err := upstreamAccountLeaseAcquireScript.Run(ctx, manager.client, []string{upstreamAccountLeaseKey(accountId)}, leaseId, limit, ttl.Milliseconds()).Slice()
	if err != nil {
		return nil, fmt.Errorf("failed to acquire upstream account lease: %w", err)
	}
	if len(result) < 1 || toInt64(result[0]) != 1 {
		return nil, ErrUpstreamAccountConcurrencyFull
	}
	return &UpstreamAccountLease{Id: leaseId, AccountId: accountId, ExpiresAt: time.Now().Add(ttl), backend: manager}, nil
}

func (manager *redisUpstreamAccountLeaseManager) Count(ctx context.Context, accountId int) (int, error) {
	if manager == nil || manager.client == nil || accountId <= 0 {
		return 0, errors.New("invalid upstream account lease count request")
	}
	count, err := upstreamAccountLeaseCountScript.Run(ctx, manager.client, []string{upstreamAccountLeaseKey(accountId)}).Int()
	return count, err
}

func (manager *redisUpstreamAccountLeaseManager) CountBatch(ctx context.Context, accountIds []int) (map[int]int, error) {
	if manager == nil || manager.client == nil {
		return nil, errors.New("invalid upstream account lease batch count request")
	}
	commands := make(map[int]*redis.Cmd, len(accountIds))
	_, err := manager.client.Pipelined(ctx, func(pipe redis.Pipeliner) error {
		for _, accountId := range accountIds {
			if accountId <= 0 {
				return errors.New("invalid upstream account id in lease batch count request")
			}
			if _, exists := commands[accountId]; exists {
				continue
			}
			commands[accountId] = upstreamAccountLeaseCountScript.Run(ctx, pipe, []string{upstreamAccountLeaseKey(accountId)})
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to count upstream account leases: %w", err)
	}
	counts := make(map[int]int, len(commands))
	for accountId, command := range commands {
		count, commandErr := command.Int()
		if commandErr != nil {
			return nil, fmt.Errorf("failed to count upstream account %d leases: %w", accountId, commandErr)
		}
		counts[accountId] = count
	}
	return counts, nil
}

func (manager *redisUpstreamAccountLeaseManager) refresh(ctx context.Context, lease *UpstreamAccountLease, ttl time.Duration) error {
	result, err := upstreamAccountLeaseRefreshScript.Run(ctx, manager.client, []string{upstreamAccountLeaseKey(lease.AccountId)}, lease.Id, ttl.Milliseconds()).Int()
	if err != nil {
		return fmt.Errorf("failed to refresh upstream account lease: %w", err)
	}
	if result != 1 {
		return errors.New("upstream account lease no longer exists")
	}
	return nil
}

func (manager *redisUpstreamAccountLeaseManager) release(ctx context.Context, lease *UpstreamAccountLease) error {
	if _, err := upstreamAccountLeaseReleaseScript.Run(ctx, manager.client, []string{upstreamAccountLeaseKey(lease.AccountId)}, lease.Id).Result(); err != nil {
		return fmt.Errorf("failed to release upstream account lease: %w", err)
	}
	return nil
}

type localUpstreamAccountLeaseManager struct {
	mu     sync.Mutex
	leases map[int]map[string]time.Time
}

func NewLocalUpstreamAccountLeaseManager() UpstreamAccountLeaseManager {
	return &localUpstreamAccountLeaseManager{leases: make(map[int]map[string]time.Time)}
}

func (manager *localUpstreamAccountLeaseManager) Acquire(_ context.Context, accountId int, limit int, ttl time.Duration) (*UpstreamAccountLease, error) {
	if manager == nil || accountId <= 0 || limit <= 0 || ttl <= 0 {
		return nil, errors.New("invalid upstream account lease request")
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	manager.cleanupLocked(accountId, time.Now())
	accountLeases := manager.leases[accountId]
	if len(accountLeases) >= limit {
		return nil, ErrUpstreamAccountConcurrencyFull
	}
	leaseId := uuid.NewString()
	expiresAt := time.Now().Add(ttl)
	if accountLeases == nil {
		accountLeases = make(map[string]time.Time)
		manager.leases[accountId] = accountLeases
	}
	accountLeases[leaseId] = expiresAt
	return &UpstreamAccountLease{Id: leaseId, AccountId: accountId, ExpiresAt: expiresAt, backend: manager}, nil
}

func (manager *localUpstreamAccountLeaseManager) Count(_ context.Context, accountId int) (int, error) {
	if manager == nil || accountId <= 0 {
		return 0, errors.New("invalid upstream account lease count request")
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	manager.cleanupLocked(accountId, time.Now())
	return len(manager.leases[accountId]), nil
}

func (manager *localUpstreamAccountLeaseManager) CountBatch(_ context.Context, accountIds []int) (map[int]int, error) {
	if manager == nil {
		return nil, errors.New("invalid upstream account lease batch count request")
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	now := time.Now()
	counts := make(map[int]int, len(accountIds))
	for _, accountId := range accountIds {
		if accountId <= 0 {
			return nil, errors.New("invalid upstream account id in lease batch count request")
		}
		manager.cleanupLocked(accountId, now)
		counts[accountId] = len(manager.leases[accountId])
	}
	return counts, nil
}

func (manager *localUpstreamAccountLeaseManager) refresh(_ context.Context, lease *UpstreamAccountLease, ttl time.Duration) error {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	manager.cleanupLocked(lease.AccountId, time.Now())
	accountLeases := manager.leases[lease.AccountId]
	if _, ok := accountLeases[lease.Id]; !ok {
		return errors.New("upstream account lease no longer exists")
	}
	accountLeases[lease.Id] = time.Now().Add(ttl)
	return nil
}

func (manager *localUpstreamAccountLeaseManager) release(_ context.Context, lease *UpstreamAccountLease) error {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if accountLeases := manager.leases[lease.AccountId]; accountLeases != nil {
		delete(accountLeases, lease.Id)
		if len(accountLeases) == 0 {
			delete(manager.leases, lease.AccountId)
		}
	}
	return nil
}

func (manager *localUpstreamAccountLeaseManager) cleanupLocked(accountId int, now time.Time) {
	accountLeases := manager.leases[accountId]
	for leaseId, expiresAt := range accountLeases {
		if !expiresAt.After(now) {
			delete(accountLeases, leaseId)
		}
	}
	if len(accountLeases) == 0 {
		delete(manager.leases, accountId)
	}
}

func upstreamAccountLeaseKey(accountId int) string {
	return fmt.Sprintf("%s%d", upstreamAccountLeaseKeyPrefix, accountId)
}

func toInt64(value any) int64 {
	switch typed := value.(type) {
	case int64:
		return typed
	case int:
		return int64(typed)
	case string:
		var parsed int64
		_, _ = fmt.Sscan(typed, &parsed)
		return parsed
	default:
		return 0
	}
}
