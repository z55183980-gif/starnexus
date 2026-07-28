package main

import (
	"encoding/base64"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestImportSnapshotIsIdempotentAndRollbackSafe(t *testing.T) {
	t.Setenv("UPSTREAM_ACCOUNT_CREDENTIAL_KEYS", `{"1":"`+base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))+`"}`)
	t.Setenv("UPSTREAM_ACCOUNT_ACTIVE_KEY_VERSION", "1")

	source, err := gorm.Open(sqlite.Open(t.TempDir()+"/source.db"), &gorm.Config{})
	require.NoError(t, err)
	sourceSQL, err := source.DB()
	require.NoError(t, err)
	defer sourceSQL.Close()
	require.NoError(t, source.AutoMigrate(&sourceAccount{}, &sourceProxy{}, &sourceGroup{}, &sourceAccountGroup{}))
	require.NoError(t, source.Create(&sourceProxy{ID: 11, Name: "proxy", Protocol: "http", Host: "127.0.0.1", Port: 8080, Status: "active", FallbackMode: "none", ExpiryWarnDays: 7}).Error)
	require.NoError(t, source.Create(&sourceProxy{ID: 12, Name: "unused", Protocol: "http", Host: "127.0.0.2", Port: 8081, Status: "active", FallbackMode: "none", ExpiryWarnDays: 7}).Error)
	require.NoError(t, source.Create(&sourceGroup{ID: 21, Name: "openai", Platform: "openai", Status: "active"}).Error)
	require.NoError(t, source.Create(&sourceGroup{ID: 22, Name: "unused-openai", Platform: "openai", Status: "active"}).Error)
	require.NoError(t, source.Create(&sourceAccount{
		ID: 31, Name: "api account", Platform: "openai", Type: "api_key",
		Credentials: []byte(`{"api_key":"sk-test"}`), Extra: []byte(`{"models":["gpt-5.6-sol"]}`),
		ProxyID: int64Pointer(11), Concurrency: 2, Priority: 40, RateMultiplier: 1,
		Status: "active", Schedulable: true, AutoPauseOnExpired: true,
	}).Error)
	require.NoError(t, source.Create(&sourceAccountGroup{AccountID: 31, GroupID: 21, Priority: 40}).Error)

	destination, err := gorm.Open(sqlite.Open(t.TempDir()+"/destination.db"), &gorm.Config{})
	require.NoError(t, err)
	destinationSQL, err := destination.DB()
	require.NoError(t, err)
	defer destinationSQL.Close()
	require.NoError(t, destination.AutoMigrate(
		&model.Channel{}, &model.UpstreamProxy{}, &model.UpstreamAccountPool{},
		&model.UpstreamAccount{}, &model.UpstreamAccountPoolMember{},
		&model.UpstreamAccountEvent{}, &model.UpstreamOAuthSession{},
	))
	model.DB = destination

	snapshot, err := loadSourceSnapshot(source)
	require.NoError(t, err)
	require.Len(t, snapshot.Accounts, 1)
	require.Len(t, snapshot.Groups, 1)
	require.Len(t, snapshot.Proxies, 1)
	require.EqualValues(t, 11, snapshot.Proxies[0].ID)
	require.Len(t, snapshot.Memberships, 1)

	first, err := importSnapshot(destination, snapshot, "test-run")
	require.NoError(t, err)
	require.Equal(t, 1, first.CreatedAccounts)
	require.Equal(t, 1, first.CreatedPools)
	require.Equal(t, 1, first.CreatedProxies)

	second, err := importSnapshot(destination, snapshot, "test-run")
	require.NoError(t, err)
	require.Equal(t, 0, second.CreatedAccounts)
	require.Equal(t, 1, second.UpdatedAccounts)

	var importedPool model.UpstreamAccountPool
	var importedMember model.UpstreamAccountPoolMember
	require.NoError(t, destination.Where("source_system = ?", sourceSystemSub2API).First(&importedPool).Error)
	require.NoError(t, destination.Where("pool_id = ?", importedPool.Id).First(&importedMember).Error)
	require.NoError(t, destination.Model(&importedMember).Updates(map[string]any{"priority": 99, "weight": 9}).Error)
	localAccountInput := service.UpstreamAccountCreateInput{
		Account: model.UpstreamAccount{
			Name: "local account", Platform: constant.UpstreamPlatformOpenAI, Type: constant.UpstreamAccountTypeAPIKey,
			Extra: "{}", Concurrency: 1, Priority: 50, Weight: 1, Status: constant.UpstreamStatusActive,
			Schedulable: true, AutoPauseOnExpired: true,
		},
		Credentials: map[string]any{"api_key": "sk-local"},
	}
	require.NoError(t, service.CreateUpstreamAccount(&localAccountInput))
	require.NoError(t, destination.Create(&model.UpstreamAccountPoolMember{
		PoolId: importedPool.Id, AccountId: localAccountInput.Account.Id, Priority: 7, Weight: 3, CreatedAt: 1,
	}).Error)
	third, err := importSnapshot(destination, snapshot, "test-run")
	require.NoError(t, err)
	require.Equal(t, 1, third.UpdatedAccounts)
	require.NoError(t, destination.Where("pool_id = ? AND account_id = ?", importedPool.Id, importedMember.AccountId).First(&importedMember).Error)
	require.Equal(t, 40, importedMember.Priority)
	require.Equal(t, 1, importedMember.Weight)
	var localMember model.UpstreamAccountPoolMember
	require.NoError(t, destination.Where("pool_id = ? AND account_id = ?", importedPool.Id, localAccountInput.Account.Id).First(&localMember).Error)
	require.Equal(t, 7, localMember.Priority)
	require.Equal(t, 3, localMember.Weight)

	var accountCount, poolCount, proxyCount, memberCount int64
	require.NoError(t, destination.Model(&model.UpstreamAccount{}).Count(&accountCount).Error)
	require.NoError(t, destination.Model(&model.UpstreamAccountPool{}).Count(&poolCount).Error)
	require.NoError(t, destination.Model(&model.UpstreamProxy{}).Count(&proxyCount).Error)
	require.NoError(t, destination.Model(&model.UpstreamAccountPoolMember{}).Count(&memberCount).Error)
	require.EqualValues(t, 2, accountCount)
	require.EqualValues(t, 1, poolCount)
	require.EqualValues(t, 1, proxyCount)
	require.EqualValues(t, 2, memberCount)

	var account model.UpstreamAccount
	require.NoError(t, destination.Where("source_system = ?", sourceSystemSub2API).First(&account).Error)
	require.EqualValues(t, 1, account.CredentialVersion)

	verified, err := verifySnapshot(destination, snapshot, "test-run")
	require.NoError(t, err)
	require.Equal(t, 1, verified.VerifiedAccounts)
	require.Equal(t, 1, verified.VerifiedPools)
	require.Equal(t, 1, verified.VerifiedProxies)
	_, err = rollback(destination, "test-run")
	require.ErrorContains(t, err, "external account membership")
	require.NoError(t, destination.Where("pool_id = ? AND account_id = ?", importedPool.Id, localAccountInput.Account.Id).
		Delete(&model.UpstreamAccountPoolMember{}).Error)
	require.NoError(t, service.DeleteUpstreamAccount(localAccountInput.Account.Id))

	var importedAccount model.UpstreamAccount
	var importedProxy model.UpstreamProxy
	require.NoError(t, destination.Where("source_system = ?", sourceSystemSub2API).First(&importedAccount).Error)
	require.NoError(t, destination.Where("source_system = ?", sourceSystemSub2API).First(&importedProxy).Error)
	externalPool := model.UpstreamAccountPool{
		Name: "local pool", Platform: "openai", CredentialType: "mixed", Status: "active",
		SchedulerConfig: "{}", CreatedAt: 1, UpdatedAt: 1,
	}
	require.NoError(t, destination.Create(&externalPool).Error)
	require.NoError(t, destination.Create(&model.UpstreamAccountPoolMember{
		PoolId: externalPool.Id, AccountId: importedAccount.Id, Weight: 1, CreatedAt: 1,
	}).Error)
	_, err = rollback(destination, "test-run")
	require.ErrorContains(t, err, "external pool membership")
	require.NoError(t, destination.Where("pool_id = ?", externalPool.Id).Delete(&model.UpstreamAccountPoolMember{}).Error)
	require.NoError(t, destination.Delete(&externalPool).Error)

	externalPool = model.UpstreamAccountPool{
		Name: "local proxy pool", Platform: "openai", CredentialType: "mixed", Status: "active",
		DefaultProxyId: &importedProxy.Id, SchedulerConfig: "{}", CreatedAt: 1, UpdatedAt: 1,
	}
	require.NoError(t, destination.Create(&externalPool).Error)
	_, err = rollback(destination, "test-run")
	require.ErrorContains(t, err, "external account pool")
	require.NoError(t, destination.Delete(&externalPool).Error)

	rolledBack, err := rollback(destination, "test-run")
	require.NoError(t, err)
	require.EqualValues(t, 1, rolledBack.RemovedAccounts)
	require.EqualValues(t, 1, rolledBack.RemovedPools)
	require.EqualValues(t, 1, rolledBack.RemovedProxies)
}

