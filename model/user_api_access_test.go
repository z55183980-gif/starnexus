package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestSuspendAndRestoreUserAPIForCyberPolicy(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&User{}, &PromptAuditPolicy{}, &UserAPIAccessEvent{}))
	setPromptAuditTestDatabases(t, db, db)

	user := &User{Username: "cyber-policy-user", Password: "password", APIStatus: common.UserAPIStatusEnabled}
	require.NoError(t, db.Create(user).Error)
	require.NoError(t, db.Create(&PromptAuditPolicy{
		UserId: user.Id, MonitorEnabled: false, DelaySeconds: PromptAuditDefaultDelaySeconds,
		CreatedBy: 9, CreatedAt: 1, UpdatedAt: 1,
	}).Error)

	changed, err := SuspendUserAPIForCyberPolicy(SuspendUserAPIInput{
		UserId: user.Id, RequestId: "req-cyber", TokenId: 7, ModelName: "gpt-test", ChannelId: 8,
		UpstreamAccountId: 9, NodeName: "node-a", Evidence: `{"error_code":"cyber_policy"}`,
	})
	require.NoError(t, err)
	require.True(t, changed)

	var stored User
	require.NoError(t, db.First(&stored, user.Id).Error)
	require.Equal(t, common.UserAPIStatusSuspended, stored.APIStatus)
	require.Zero(t, stored.APISuspendedUntil)
	require.Equal(t, UserAPIAccessSourceCyber, stored.APISuspendedReason)
	require.NotZero(t, stored.APISuspendedAt)

	var policy PromptAuditPolicy
	require.NoError(t, db.Where("user_id = ?", user.Id).First(&policy).Error)
	require.True(t, policy.MonitorEnabled)
	require.Equal(t, 9, policy.CreatedBy)

	changed, err = SuspendUserAPIForCyberPolicy(SuspendUserAPIInput{UserId: user.Id})
	require.NoError(t, err)
	require.False(t, changed)
	var suspendEvents int64
	require.NoError(t, db.Model(&UserAPIAccessEvent{}).
		Where("user_id = ? AND action = ?", user.Id, UserAPIAccessActionSuspend).
		Count(&suspendEvents).Error)
	require.Equal(t, int64(1), suspendEvents)

	views, total, err := ListSuspendedUsers("cyber-policy", 0, 20)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, views, 1)
	require.Equal(t, "req-cyber", views[0].RequestId)
	require.Equal(t, `{"error_code":"cyber_policy"}`, views[0].Evidence)
	require.True(t, views[0].PromptMonitoringAdded)

	restored, err := RestoreUserAPIAccess(user.Id, 100, "reviewed by administrator")
	require.NoError(t, err)
	require.True(t, restored)
	require.NoError(t, db.First(&stored, user.Id).Error)
	require.Equal(t, common.UserAPIStatusEnabled, stored.APIStatus)
	require.Zero(t, stored.APISuspendedAt)
	require.NoError(t, db.Where("user_id = ?", user.Id).First(&policy).Error)
	require.True(t, policy.MonitorEnabled)
}

func TestTemporaryUserAPISuspensionExpiresLazily(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&User{}, &PromptAuditPolicy{}, &UserAPIAccessEvent{}))
	setPromptAuditTestDatabases(t, db, db)

	user := &User{Username: "temporary-cyber-user", Password: "password", APIStatus: common.UserAPIStatusEnabled}
	require.NoError(t, db.Create(user).Error)
	changed, err := SuspendUserAPIForCyberPolicy(SuspendUserAPIInput{
		UserId: user.Id, ChannelId: 8, DurationSeconds: 3600,
	})
	require.NoError(t, err)
	require.True(t, changed)

	var stored User
	require.NoError(t, db.First(&stored, user.Id).Error)
	require.Greater(t, stored.APISuspendedUntil, stored.APISuspendedAt)

	blocked, err := ResolveUserAPIAccessSuspension(user.Id, stored.APIStatus, stored.APISuspendedUntil)
	require.NoError(t, err)
	require.True(t, blocked)

	require.NoError(t, db.Model(&User{}).Where("id = ?", user.Id).
		Update("api_suspended_until", common.GetTimestamp()-1).Error)
	blocked, err = ResolveUserAPIAccessSuspension(user.Id, stored.APIStatus, common.GetTimestamp()-1)
	require.NoError(t, err)
	require.False(t, blocked)
	require.NoError(t, db.First(&stored, user.Id).Error)
	require.Equal(t, common.UserAPIStatusEnabled, stored.APIStatus)
	var restoreEvent UserAPIAccessEvent
	require.NoError(t, db.Where("user_id = ? AND action = ?", user.Id, UserAPIAccessActionRestore).
		First(&restoreEvent).Error)
	require.Equal(t, "system", restoreEvent.Source)

	require.NoError(t, db.Model(&User{}).Where("id = ?", user.Id).Updates(map[string]any{
		"api_status": common.UserAPIStatusSuspended, "api_suspended_until": common.GetTimestamp() - 1,
	}).Error)
	changed, err = SuspendUserAPIForCyberPolicy(SuspendUserAPIInput{
		UserId: user.Id, ChannelId: 8, DurationSeconds: 7200,
	})
	require.NoError(t, err)
	require.True(t, changed)
	require.NoError(t, db.First(&stored, user.Id).Error)
	require.GreaterOrEqual(t, stored.APISuspendedUntil, common.GetTimestamp()+7199)
}
