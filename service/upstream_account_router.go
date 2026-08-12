package service

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
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
	PoolId                  int
	ChannelType             int
	AllowedAccountTypes     []string
	Model                   string
	RequestPath             string
	CompactRequest          bool
	CodexClient             bool
	CodexAppServer          bool
	ChannelProxy            string
	ExcludedIds             map[int]struct{}
	RequestId               string
	ResponsesWebSocket      bool
	PreferredAccountId      int
	RequirePreferred        bool
	RequiredWebSocketMode   string
	AccountAffinitySeed     string
	AccountAffinityKeyFP    string
	AccountAffinityTTL      int
	AccountAffinityFallback *UpstreamAccountAffinityFallbackCandidate
	PreferredWaitTimeout    time.Duration
	PreferredMaxWaiters     int
	PreferredWaitConfigured bool
}

type UpstreamAccountAffinitySelectionInfo struct {
	Enabled            bool
	Source             string
	KeyFingerprint     string
	CacheHit           bool
	PreferredAccountID int
	SelectedAccountID  int
	Outcome            string
}

type UpstreamAccountAffinityFallbackInfo struct {
	Mode           string `json:"mode"`
	Eligible       bool   `json:"eligible"`
	Applied        bool   `json:"applied"`
	Sampled        bool   `json:"sampled"`
	SamplePercent  int    `json:"sample_percent"`
	BodyBytes      int64  `json:"body_bytes"`
	PrefixBytes    int    `json:"prefix_bytes"`
	KeyFingerprint string `json:"key_fp,omitempty"`
	Outcome        string `json:"outcome"`
}

type UpstreamAccountSelection struct {
	Pool             model.UpstreamAccountPool
	Account          model.UpstreamAccount
	Credentials      map[string]any
	MappedModel      string
	ModelMapped      bool
	Proxy            *model.UpstreamProxy
	ProxyURL         string
	Lease            *UpstreamAccountLease
	Affinity         *UpstreamAccountAffinitySelectionInfo
	AffinityFallback *UpstreamAccountAffinityFallbackInfo

	leaseTTL      time.Duration
	refreshCancel context.CancelFunc
	releaseOnce   sync.Once
	releaseErr    error
}

type UpstreamAccountRouter struct {
	leaseManager           UpstreamAccountLeaseManager
	leaseTTL               time.Duration
	concurrencyWaitTimeout time.Duration
	concurrencyWaitPoll    time.Duration
	maxWaiters             int
	waiterMu               sync.Mutex
	waiters                int
	affinityStore          upstreamAccountAffinityStore
}

type upstreamAccountCandidate struct {
	account            model.UpstreamAccount
	membershipPriority int
	membershipWeight   int
	currentLoad        int
	loadRate           float64
}

type upstreamAccountSelectionExhaustedError struct {
	poolID          int
	exclusions      map[string]int
	concurrencyFull bool
}

func (err *upstreamAccountSelectionExhaustedError) Error() string {
	if err == nil {
		return "upstream account selection exhausted"
	}
	return fmt.Sprintf("no available upstream account in pool %d after acquisition (%s)", err.poolID, formatUpstreamAccountExclusions(err.exclusions))
}

