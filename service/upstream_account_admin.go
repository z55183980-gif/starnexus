package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

const (
	upstreamAccountCredentialRecordKind = "account"
	upstreamProxyAuthRecordKind         = "proxy"
)

type UpstreamAccountListFilter struct {
	Platform    string
	Type        string
	Status      string
	Search      string
	PoolId      int
	ProxyId     int
	Schedulable *bool
	Page        int
	PageSize    int
}

type UpstreamAccountView struct {
	model.UpstreamAccount
	CredentialConfigured bool                    `json:"credential_configured"`
	PoolIds              []int                   `json:"pool_ids"`
	CurrentConcurrency   int                     `json:"current_concurrency"`
	Metadata             UpstreamAccountMetadata `json:"metadata"`
}

type UpstreamAccountMetadata struct {
	Email            string `json:"email,omitempty"`
	PlanType         string `json:"plan_type,omitempty"`
	PrivacyMode      string `json:"privacy_mode,omitempty"`
	CompactMode      string `json:"compact_mode,omitempty"`
	CompactSupported bool   `json:"compact_supported"`
}

type UpstreamAccountPoolView struct {
	model.UpstreamAccountPool
	AccountCount    int64  `json:"account_count"`
	ActiveCount     int64  `json:"active_count"`
	ChannelCount    int64  `json:"channel_count"`
	AttemptCount24h int64  `json:"attempt_count_24h"`
	SuccessCount24h int64  `json:"success_count_24h"`
	ErrorCount24h   int64  `json:"error_count_24h"`
	LastEventAt     *int64 `json:"last_event_at"`
}

type UpstreamAccountPoolMemberInput struct {
	AccountId int `json:"account_id"`
	Priority  int `json:"priority"`
	Weight    int `json:"weight"`
}

type UpstreamAccountPoolMemberView struct {
	model.UpstreamAccountPoolMember
	Name        string `json:"name"`
	Platform    string `json:"platform"`
	Type        string `json:"type"`
	Status      string `json:"status"`
	Schedulable bool   `json:"schedulable"`
}

type UpstreamProxyView struct {
	model.UpstreamProxy
	AuthConfigured bool   `json:"auth_configured"`
	AuthUsername   string `json:"username"`
	AuthPassword   string `json:"password,omitempty"`
	AccountCount   int64  `json:"account_count"`
	PoolCount      int64  `json:"pool_count"`
	BackupCount    int64  `json:"backup_count"`
}

type UpstreamAccountCreateInput struct {
	Account     model.UpstreamAccount
	Credentials map[string]any
	PoolIds     []int
}

type UpstreamAccountUpdateInput struct {
	Account     model.UpstreamAccount
	Credentials *map[string]any
	PoolIds     *[]int
}

type UpstreamAccountRecoveryScope string

const (
	UpstreamAccountRecoveryAll       UpstreamAccountRecoveryScope = "all"
	UpstreamAccountRecoveryRateLimit UpstreamAccountRecoveryScope = "rate_limit"
	UpstreamAccountRecoveryTemporary UpstreamAccountRecoveryScope = "temporary"
)

type UpstreamProxyAuthInput struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type UpstreamProxyCreateInput struct {
	Proxy model.UpstreamProxy
	Auth  UpstreamProxyAuthInput
}

type UpstreamProxyUpdateInput struct {
	Proxy model.UpstreamProxy
	Auth  *UpstreamProxyAuthInput
}

