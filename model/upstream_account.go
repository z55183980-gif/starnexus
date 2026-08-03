package model

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
)

type UpstreamAccountPool struct {
	Id              int     `json:"id"`
	Name            string  `json:"name" gorm:"type:varchar(128);not null;uniqueIndex"`
	Description     string  `json:"description" gorm:"type:varchar(512);default:''"`
	Platform        string  `json:"platform" gorm:"type:varchar(32);not null;index"`
	CredentialType  string  `json:"credential_type" gorm:"type:varchar(32);not null;default:'mixed'"`
	Status          string  `json:"status" gorm:"type:varchar(16);not null;default:'active';index"`
	DefaultProxyId  *int    `json:"default_proxy_id" gorm:"index"`
	SchedulerConfig string  `json:"scheduler_config" gorm:"type:text;not null"`
	SourceSystem    *string `json:"source_system,omitempty" gorm:"type:varchar(32);uniqueIndex:idx_upstream_pool_source,priority:1"`
	SourceId        *int64  `json:"source_id,omitempty" gorm:"uniqueIndex:idx_upstream_pool_source,priority:2"`
	ImportRunId     *string `json:"import_run_id,omitempty" gorm:"type:varchar(64);index"`
	CreatedAt       int64   `json:"created_at" gorm:"bigint;not null"`
	UpdatedAt       int64   `json:"updated_at" gorm:"bigint;not null"`
}

func (UpstreamAccountPool) TableName() string { return "upstream_account_pools" }

type UpstreamAccountSchedulerConfig struct {
	Version                   int                `json:"version"`
	TopK                      int                `json:"top_k"`
	Strategy                  string             `json:"strategy,omitempty"`
	PrioritySource            string             `json:"priority_source,omitempty"`
	ModelRoutingEnabled       bool               `json:"model_routing_enabled"`
	DefaultMappedModel        string             `json:"default_mapped_model"`
	ModelRouting              map[string][]int64 `json:"model_routing"`
	ModelRoutingIdSpace       string             `json:"model_routing_id_space"`
	SessionAffinityEnabled    bool               `json:"session_affinity_enabled,omitempty"`
	SessionAffinityTTLSeconds int                `json:"session_affinity_ttl_seconds,omitempty"`
	SessionAffinityWaitMs     int                `json:"session_affinity_wait_ms,omitempty"`
	SessionAffinityMaxWaiters int                `json:"session_affinity_max_waiters,omitempty"`
}

func ParseUpstreamAccountSchedulerConfig(raw string) (UpstreamAccountSchedulerConfig, error) {
	config := UpstreamAccountSchedulerConfig{Version: 1}
	if strings.TrimSpace(raw) == "" || strings.TrimSpace(raw) == "{}" {
		return config, nil
	}
	if err := common.UnmarshalJsonStr(raw, &config); err != nil {
		return config, errors.New("scheduler_config must be a JSON object")
	}
	if config.Version == 0 {
		config.Version = 1
	}
	if config.Version != 1 && config.Version != 2 {
		return config, fmt.Errorf("unsupported scheduler_config version %d", config.Version)
	}
	if config.TopK < 0 || config.TopK > 100 {
		return config, errors.New("scheduler_config top_k must be between 0 and 100")
	}
	if strings.TrimSpace(config.Strategy) == "" {
		if config.Version == 2 {
			config.Strategy = "load_score"
		} else {
			config.Strategy = "priority_tier"
		}
	}
	switch config.Strategy {
	case "priority_tier", "load_score":
	default:
		return config, errors.New("scheduler_config strategy must be priority_tier or load_score")
	}
	if config.Strategy == "load_score" && strings.TrimSpace(config.PrioritySource) == "" {
		config.PrioritySource = "account"
	}
	switch config.PrioritySource {
	case "", "account", "member":
	default:
		return config, errors.New("scheduler_config priority_source must be account or member")
	}
	switch config.ModelRoutingIdSpace {
	case "", "source", "destination":
	default:
		return config, errors.New("scheduler_config model_routing_id_space must be source or destination")
	}
	for pattern, accountIds := range config.ModelRouting {
		if strings.TrimSpace(pattern) == "" {
			return config, errors.New("scheduler_config model_routing contains an empty model pattern")
		}
		for _, accountId := range accountIds {
			if accountId <= 0 {
				return config, errors.New("scheduler_config model_routing account ids must be positive")
			}
		}
	}
	if config.SessionAffinityEnabled {
		if config.SessionAffinityTTLSeconds == 0 {
			config.SessionAffinityTTLSeconds = 3600
		}
		if config.SessionAffinityWaitMs == 0 {
			config.SessionAffinityWaitMs = 1500
		}
		if config.SessionAffinityMaxWaiters == 0 {
			config.SessionAffinityMaxWaiters = 3
		}
	}
	if config.SessionAffinityTTLSeconds < 0 || config.SessionAffinityTTLSeconds > 86400 {
		return config, errors.New("scheduler_config session_affinity_ttl_seconds must be between 0 and 86400")
	}
	if config.SessionAffinityEnabled && config.SessionAffinityTTLSeconds < 60 {
		return config, errors.New("scheduler_config session_affinity_ttl_seconds must be at least 60 when enabled")
	}
	if config.SessionAffinityWaitMs < 0 || config.SessionAffinityWaitMs > 10000 {
		return config, errors.New("scheduler_config session_affinity_wait_ms must be between 0 and 10000")
	}
	if config.SessionAffinityMaxWaiters < 0 || config.SessionAffinityMaxWaiters > 1000 {
		return config, errors.New("scheduler_config session_affinity_max_waiters must be between 0 and 1000")
	}
	return config, nil
}

