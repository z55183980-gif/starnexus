package service

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
)

type UpstreamAccountSelectionRequest struct {
	PoolId       int
	ChannelType  int
	Model        string
	ChannelProxy string
	ExcludedIds  map[int]struct{}
	RequestId    string
}

type UpstreamAccountSelection struct {
	Pool        model.UpstreamAccountPool
	Account     model.UpstreamAccount
	Credentials map[string]any
	Proxy       *model.UpstreamProxy
	ProxyURL    string
	Lease       *UpstreamAccountLease

	leaseTTL      time.Duration
	refreshCancel context.CancelFunc
	releaseOnce   sync.Once
	releaseErr    error
}

type UpstreamAccountRouter struct {
	leaseManager UpstreamAccountLeaseManager
	leaseTTL     time.Duration
}

type upstreamAccountCandidate struct {
	account            model.UpstreamAccount
	membershipPriority int
	membershipWeight   int
	currentLoad        int
	loadRate           float64
}

func NewUpstreamAccountRouter(leaseManager UpstreamAccountLeaseManager, leaseTTL time.Duration) (*UpstreamAccountRouter, error) {
	if leaseManager == nil {
		return nil, errors.New("upstream account lease manager is required")
	}
	if leaseTTL <= 0 {
		leaseTTL = time.Duration(common.GetEnvOrDefault("UPSTREAM_ACCOUNT_LEASE_TTL_SECONDS", 120)) * time.Second
	}
	if leaseTTL < 30*time.Second {
		leaseTTL = 30 * time.Second
	}
	return &UpstreamAccountRouter{leaseManager: leaseManager, leaseTTL: leaseTTL}, nil
}

func NewConfiguredUpstreamAccountRouter() (*UpstreamAccountRouter, error) {
	manager, err := NewUpstreamAccountLeaseManager()
	if err != nil {
		return nil, err
	}
	return NewUpstreamAccountRouter(manager, 0)
}

var (
	configuredUpstreamAccountRouterOnce sync.Once
	configuredUpstreamAccountRouter     *UpstreamAccountRouter
	configuredUpstreamAccountRouterErr  error
)

var upstreamAccountLeaseRefreshMinInterval = 10 * time.Second

func GetConfiguredUpstreamAccountRouter() (*UpstreamAccountRouter, error) {
	configuredUpstreamAccountRouterOnce.Do(func() {
		configuredUpstreamAccountRouter, configuredUpstreamAccountRouterErr = NewConfiguredUpstreamAccountRouter()
	})
	return configuredUpstreamAccountRouter, configuredUpstreamAccountRouterErr
}

func (router *UpstreamAccountRouter) Select(ctx context.Context, request UpstreamAccountSelectionRequest) (*UpstreamAccountSelection, error) {
	if router == nil || router.leaseManager == nil || request.PoolId <= 0 {
		return nil, errors.New("invalid upstream account selection request")
	}
	var pool model.UpstreamAccountPool
	if err := model.DB.First(&pool, request.PoolId).Error; err != nil {
		return nil, fmt.Errorf("failed to load upstream account pool: %w", err)
	}
	if pool.Status != constant.UpstreamStatusActive {
		return nil, errors.New("upstream account pool is inactive")
	}
	requiredType, err := upstreamAccountTypeForChannel(request.ChannelType)
	if err != nil {
		return nil, err
	}
	candidates, exclusions, err := router.loadCandidates(ctx, pool, requiredType, request.Model, request.ExcludedIds)
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no eligible upstream account in pool %d (%s)", pool.Id, formatUpstreamAccountExclusions(exclusions))
	}

	for _, candidate := range weightedUpstreamCandidateOrder(candidates) {
		lease, acquireErr := router.leaseManager.Acquire(ctx, candidate.account.Id, candidate.account.Concurrency, router.leaseTTL)
		if errors.Is(acquireErr, ErrUpstreamAccountConcurrencyFull) {
			exclusions["concurrency_full"]++
			continue
		}
		if acquireErr != nil {
			return nil, acquireErr
		}
		credentials, decryptErr := DecryptUpstreamAccountCredentials(&candidate.account)
		if decryptErr != nil {
			_ = lease.Release(context.Background())
			exclusions["credential_invalid"]++
			continue
		}
		proxy, proxyURL, proxyErr := resolveUpstreamProxy(ctx, &candidate.account, &pool, request.ChannelProxy)
		if proxyErr != nil {
			_ = lease.Release(context.Background())
			exclusions["proxy_unavailable"]++
			continue
		}
		return &UpstreamAccountSelection{
			Pool: pool, Account: candidate.account, Credentials: credentials,
			Proxy: proxy, ProxyURL: proxyURL, Lease: lease, leaseTTL: router.leaseTTL,
		}, nil
	}
	return nil, fmt.Errorf("no available upstream account in pool %d after acquisition (%s)", pool.Id, formatUpstreamAccountExclusions(exclusions))
}