func TestUniqueImportedPoolNameHandlesRepeatedCollisions(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/names.db"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&model.UpstreamAccountPool{}))
	for _, name := range []string{"group", "group [sub2api:9]"} {
		require.NoError(t, db.Create(&model.UpstreamAccountPool{
			Name: name, Platform: "openai", CredentialType: "mixed", Status: "active",
			SchedulerConfig: "{}", CreatedAt: 1, UpdatedAt: 1,
		}).Error)
	}
	require.Equal(t, "group [sub2api:9-2]", uniqueImportedPoolName(db, "group", 9, 0))
	require.Equal(t, "sub2api-group-10", uniqueImportedPoolName(db, " ", 10, 0))
}

func TestPoolCredentialType(t *testing.T) {
	snapshot := &sourceSnapshot{
		Memberships:  []sourceAccountGroup{{AccountID: 1, GroupID: 9}, {AccountID: 2, GroupID: 9}},
		AccountTypes: map[int64]string{1: "oauth", 2: "apikey"},
	}
	require.Equal(t, "mixed", poolCredentialType(9, snapshot))
	snapshot.Memberships = snapshot.Memberships[:1]
	require.Equal(t, "oauth", poolCredentialType(9, snapshot))
}

func TestValidateSourceProxyRejectsSilentProtocolFallbacks(t *testing.T) {
	require.ErrorContains(t, validateSourceProxy(sourceProxy{Protocol: "unknown", FallbackMode: "none"}), "unsupported proxy protocol")
	require.ErrorContains(t, validateSourceProxy(sourceProxy{Protocol: "http", FallbackMode: "proxy"}), "backup_proxy_id")
	require.NoError(t, validateSourceProxy(sourceProxy{Protocol: "socks5h", FallbackMode: "direct"}))
}

func TestNormalizeSourceAccountCredentialsMapsLegacyChatGPTAccountID(t *testing.T) {
	legacy := map[string]any{"access_token": "token", "chatgpt_account_id": "acct-legacy"}
	normalized := normalizeSourceAccountCredentials(constant.UpstreamAccountTypeOAuth, legacy)
	require.Equal(t, "acct-legacy", normalized["account_id"])
	require.NotContains(t, legacy, "account_id")

	current := map[string]any{"account_id": "acct-current", "chatgpt_account_id": "acct-legacy"}
	normalized = normalizeSourceAccountCredentials(constant.UpstreamAccountTypeOAuth, current)
	require.Equal(t, "acct-current", normalized["account_id"])
}
