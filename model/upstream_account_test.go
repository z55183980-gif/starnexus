package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestUpstreamAccountModelsMigrateOnSQLite(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&UpstreamProxy{},
		&UpstreamAccountPool{},
		&UpstreamAccount{},
		&UpstreamAccountPoolMember{},
		&UpstreamAccountEvent{},
		&UpstreamOAuthSession{},
		&Channel{},
	))
	for _, table := range []string{
		"upstream_proxies",
		"upstream_account_pools",
		"upstream_accounts",
		"upstream_account_pool_members",
		"upstream_account_events",
		"upstream_oauth_sessions",
	} {
		require.True(t, db.Migrator().HasTable(table), table)
	}
	require.True(t, db.Migrator().HasColumn(&Channel{}, "credential_source"))
	require.True(t, db.Migrator().HasColumn(&Channel{}, "upstream_account_pool_id"))
	require.True(t, db.Migrator().HasColumn(&UpstreamProxy{}, "username"))
	require.True(t, db.Migrator().HasColumn(&UpstreamProxy{}, "password"))
}

func TestUpstreamAccountSchedulableGate(t *testing.T) {
	now := time.Now().Unix()
	account := &UpstreamAccount{Status: constant.UpstreamStatusActive, Schedulable: true}
	require.True(t, account.IsSchedulableAt(now))

	future := now + 60
	account.RateLimitResetAt = &future
	require.False(t, account.IsSchedulableAt(now))
	account.RateLimitResetAt = nil
	account.ExpiresAt = &now
	account.AutoPauseOnExpired = true
	require.False(t, account.IsSchedulableAt(now))
}

func TestValidateAnthropicUpstreamAccountTypes(t *testing.T) {
	for _, accountType := range []string{
		constant.UpstreamAccountTypeOAuth,
		constant.UpstreamAccountTypeSetupToken,
		constant.UpstreamAccountTypeAPIKey,
		constant.UpstreamAccountTypeBedrock,
		constant.UpstreamAccountTypeServiceAccount,
	} {
		account := &UpstreamAccount{
			Name: "anthropic", Platform: constant.UpstreamPlatformAnthropic, Type: accountType,
			Extra: "{}", Concurrency: 1, Priority: 50, Weight: 1,
			Status: constant.UpstreamStatusActive, Schedulable: true,
		}
		require.NoError(t, ValidateUpstreamAccount(account), accountType)
	}

	account := &UpstreamAccount{
		Name: "invalid", Platform: constant.UpstreamPlatformOpenAI, Type: constant.UpstreamAccountTypeBedrock,
		Extra: "{}", Concurrency: 1, Priority: 50, Weight: 1, Status: constant.UpstreamStatusActive,
	}
	require.Error(t, ValidateUpstreamAccount(account))
}

func TestValidateUpstreamProxy(t *testing.T) {
	backupId := 2
	proxy := &UpstreamProxy{
		Name:          "test",
		Protocol:      constant.UpstreamProxyProtocolSOCKS5H,
		Host:          "127.0.0.1",
		Port:          1080,
		Status:        constant.UpstreamStatusActive,
		FallbackMode:  constant.UpstreamProxyFallbackProxy,
		BackupProxyId: &backupId,
	}
	require.NoError(t, ValidateUpstreamProxy(proxy))
	url, err := proxy.URL("user", "pass")
	require.NoError(t, err)
	require.Equal(t, "socks5h://user:pass@127.0.0.1:1080", url)
}

