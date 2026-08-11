package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/bytedance/gopkg/util/gopool"
	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
)

const (
	upstreamOAuthRefreshDefaultIntervalMinutes  = 10
	upstreamOAuthRefreshDefaultThresholdMinutes = 24 * 60
	upstreamOAuthRefreshDefaultTimeoutSeconds   = 30
	upstreamOAuthRefreshBatchSize               = 200
	upstreamOAuthRefreshFailureBackoff          = 5 * time.Minute
	upstreamOAuthRefreshLockPrefix              = "new-api:upstream-account:oauth-refresh:"
)

var (
	upstreamOAuthRefreshOnce                 sync.Once
	upstreamOAuthRefreshRunning              atomic.Bool
	upstreamOAuthRefreshLocal                sync.Map
	errUpstreamOAuthRefreshLocked            = errors.New("upstream OAuth account refresh is already running")
	errUpstreamOAuthRefreshUnsupported       = errors.New("account does not support OAuth refresh")
	errUpstreamOAuthRefreshExternallyManaged = errors.New("OAuth refresh is owned by an external system")
	upstreamOAuthRefreshUnlock               = redis.NewScript(`
if redis.call('GET', KEYS[1]) == ARGV[1] then
  return redis.call('DEL', KEYS[1])
end
return 0
`)
)

func StartUpstreamAccountOAuthRefreshTask() {
	upstreamOAuthRefreshOnce.Do(func() {
		if !common.GetEnvOrDefaultBool("UPSTREAM_ACCOUNT_REFRESH_WORKER_ENABLED", true) {
			return
		}
		if (!common.RedisEnabled || common.RDB == nil) && !common.GetEnvOrDefaultBool("UPSTREAM_ACCOUNT_ALLOW_LOCAL_LEASES", false) {
			logger.LogWarn(context.Background(), "upstream account OAuth refresh task disabled: Redis is unavailable")
			return
		}

		intervalMinutes := common.GetEnvOrDefault("UPSTREAM_ACCOUNT_REFRESH_INTERVAL_MINUTES", upstreamOAuthRefreshDefaultIntervalMinutes)
		if intervalMinutes < 1 {
			intervalMinutes = 1
		}
		interval := time.Duration(intervalMinutes) * time.Minute
		gopool.Go(func() {
			logger.LogInfo(context.Background(), fmt.Sprintf("upstream account OAuth refresh task started: tick=%s", interval))
			ticker := time.NewTicker(interval)
			defer ticker.Stop()

			runUpstreamAccountOAuthRefreshOnce()
			for range ticker.C {
				runUpstreamAccountOAuthRefreshOnce()
			}
		})
	})
}