func (config UpstreamAccountSchedulerConfig) RoutingAccountIds(modelName string) []int64 {
	modelName = strings.TrimSpace(modelName)
	if !config.ModelRoutingEnabled || modelName == "" || len(config.ModelRouting) == 0 {
		return nil
	}
	if accountIds := config.ModelRouting[modelName]; len(accountIds) > 0 {
		return accountIds
	}
	bestPattern := ""
	for pattern, accountIds := range config.ModelRouting {
		if len(accountIds) == 0 || !strings.HasSuffix(pattern, "*") {
			continue
		}
		prefix := strings.TrimSuffix(pattern, "*")
		if strings.HasPrefix(modelName, prefix) && (len(pattern) > len(bestPattern) || (len(pattern) == len(bestPattern) && pattern < bestPattern)) {
			bestPattern = pattern
		}
	}
	return config.ModelRouting[bestPattern]
}

type UpstreamAccount struct {
	Id                      int      `json:"id"`
	Name                    string   `json:"name" gorm:"type:varchar(255);not null;index"`
	Notes                   *string  `json:"notes,omitempty" gorm:"type:text"`
	Platform                string   `json:"platform" gorm:"type:varchar(32);not null;index"`
	Type                    string   `json:"type" gorm:"type:varchar(32);not null;index"`
	CredentialCiphertext    string   `json:"-" gorm:"type:text;not null"`
	CredentialNonce         string   `json:"-" gorm:"type:varchar(64);not null"`
	CredentialKeyVersion    int      `json:"credential_key_version" gorm:"not null"`
	CredentialVersion       int64    `json:"credential_version" gorm:"bigint;not null;default:1"`
	Extra                   string   `json:"extra" gorm:"type:text;not null"`
	ProxyId                 *int     `json:"proxy_id" gorm:"index"`
	ProxyFallbackOriginId   *int     `json:"proxy_fallback_origin_id" gorm:"index"`
	Concurrency             int      `json:"concurrency" gorm:"not null;default:1"`
	Priority                int      `json:"priority" gorm:"not null;default:50;index"`
	Weight                  int      `json:"weight" gorm:"not null;default:1"`
	LoadFactor              *int     `json:"load_factor"`
	RateMultiplier          *float64 `json:"rate_multiplier"`
	Status                  string   `json:"status" gorm:"type:varchar(16);not null;default:'active';index"`
	Schedulable             bool     `json:"schedulable" gorm:"not null;default:true;index"`
	ErrorMessage            string   `json:"error_message" gorm:"type:text;not null"`
	LastUsedAt              *int64   `json:"last_used_at" gorm:"bigint"`
	RateLimitedAt           *int64   `json:"rate_limited_at" gorm:"bigint"`
	RateLimitResetAt        *int64   `json:"rate_limit_reset_at" gorm:"bigint;index"`
	OverloadUntil           *int64   `json:"overload_until" gorm:"bigint;index"`
	TempUnschedulableUntil  *int64   `json:"temp_unschedulable_until" gorm:"bigint;index"`
	TempUnschedulableReason string   `json:"temp_unschedulable_reason" gorm:"type:varchar(128);not null"`
	SessionWindowStart      *int64   `json:"session_window_start" gorm:"bigint"`
	SessionWindowEnd        *int64   `json:"session_window_end" gorm:"bigint"`
	SessionWindowStatus     string   `json:"session_window_status" gorm:"type:varchar(32);not null"`
	ExpiresAt               *int64   `json:"expires_at" gorm:"bigint;index"`
	AutoPauseOnExpired      bool     `json:"auto_pause_on_expired" gorm:"not null;default:true"`
	OAuthRefreshOwner       string   `json:"oauth_refresh_owner" gorm:"column:oauth_refresh_owner;type:varchar(16);not null;default:'external';index"`
	SourceSystem            *string  `json:"source_system,omitempty" gorm:"type:varchar(32);uniqueIndex:idx_upstream_account_source,priority:1"`
	SourceId                *int64   `json:"source_id,omitempty" gorm:"uniqueIndex:idx_upstream_account_source,priority:2"`
	ImportRunId             *string  `json:"import_run_id,omitempty" gorm:"type:varchar(64);index"`
	CreatedAt               int64    `json:"created_at" gorm:"bigint;not null"`
	UpdatedAt               int64    `json:"updated_at" gorm:"bigint;not null"`
}