func TestParseUpstreamAccountSchedulerConfig(t *testing.T) {
	config, err := ParseUpstreamAccountSchedulerConfig(`{"version":1,"top_k":3,"model_routing_enabled":true,"model_routing":{"gpt-5.*":[7]},"model_routing_id_space":"destination","future_option":true}`)
	require.NoError(t, err)
	require.Equal(t, 1, config.Version)
	require.Equal(t, 3, config.TopK)
	require.Equal(t, []int64{7}, config.RoutingAccountIds("gpt-5.6-sol"))
	require.Nil(t, config.RoutingAccountIds("gpt-4.1"))

	config, err = ParseUpstreamAccountSchedulerConfig(`{"version":2,"top_k":7}`)
	require.NoError(t, err)
	require.Equal(t, "load_score", config.Strategy)
	require.Equal(t, "account", config.PrioritySource)
	config, err = ParseUpstreamAccountSchedulerConfig(`{"version":1,"strategy":"load_score","priority_source":"account"}`)
	require.NoError(t, err)
	require.Equal(t, "load_score", config.Strategy)
	require.Equal(t, "account", config.PrioritySource)
	config, err = ParseUpstreamAccountSchedulerConfig(`{"version":2,"priority_source":"member"}`)
	require.NoError(t, err)
	require.Equal(t, "member", config.PrioritySource)
	config, err = ParseUpstreamAccountSchedulerConfig(`{"version":2,"session_affinity_enabled":true}`)
	require.NoError(t, err)
	require.True(t, config.SessionAffinityEnabled)
	require.Equal(t, 3600, config.SessionAffinityTTLSeconds)
	require.Equal(t, 1500, config.SessionAffinityWaitMs)
	require.Equal(t, 3, config.SessionAffinityMaxWaiters)
	config, err = ParseUpstreamAccountSchedulerConfig(`{"version":2,"session_affinity_enabled":true,"session_affinity_ttl_seconds":3600,"session_affinity_wait_ms":0,"session_affinity_max_waiters":0}`)
	require.NoError(t, err)
	require.Zero(t, config.SessionAffinityWaitMs)
	require.Zero(t, config.SessionAffinityMaxWaiters)
	_, err = ParseUpstreamAccountSchedulerConfig(`{"session_affinity_enabled":true,"session_affinity_ttl_seconds":30}`)
	require.ErrorContains(t, err, "session_affinity_ttl_seconds")
	_, err = ParseUpstreamAccountSchedulerConfig(`{"session_affinity_wait_ms":10001}`)
	require.ErrorContains(t, err, "session_affinity_wait_ms")
	_, err = ParseUpstreamAccountSchedulerConfig(`{"version":2,"priority_source":"invalid"}`)
	require.ErrorContains(t, err, "priority_source")
	_, err = ParseUpstreamAccountSchedulerConfig(`{"version":1,"strategy":"invalid"}`)
	require.ErrorContains(t, err, "strategy")
	_, err = ParseUpstreamAccountSchedulerConfig(`{"version":3}`)
	require.ErrorContains(t, err, "unsupported scheduler_config version")
	_, err = ParseUpstreamAccountSchedulerConfig(`{"top_k":101}`)
	require.ErrorContains(t, err, "top_k")

	config, err = ParseUpstreamAccountSchedulerConfig(`{"version":2,"session_affinity_enabled":true,"session_affinity_fallback_mode":"observe","session_affinity_fallback_models":["gpt-5.6-luna"],"session_affinity_fallback_min_body_bytes":300000}`)
	require.NoError(t, err)
	require.Equal(t, "observe", config.SessionAffinityFallbackMode)
	require.Equal(t, 300000, config.SessionAffinityFallbackMinBodyBytes)
	require.True(t, config.MatchesSessionAffinityFallbackModel("gpt-5.6-luna"))
	require.False(t, config.MatchesSessionAffinityFallbackModel("gpt-5.6-sol"))

	config, err = ParseUpstreamAccountSchedulerConfig(`{"version":2,"session_affinity_fallback_mode":"prefix","session_affinity_fallback_models":["gpt-5.6-*"],"session_affinity_fallback_sample_percent":10}`)
	require.NoError(t, err)
	require.True(t, config.MatchesSessionAffinityFallbackModel("gpt-5.6-luna"))
	_, err = ParseUpstreamAccountSchedulerConfig(`{"session_affinity_fallback_mode":"prefix","session_affinity_fallback_models":["gpt-5.6-luna"]}`)
	require.ErrorContains(t, err, "sample_percent")
	_, err = ParseUpstreamAccountSchedulerConfig(`{"session_affinity_fallback_mode":"observe"}`)
	require.ErrorContains(t, err, "fallback_models")
}