func (selection *UpstreamAccountSelection) StartLeaseRefresh(parent context.Context, onLeaseLost func(error)) {
	if selection == nil || selection.Lease == nil || selection.refreshCancel != nil {
		return
	}
	refreshContext, cancel := context.WithCancel(parent)
	selection.refreshCancel = cancel
	interval := selection.leaseTTL / 3
	if interval < upstreamAccountLeaseRefreshMinInterval {
		interval = upstreamAccountLeaseRefreshMinInterval
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-refreshContext.Done():
				return
			case <-ticker.C:
				if err := selection.Lease.Refresh(refreshContext, selection.leaseTTL); err != nil {
					if onLeaseLost != nil && !errors.Is(err, context.Canceled) {
						onLeaseLost(fmt.Errorf("upstream account lease lost: %w", err))
					}
					return
				}
			}
		}
	}()
}

func (selection *UpstreamAccountSelection) Release(ctx context.Context) error {
	if selection == nil {
		return nil
	}
	selection.releaseOnce.Do(func() {
		if selection.refreshCancel != nil {
			selection.refreshCancel()
		}
		if selection.Lease != nil {
			selection.releaseErr = selection.Lease.Release(ctx)
		}
	})
	return selection.releaseErr
}

func (router *UpstreamAccountRouter) loadCandidates(ctx context.Context, pool model.UpstreamAccountPool, requiredType string, modelName string, excludedIds map[int]struct{}) ([]upstreamAccountCandidate, map[string]int, error) {
	var members []model.UpstreamAccountPoolMember
	if err := model.DB.Where("pool_id = ?", pool.Id).Find(&members).Error; err != nil {
		return nil, nil, err
	}
	exclusions := make(map[string]int)
	if len(members) == 0 {
		exclusions["pool_empty"] = 1
		return nil, exclusions, nil
	}
	accountIds := make([]int, 0, len(members))
	membersByAccount := make(map[int]model.UpstreamAccountPoolMember, len(members))
	for _, member := range members {
		accountIds = append(accountIds, member.AccountId)
		membersByAccount[member.AccountId] = member
	}
	var accounts []model.UpstreamAccount
	if err := model.DB.Where("id IN ?", accountIds).Find(&accounts).Error; err != nil {
		return nil, nil, err
	}
	leaseCounts, err := router.leaseManager.CountBatch(ctx, accountIds)
	if err != nil {
		return nil, nil, err
	}
	now := time.Now().Unix()
	candidates := make([]upstreamAccountCandidate, 0, len(accounts))
	for _, account := range accounts {
		if _, excluded := excludedIds[account.Id]; excluded {
			exclusions["excluded"]++
			continue
		}
		if account.Platform != pool.Platform || account.Type != requiredType {
			exclusions["credential_type_mismatch"]++
			continue
		}
		if !account.IsSchedulableAt(now) {
			exclusions[upstreamAccountIneligibleReason(account, now)]++
			continue
		}
		supportsModel, capabilityErr := upstreamAccountSupportsModel(account.Extra, modelName)
		if capabilityErr != nil {
			exclusions["capability_invalid"]++
			continue
		}
		if !supportsModel {
			exclusions["model_unsupported"]++
			continue
		}
		load := leaseCounts[account.Id]
		if load >= account.Concurrency {
			exclusions["concurrency_full"]++
			continue
		}
		member := membersByAccount[account.Id]
		priority := member.Priority
		weight := account.Weight
		if member.Weight > 0 {
			weight = member.Weight
		}
		if weight <= 0 {
			weight = 1
		}
		candidates = append(candidates, upstreamAccountCandidate{
			account: account, membershipPriority: priority, membershipWeight: weight,
			currentLoad: load, loadRate: float64(load) / float64(account.EffectiveLoadFactor()),
		})
	}
	if len(candidates) == 0 {
		return nil, exclusions, nil
	}
	bestPriority := candidates[0].membershipPriority
	for _, candidate := range candidates[1:] {
		if candidate.membershipPriority < bestPriority {
			bestPriority = candidate.membershipPriority
		}
	}
	priorityTier := candidates[:0]
	for _, candidate := range candidates {
		if candidate.membershipPriority == bestPriority {
			priorityTier = append(priorityTier, candidate)
		} else {
			exclusions["lower_priority"]++
		}
	}
	schedulerConfig, err := model.ParseUpstreamAccountSchedulerConfig(pool.SchedulerConfig)
	if err != nil {
		return nil, nil, err
	}
	if schedulerConfig.TopK > 0 && len(priorityTier) > schedulerConfig.TopK {
		sort.SliceStable(priorityTier, func(i, j int) bool {
			if priorityTier[i].loadRate == priorityTier[j].loadRate {
				return priorityTier[i].account.Id < priorityTier[j].account.Id
			}
			return priorityTier[i].loadRate < priorityTier[j].loadRate
		})
		exclusions["outside_top_k"] += len(priorityTier) - schedulerConfig.TopK
		priorityTier = priorityTier[:schedulerConfig.TopK]
	}
	return priorityTier, exclusions, nil
}

