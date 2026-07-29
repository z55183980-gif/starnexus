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

	_, err = ParseUpstreamAccountSchedulerConfig(`{"version":2}`)
	require.ErrorContains(t, err, "unsupported scheduler_config version")
	_, err = ParseUpstreamAccountSchedulerConfig(`{"top_k":101}`)
	require.ErrorContains(t, err, "top_k")
}
