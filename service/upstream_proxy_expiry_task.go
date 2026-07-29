package service

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/bytedance/gopkg/util/gopool"
	"gorm.io/gorm"
)

const upstreamProxyExpiryTickInterval = time.Minute

var (
	upstreamProxyExpiryOnce    sync.Once
	upstreamProxyExpiryRunning atomic.Bool
)

func StartUpstreamProxyExpiryTask() {
	upstreamProxyExpiryOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}
		gopool.Go(func() {
			logger.LogInfo(context.Background(), fmt.Sprintf("upstream proxy expiry task started: tick=%s", upstreamProxyExpiryTickInterval))
			ticker := time.NewTicker(upstreamProxyExpiryTickInterval)
			defer ticker.Stop()

			runUpstreamProxyExpiryOnce()
			for range ticker.C {
				runUpstreamProxyExpiryOnce()
			}
		})
	})
}

func runUpstreamProxyExpiryOnce() {
	if !upstreamProxyExpiryRunning.CompareAndSwap(false, true) {
		return
	}
	defer upstreamProxyExpiryRunning.Store(false)

	changed, err := SweepExpiredUpstreamProxies(common.GetTimestamp())
	if err != nil {
		logger.LogWarn(context.Background(), fmt.Sprintf("upstream proxy expiry task failed: %v", err))
		return
	}
	if changed > 0 {
		logger.LogInfo(context.Background(), fmt.Sprintf("upstream proxy expiry task re-routed %d account(s)", changed))
	}
}

// SweepExpiredUpstreamProxies marks due active proxies as expired and applies
// their configured fallback to directly bound accounts.
func SweepExpiredUpstreamProxies(now int64) (int64, error) {
	var proxies []model.UpstreamProxy
	if err := model.DB.Order("id ASC").Find(&proxies).Error; err != nil {
		return 0, err
	}
	byId := make(map[int]model.UpstreamProxy, len(proxies))
	for _, proxy := range proxies {
		byId[proxy.Id] = proxy
	}

	var totalChanged int64
	for _, proxy := range proxies {
		if proxy.Status != constant.UpstreamStatusActive || proxy.ExpiresAt == nil || *proxy.ExpiresAt > now {
			continue
		}
		targetId, change := resolveExpiredUpstreamProxyFallback(proxy, byId, now)
		var changed int64
		err := model.DB.Transaction(func(tx *gorm.DB) error {
			result := tx.Model(&model.UpstreamProxy{}).
				Where("id = ? AND status = ? AND expires_at IS NOT NULL AND expires_at <= ?", proxy.Id, constant.UpstreamStatusActive, now).
				Updates(map[string]any{"status": constant.UpstreamStatusExpired, "updated_at": now})
			if result.Error != nil || result.RowsAffected == 0 {
				return result.Error
			}
			if !change {
				return nil
			}
			updates := map[string]any{
				"proxy_id":                 targetId,
				"proxy_fallback_origin_id": proxy.Id,
				"updated_at":               now,
			}
			accountResult := tx.Model(&model.UpstreamAccount{}).
				Where("proxy_id = ? AND proxy_fallback_origin_id IS NULL", proxy.Id).
				Updates(updates)
			changed = accountResult.RowsAffected
			return accountResult.Error
		})
		if err != nil {
			return totalChanged, err
		}
		totalChanged += changed
	}
	return totalChanged, nil
}

// resolveExpiredUpstreamProxyFallback mirrors Sub2API's expiry behavior:
// none keeps account bindings, direct clears proxy_id, and proxy walks the
// configured chain until it finds a non-expired target.
func resolveExpiredUpstreamProxyFallback(start model.UpstreamProxy, byId map[int]model.UpstreamProxy, now int64) (*int, bool) {
	switch start.FallbackMode {
	case constant.UpstreamProxyFallbackDirect:
		return nil, true
	case constant.UpstreamProxyFallbackProxy:
		visited := map[int]struct{}{start.Id: {}}
		currentId := start.BackupProxyId
		for currentId != nil {
			if _, exists := visited[*currentId]; exists {
				return nil, false
			}
			proxy, exists := byId[*currentId]
			if !exists {
				return nil, false
			}
			isExpired := proxy.ExpiresAt != nil && *proxy.ExpiresAt <= now
			if !isExpired && proxy.Status != constant.UpstreamStatusExpired {
				targetId := proxy.Id
				return &targetId, true
			}
			visited[*currentId] = struct{}{}
			switch proxy.FallbackMode {
			case constant.UpstreamProxyFallbackDirect:
				return nil, true
			case constant.UpstreamProxyFallbackProxy:
				currentId = proxy.BackupProxyId
			default:
				return nil, false
			}
		}
	}
	return nil, false
}