type upstreamAccountCapabilities struct {
	Models          []string `json:"models"`
	SupportedModels []string `json:"supported_models"`
	ModelPrefixes   []string `json:"model_prefixes"`
	ExcludedModels  []string `json:"excluded_models"`
}

func upstreamAccountSupportsModel(extra string, modelName string) (bool, error) {
	modelName = strings.ToLower(strings.TrimSpace(modelName))
	if modelName == "" || strings.TrimSpace(extra) == "" || strings.TrimSpace(extra) == "{}" {
		return true, nil
	}
	var capabilities upstreamAccountCapabilities
	if err := common.UnmarshalJsonStr(extra, &capabilities); err != nil {
		return false, err
	}
	for _, rule := range capabilities.ExcludedModels {
		if upstreamModelRuleMatches(rule, modelName) {
			return false, nil
		}
	}
	allowedRules := make([]string, 0, len(capabilities.Models)+len(capabilities.SupportedModels)+len(capabilities.ModelPrefixes))
	allowedRules = append(allowedRules, capabilities.Models...)
	allowedRules = append(allowedRules, capabilities.SupportedModels...)
	for _, prefix := range capabilities.ModelPrefixes {
		prefix = strings.TrimSpace(prefix)
		if prefix != "" {
			allowedRules = append(allowedRules, prefix+"*")
		}
	}
	if len(allowedRules) == 0 {
		return true, nil
	}
	for _, rule := range allowedRules {
		if upstreamModelRuleMatches(rule, modelName) {
			return true, nil
		}
	}
	return false, nil
}

func upstreamModelRuleMatches(rule string, modelName string) bool {
	rule = strings.ToLower(strings.TrimSpace(rule))
	if rule == "" {
		return false
	}
	if rule == modelName || rule == "*" {
		return true
	}
	matched, err := path.Match(rule, modelName)
	return err == nil && matched
}