func ListUpstreamAccountPools() ([]UpstreamAccountPoolView, error) {
	var pools []model.UpstreamAccountPool
	if err := model.DB.Order("id ASC").Find(&pools).Error; err != nil {
		return nil, err
	}
	views := make([]UpstreamAccountPoolView, 0, len(pools))
	for _, pool := range pools {
		var accountCount int64
		if err := model.DB.Model(&model.UpstreamAccountPoolMember{}).Where("pool_id = ?", pool.Id).Count(&accountCount).Error; err != nil {
			return nil, err
		}
		var channelCount int64
		if err := model.DB.Model(&model.Channel{}).Where("upstream_account_pool_id = ?", pool.Id).Count(&channelCount).Error; err != nil {
			return nil, err
		}
		view := UpstreamAccountPoolView{UpstreamAccountPool: pool, AccountCount: accountCount, ChannelCount: channelCount}
		if err := populateUpstreamAccountPoolRuntimeStats(&view); err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	return views, nil
}

func GetUpstreamAccountPool(id int) (*UpstreamAccountPoolView, error) {
	if id <= 0 {
		return nil, errors.New("invalid upstream account pool id")
	}
	var pool model.UpstreamAccountPool
	if err := model.DB.First(&pool, id).Error; err != nil {
		return nil, err
	}
	var accountCount int64
	if err := model.DB.Model(&model.UpstreamAccountPoolMember{}).Where("pool_id = ?", id).Count(&accountCount).Error; err != nil {
		return nil, err
	}
	var channelCount int64
	if err := model.DB.Model(&model.Channel{}).Where("upstream_account_pool_id = ?", id).Count(&channelCount).Error; err != nil {
		return nil, err
	}
	view := &UpstreamAccountPoolView{UpstreamAccountPool: pool, AccountCount: accountCount, ChannelCount: channelCount}
	if err := populateUpstreamAccountPoolRuntimeStats(view); err != nil {
		return nil, err
	}
	return view, nil
}

func populateUpstreamAccountPoolRuntimeStats(view *UpstreamAccountPoolView) error {
	if view == nil || view.Id <= 0 {
		return errors.New("invalid upstream account pool stats request")
	}
	activeQuery := model.DB.Model(&model.UpstreamAccount{}).
		Where("status = ? AND schedulable = ?", constant.UpstreamStatusActive, true).
		Where("id IN (?)", model.DB.Model(&model.UpstreamAccountPoolMember{}).Select("account_id").Where("pool_id = ?", view.Id))
	if err := activeQuery.Count(&view.ActiveCount).Error; err != nil {
		return err
	}
	cutoff := common.GetTimestamp() - int64((24 * time.Hour).Seconds())
	counts := []struct {
		eventType string
		value     *int64
	}{
		{"request_selected", &view.AttemptCount24h},
		{"request_success", &view.SuccessCount24h},
		{"request_error", &view.ErrorCount24h},
	}
	for _, item := range counts {
		if err := model.DB.Model(&model.UpstreamAccountEvent{}).
			Where("pool_id = ? AND event_type = ? AND created_at >= ?", view.Id, item.eventType, cutoff).
			Count(item.value).Error; err != nil {
			return err
		}
	}
	var last model.UpstreamAccountEvent
	err := model.DB.Where("pool_id = ?", view.Id).Order("created_at DESC").Order("id DESC").First(&last).Error
	if err == nil {
		view.LastEventAt = &last.CreatedAt
		return nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	return err
}

func CreateUpstreamAccountPool(pool *model.UpstreamAccountPool) error {
	if err := model.ValidateUpstreamAccountPool(pool); err != nil {
		return err
	}
	return model.DB.Transaction(func(tx *gorm.DB) error {
		if err := validatePoolProxy(tx, pool.DefaultProxyId); err != nil {
			return err
		}
		now := common.GetTimestamp()
		pool.CreatedAt = now
		pool.UpdatedAt = now
		return tx.Create(pool).Error
	})
}

func UpdateUpstreamAccountPool(pool *model.UpstreamAccountPool) error {
	if pool == nil || pool.Id <= 0 {
		return errors.New("invalid upstream account pool")
	}
	if err := model.ValidateUpstreamAccountPool(pool); err != nil {
		return err
	}
	return model.DB.Transaction(func(tx *gorm.DB) error {
		var current model.UpstreamAccountPool
		if err := tx.First(&current, pool.Id).Error; err != nil {
			return err
		}
		if current.Platform != pool.Platform {
			var members int64
			if err := tx.Model(&model.UpstreamAccountPoolMember{}).Where("pool_id = ?", pool.Id).Count(&members).Error; err != nil {
				return err
			}
			if members > 0 {
				return errors.New("cannot change the platform of a non-empty account pool")
			}
		}
		if pool.CredentialType != "mixed" {
			var incompatibleMembers int64
			if err := tx.Model(&model.UpstreamAccountPoolMember{}).
				Joins("JOIN upstream_accounts ON upstream_accounts.id = upstream_account_pool_members.account_id").
				Where("upstream_account_pool_members.pool_id = ? AND (upstream_accounts.platform <> ? OR upstream_accounts.type <> ?)",
					pool.Id, pool.Platform, pool.CredentialType).
				Count(&incompatibleMembers).Error; err != nil {
				return err
			}
			if incompatibleMembers > 0 {
				return fmt.Errorf("account pool contains %d incompatible account(s)", incompatibleMembers)
			}
		}
		if err := validatePoolProxy(tx, pool.DefaultProxyId); err != nil {
			return err
		}
		return tx.Model(&model.UpstreamAccountPool{}).Where("id = ?", pool.Id).Updates(map[string]any{
			"name":             pool.Name,
			"description":      pool.Description,
			"platform":         pool.Platform,
			"credential_type":  pool.CredentialType,
			"status":           pool.Status,
			"default_proxy_id": pool.DefaultProxyId,
			"scheduler_config": pool.SchedulerConfig,
			"updated_at":       common.GetTimestamp(),
		}).Error
	})
}

func DeleteUpstreamAccountPool(id int) error {
	if id <= 0 {
		return errors.New("invalid upstream account pool id")
	}
	return model.DB.Transaction(func(tx *gorm.DB) error {
		var pool model.UpstreamAccountPool
		if err := tx.First(&pool, id).Error; err != nil {
			return err
		}
		var channelCount int64
		if err := tx.Model(&model.Channel{}).Where("upstream_account_pool_id = ?", id).Count(&channelCount).Error; err != nil {
			return err
		}
		if channelCount > 0 {
			return fmt.Errorf("account pool is referenced by %d channel(s)", channelCount)
		}
		var memberCount int64
		if err := tx.Model(&model.UpstreamAccountPoolMember{}).Where("pool_id = ?", id).Count(&memberCount).Error; err != nil {
			return err
		}
		if memberCount > 0 {
			return fmt.Errorf("account pool still contains %d account(s)", memberCount)
		}
		return tx.Delete(&pool).Error
	})
}

func ListUpstreamAccountPoolMembers(poolId int) ([]UpstreamAccountPoolMemberView, error) {
	if poolId <= 0 {
		return nil, errors.New("invalid upstream account pool id")
	}
	var members []model.UpstreamAccountPoolMember
	if err := model.DB.Where("pool_id = ?", poolId).Order("priority ASC").Order("account_id ASC").Find(&members).Error; err != nil {
		return nil, err
	}
	views := make([]UpstreamAccountPoolMemberView, 0, len(members))
	for _, member := range members {
		var account model.UpstreamAccount
		if err := model.DB.First(&account, member.AccountId).Error; err != nil {
			return nil, err
		}
		views = append(views, UpstreamAccountPoolMemberView{
			UpstreamAccountPoolMember: member,
			Name:                      account.Name, Platform: account.Platform, Type: account.Type,
			Status: account.Status, Schedulable: account.Schedulable,
		})
	}
	return views, nil
}

func ReplaceUpstreamAccountPoolMembers(poolId int, inputs []UpstreamAccountPoolMemberInput) error {
	if poolId <= 0 {
		return errors.New("invalid upstream account pool id")
	}
	return model.DB.Transaction(func(tx *gorm.DB) error {
		var pool model.UpstreamAccountPool
		if err := tx.First(&pool, poolId).Error; err != nil {
			return err
		}
		seen := make(map[int]struct{}, len(inputs))
		members := make([]model.UpstreamAccountPoolMember, 0, len(inputs))
		now := common.GetTimestamp()
		for _, input := range inputs {
			if input.AccountId <= 0 {
				return errors.New("account pool member account_id must be positive")
			}
			if input.Priority < 0 {
				return fmt.Errorf("account %d pool priority cannot be negative", input.AccountId)
			}
			if _, exists := seen[input.AccountId]; exists {
				return fmt.Errorf("account %d is duplicated in the account pool", input.AccountId)
			}
			seen[input.AccountId] = struct{}{}
			var account model.UpstreamAccount
			if err := tx.First(&account, input.AccountId).Error; err != nil {
				return fmt.Errorf("account %d is invalid: %w", input.AccountId, err)
			}
			if account.Platform != pool.Platform {
				return fmt.Errorf("account %d platform does not match pool platform", input.AccountId)
			}
			if pool.CredentialType != "mixed" && account.Type != pool.CredentialType {
				return fmt.Errorf("account %d credential type does not match pool", input.AccountId)
			}
			weight := input.Weight
			if weight <= 0 {
				weight = 1
			}
			members = append(members, model.UpstreamAccountPoolMember{
				PoolId: poolId, AccountId: input.AccountId, Priority: input.Priority, Weight: weight, CreatedAt: now,
			})
		}
		if err := tx.Where("pool_id = ?", poolId).Delete(&model.UpstreamAccountPoolMember{}).Error; err != nil {
			return err
		}
		if len(members) == 0 {
			return nil
		}
		return tx.Create(&members).Error
	})
}

func ListUpstreamAccounts(filter UpstreamAccountListFilter) ([]UpstreamAccountView, int64, error) {
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.PageSize < 1 {
		filter.PageSize = common.ItemsPerPage
	}
	if filter.PageSize > 100 {
		filter.PageSize = 100
	}
	query := model.DB.Model(&model.UpstreamAccount{})
	if platform := strings.ToLower(strings.TrimSpace(filter.Platform)); platform != "" {
		query = query.Where("platform = ?", platform)
	}
	if accountType := strings.ToLower(strings.TrimSpace(filter.Type)); accountType != "" {
		query = query.Where("type = ?", accountType)
	}
	if status := strings.ToLower(strings.TrimSpace(filter.Status)); status != "" {
		query = query.Where("status = ?", status)
	}
	if filter.Schedulable != nil {
		query = query.Where("schedulable = ?", *filter.Schedulable)
	}
	if search := strings.ToLower(strings.TrimSpace(filter.Search)); search != "" {
		query = query.Where("LOWER(name) LIKE ?", "%"+search+"%")
	}
	if filter.PoolId > 0 {
		query = query.Where("id IN (?)", model.DB.Model(&model.UpstreamAccountPoolMember{}).Select("account_id").Where("pool_id = ?", filter.PoolId))
	}
	if filter.ProxyId > 0 {
		query = query.Where("proxy_id = ?", filter.ProxyId)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var accounts []model.UpstreamAccount
	if err := query.Order("priority ASC").Order("id ASC").Limit(filter.PageSize).Offset((filter.Page - 1) * filter.PageSize).Find(&accounts).Error; err != nil {
		return nil, 0, err
	}
	accountIds := make([]int, 0, len(accounts))
	for _, account := range accounts {
		accountIds = append(accountIds, account.Id)
	}
	leaseCounts := currentUpstreamAccountLeaseCounts(accountIds)
	views := make([]UpstreamAccountView, 0, len(accounts))
	for _, account := range accounts {
		poolIds, err := listUpstreamAccountPoolIds(model.DB, account.Id)
		if err != nil {
			return nil, 0, err
		}
		view := upstreamAccountView(account, poolIds)
		view.CurrentConcurrency = leaseCounts[account.Id]
		views = append(views, view)
	}
	return views, total, nil
}

func GetUpstreamAccount(id int) (*UpstreamAccountView, error) {
	if id <= 0 {
		return nil, errors.New("invalid upstream account id")
	}
	var account model.UpstreamAccount
	if err := model.DB.First(&account, id).Error; err != nil {
		return nil, err
	}
	poolIds, err := listUpstreamAccountPoolIds(model.DB, id)
	if err != nil {
		return nil, err
	}
	view := upstreamAccountView(account, poolIds)
	view.CurrentConcurrency = currentUpstreamAccountLeaseCounts([]int{id})[id]
	return &view, nil
}

func CreateUpstreamAccount(input *UpstreamAccountCreateInput) error {
	if input == nil {
		return errors.New("upstream account input is required")
	}
	if err := model.ValidateUpstreamAccount(&input.Account); err != nil {
		return err
	}
	if err := validateUpstreamCredentialPayload(input.Account.Platform, input.Account.Type, input.Credentials); err != nil {
		return err
	}
	keyring, err := LoadUpstreamCredentialKeyringFromEnv()
	if err != nil {
		return err
	}
	poolIds := normalizePositiveIds(input.PoolIds)
	return model.DB.Transaction(func(tx *gorm.DB) error {
		if err := validateAccountReferences(tx, &input.Account, poolIds); err != nil {
			return err
		}
		now := common.GetTimestamp()
		input.Account.CreatedAt = now
		input.Account.UpdatedAt = now
		input.Account.CredentialVersion = 1
		input.Account.CredentialKeyVersion = keyring.ActiveVersion()
		input.Account.CredentialCiphertext = ""
		input.Account.CredentialNonce = ""
		explicitPriority := input.Account.Priority
		explicitSchedulable := input.Account.Schedulable
		explicitAutoPauseOnExpired := input.Account.AutoPauseOnExpired
		explicitOAuthRefreshOwner := input.Account.OAuthRefreshOwner
		if err := tx.Create(&input.Account).Error; err != nil {
			return err
		}
		// GORM applies default tags to zero values on Create. Re-apply fields
		// where zero/false is an explicit administrative choice.
		input.Account.Priority = explicitPriority
		input.Account.Schedulable = explicitSchedulable
		input.Account.AutoPauseOnExpired = explicitAutoPauseOnExpired
		input.Account.OAuthRefreshOwner = explicitOAuthRefreshOwner
		if err := tx.Model(&model.UpstreamAccount{}).Where("id = ?", input.Account.Id).Updates(map[string]any{
			"priority":              explicitPriority,
			"schedulable":           explicitSchedulable,
			"auto_pause_on_expired": explicitAutoPauseOnExpired,
			"oauth_refresh_owner":   explicitOAuthRefreshOwner,
		}).Error; err != nil {
			return err
		}
		envelope, err := keyring.EncryptJSON(upstreamAccountCredentialRecordKind, input.Account.Id, input.Account.CredentialVersion, input.Credentials)
		if err != nil {
			return err
		}
		if err := tx.Model(&model.UpstreamAccount{}).Where("id = ?", input.Account.Id).Updates(map[string]any{
			"credential_ciphertext":  envelope.Ciphertext,
			"credential_nonce":       envelope.Nonce,
			"credential_key_version": envelope.KeyVersion,
		}).Error; err != nil {
			return err
		}
		input.Account.CredentialCiphertext = envelope.Ciphertext
		input.Account.CredentialNonce = envelope.Nonce
		input.Account.CredentialKeyVersion = envelope.KeyVersion
		return replaceUpstreamAccountMemberships(tx, input.Account.Id, poolIds, input.Account.Priority, input.Account.Weight)
	})
}

func UpdateUpstreamAccount(input *UpstreamAccountUpdateInput) error {
	if input == nil || input.Account.Id <= 0 {
		return errors.New("invalid upstream account input")
	}
	if err := model.ValidateUpstreamAccount(&input.Account); err != nil {
		return err
	}
	var keyring *UpstreamCredentialKeyring
	var err error
	if input.Credentials != nil {
		if err = validateUpstreamCredentialPayload(input.Account.Platform, input.Account.Type, *input.Credentials); err != nil {
			return err
		}
		keyring, err = LoadUpstreamCredentialKeyringFromEnv()
		if err != nil {
			return err
		}
	}
	return model.DB.Transaction(func(tx *gorm.DB) error {
		var current model.UpstreamAccount
		if err := tx.First(&current, input.Account.Id).Error; err != nil {
			return err
		}
		if current.Type != input.Account.Type && input.Credentials == nil {
			return errors.New("changing account type requires replacement credentials")
		}
		poolIds, err := listUpstreamAccountPoolIds(tx, current.Id)
		if err != nil {
			return err
		}
		if input.PoolIds != nil {
			poolIds = normalizePositiveIds(*input.PoolIds)
		}
		if err := validateAccountReferences(tx, &input.Account, poolIds); err != nil {
			return err
		}
		updates := map[string]any{
			"name": input.Account.Name, "notes": input.Account.Notes, "platform": input.Account.Platform,
			"type": input.Account.Type, "extra": input.Account.Extra, "proxy_id": input.Account.ProxyId,
			"proxy_fallback_origin_id": input.Account.ProxyFallbackOriginId,
			"concurrency":              input.Account.Concurrency, "priority": input.Account.Priority, "weight": input.Account.Weight,
			"load_factor": input.Account.LoadFactor, "rate_multiplier": input.Account.RateMultiplier,
			"status": input.Account.Status, "schedulable": input.Account.Schedulable,
			"expires_at": input.Account.ExpiresAt, "auto_pause_on_expired": input.Account.AutoPauseOnExpired,
			"oauth_refresh_owner": input.Account.OAuthRefreshOwner,
			"updated_at":          common.GetTimestamp(),
		}
		if input.Credentials != nil {
			newVersion := current.CredentialVersion + 1
			envelope, err := keyring.EncryptJSON(upstreamAccountCredentialRecordKind, current.Id, newVersion, *input.Credentials)
			if err != nil {
				return err
			}
			updates["credential_ciphertext"] = envelope.Ciphertext
			updates["credential_nonce"] = envelope.Nonce
			updates["credential_key_version"] = envelope.KeyVersion
			updates["credential_version"] = newVersion
		}
		result := tx.Model(&model.UpstreamAccount{}).Where("id = ? AND credential_version = ?", current.Id, current.CredentialVersion).Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("upstream account was updated concurrently")
		}
		if input.PoolIds != nil {
			return replaceUpstreamAccountMemberships(tx, current.Id, poolIds, input.Account.Priority, input.Account.Weight)
		}
		return nil
	})
}

func DeleteUpstreamAccount(id int) error {
	if id <= 0 {
		return errors.New("invalid upstream account id")
	}
	return model.DB.Transaction(func(tx *gorm.DB) error {
		var account model.UpstreamAccount
		if err := tx.First(&account, id).Error; err != nil {
			return err
		}
		if err := tx.Where("account_id = ?", id).Delete(&model.UpstreamAccountPoolMember{}).Error; err != nil {
			return err
		}
		return tx.Delete(&account).Error
	})
}

func RecoverUpstreamAccountRuntimeState(id int, scope UpstreamAccountRecoveryScope) error {
	if id <= 0 {
		return errors.New("invalid upstream account id")
	}
	if scope == "" {
		scope = UpstreamAccountRecoveryAll
	}
	if scope != UpstreamAccountRecoveryAll && scope != UpstreamAccountRecoveryRateLimit && scope != UpstreamAccountRecoveryTemporary {
		return errors.New("unsupported upstream account recovery scope")
	}
	return model.DB.Transaction(func(tx *gorm.DB) error {
		var account model.UpstreamAccount
		if err := tx.First(&account, id).Error; err != nil {
			return err
		}
		updates := map[string]any{"updated_at": common.GetTimestamp()}
		if scope == UpstreamAccountRecoveryAll || scope == UpstreamAccountRecoveryRateLimit {
			updates["rate_limited_at"] = nil
			updates["rate_limit_reset_at"] = nil
			updates["session_window_start"] = nil
			updates["session_window_end"] = nil
			updates["session_window_status"] = ""
		}
		if scope == UpstreamAccountRecoveryAll || scope == UpstreamAccountRecoveryTemporary {
			updates["overload_until"] = nil
			updates["temp_unschedulable_until"] = nil
			updates["temp_unschedulable_reason"] = ""
		}
		if scope == UpstreamAccountRecoveryAll {
			updates["error_message"] = ""
			if account.ExpiresAt != nil && *account.ExpiresAt <= common.GetTimestamp() {
				updates["status"] = constant.UpstreamStatusExpired
				updates["schedulable"] = false
			} else {
				updates["status"] = constant.UpstreamStatusActive
				updates["schedulable"] = true
			}
		}
		return tx.Model(&model.UpstreamAccount{}).Where("id = ?", id).Updates(updates).Error
	})
}

func ListUpstreamProxies() ([]UpstreamProxyView, error) {
	var proxies []model.UpstreamProxy
	if err := model.DB.Order("id ASC").Find(&proxies).Error; err != nil {
		return nil, err
	}
	views := make([]UpstreamProxyView, 0, len(proxies))
	for _, proxy := range proxies {
		view, err := upstreamProxyView(model.DB, proxy)
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	return views, nil
}

func GetUpstreamProxy(id int) (*UpstreamProxyView, error) {
	if id <= 0 {
		return nil, errors.New("invalid upstream proxy id")
	}
	var proxy model.UpstreamProxy
	if err := model.DB.First(&proxy, id).Error; err != nil {
		return nil, err
	}
	view, err := upstreamProxyView(model.DB, proxy)
	if err != nil {
		return nil, err
	}
	return &view, nil
}

func CreateUpstreamProxy(input *UpstreamProxyCreateInput) error {
	if input == nil {
		return errors.New("upstream proxy input is required")
	}
	if err := model.ValidateUpstreamProxy(&input.Proxy); err != nil {
		return err
	}
	return model.DB.Transaction(func(tx *gorm.DB) error {
		if err := validateProxyFallback(tx, &input.Proxy); err != nil {
			return err
		}
		now := common.GetTimestamp()
		input.Proxy.CreatedAt = now
		input.Proxy.UpdatedAt = now
		input.Proxy.Username = input.Auth.Username
		input.Proxy.Password = input.Auth.Password
		input.Proxy.AuthCredentialVersion = 1
		input.Proxy.AuthKeyVersion = 0
		input.Proxy.AuthCiphertext = ""
		input.Proxy.AuthNonce = ""
		return tx.Create(&input.Proxy).Error
	})
}

func UpdateUpstreamProxy(input *UpstreamProxyUpdateInput) error {
	if input == nil || input.Proxy.Id <= 0 {
		return errors.New("invalid upstream proxy input")
	}
	if err := model.ValidateUpstreamProxy(&input.Proxy); err != nil {
		return err
	}
	return model.DB.Transaction(func(tx *gorm.DB) error {
		var current model.UpstreamProxy
		if err := tx.First(&current, input.Proxy.Id).Error; err != nil {
			return err
		}
		if err := validateProxyFallback(tx, &input.Proxy); err != nil {
			return err
		}
		updates := map[string]any{
			"name": input.Proxy.Name, "protocol": input.Proxy.Protocol, "host": input.Proxy.Host,
			"port": input.Proxy.Port, "status": input.Proxy.Status, "expires_at": input.Proxy.ExpiresAt,
			"fallback_mode": input.Proxy.FallbackMode, "backup_proxy_id": input.Proxy.BackupProxyId,
			"expiry_warn_days": input.Proxy.ExpiryWarnDays, "updated_at": common.GetTimestamp(),
		}
		if input.Auth != nil {
			username := strings.TrimSpace(input.Auth.Username)
			password := strings.TrimSpace(input.Auth.Password)
			if username != "" || password != "" {
				currentAuth, err := DecryptUpstreamProxyAuth(&current)
				if err != nil {
					return err
				}
				if username != "" {
					currentAuth.Username = username
				}
				if password != "" {
					currentAuth.Password = password
				}
				newVersion := current.AuthCredentialVersion + 1
				updates["username"] = currentAuth.Username
				updates["password"] = currentAuth.Password
				updates["auth_ciphertext"] = ""
				updates["auth_nonce"] = ""
				updates["auth_key_version"] = 0
				updates["auth_credential_version"] = newVersion
			}
		}
		result := tx.Model(&model.UpstreamProxy{}).Where("id = ? AND auth_credential_version = ?", current.Id, current.AuthCredentialVersion).Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("upstream proxy was updated concurrently")
		}
		return nil
	})
}

func DeleteUpstreamProxy(id int) error {
	if id <= 0 {
		return errors.New("invalid upstream proxy id")
	}
	return model.DB.Transaction(func(tx *gorm.DB) error {
		var proxy model.UpstreamProxy
		if err := tx.First(&proxy, id).Error; err != nil {
			return err
		}
		checks := []struct {
			model any
			query string
			label string
		}{
			{&model.UpstreamAccount{}, "proxy_id = ? OR proxy_fallback_origin_id = ?", "account"},
			{&model.UpstreamAccountPool{}, "default_proxy_id = ?", "account pool"},
			{&model.UpstreamProxy{}, "backup_proxy_id = ?", "proxy fallback"},
		}
		for _, check := range checks {
			var count int64
			query := tx.Model(check.model)
			if strings.Contains(check.query, " OR ") {
				query = query.Where(check.query, id, id)
			} else {
				query = query.Where(check.query, id)
			}
			if err := query.Count(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				return fmt.Errorf("proxy is referenced by %d %s record(s)", count, check.label)
			}
		}
		return tx.Delete(&proxy).Error
	})
}

func DecryptUpstreamAccountCredentials(account *model.UpstreamAccount) (map[string]any, error) {
	if account == nil || account.Id <= 0 {
		return nil, errors.New("invalid upstream account")
	}
	keyring, err := LoadUpstreamCredentialKeyringFromEnv()
	if err != nil {
		return nil, err
	}
	var credentials map[string]any
	err = keyring.DecryptJSON(UpstreamCredentialEnvelope{
		Ciphertext: account.CredentialCiphertext,
		Nonce:      account.CredentialNonce,
		KeyVersion: account.CredentialKeyVersion,
	}, upstreamAccountCredentialRecordKind, account.Id, account.CredentialVersion, &credentials)
	return credentials, err
}

func DecryptUpstreamProxyAuth(proxy *model.UpstreamProxy) (*UpstreamProxyAuthInput, error) {
	if proxy == nil || proxy.Id <= 0 {
		return nil, errors.New("invalid upstream proxy")
	}
	if proxy.Username != "" || proxy.Password != "" || proxy.AuthCiphertext == "" {
		return &UpstreamProxyAuthInput{Username: proxy.Username, Password: proxy.Password}, nil
	}
	keyring, err := LoadUpstreamCredentialKeyringFromEnv()
	if err != nil {
		return nil, err
	}
	var auth UpstreamProxyAuthInput
	err = keyring.DecryptJSON(UpstreamCredentialEnvelope{
		Ciphertext: proxy.AuthCiphertext,
		Nonce:      proxy.AuthNonce,
		KeyVersion: proxy.AuthKeyVersion,
	}, upstreamProxyAuthRecordKind, proxy.Id, proxy.AuthCredentialVersion, &auth)
	return &auth, err
}

func validateUpstreamCredentialPayload(platform string, accountType string, credentials map[string]any) error {
	if credentials == nil {
		return errors.New("account credentials are required")
	}
	getString := func(key string) string {
		value, ok := credentials[key]
		if !ok || value == nil {
			return ""
		}
		return strings.TrimSpace(fmt.Sprintf("%v", value))
	}
	platform = strings.ToLower(strings.TrimSpace(platform))
	accountType = strings.ToLower(strings.TrimSpace(accountType))
	if !model.IsSupportedUpstreamAccountType(platform, accountType) {
		return errors.New("unsupported account type")
	}
	switch accountType {
	case constant.UpstreamAccountTypeOAuth:
		if getString("access_token") == "" {
			return errors.New("OAuth credentials require access_token")
		}
		if platform == constant.UpstreamPlatformOpenAI && getString("account_id") == "" {
			return errors.New("OpenAI OAuth credentials require account_id")
		}
	case constant.UpstreamAccountTypeSetupToken:
		if getString("access_token") == "" {
			return errors.New("setup token credentials require access_token")
		}
	case constant.UpstreamAccountTypeAPIKey:
		if getString("api_key") == "" {
			return errors.New("API key credentials require api_key")
		}
	case constant.UpstreamAccountTypeBedrock:
		authMethod := strings.ToLower(getString("auth_mode"))
		switch authMethod {
		case "api_key":
			if getString("api_key") == "" || getString("aws_region") == "" {
				return errors.New("Bedrock API key credentials require api_key and region")
			}
		case "sigv4", "":
			if getString("aws_access_key_id") == "" || getString("aws_secret_access_key") == "" || getString("aws_region") == "" {
				return errors.New("Bedrock SigV4 credentials require access_key_id, secret_access_key, and region")
			}
		default:
			return errors.New("unsupported Bedrock authentication method")
		}
	case constant.UpstreamAccountTypeServiceAccount:
		raw := getString("service_account_json")
		if raw == "" {
			return errors.New("Vertex credentials require service_account_json")
		}
		var serviceAccount map[string]any
		if err := common.Unmarshal([]byte(raw), &serviceAccount); err != nil {
			return errors.New("Vertex service_account_json must be valid JSON")
		}
		for _, key := range []string{"project_id", "client_email", "private_key"} {
			value := strings.TrimSpace(fmt.Sprintf("%v", serviceAccount[key]))
			if value == "" || value == "<nil>" {
				return fmt.Errorf("Vertex service_account_json requires %s", key)
			}
		}
	default:
		return errors.New("unsupported account type")
	}
	return nil
}

func validateAccountReferences(tx *gorm.DB, account *model.UpstreamAccount, poolIds []int) error {
	if account.ProxyId != nil {
		var proxy model.UpstreamProxy
		if err := tx.First(&proxy, *account.ProxyId).Error; err != nil {
			return fmt.Errorf("account proxy is invalid: %w", err)
		}
	}
	for _, poolId := range poolIds {
		var pool model.UpstreamAccountPool
		if err := tx.First(&pool, poolId).Error; err != nil {
			return fmt.Errorf("account pool %d is invalid: %w", poolId, err)
		}
		if pool.Platform != account.Platform {
			return fmt.Errorf("account pool %d platform does not match account platform", poolId)
		}
		if pool.CredentialType != "mixed" && pool.CredentialType != account.Type {
			return fmt.Errorf("account pool %d credential type does not match account type", poolId)
		}
	}
	return nil
}

func validatePoolProxy(tx *gorm.DB, proxyId *int) error {
	if proxyId == nil {
		return nil
	}
	var proxy model.UpstreamProxy
	if err := tx.First(&proxy, *proxyId).Error; err != nil {
		return fmt.Errorf("default proxy is invalid: %w", err)
	}
	return nil
}

func validateProxyFallback(tx *gorm.DB, proxy *model.UpstreamProxy) error {
	if proxy.BackupProxyId == nil {
		return nil
	}
	visited := map[int]struct{}{}
	if proxy.Id > 0 {
		visited[proxy.Id] = struct{}{}
	}
	nextId := proxy.BackupProxyId
	for nextId != nil {
		if _, exists := visited[*nextId]; exists {
			return errors.New("proxy fallback chain contains a cycle")
		}
		visited[*nextId] = struct{}{}
		var next model.UpstreamProxy
		if err := tx.First(&next, *nextId).Error; err != nil {
			return fmt.Errorf("backup proxy is invalid: %w", err)
		}
		if next.FallbackMode != constant.UpstreamProxyFallbackProxy {
			break
		}
		nextId = next.BackupProxyId
	}
	return nil
}

func replaceUpstreamAccountMemberships(tx *gorm.DB, accountId int, poolIds []int, defaultPriority int, defaultWeight int) error {
	var existing []model.UpstreamAccountPoolMember
	if err := tx.Where("account_id = ?", accountId).Find(&existing).Error; err != nil {
		return err
	}
	desired := make(map[int]struct{}, len(poolIds))
	for _, poolId := range poolIds {
		desired[poolId] = struct{}{}
	}
	existingByPool := make(map[int]struct{}, len(existing))
	removeIds := make([]int, 0)
	for _, member := range existing {
		existingByPool[member.PoolId] = struct{}{}
		if _, keep := desired[member.PoolId]; !keep {
			removeIds = append(removeIds, member.PoolId)
		}
	}
	if len(removeIds) > 0 {
		if err := tx.Where("account_id = ? AND pool_id IN ?", accountId, removeIds).Delete(&model.UpstreamAccountPoolMember{}).Error; err != nil {
			return err
		}
	}
	if defaultWeight <= 0 {
		defaultWeight = 1
	}
	now := common.GetTimestamp()
	members := make([]model.UpstreamAccountPoolMember, 0, len(poolIds))
	for _, poolId := range poolIds {
		if _, exists := existingByPool[poolId]; exists {
			continue
		}
		members = append(members, model.UpstreamAccountPoolMember{
			PoolId: poolId, AccountId: accountId, Priority: defaultPriority,
			Weight: defaultWeight, CreatedAt: now,
		})
	}
	if len(members) == 0 {
		return nil
	}
	return tx.Create(&members).Error
}

func listUpstreamAccountPoolIds(db *gorm.DB, accountId int) ([]int, error) {
	var members []model.UpstreamAccountPoolMember
	if err := db.Where("account_id = ?", accountId).Order("pool_id ASC").Find(&members).Error; err != nil {
		return nil, err
	}
	poolIds := make([]int, 0, len(members))
	for _, member := range members {
		poolIds = append(poolIds, member.PoolId)
	}
	return poolIds, nil
}

func normalizePositiveIds(ids []int) []int {
	set := make(map[int]struct{}, len(ids))
	for _, id := range ids {
		if id > 0 {
			set[id] = struct{}{}
		}
	}
	normalized := make([]int, 0, len(set))
	for id := range set {
		normalized = append(normalized, id)
	}
	sort.Ints(normalized)
	return normalized
}

func upstreamAccountView(account model.UpstreamAccount, poolIds []int) UpstreamAccountView {
	configured := account.CredentialCiphertext != ""
	metadata := upstreamAccountMetadata(&account)
	account.CredentialCiphertext = ""
	account.CredentialNonce = ""
	return UpstreamAccountView{UpstreamAccount: account, CredentialConfigured: configured, PoolIds: poolIds, Metadata: metadata}
}

func currentUpstreamAccountLeaseCounts(accountIds []int) map[int]int {
	counts := make(map[int]int, len(accountIds))
	if len(accountIds) == 0 {
		return counts
	}
	router, err := GetConfiguredUpstreamAccountRouter()
	if err != nil || router == nil || router.leaseManager == nil {
		return counts
	}
	current, err := router.leaseManager.CountBatch(context.Background(), accountIds)
	if err != nil {
		return counts
	}
	return current
}

func upstreamAccountMetadata(account *model.UpstreamAccount) UpstreamAccountMetadata {
	metadata := UpstreamAccountMetadata{}
	if account == nil {
		return metadata
	}
	if credentials, err := DecryptUpstreamAccountCredentials(account); err == nil {
		metadata.Email = upstreamCredentialMapString(credentials, "email")
		metadata.PlanType = upstreamCredentialMapString(credentials, "plan_type")
		if metadata.PlanType == "" {
			metadata.PlanType = upstreamCredentialMapString(credentials, "plan")
		}
		metadata.PrivacyMode = upstreamCredentialMapString(credentials, "privacy_mode")
		for _, key := range []string{"metadata", "live_identity"} {
			nested, _ := credentials[key].(map[string]any)
			if metadata.Email == "" {
				metadata.Email = upstreamCredentialMapString(nested, "email")
			}
			if metadata.PlanType == "" {
				metadata.PlanType = upstreamCredentialMapString(nested, "plan")
			}
			if metadata.PrivacyMode == "" {
				metadata.PrivacyMode = upstreamCredentialMapString(nested, "privacy_mode")
			}
		}
	}
	var extra map[string]any
	if common.UnmarshalJsonStr(account.Extra, &extra) == nil {
		if metadata.PrivacyMode == "" {
			metadata.PrivacyMode = upstreamCredentialMapString(extra, "privacy_mode")
		}
	}
	if account.Platform == constant.UpstreamPlatformOpenAI {
		options, err := model.ParseUpstreamAccountOptions(account.Extra)
		if err != nil {
			return metadata
		}
		metadata.CompactMode = options.OpenAICompactMode
		metadata.CompactSupported = options.AllowsOpenAICompact()
	}
	return metadata
}

func upstreamProxyView(db *gorm.DB, proxy model.UpstreamProxy) (UpstreamProxyView, error) {
	auth := UpstreamProxyAuthInput{Username: proxy.Username, Password: proxy.Password}
	if auth.Username == "" && auth.Password == "" && proxy.AuthCiphertext != "" {
		if legacyAuth, err := DecryptUpstreamProxyAuth(&proxy); err == nil {
			auth = *legacyAuth
		}
	}
	view := UpstreamProxyView{
		UpstreamProxy: proxy,
		AuthConfigured: auth.Username != "" || auth.Password != "" ||
			(proxy.AuthCiphertext != "" && proxy.AuthNonce != "" && proxy.AuthKeyVersion > 0),
		AuthUsername: auth.Username,
		AuthPassword: auth.Password,
	}
	view.AuthCiphertext = ""
	view.AuthNonce = ""
	if err := db.Model(&model.UpstreamAccount{}).Where("proxy_id = ?", proxy.Id).Count(&view.AccountCount).Error; err != nil {
		return UpstreamProxyView{}, err
	}
	if err := db.Model(&model.UpstreamAccountPool{}).Where("default_proxy_id = ?", proxy.Id).Count(&view.PoolCount).Error; err != nil {
		return UpstreamProxyView{}, err
	}
	if err := db.Model(&model.UpstreamProxy{}).Where("backup_proxy_id = ?", proxy.Id).Count(&view.BackupCount).Error; err != nil {
		return UpstreamProxyView{}, err
	}
	return view, nil
}