func runUpstreamAccountOAuthRefreshOnce() {
	if !upstreamOAuthRefreshRunning.CompareAndSwap(false, true) {
		return
	}
	defer upstreamOAuthRefreshRunning.Store(false)

	now := common.GetTimestamp()
	cleanupUpstreamOAuthSessions(now)
	thresholdMinutes := common.GetEnvOrDefault("UPSTREAM_ACCOUNT_REFRESH_THRESHOLD_MINUTES", upstreamOAuthRefreshDefaultThresholdMinutes)
	if thresholdMinutes < 1 {
		thresholdMinutes = 1
	}
	refreshBefore := now + int64((time.Duration(thresholdMinutes) * time.Minute).Seconds())
	timeoutSeconds := common.GetEnvOrDefault("UPSTREAM_ACCOUNT_REFRESH_TIMEOUT_SECONDS", upstreamOAuthRefreshDefaultTimeoutSeconds)
	if timeoutSeconds < 1 {
		timeoutSeconds = 1
	}
	refreshTimeout := time.Duration(timeoutSeconds) * time.Second
	if restored, err := restoreMissingManagedOAuthExpirations(); err != nil {
		logger.LogWarn(context.Background(), fmt.Sprintf("upstream account OAuth expiry restoration failed: %v", err))
	} else if restored > 0 {
		logger.LogInfo(context.Background(), fmt.Sprintf("upstream account OAuth expiry restored: accounts=%d", restored))
	}

	lastId := 0
	for {
		var accounts []model.UpstreamAccount
		err := model.DB.
			Where("id > ? AND oauth_refresh_owner = ? AND expires_at IS NOT NULL AND expires_at <= ?", lastId, constant.UpstreamOAuthRefreshOwnerStarNexus, refreshBefore).
			Where("(platform = ? AND type = ?) OR (platform = ? AND type IN ?)",
				constant.UpstreamPlatformOpenAI, constant.UpstreamAccountTypeOAuth,
				constant.UpstreamPlatformAnthropic, []string{constant.UpstreamAccountTypeOAuth, constant.UpstreamAccountTypeSetupToken}).
			Where("temp_unschedulable_reason <> ?", constant.UpstreamAccountReasonOAuthRefreshPermanent).
			Where("temp_unschedulable_reason <> ? OR temp_unschedulable_until IS NULL OR temp_unschedulable_until <= ?", constant.UpstreamAccountReasonOAuthRefreshFailed, now).
			Order("id ASC").
			Limit(upstreamOAuthRefreshBatchSize).
			Find(&accounts).Error
		if err != nil {
			logger.LogWarn(context.Background(), "upstream account OAuth refresh: account query failed")
			return
		}
		if len(accounts) == 0 {
			return
		}
		for i := range accounts {
			account := &accounts[i]
			lastId = account.Id
			if !shouldRefreshUpstreamOAuthAccount(account, refreshBefore) {
				continue
			}
			ctx, cancel := context.WithTimeout(context.Background(), refreshTimeout)
			err := withUpstreamOAuthRefreshLock(ctx, account.Id, refreshTimeout+30*time.Second, func(lockCtx context.Context) error {
				_, refreshErr := refreshUpstreamOAuthAccountUnlocked(lockCtx, account.Id)
				return refreshErr
			})
			cancel()
			if errors.Is(err, errUpstreamOAuthRefreshLocked) {
				continue
			}
			if err != nil {
				recordUpstreamOAuthRefreshFailure(account.Id, err)
				logger.LogWarn(context.Background(), fmt.Sprintf("upstream account OAuth refresh failed: account_id=%d result=refresh_failed", account.Id))
				continue
			}
			logger.LogInfo(context.Background(), fmt.Sprintf("upstream account OAuth refreshed: account_id=%d", account.Id))
		}
		if len(accounts) < upstreamOAuthRefreshBatchSize {
			return
		}
	}
}

func restoreMissingManagedOAuthExpirations() (int64, error) {
	var restored int64
	lastId := 0
	for {
		var accounts []model.UpstreamAccount
		if err := model.DB.
			Where("id > ? AND oauth_refresh_owner = ? AND expires_at IS NULL", lastId, constant.UpstreamOAuthRefreshOwnerStarNexus).
			Where("(platform = ? AND type = ?) OR (platform = ? AND type IN ?)",
				constant.UpstreamPlatformOpenAI, constant.UpstreamAccountTypeOAuth,
				constant.UpstreamPlatformAnthropic, []string{constant.UpstreamAccountTypeOAuth, constant.UpstreamAccountTypeSetupToken}).
			Order("id ASC").Limit(upstreamOAuthRefreshBatchSize).Find(&accounts).Error; err != nil {
			return restored, err
		}
		if len(accounts) == 0 {
			return restored, nil
		}
		for i := range accounts {
			account := &accounts[i]
			lastId = account.Id
			credentials, err := DecryptUpstreamAccountCredentials(account)
			if err != nil {
				continue
			}
			expiresAt, ok := upstreamOAuthCredentialExpiry(credentials)
			if !ok {
				continue
			}
			result := model.DB.Model(&model.UpstreamAccount{}).
				Where("id = ? AND expires_at IS NULL", account.Id).
				Updates(map[string]any{"expires_at": expiresAt, "updated_at": common.GetTimestamp()})
			if result.Error != nil {
				return restored, result.Error
			}
			restored += result.RowsAffected
		}
		if len(accounts) < upstreamOAuthRefreshBatchSize {
			return restored, nil
		}
	}
}