func (UpstreamAccount) TableName() string { return "upstream_accounts" }

func (account *UpstreamAccount) EffectiveLoadFactor() int {
	if account == nil {
		return 1
	}
	if account.LoadFactor != nil && *account.LoadFactor > 0 {
		return *account.LoadFactor
	}
	if account.Concurrency > 0 {
		return account.Concurrency
	}
	return 1
}

func (account *UpstreamAccount) IsSchedulableAt(now int64) bool {
	if account == nil || account.Status != constant.UpstreamStatusActive || !account.Schedulable {
		return false
	}
	if constant.UpstreamAccountOAuthRefreshBlocksScheduling(account.TempUnschedulableReason) {
		return false
	}
	if account.AutoPauseOnExpired && timestampInFutureOrEqual(now, account.ExpiresAt) {
		return false
	}
	if timestampInFuture(now, account.RateLimitResetAt) || timestampInFuture(now, account.OverloadUntil) || timestampInFuture(now, account.TempUnschedulableUntil) {
		return false
	}
	return true
}

func timestampInFuture(now int64, value *int64) bool {
	return value != nil && *value > now
}

func timestampInFutureOrEqual(now int64, value *int64) bool {
	return value != nil && *value <= now
}

type UpstreamAccountPoolMember struct {
	PoolId    int   `json:"pool_id" gorm:"primaryKey;autoIncrement:false"`
	AccountId int   `json:"account_id" gorm:"primaryKey;autoIncrement:false;index"`
	Priority  int   `json:"priority" gorm:"not null;default:0"`
	Weight    int   `json:"weight" gorm:"not null;default:1"`
	CreatedAt int64 `json:"created_at" gorm:"bigint;not null"`
}

func (UpstreamAccountPoolMember) TableName() string { return "upstream_account_pool_members" }

type UpstreamProxy struct {
	Id                    int     `json:"id"`
	Name                  string  `json:"name" gorm:"type:varchar(128);not null;index"`
	Protocol              string  `json:"protocol" gorm:"type:varchar(16);not null;index"`
	Host                  string  `json:"host" gorm:"type:varchar(255);not null;index"`
	Port                  int     `json:"port" gorm:"not null"`
	Username              string  `json:"-" gorm:"type:varchar(255)"`
	Password              string  `json:"-" gorm:"type:text"`
	AuthCiphertext        string  `json:"-" gorm:"type:text;not null"`
	AuthNonce             string  `json:"-" gorm:"type:varchar(64);not null"`
	AuthKeyVersion        int     `json:"auth_key_version" gorm:"not null"`
	AuthCredentialVersion int64   `json:"auth_credential_version" gorm:"bigint;not null;default:1"`
	Status                string  `json:"status" gorm:"type:varchar(16);not null;default:'active';index"`
	ExpiresAt             *int64  `json:"expires_at" gorm:"bigint;index"`
	FallbackMode          string  `json:"fallback_mode" gorm:"type:varchar(16);not null;default:'none'"`
	BackupProxyId         *int    `json:"backup_proxy_id" gorm:"index"`
	ExpiryWarnDays        int     `json:"expiry_warn_days" gorm:"not null;default:0"`
	LastTestAt            *int64  `json:"last_test_at" gorm:"bigint"`
	LatencyMs             *int64  `json:"latency_ms" gorm:"bigint"`
	LatencyStatus         string  `json:"latency_status" gorm:"type:varchar(32);not null"`
	LatencyMessage        string  `json:"latency_message" gorm:"type:varchar(512);not null"`
	ObservedIp            string  `json:"observed_ip" gorm:"type:varchar(64);not null"`
	ObservedCountry       string  `json:"observed_country" gorm:"type:varchar(64);not null"`
	ObservedRegion        string  `json:"observed_region" gorm:"type:varchar(64);not null"`
	ObservedCity          string  `json:"observed_city" gorm:"type:varchar(64);not null"`
	SourceSystem          *string `json:"source_system,omitempty" gorm:"type:varchar(32);uniqueIndex:idx_upstream_proxy_source,priority:1"`
	SourceId              *int64  `json:"source_id,omitempty" gorm:"uniqueIndex:idx_upstream_proxy_source,priority:2"`
	ImportRunId           *string `json:"import_run_id,omitempty" gorm:"type:varchar(64);index"`
	CreatedAt             int64   `json:"created_at" gorm:"bigint;not null"`
	UpdatedAt             int64   `json:"updated_at" gorm:"bigint;not null"`
}