func upstreamAccountSelectionWasConcurrencyFull(err error) bool {
	var exhausted *upstreamAccountSelectionExhaustedError
	return errors.As(err, &exhausted) && exhausted.concurrencyFull
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
	waitTimeout := time.Duration(common.GetEnvOrDefault("UPSTREAM_ACCOUNT_CONCURRENCY_WAIT_MS", 1500)) * time.Millisecond
	if waitTimeout < 0 {
		waitTimeout = 0
	}
	waitPoll := time.Duration(common.GetEnvOrDefault("UPSTREAM_ACCOUNT_CONCURRENCY_WAIT_POLL_MS", 50)) * time.Millisecond
	if waitPoll < 10*time.Millisecond {
		waitPoll = 10 * time.Millisecond
	}
	maxWaiters := common.GetEnvOrDefault("UPSTREAM_ACCOUNT_MAX_WAITERS", 64)
	if maxWaiters < 0 {
		maxWaiters = 0
	}
	return &UpstreamAccountRouter{
		leaseManager: leaseManager, leaseTTL: leaseTTL,
		concurrencyWaitTimeout: waitTimeout, concurrencyWaitPoll: waitPoll, maxWaiters: maxWaiters,
		affinityStore: newDefaultUpstreamAccountAffinityStore(),
	}, nil
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
	policy, policyEnabled := router.resolveAccountAffinityPolicy(request)
	affinity, enabled, fallbackInfo := router.resolveAccountAffinity(ctx, request, policy, policyEnabled)
	if policyEnabled && request.PreferredAccountId > 0 && !request.RequirePreferred && !request.ResponsesWebSocket {
		preferredRequest := request
		preferredRequest.AccountAffinitySeed = ""
		preferredRequest.RequirePreferred = true
		preferredRequest.PreferredWaitTimeout = policy.waitTimeout
		preferredRequest.PreferredMaxWaiters = policy.maxWaiters
		preferredRequest.PreferredWaitConfigured = true
		selection, err := router.selectWithoutAccountAffinity(ctx, preferredRequest)
		if err == nil {
			return attachUpstreamAccountAffinityFallback(router.finishResponseAccountAffinity(ctx, request, selection, affinity, "response_hit"), fallbackInfo), nil
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		if affinity != nil && upstreamAccountSelectionWasConcurrencyFull(err) {
			affinity.preserveBinding = true
		}

		fallbackExclusions := cloneUpstreamAccountExclusions(request.ExcludedIds)
		fallbackExclusions[request.PreferredAccountId] = struct{}{}
		if affinity != nil && affinity.CacheHit && affinity.PreferredAccountID > 0 && affinity.PreferredAccountID != request.PreferredAccountId {
			stickyRequest := request
			stickyRequest.AccountAffinitySeed = ""
			stickyRequest.PreferredAccountId = affinity.PreferredAccountID
			stickyRequest.RequirePreferred = true
			stickyRequest.PreferredWaitTimeout = affinity.waitTimeout
			stickyRequest.PreferredMaxWaiters = affinity.maxWaiters
			stickyRequest.PreferredWaitConfigured = true
			stickyRequest.ExcludedIds = fallbackExclusions
			selection, stickyErr := router.selectWithoutAccountAffinity(ctx, stickyRequest)
			if stickyErr == nil {
				return attachUpstreamAccountAffinityFallback(router.finishResponseAccountAffinity(ctx, request, selection, affinity, "response_fallback_session_hit"), fallbackInfo), nil
			}
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			if upstreamAccountSelectionWasConcurrencyFull(stickyErr) {
				affinity.preserveBinding = true
			}
			fallbackExclusions[affinity.PreferredAccountID] = struct{}{}
		}

		fallbackRequest := request
		fallbackRequest.AccountAffinitySeed = ""
		fallbackRequest.ExcludedIds = fallbackExclusions
		selection, err = router.selectWithoutAccountAffinity(ctx, fallbackRequest)
		if err != nil {
			return nil, err
		}
		return attachUpstreamAccountAffinityFallback(router.finishResponseAccountAffinity(ctx, request, selection, affinity, "response_fallback"), fallbackInfo), nil
	}
	if !enabled {
		selection, err := router.selectWithoutAccountAffinity(ctx, request)
		return attachUpstreamAccountAffinityFallback(selection, fallbackInfo), err
	}

	if affinity.CacheHit && request.PreferredAccountId <= 0 {
		stickyRequest := request
		stickyRequest.AccountAffinitySeed = ""
		stickyRequest.PreferredAccountId = affinity.PreferredAccountID
		stickyRequest.RequirePreferred = true
		stickyRequest.PreferredWaitTimeout = affinity.waitTimeout
		stickyRequest.PreferredMaxWaiters = affinity.maxWaiters
		stickyRequest.PreferredWaitConfigured = true
		selection, err := router.selectWithoutAccountAffinity(ctx, stickyRequest)
		if err == nil {
			affinity.Outcome = "sticky_hit"
			return attachUpstreamAccountAffinityFallback(router.finishAccountAffinity(ctx, request, selection, affinity), fallbackInfo), nil
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		if upstreamAccountSelectionWasConcurrencyFull(err) {
			affinity.preserveBinding = true
		}

		fallbackRequest := request
		fallbackRequest.AccountAffinitySeed = ""
		fallbackRequest.ExcludedIds = cloneUpstreamAccountExclusions(request.ExcludedIds)
		fallbackRequest.ExcludedIds[affinity.PreferredAccountID] = struct{}{}
		selection, err = router.selectWithoutAccountAffinity(ctx, fallbackRequest)
		if err != nil {
			return nil, err
		}
		affinity.Outcome = "sticky_fallback"
		return attachUpstreamAccountAffinityFallback(router.finishAccountAffinity(ctx, request, selection, affinity), fallbackInfo), nil
	}

	plainRequest := request
	plainRequest.AccountAffinitySeed = ""
	selection, err := router.selectWithoutAccountAffinity(ctx, plainRequest)
	if err != nil {
		return nil, err
	}
	if affinity.CacheHit {
		affinity.Outcome = "preferred_override"
	} else {
		affinity.Outcome = "bound_new"
	}
	return attachUpstreamAccountAffinityFallback(router.finishAccountAffinity(ctx, request, selection, affinity), fallbackInfo), nil
}

func attachUpstreamAccountAffinityFallback(selection *UpstreamAccountSelection, info *UpstreamAccountAffinityFallbackInfo) *UpstreamAccountSelection {
	if selection != nil {
		selection.AffinityFallback = info
	}
	return selection
}

type resolvedUpstreamAccountAffinity struct {
	UpstreamAccountAffinitySelectionInfo
	key             string
	ttl             time.Duration
	waitTimeout     time.Duration
	maxWaiters      int
	preserveBinding bool
}

type upstreamAccountAffinityPolicy struct {
	ttl                   time.Duration
	waitTimeout           time.Duration
	maxWaiters            int
	fallbackMode          string
	fallbackMinBodyBytes  int
	fallbackSamplePercent int
	fallbackModelAllowed  bool
}

func (router *UpstreamAccountRouter) resolveAccountAffinityPolicy(request UpstreamAccountSelectionRequest) (upstreamAccountAffinityPolicy, bool) {
	if router == nil || router.affinityStore == nil || request.ResponsesWebSocket || request.PoolId <= 0 || strings.TrimSpace(request.RequestPath) != "/v1/responses" {
		return upstreamAccountAffinityPolicy{}, false
	}
	var pool model.UpstreamAccountPool
	if err := model.DB.First(&pool, request.PoolId).Error; err != nil || pool.Platform != constant.UpstreamPlatformOpenAI {
		return upstreamAccountAffinityPolicy{}, false
	}
	config, err := model.ParseUpstreamAccountSchedulerConfig(pool.SchedulerConfig)
	if err != nil || !config.SessionAffinityEnabled {
		return upstreamAccountAffinityPolicy{}, false
	}
	ttlSeconds := config.SessionAffinityTTLSeconds
	if request.AccountAffinityTTL > 0 && request.AccountAffinityTTL < ttlSeconds {
		ttlSeconds = request.AccountAffinityTTL
	}
	if ttlSeconds <= 0 {
		ttlSeconds = 3600
	}
	return upstreamAccountAffinityPolicy{
		ttl:                   time.Duration(ttlSeconds) * time.Second,
		waitTimeout:           time.Duration(config.SessionAffinityWaitMs) * time.Millisecond,
		maxWaiters:            config.SessionAffinityMaxWaiters,
		fallbackMode:          config.SessionAffinityFallbackMode,
		fallbackMinBodyBytes:  config.SessionAffinityFallbackMinBodyBytes,
		fallbackSamplePercent: config.SessionAffinityFallbackSamplePercent,
		fallbackModelAllowed:  config.MatchesSessionAffinityFallbackModel(request.Model),
	}, true
}

func (router *UpstreamAccountRouter) resolveAccountAffinity(ctx context.Context, request UpstreamAccountSelectionRequest, policy upstreamAccountAffinityPolicy, policyEnabled bool) (*resolvedUpstreamAccountAffinity, bool, *UpstreamAccountAffinityFallbackInfo) {
	seed := strings.TrimSpace(request.AccountAffinitySeed)
	fingerprint := request.AccountAffinityKeyFP
	source := "session"
	var fallbackInfo *UpstreamAccountAffinityFallbackInfo
	if seed == "" && request.AccountAffinityFallback != nil {
		candidate := request.AccountAffinityFallback
		fallbackMode := policy.fallbackMode
		if fallbackMode == "" {
			fallbackMode = "off"
		}
		fallbackInfo = &UpstreamAccountAffinityFallbackInfo{
			Mode: fallbackMode, SamplePercent: policy.fallbackSamplePercent,
			BodyBytes: candidate.BodyBytes, PrefixBytes: candidate.PrefixBytes,
			KeyFingerprint: candidate.KeyFingerprint,
		}
		switch {
		case !policyEnabled || fallbackMode == "off":
			fallbackInfo.Outcome = "disabled"
		case request.PreferredAccountId > 0:
			fallbackInfo.Outcome = "continuation"
		case !policy.fallbackModelAllowed:
			fallbackInfo.Outcome = "model_not_allowed"
		case candidate.BodyBytes < int64(policy.fallbackMinBodyBytes):
			fallbackInfo.Outcome = "body_too_small"
		default:
			fallbackInfo.Eligible = true
			if fallbackMode == "observe" {
				fallbackInfo.Outcome = "observed"
			} else if !upstreamAccountAffinityFallbackSampled(candidate.KeySeed, policy.fallbackSamplePercent) {
				fallbackInfo.Outcome = "not_sampled"
			} else {
				fallbackInfo.Sampled = true
				fallbackInfo.Applied = true
				fallbackInfo.Outcome = "applied"
				seed = candidate.KeySeed
				fingerprint = candidate.KeyFingerprint
				source = "prefix"
			}
		}
	}
	if !policyEnabled || seed == "" {
		return nil, false, fallbackInfo
	}
	key := buildUpstreamAccountAffinityKey(request.PoolId, request.Model, seed)
	resolved := &resolvedUpstreamAccountAffinity{
		UpstreamAccountAffinitySelectionInfo: UpstreamAccountAffinitySelectionInfo{
			Enabled: true, Source: source, KeyFingerprint: fingerprint,
		},
		key: key, ttl: policy.ttl, waitTimeout: policy.waitTimeout, maxWaiters: policy.maxWaiters,
	}
	accountID, found, getErr := router.affinityStore.Get(ctx, key)
	if getErr != nil {
		common.SysError(fmt.Sprintf("upstream account affinity cache get failed: pool=%d err=%v", request.PoolId, getErr))
		resolved.Outcome = "cache_error"
		return resolved, true, fallbackInfo
	}
	resolved.CacheHit = found && accountID > 0
	resolved.PreferredAccountID = accountID
	return resolved, true, fallbackInfo
}

func upstreamAccountAffinityFallbackSampled(seed string, percent int) bool {
	if percent >= 100 {
		return true
	}
	if percent <= 0 || strings.TrimSpace(seed) == "" {
		return false
	}
	digest := common.Sha256Raw([]byte(seed))
	return int(binary.BigEndian.Uint64(digest[:8])%100) < percent
}

func (router *UpstreamAccountRouter) finishResponseAccountAffinity(ctx context.Context, request UpstreamAccountSelectionRequest, selection *UpstreamAccountSelection, affinity *resolvedUpstreamAccountAffinity, outcome string) *UpstreamAccountSelection {
	selection = router.finishAccountAffinity(ctx, request, selection, affinity)
	if selection == nil {
		return nil
	}
	if selection.Affinity == nil {
		selection.Affinity = &UpstreamAccountAffinitySelectionInfo{
			Enabled: true, CacheHit: true, PreferredAccountID: request.PreferredAccountId,
		}
	}
	selection.Affinity.Source = "response"
	selection.Affinity.PreferredAccountID = request.PreferredAccountId
	selection.Affinity.SelectedAccountID = selection.Account.Id
	selection.Affinity.Outcome = outcome
	return selection
}

func (router *UpstreamAccountRouter) finishAccountAffinity(ctx context.Context, request UpstreamAccountSelectionRequest, selection *UpstreamAccountSelection, affinity *resolvedUpstreamAccountAffinity) *UpstreamAccountSelection {
	if selection == nil || affinity == nil {
		return selection
	}
	affinity.SelectedAccountID = selection.Account.Id
	if selection.Account.Platform != constant.UpstreamPlatformOpenAI || selection.Account.Type != constant.UpstreamAccountTypeOAuth {
		affinity.Outcome = "ineligible_account_type"
		selection.Affinity = &affinity.UpstreamAccountAffinitySelectionInfo
		return selection
	}
	if affinity.preserveBinding && affinity.CacheHit && affinity.PreferredAccountID > 0 {
		selection.Affinity = &affinity.UpstreamAccountAffinitySelectionInfo
		return selection
	}
	if affinity.CacheHit && affinity.PreferredAccountID > 0 {
		replaced, err := router.affinityStore.Replace(ctx, affinity.key, affinity.PreferredAccountID, selection.Account.Id, affinity.ttl)
		if err != nil {
			common.SysError(fmt.Sprintf("upstream account affinity cache replace failed: pool=%d err=%v", request.PoolId, err))
			affinity.Outcome = "cache_error"
		} else if !replaced {
			affinity.Outcome = "binding_changed"
		}
	} else {
		winner, installed, err := router.affinityStore.BindIfAbsent(ctx, affinity.key, selection.Account.Id, affinity.ttl)
		if err != nil {
			common.SysError(fmt.Sprintf("upstream account affinity cache bind failed: pool=%d err=%v", request.PoolId, err))
			affinity.Outcome = "cache_error"
		} else if !installed && winner != selection.Account.Id {
			affinity.Outcome = "binding_race"
			affinity.PreferredAccountID = winner
		}
	}
	selection.Affinity = &affinity.UpstreamAccountAffinitySelectionInfo
	return selection
}

func cloneUpstreamAccountExclusions(source map[int]struct{}) map[int]struct{} {
	result := make(map[int]struct{}, len(source)+1)
	for accountID := range source {
		result[accountID] = struct{}{}
	}
	return result
}

func (router *UpstreamAccountRouter) selectWithoutAccountAffinity(ctx context.Context, request UpstreamAccountSelectionRequest) (*UpstreamAccountSelection, error) {
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
	allowedTypes, err := upstreamAccountTypesForSelection(request)
	if err != nil {
		return nil, err
	}
	candidates, exclusions, err := router.loadCandidates(ctx, pool, allowedTypes, request, request.ExcludedIds)
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no eligible upstream account in pool %d (%s)", pool.Id, formatUpstreamAccountExclusions(exclusions))
	}
	schedulerConfig, err := model.ParseUpstreamAccountSchedulerConfig(pool.SchedulerConfig)
	if err != nil {
		return nil, err
	}

	waitTimeout := router.concurrencyWaitTimeout
	if request.RequirePreferred && request.PreferredWaitConfigured {
		waitTimeout = request.PreferredWaitTimeout
	}
	waitDeadline := time.Now().Add(waitTimeout)
	waiting := false
	preferredWaiting := false
	lastConcurrencyFull := false
	defer func() {
		if waiting {
			router.endConcurrencyWait()
		}
		if preferredWaiting {
			router.endPreferredConcurrencyWait(request.PoolId, request.PreferredAccountId)
		}
	}()
	for {
		concurrencyFull := false
		orderedCandidates := weightedUpstreamCandidateOrder(candidates, schedulerConfig)
		if !request.RequirePreferred {
			orderedCandidates = preferUpstreamAccountCandidate(orderedCandidates, request.PreferredAccountId)
		}
		for _, candidate := range orderedCandidates {
			lease, acquireErr := router.leaseManager.Acquire(ctx, candidate.account.Id, candidate.account.Concurrency, router.leaseTTL)
			if errors.Is(acquireErr, ErrUpstreamAccountConcurrencyFull) {
				concurrencyFull = true
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
			options, optionsErr := model.ParseUpstreamAccountOptionsWithCredentials(candidate.account.Extra, credentials)
			if optionsErr != nil {
				_ = lease.Release(context.Background())
				exclusions["option_invalid"]++
				continue
			}
			if candidate.account.Platform == constant.UpstreamPlatformOpenAI && !options.SupportsOpenAIEndpoint(request.RequestPath) {
				_ = lease.Release(context.Background())
				exclusions["endpoint_unsupported"]++
				continue
			}
			mappedModel, modelMapped, modelSupported := resolveUpstreamAccountModel(&candidate.account, credentials, options, request.Model, request.CompactRequest, schedulerConfig.DefaultMappedModel)
			if !modelSupported {
				_ = lease.Release(context.Background())
				exclusions["model_unsupported"]++
				continue
			}
			if IsUpstreamAccountModelTransientBlocked(candidate.account.Id, mappedModel) {
				_ = lease.Release(context.Background())
				exclusions["model_capacity_transient"]++
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
				MappedModel: mappedModel, ModelMapped: modelMapped,
				Proxy: proxy, ProxyURL: proxyURL, Lease: lease, leaseTTL: router.leaseTTL,
			}, nil
		}
		lastConcurrencyFull = concurrencyFull
		if !concurrencyFull || waitTimeout <= 0 || time.Now().After(waitDeadline) {
			break
		}
		if !waiting {
			if request.RequirePreferred && request.PreferredWaitConfigured {
				if request.PreferredMaxWaiters <= 0 {
					exclusions["preferred_wait_disabled"]++
					break
				}
				if !router.beginPreferredConcurrencyWait(ctx, request.PoolId, request.PreferredAccountId, request.PreferredMaxWaiters, waitTimeout) {
					exclusions["preferred_wait_queue_full"]++
					break
				}
				preferredWaiting = true
			}
			if !router.beginConcurrencyWait() {
				if preferredWaiting {
					router.endPreferredConcurrencyWait(request.PoolId, request.PreferredAccountId)
					preferredWaiting = false
				}
				exclusions["wait_queue_full"]++
				break
			}
			waiting = true
		}
		if !router.waitForConcurrency(ctx, waitDeadline) {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			break
		}
	}
	return nil, &upstreamAccountSelectionExhaustedError{
		poolID: pool.Id, exclusions: exclusions, concurrencyFull: lastConcurrencyFull,
	}
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

func (router *UpstreamAccountRouter) loadCandidates(ctx context.Context, pool model.UpstreamAccountPool, allowedTypes map[string]struct{}, request UpstreamAccountSelectionRequest, excludedIds map[int]struct{}) ([]upstreamAccountCandidate, map[string]int, error) {
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
		if _, allowed := allowedTypes[account.Type]; account.Platform != pool.Platform || !allowed {
			exclusions["credential_type_mismatch"]++
			continue
		}
		if !account.IsSchedulableAt(now) {
			exclusions[upstreamAccountIneligibleReason(account, now)]++
			continue
		}
		options, optionsErr := model.ParseUpstreamAccountOptions(account.Extra)
		if optionsErr != nil {
			exclusions["option_invalid"]++
			continue
		}
		if request.ResponsesWebSocket && account.Platform == constant.UpstreamPlatformOpenAI &&
			options.OpenAIWSMode(account.Type) == model.UpstreamOpenAIWSModeOff {
			exclusions["websocket_disabled"]++
			continue
		}
		if requiredMode := strings.TrimSpace(request.RequiredWebSocketMode); requiredMode != "" &&
			account.Platform == constant.UpstreamPlatformOpenAI && options.OpenAIWSMode(account.Type) != requiredMode {
			exclusions["websocket_mode_mismatch"]++
			continue
		}
		if request.CompactRequest && account.Platform == constant.UpstreamPlatformOpenAI && !options.AllowsOpenAICompact() {
			exclusions["compact_unsupported"]++
			continue
		}
		if account.Platform == constant.UpstreamPlatformOpenAI && account.Type == constant.UpstreamAccountTypeOAuth && options.CodexCLIOnly {
			if !request.CodexClient || (request.CodexAppServer && !options.CodexCLIOnlyAllowAppServer) {
				exclusions["codex_client_restricted"]++
				continue
			}
		}
		supportsModel, capabilityErr := upstreamAccountSupportsModel(account.Extra, request.Model)
		if capabilityErr != nil {
			exclusions["capability_invalid"]++
			continue
		}
		if !supportsModel {
			exclusions["model_unsupported"]++
			continue
		}
		load := leaseCounts[account.Id]
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
	schedulerConfig, err := model.ParseUpstreamAccountSchedulerConfig(pool.SchedulerConfig)
	if err != nil {
		return nil, nil, err
	}
	if routed := routedUpstreamCandidates(pool, schedulerConfig, request.Model, candidates); len(routed) > 0 {
		exclusions["outside_model_route"] += len(candidates) - len(routed)
		candidates = routed
	}
	// A continued native WebSocket must stay on the same account, but the
	// preferred account must still be eligible for the configured model route.
	if request.PreferredAccountId > 0 && request.RequirePreferred {
		for _, candidate := range candidates {
			if candidate.account.Id == request.PreferredAccountId {
				return []upstreamAccountCandidate{candidate}, exclusions, nil
			}
		}
		exclusions["preferred_account_unavailable"]++
		return nil, exclusions, nil
	}
	return candidates, exclusions, nil
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

func resolveUpstreamAccountModel(account *model.UpstreamAccount, credentials map[string]any, options model.UpstreamAccountOptions, requestedModel string, compactRequest bool, defaultMappedModel string) (string, bool, bool) {
	requestedModel = strings.TrimSpace(requestedModel)
	if compactRequest {
		if mappedModel, matched := resolveUpstreamAccountModelMapping(options.CompactModelMapping, requestedModel); matched {
			return mappedModel, mappedModel != requestedModel, mappedModel != ""
		}
	}
	if requestedModel == "" {
		return requestedModel, false, true
	}
	// OpenAI passthrough keeps the client model unchanged and must not use a
	// stale account whitelist to reject otherwise valid models. Compact-only
	// mappings remain effective because /responses/compact explicitly supports
	// that override even in passthrough mode.
	if account != nil && account.Platform == constant.UpstreamPlatformOpenAI && options.OpenAIPassthrough {
		return requestedModel, false, true
	}
	mapping := upstreamAccountModelMapping(credentials)
	if len(mapping) == 0 {
		if account != nil && account.Platform == constant.UpstreamPlatformOpenAI && account.Type == constant.UpstreamAccountTypeOAuth &&
			!isOpenAIOAuthServableModel(requestedModel) {
			return requestedModel, false, false
		}
		if isClaudeMessagesDispatchModel(requestedModel) && strings.TrimSpace(defaultMappedModel) != "" {
			return strings.TrimSpace(defaultMappedModel), true, true
		}
		return requestedModel, false, true
	}
	if mappedModel, matched := resolveUpstreamAccountModelMapping(mapping, requestedModel); matched {
		return mappedModel, mappedModel != requestedModel, mappedModel != ""
	}
	if isClaudeMessagesDispatchModel(requestedModel) && strings.TrimSpace(defaultMappedModel) != "" {
		return strings.TrimSpace(defaultMappedModel), true, true
	}
	return requestedModel, false, false
}

var openAIOAuthForeignModelPrefixes = []string{
	"deepseek-", "glm-", "kimi-", "moonshot-", "qwen-", "qwen2-", "qwen3-", "qwen4-", "qwq-",
	"minimax-", "gemini-", "gemma-", "grok-", "doubao-", "hunyuan-", "llama-", "llama2-", "llama3-",
	"meta-llama", "mistral-", "mixtral-", "baichuan-", "ernie-", "step-", "seed-", "yi-",
}

func isOpenAIOAuthServableModel(requestedModel string) bool {
	modelName := strings.TrimSpace(requestedModel)
	if separator := strings.LastIndex(modelName, "/"); separator >= 0 {
		modelName = modelName[separator+1:]
	}
	modelName = strings.ToLower(strings.TrimSpace(modelName))
	if modelName == "" {
		return true
	}
	for _, prefix := range openAIOAuthForeignModelPrefixes {
		if strings.HasPrefix(modelName, prefix) {
			return false
		}
	}
	return true
}

func resolveUpstreamAccountModelMapping(mapping map[string]string, requestedModel string) (string, bool) {
	if len(mapping) == 0 {
		return requestedModel, false
	}
	if mappedModel, ok := mapping[requestedModel]; ok {
		return strings.TrimSpace(mappedModel), true
	}
	bestPattern := ""
	for pattern := range mapping {
		pattern = strings.TrimSpace(pattern)
		if !strings.HasSuffix(pattern, "*") || !strings.HasPrefix(requestedModel, strings.TrimSuffix(pattern, "*")) {
			continue
		}
		if len(pattern) > len(bestPattern) || (len(pattern) == len(bestPattern) && pattern < bestPattern) {
			bestPattern = pattern
		}
	}
	if bestPattern == "" {
		return requestedModel, false
	}
	return strings.TrimSpace(mapping[bestPattern]), true
}

func isClaudeMessagesDispatchModel(modelName string) bool {
	modelName = strings.ToLower(strings.TrimSpace(modelName))
	return strings.HasPrefix(modelName, "claude") &&
		(strings.Contains(modelName, "opus") || strings.Contains(modelName, "sonnet") || strings.Contains(modelName, "haiku"))
}

func upstreamAccountModelMapping(credentials map[string]any) map[string]string {
	if len(credentials) == 0 {
		return nil
	}
	raw, ok := credentials["model_mapping"]
	if !ok || raw == nil {
		return nil
	}
	switch mapping := raw.(type) {
	case map[string]string:
		return mapping
	case map[string]any:
		result := make(map[string]string, len(mapping))
		for pattern, target := range mapping {
			if targetString, ok := target.(string); ok {
				result[pattern] = targetString
			}
		}
		return result
	default:
		return nil
	}
}

func routedUpstreamCandidates(pool model.UpstreamAccountPool, config model.UpstreamAccountSchedulerConfig, modelName string, candidates []upstreamAccountCandidate) []upstreamAccountCandidate {
	routingIds := config.RoutingAccountIds(modelName)
	if len(routingIds) == 0 {
		return nil
	}
	idSet := make(map[int64]struct{}, len(routingIds))
	for _, accountId := range routingIds {
		idSet[accountId] = struct{}{}
	}
	idSpace := config.ModelRoutingIdSpace
	if idSpace == "" && pool.SourceSystem != nil && strings.EqualFold(strings.TrimSpace(*pool.SourceSystem), "sub2api") {
		idSpace = "source"
	}
	routed := make([]upstreamAccountCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		var accountId int64
		if idSpace == "source" {
			if candidate.account.SourceId == nil {
				continue
			}
			accountId = *candidate.account.SourceId
		} else {
			accountId = int64(candidate.account.Id)
		}
		if _, ok := idSet[accountId]; ok {
			routed = append(routed, candidate)
		}
	}
	return routed
}

func (router *UpstreamAccountRouter) beginConcurrencyWait() bool {
	if router == nil || router.maxWaiters <= 0 {
		return false
	}
	router.waiterMu.Lock()
	defer router.waiterMu.Unlock()
	if router.waiters >= router.maxWaiters {
		return false
	}
	router.waiters++
	return true
}

func (router *UpstreamAccountRouter) endConcurrencyWait() {
	if router == nil {
		return
	}
	router.waiterMu.Lock()
	if router.waiters > 0 {
		router.waiters--
	}
	router.waiterMu.Unlock()
}

func (router *UpstreamAccountRouter) beginPreferredConcurrencyWait(ctx context.Context, poolID int, accountID int, maxWaiters int, waitTimeout time.Duration) bool {
	if router == nil || router.affinityStore == nil || poolID <= 0 || accountID <= 0 || maxWaiters <= 0 {
		return false
	}
	ttl := waitTimeout + 5*time.Second
	if ttl < 30*time.Second {
		ttl = 30 * time.Second
	}
	acquired, err := router.affinityStore.AcquireWaiter(ctx, buildUpstreamAccountAffinityWaiterKey(poolID, accountID), maxWaiters, ttl)
	if err != nil {
		common.SysError(fmt.Sprintf("upstream account affinity waiter acquire failed: pool=%d account=%d err=%v", poolID, accountID, err))
		return false
	}
	return acquired
}

func (router *UpstreamAccountRouter) endPreferredConcurrencyWait(poolID int, accountID int) {
	if router == nil || router.affinityStore == nil || poolID <= 0 || accountID <= 0 {
		return
	}
	releaseCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := router.affinityStore.ReleaseWaiter(releaseCtx, buildUpstreamAccountAffinityWaiterKey(poolID, accountID)); err != nil {
		common.SysError(fmt.Sprintf("upstream account affinity waiter release failed: pool=%d account=%d err=%v", poolID, accountID, err))
	}
}

func (router *UpstreamAccountRouter) waitForConcurrency(ctx context.Context, deadline time.Time) bool {
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return false
	}
	delay := router.concurrencyWaitPoll
	if delay > remaining {
		delay = remaining
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return time.Now().Before(deadline)
	}
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

func weightedUpstreamCandidateOrder(candidates []upstreamAccountCandidate, config model.UpstreamAccountSchedulerConfig) []upstreamAccountCandidate {
	if config.Version == 2 || config.Strategy == "load_score" {
		return scoredUpstreamCandidateOrder(candidates, config)
	}
	return tieredUpstreamCandidateOrder(candidates, config.TopK)
}

func tieredUpstreamCandidateOrder(candidates []upstreamAccountCandidate, topK int) []upstreamAccountCandidate {
	pool := append([]upstreamAccountCandidate(nil), candidates...)
	sort.SliceStable(pool, func(i, j int) bool {
		if pool[i].membershipPriority == pool[j].membershipPriority {
			if pool[i].loadRate == pool[j].loadRate {
				return pool[i].account.Id < pool[j].account.Id
			}
			return pool[i].loadRate < pool[j].loadRate
		}
		return pool[i].membershipPriority < pool[j].membershipPriority
	})

	order := make([]upstreamAccountCandidate, 0, len(pool))
	for start := 0; start < len(pool); {
		end := start + 1
		for end < len(pool) && pool[end].membershipPriority == pool[start].membershipPriority {
			end++
		}
		tier := pool[start:end]
		if topK > 0 && len(tier) > topK {
			order = append(order, weightedUpstreamCandidateTierOrder(tier[:topK])...)
			order = append(order, weightedUpstreamCandidateTierOrder(tier[topK:])...)
		} else {
			order = append(order, weightedUpstreamCandidateTierOrder(tier)...)
		}
		start = end
	}
	return order
}

type scoredUpstreamCandidate struct {
	candidate upstreamAccountCandidate
	priority  int
	score     float64
}

func scoredUpstreamCandidateOrder(candidates []upstreamAccountCandidate, config model.UpstreamAccountSchedulerConfig) []upstreamAccountCandidate {
	if len(candidates) == 0 {
		return nil
	}
	priorityFor := func(candidate upstreamAccountCandidate) int {
		if config.PrioritySource == "member" {
			return candidate.membershipPriority
		}
		return candidate.account.Priority
	}
	minPriority, maxPriority := priorityFor(candidates[0]), priorityFor(candidates[0])
	for _, candidate := range candidates[1:] {
		priority := priorityFor(candidate)
		if priority < minPriority {
			minPriority = priority
		}
		if priority > maxPriority {
			maxPriority = priority
		}
	}

	ranked := make([]scoredUpstreamCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		priority := priorityFor(candidate)
		priorityFactor := 1.0
		if maxPriority > minPriority {
			priorityFactor = 1 - float64(priority-minPriority)/float64(maxPriority-minPriority)
		}
		loadFactor := 1 - candidate.loadRate
		if loadFactor < 0 {
			loadFactor = 0
		}
		if loadFactor > 1 {
			loadFactor = 1
		}
		ranked = append(ranked, scoredUpstreamCandidate{
			candidate: candidate,
			priority:  priority,
			score:     priorityFactor + loadFactor,
		})
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		if ranked[i].priority != ranked[j].priority {
			return ranked[i].priority < ranked[j].priority
		}
		if ranked[i].candidate.loadRate != ranked[j].candidate.loadRate {
			return ranked[i].candidate.loadRate < ranked[j].candidate.loadRate
		}
		return ranked[i].candidate.account.Id < ranked[j].candidate.account.Id
	})

	primaryCount := len(ranked)
	if config.TopK > 0 && primaryCount > config.TopK {
		primaryCount = config.TopK
	}
	order := weightedScoredUpstreamCandidateOrder(ranked[:primaryCount])
	for _, item := range ranked[primaryCount:] {
		order = append(order, item.candidate)
	}
	return order
}

func weightedScoredUpstreamCandidateOrder(candidates []scoredUpstreamCandidate) []upstreamAccountCandidate {
	pool := append([]scoredUpstreamCandidate(nil), candidates...)
	weights := make([]float64, len(pool))
	if len(pool) > 0 {
		minScore := pool[0].score
		for _, candidate := range pool[1:] {
			if candidate.score < minScore {
				minScore = candidate.score
			}
		}
		for index, candidate := range pool {
			weight := (candidate.score - minScore) + 1
			weight *= float64(candidate.candidate.membershipWeight)
			if weight <= 0 || math.IsNaN(weight) || math.IsInf(weight, 0) {
				weight = 1
			}
			weights[index] = weight
		}
	}
	order := make([]upstreamAccountCandidate, 0, len(pool))
	for len(pool) > 0 {
		total := 0.0
		for _, weight := range weights {
			total += weight
		}
		selected := 0
		if total > 0 {
			pick := rand.Float64() * total
			for index, weight := range weights {
				pick -= weight
				if pick <= 0 {
					selected = index
					break
				}
			}
		}
		order = append(order, pool[selected].candidate)
		pool = append(pool[:selected], pool[selected+1:]...)
		weights = append(weights[:selected], weights[selected+1:]...)
	}
	return order
}

func weightedUpstreamCandidateTierOrder(candidates []upstreamAccountCandidate) []upstreamAccountCandidate {
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

func preferUpstreamAccountCandidate(candidates []upstreamAccountCandidate, accountID int) []upstreamAccountCandidate {
	if accountID <= 0 || len(candidates) < 2 {
		return candidates
	}
	for index, candidate := range candidates {
		if candidate.account.Id != accountID || index == 0 {
			continue
		}
		preferred := candidate
		copy(candidates[1:index+1], candidates[0:index])
		candidates[0] = preferred
		break
	}
	return candidates
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

func upstreamAccountTypesForChannel(channelType int) (map[string]struct{}, error) {
	switch channelType {
	case constant.ChannelTypeCodex:
		return map[string]struct{}{constant.UpstreamAccountTypeOAuth: {}}, nil
	case constant.ChannelTypeOpenAI:
		return map[string]struct{}{constant.UpstreamAccountTypeAPIKey: {}}, nil
	case constant.ChannelTypeAnthropic:
		return map[string]struct{}{
			constant.UpstreamAccountTypeOAuth: {}, constant.UpstreamAccountTypeSetupToken: {},
			constant.UpstreamAccountTypeAPIKey: {}, constant.UpstreamAccountTypeBedrock: {},
			constant.UpstreamAccountTypeServiceAccount: {},
		}, nil
	default:
		return nil, errors.New("channel type does not support a local upstream account pool")
	}
}

func upstreamAccountTypesForSelection(request UpstreamAccountSelectionRequest) (map[string]struct{}, error) {
	if len(request.AllowedAccountTypes) == 0 {
		return upstreamAccountTypesForChannel(request.ChannelType)
	}

	allowedTypes := make(map[string]struct{}, len(request.AllowedAccountTypes))
	for _, accountType := range request.AllowedAccountTypes {
		accountType = strings.ToLower(strings.TrimSpace(accountType))
		switch accountType {
		case constant.UpstreamAccountTypeOAuth, constant.UpstreamAccountTypeSetupToken,
			constant.UpstreamAccountTypeAPIKey, constant.UpstreamAccountTypeBedrock,
			constant.UpstreamAccountTypeServiceAccount:
			allowedTypes[accountType] = struct{}{}
		default:
			return nil, fmt.Errorf("unsupported upstream account type %q", accountType)
		}
	}
	if len(allowedTypes) == 0 {
		return nil, errors.New("at least one upstream account type is required")
	}
	return allowedTypes, nil
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