func weightedUpstreamCandidateOrder(candidates []upstreamAccountCandidate) []upstreamAccountCandidate {
	pool := append([]upstreamAccountCandidate(nil), candidates...)
	order := make([]upstreamAccountCandidate, 0, len(pool))
	for len(pool) > 0 {
		total := 0.0
		weights := make([]float64, len(pool))
		for i, candidate := range pool {
			freeFactor := 1 - candidate.loadRate
			if freeFactor < 0.05 {
				freeFactor = 0.05
			}
			weights[i] = float64(candidate.membershipWeight) * freeFactor
			total += weights[i]
		}
		selected := 0
		if total > 0 {
			pick := rand.Float64() * total
			for i, weight := range weights {
				pick -= weight
				if pick <= 0 {
					selected = i
					break
				}
			}
		}
		order = append(order, pool[selected])
		pool = append(pool[:selected], pool[selected+1:]...)
	}
	return order
}

func resolveUpstreamProxy(_ context.Context, account *model.UpstreamAccount, pool *model.UpstreamAccountPool, channelProxy string) (*model.UpstreamProxy, string, error) {
	var proxyId *int
	if account != nil && account.ProxyFallbackOriginId != nil {
		proxyId = account.ProxyFallbackOriginId
	} else if account != nil && account.ProxyId != nil {
		proxyId = account.ProxyId
	} else if pool != nil && pool.DefaultProxyId != nil {
		proxyId = pool.DefaultProxyId
	}
	if proxyId == nil {
		return nil, strings.TrimSpace(channelProxy), nil
	}
	visited := make(map[int]struct{})
	currentId := proxyId
	for currentId != nil {
		if _, exists := visited[*currentId]; exists {
			return nil, "", errors.New("upstream proxy fallback chain contains a cycle")
		}
		visited[*currentId] = struct{}{}
		var proxy model.UpstreamProxy
		if err := model.DB.First(&proxy, *currentId).Error; err != nil {
			return nil, "", fmt.Errorf("failed to load upstream proxy: %w", err)
		}
		if proxy.IsUsableAt(time.Now().Unix()) {
			auth, err := DecryptUpstreamProxyAuth(&proxy)
			if err != nil {
				return nil, "", err
			}
			proxyURL, err := proxy.URL(auth.Username, auth.Password)
			return &proxy, proxyURL, err
		}
		switch proxy.FallbackMode {
		case constant.UpstreamProxyFallbackProxy:
			currentId = proxy.BackupProxyId
		case constant.UpstreamProxyFallbackDirect:
			return nil, "", nil
		default:
			return nil, "", errors.New("upstream proxy is unavailable and has no usable fallback")
		}
	}
	return nil, "", errors.New("upstream proxy fallback chain is exhausted")
}

func upstreamAccountTypeForChannel(channelType int) (string, error) {
	switch channelType {
	case constant.ChannelTypeCodex:
		return constant.UpstreamAccountTypeOAuth, nil
	case constant.ChannelTypeOpenAI:
		return constant.UpstreamAccountTypeAPIKey, nil
	default:
		return "", errors.New("channel type does not support a local upstream account pool")
	}
}

func upstreamAccountIneligibleReason(account model.UpstreamAccount, now int64) string {
	if account.Status != constant.UpstreamStatusActive {
		return "inactive"
	}
	if !account.Schedulable {
		return "not_schedulable"
	}
	if account.AutoPauseOnExpired && account.ExpiresAt != nil && *account.ExpiresAt <= now {
		return "expired"
	}
	if account.RateLimitResetAt != nil && *account.RateLimitResetAt > now {
		return "rate_limited"
	}
	if account.OverloadUntil != nil && *account.OverloadUntil > now {
		return "overloaded"
	}
	if account.TempUnschedulableUntil != nil && *account.TempUnschedulableUntil > now {
		return "temporarily_unschedulable"
	}
	return "not_schedulable"
}

func formatUpstreamAccountExclusions(exclusions map[string]int) string {
	if len(exclusions) == 0 {
		return "no candidates"
	}
	keys := make([]string, 0, len(exclusions))
	for key := range exclusions {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", key, exclusions[key]))
	}
	return strings.Join(parts, ", ")
}