func (UpstreamProxy) TableName() string { return "upstream_proxies" }

func (proxy *UpstreamProxy) IsUsableAt(now int64) bool {
	return proxy != nil && proxy.Status == constant.UpstreamStatusActive && (proxy.ExpiresAt == nil || *proxy.ExpiresAt > now)
}

func (proxy *UpstreamProxy) URL(username string, password string) (string, error) {
	if err := ValidateUpstreamProxy(proxy); err != nil {
		return "", err
	}
	parsed := &url.URL{Scheme: proxy.Protocol, Host: net.JoinHostPort(proxy.Host, strconv.Itoa(proxy.Port))}
	if username != "" {
		parsed.User = url.UserPassword(username, password)
	}
	return parsed.String(), nil
}

type UpstreamAccountEvent struct {
	Id        int    `json:"id"`
	AccountId *int   `json:"account_id,omitempty" gorm:"index"`
	PoolId    *int   `json:"pool_id,omitempty" gorm:"index"`
	ProxyId   *int   `json:"proxy_id,omitempty" gorm:"index"`
	ChannelId *int   `json:"channel_id,omitempty" gorm:"index"`
	RequestId string `json:"request_id" gorm:"type:varchar(128);not null;index"`
	LeaseId   string `json:"lease_id" gorm:"type:varchar(64);not null;index"`
	EventType string `json:"event_type" gorm:"type:varchar(64);not null;index"`
	Result    string `json:"result" gorm:"type:varchar(32);not null;index"`
	Message   string `json:"message" gorm:"type:varchar(512);not null"`
	Metadata  string `json:"metadata" gorm:"type:text;not null"`
	CreatedAt int64  `json:"created_at" gorm:"bigint;not null;index"`
}

func (UpstreamAccountEvent) TableName() string { return "upstream_account_events" }

type UpstreamAccountScheduledTestPlan struct {
	Id              int    `json:"id"`
	AccountId       int    `json:"account_id" gorm:"not null;index"`
	Name            string `json:"name" gorm:"type:varchar(128);not null"`
	Model           string `json:"model" gorm:"type:varchar(128);not null"`
	IntervalMinutes int    `json:"interval_minutes" gorm:"not null;default:60"`
	Enabled         bool   `json:"enabled" gorm:"not null;index"`
	AutoRecover     bool   `json:"auto_recover" gorm:"not null"`
	NextRunAt       *int64 `json:"next_run_at" gorm:"bigint;index"`
	LastRunAt       *int64 `json:"last_run_at" gorm:"bigint"`
	CreatedAt       int64  `json:"created_at" gorm:"bigint;not null"`
	UpdatedAt       int64  `json:"updated_at" gorm:"bigint;not null"`
}

func (UpstreamAccountScheduledTestPlan) TableName() string {
	return "upstream_account_scheduled_test_plans"
}

type UpstreamAccountScheduledTestResult struct {
	Id                   int    `json:"id"`
	PlanId               int    `json:"plan_id" gorm:"not null;index"`
	AccountId            int    `json:"account_id" gorm:"not null;index"`
	Success              bool   `json:"success" gorm:"not null"`
	StatusCode           int    `json:"status_code" gorm:"not null"`
	LatencyMs            int64  `json:"latency_ms" gorm:"bigint;not null"`
	FirstOutputLatencyMs int64  `json:"first_output_latency_ms" gorm:"bigint;not null"`
	Result               string `json:"result" gorm:"type:varchar(64);not null"`
	Model                string `json:"model" gorm:"type:varchar(128);not null"`
	CreatedAt            int64  `json:"created_at" gorm:"bigint;not null;index"`
}

