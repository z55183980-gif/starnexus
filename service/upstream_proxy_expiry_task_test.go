package service

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestSweepExpiredUpstreamProxiesMarksExpiredAndMovesAccounts(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	backup := createRouterTestProxy(t, "backup-proxy", 1084)
	origin := createRouterTestProxy(t, "expired-proxy", 1085)
	expiredAt := time.Now().Add(-time.Hour).Unix()
	origin.ExpiresAt = &expiredAt
	origin.FallbackMode = constant.UpstreamProxyFallbackProxy
	origin.BackupProxyId = &backup.Id
	require.NoError(t, UpdateUpstreamProxy(&UpstreamProxyUpdateInput{Proxy: origin}))

	now := common.GetTimestamp()
	account := model.UpstreamAccount{
		Name: "proxy-expiry-account", Platform: constant.UpstreamPlatformOpenAI,
		Type: constant.UpstreamAccountTypeOAuth, Extra: "{}", ProxyId: &origin.Id,
		Concurrency: 1, Priority: 1, Weight: 1, Status: constant.UpstreamStatusActive,
		Schedulable: true, OAuthRefreshOwner: constant.UpstreamOAuthRefreshOwnerExternal,
		CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, model.DB.Create(&account).Error)

	changed, err := SweepExpiredUpstreamProxies(now)
	require.NoError(t, err)
	require.EqualValues(t, 1, changed)

	var storedProxy model.UpstreamProxy
	require.NoError(t, model.DB.First(&storedProxy, origin.Id).Error)
	require.Equal(t, constant.UpstreamStatusExpired, storedProxy.Status)

	var storedAccount model.UpstreamAccount
	require.NoError(t, model.DB.First(&storedAccount, account.Id).Error)
	require.Equal(t, backup.Id, *storedAccount.ProxyId)
	require.Equal(t, origin.Id, *storedAccount.ProxyFallbackOriginId)

	changed, err = SweepExpiredUpstreamProxies(now)
	require.NoError(t, err)
	require.Zero(t, changed)
}