func shouldRefreshUpstreamOAuthAccount(account *model.UpstreamAccount, refreshBefore int64) bool {
	return account != nil &&
		isRefreshableUpstreamOAuthAccount(account.Platform, account.Type) &&
		account.OAuthRefreshOwner == constant.UpstreamOAuthRefreshOwnerStarNexus &&
		account.ExpiresAt != nil &&
		*account.ExpiresAt <= refreshBefore &&
		account.TempUnschedulableReason != constant.UpstreamAccountReasonOAuthRefreshPermanent &&
		!(account.TempUnschedulableReason == constant.UpstreamAccountReasonOAuthRefreshFailed && account.TempUnschedulableUntil != nil && *account.TempUnschedulableUntil > common.GetTimestamp()) &&
		account.Status != constant.UpstreamStatusInactive &&
		account.CredentialCiphertext != ""
}

func withUpstreamOAuthRefreshLock(ctx context.Context, accountId int, ttl time.Duration, fn func(context.Context) error) error {
	if accountId <= 0 || ttl <= 0 || fn == nil {
		return errors.New("invalid upstream OAuth refresh lock request")
	}
	if common.RedisEnabled && common.RDB != nil {
		key := fmt.Sprintf("%s%d", upstreamOAuthRefreshLockPrefix, accountId)
		owner := uuid.NewString()
		acquired, err := common.RDB.SetNX(ctx, key, owner, ttl).Result()
		if err != nil {
			return fmt.Errorf("acquire upstream OAuth refresh lock: %w", err)
		}
		if !acquired {
			return errUpstreamOAuthRefreshLocked
		}
		defer func() {
			releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, _ = upstreamOAuthRefreshUnlock.Run(releaseCtx, common.RDB, []string{key}, owner).Result()
		}()
		return fn(ctx)
	}
	if !common.GetEnvOrDefaultBool("UPSTREAM_ACCOUNT_ALLOW_LOCAL_LEASES", false) {
		return errors.New("Redis is required for upstream OAuth refresh locking")
	}
	key := fmt.Sprintf("%d", accountId)
	if _, loaded := upstreamOAuthRefreshLocal.LoadOrStore(key, struct{}{}); loaded {
		return errUpstreamOAuthRefreshLocked
	}
	defer upstreamOAuthRefreshLocal.Delete(key)
	return fn(ctx)
}

func recordUpstreamOAuthRefreshFailure(accountId int, refreshErr error) {
	if accountId <= 0 || refreshErr == nil {
		return
	}
	now := common.GetTimestamp()
	updates := map[string]any{
		"error_message":             "OAuth credential refresh failed; retry is pending",
		"temp_unschedulable_until":  now + int64(upstreamOAuthRefreshFailureBackoff.Seconds()),
		"temp_unschedulable_reason": constant.UpstreamAccountReasonOAuthRefreshFailed,
		"updated_at":                now,
	}
	result := "retry_pending"
	message := "OAuth credential refresh failed; retry is pending"
	if isNonRetryableUpstreamOAuthRefreshError(refreshErr) {
		updates["status"] = constant.UpstreamStatusError
		updates["schedulable"] = false
		updates["error_message"] = "OAuth credentials require reauthorization"
		updates["temp_unschedulable_until"] = nil
		updates["temp_unschedulable_reason"] = constant.UpstreamAccountReasonOAuthRefreshPermanent
		result = "reauthorization_required"
		message = "OAuth credentials require reauthorization"
	}
	dbResult := model.DB.Model(&model.UpstreamAccount{}).Where("id = ?", accountId).Updates(updates)
	if dbResult.Error != nil || dbResult.RowsAffected != 1 {
		return
	}
	recordUpstreamAccountEvent(accountId, 0, "oauth_refresh", result, message, nil)
}

func cleanupUpstreamOAuthSessions(now int64) {
	retentionBefore := now - int64((24 * time.Hour).Seconds())
	_ = model.DB.Where("expires_at <= ? OR (used_at > 0 AND used_at <= ?)", now, retentionBefore).Delete(&model.UpstreamOAuthSession{}).Error
}