func (UpstreamAccountScheduledTestResult) TableName() string {
	return "upstream_account_scheduled_test_results"
}

type UpstreamOAuthSession struct {
	Id                 int    `json:"id"`
	StateHash          string `json:"-" gorm:"type:varchar(64);not null;uniqueIndex"`
	VerifierCiphertext string `json:"-" gorm:"type:text;not null"`
	VerifierNonce      string `json:"-" gorm:"type:varchar(64);not null"`
	VerifierKeyVersion int    `json:"-" gorm:"not null"`
	VerifierVersion    int64  `json:"-" gorm:"bigint;not null;default:1"`
	AccountId          *int   `json:"account_id,omitempty" gorm:"index"`
	ProxyId            *int   `json:"proxy_id,omitempty" gorm:"index"`
	CreatedAt          int64  `json:"created_at" gorm:"bigint;not null"`
	ExpiresAt          int64  `json:"expires_at" gorm:"bigint;not null;index"`
	UsedAt             int64  `json:"used_at" gorm:"bigint;not null;default:0;index"`
}

func (UpstreamOAuthSession) TableName() string { return "upstream_oauth_sessions" }

func ValidateUpstreamAccountPool(pool *UpstreamAccountPool) error {
	if pool == nil {
		return errors.New("upstream account pool is required")
	}
	pool.Name = strings.TrimSpace(pool.Name)
	pool.Platform = strings.ToLower(strings.TrimSpace(pool.Platform))
	pool.CredentialType = strings.ToLower(strings.TrimSpace(pool.CredentialType))
	pool.Status = strings.ToLower(strings.TrimSpace(pool.Status))
	if pool.Name == "" {
		return errors.New("pool name is required")
	}
	if !IsSupportedUpstreamPlatform(pool.Platform) {
		return errors.New("unsupported pool platform")
	}
	if pool.CredentialType == "" {
		pool.CredentialType = "mixed"
	}
	if pool.CredentialType != "mixed" && !IsSupportedUpstreamAccountType(pool.Platform, pool.CredentialType) {
		return errors.New("unsupported pool credential type")
	}
	if pool.Status == "" {
		pool.Status = constant.UpstreamStatusActive
	}
	if pool.Status != constant.UpstreamStatusActive && pool.Status != constant.UpstreamStatusInactive {
		return errors.New("unsupported pool status")
	}
	if strings.TrimSpace(pool.SchedulerConfig) == "" {
		pool.SchedulerConfig = "{}"
	}
	if _, err := ParseUpstreamAccountSchedulerConfig(pool.SchedulerConfig); err != nil {
		return err
	}
	return nil
}

func ValidateUpstreamAccount(account *UpstreamAccount) error {
	if account == nil {
		return errors.New("upstream account is required")
	}
	account.Name = strings.TrimSpace(account.Name)
	account.Platform = strings.ToLower(strings.TrimSpace(account.Platform))
	account.Type = strings.ToLower(strings.TrimSpace(account.Type))
	account.Status = strings.ToLower(strings.TrimSpace(account.Status))
	account.OAuthRefreshOwner = strings.ToLower(strings.TrimSpace(account.OAuthRefreshOwner))
	if account.Name == "" {
		return errors.New("account name is required")
	}
	if !IsSupportedUpstreamPlatform(account.Platform) {
		return errors.New("unsupported account platform")
	}
	if !IsSupportedUpstreamAccountType(account.Platform, account.Type) {
		return errors.New("unsupported account type")
	}
	if account.Concurrency <= 0 {
		return errors.New("account concurrency must be positive")
	}
	if account.Priority < 0 {
		return errors.New("account priority cannot be negative")
	}
	if account.Weight <= 0 {
		return errors.New("account weight must be positive")
	}
	if account.LoadFactor != nil && *account.LoadFactor <= 0 {
		return errors.New("account load factor must be positive")
	}
	if account.RateMultiplier != nil && *account.RateMultiplier < 0 {
		return errors.New("account rate multiplier cannot be negative")
	}
	if account.Status == "" {
		account.Status = constant.UpstreamStatusActive
	}
	if account.Status != constant.UpstreamStatusActive && account.Status != constant.UpstreamStatusInactive && account.Status != constant.UpstreamStatusError && account.Status != constant.UpstreamStatusExpired {
		return errors.New("unsupported account status")
	}
	if account.OAuthRefreshOwner == "" {
		account.OAuthRefreshOwner = constant.UpstreamOAuthRefreshOwnerExternal
	}
	if account.OAuthRefreshOwner != constant.UpstreamOAuthRefreshOwnerExternal && account.OAuthRefreshOwner != constant.UpstreamOAuthRefreshOwnerStarNexus {
		return errors.New("unsupported OAuth refresh owner")
	}
	if strings.TrimSpace(account.Extra) == "" {
		account.Extra = "{}"
	}
	var extra map[string]any
	if err := common.UnmarshalJsonStr(account.Extra, &extra); err != nil {
		return errors.New("account extra must be a JSON object")
	}
	return ValidateUpstreamAccountOptions(account)
}

func IsSupportedUpstreamPlatform(platform string) bool {
	switch strings.ToLower(strings.TrimSpace(platform)) {
	case constant.UpstreamPlatformOpenAI, constant.UpstreamPlatformAnthropic:
		return true
	default:
		return false
	}
}

func IsSupportedUpstreamAccountType(platform string, accountType string) bool {
	platform = strings.ToLower(strings.TrimSpace(platform))
	accountType = strings.ToLower(strings.TrimSpace(accountType))
	switch platform {
	case constant.UpstreamPlatformOpenAI:
		return accountType == constant.UpstreamAccountTypeOAuth || accountType == constant.UpstreamAccountTypeAPIKey
	case constant.UpstreamPlatformAnthropic:
		switch accountType {
		case constant.UpstreamAccountTypeOAuth,
			constant.UpstreamAccountTypeSetupToken,
			constant.UpstreamAccountTypeAPIKey,
			constant.UpstreamAccountTypeBedrock,
			constant.UpstreamAccountTypeServiceAccount:
			return true
		}
	}
	return false
}

func ValidateUpstreamProxy(proxy *UpstreamProxy) error {
	if proxy == nil {
		return errors.New("upstream proxy is required")
	}
	proxy.Name = strings.TrimSpace(proxy.Name)
	proxy.Protocol = strings.ToLower(strings.TrimSpace(proxy.Protocol))
	proxy.Host = strings.TrimSpace(proxy.Host)
	proxy.Status = strings.ToLower(strings.TrimSpace(proxy.Status))
	proxy.FallbackMode = strings.ToLower(strings.TrimSpace(proxy.FallbackMode))
	if proxy.Name == "" || proxy.Host == "" {
		return errors.New("proxy name and host are required")
	}
	if proxy.Port < 1 || proxy.Port > 65535 {
		return errors.New("proxy port is invalid")
	}
	switch proxy.Protocol {
	case constant.UpstreamProxyProtocolHTTP, constant.UpstreamProxyProtocolHTTPS, constant.UpstreamProxyProtocolSOCKS5, constant.UpstreamProxyProtocolSOCKS5H:
	default:
		return errors.New("unsupported proxy protocol")
	}
	if proxy.Status == "" {
		proxy.Status = constant.UpstreamStatusActive
	}
	if proxy.Status != constant.UpstreamStatusActive && proxy.Status != constant.UpstreamStatusInactive && proxy.Status != constant.UpstreamStatusExpired {
		return errors.New("unsupported proxy status")
	}
	if proxy.FallbackMode == "" {
		proxy.FallbackMode = constant.UpstreamProxyFallbackNone
	}
	switch proxy.FallbackMode {
	case constant.UpstreamProxyFallbackNone, constant.UpstreamProxyFallbackProxy, constant.UpstreamProxyFallbackDirect:
	default:
		return errors.New("unsupported proxy fallback mode")
	}
	if proxy.FallbackMode == constant.UpstreamProxyFallbackProxy && proxy.BackupProxyId == nil {
		return errors.New("backup proxy is required for proxy fallback")
	}
	if proxy.BackupProxyId != nil && proxy.Id > 0 && *proxy.BackupProxyId == proxy.Id {
		return fmt.Errorf("proxy %d cannot fall back to itself", proxy.Id)
	}
	return nil
}

func SetUpstreamModelCreateTimes(createdAt *int64, updatedAt *int64) {
	now := time.Now().Unix()
	if createdAt != nil && *createdAt == 0 {
		*createdAt = now
	}
	if updatedAt != nil {
		*updatedAt = now
	}
}
