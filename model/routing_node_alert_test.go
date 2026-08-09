package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestRoutingNodeAlertUnreadCursor(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	originalDB := DB
	originalRedisEnabled := common.RedisEnabled
	DB = db
	common.RedisEnabled = false
	t.Cleanup(func() {
		DB = originalDB
		common.RedisEnabled = originalRedisEnabled
	})
	require.NoError(t, db.AutoMigrate(
		&User{},
		&RoutingNodeAlertState{},
		&RoutingNodeAlertEvent{},
	))
	user := &User{Username: "alert-reader", Password: "test-password", Role: common.RoleRootUser, Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(user).Error)
	state := &RoutingNodeAlertState{
		NodeId: 1, NodeKey: "s4", NodeName: "S4", RuleKey: "cpu_usage",
		Metric: "cpu_usage_percent", Status: RoutingNodeAlertStatusFiring,
		Severity: RoutingNodeAlertSeverityCritical, WarningThreshold: 85,
		CriticalThreshold: 95, CurrentValue: 99, CreatedAt: 1, UpdatedAt: 1,
	}
	require.NoError(t, db.Create(state).Error)
	triggered := &RoutingNodeAlertEvent{
		StateId: state.Id, NodeId: state.NodeId, RuleKey: state.RuleKey,
		EventType: RoutingNodeAlertEventTriggered, Severity: state.Severity,
		Value: 99, Threshold: 95, CreatedAt: 1,
	}
	require.NoError(t, db.Create(triggered).Error)

	summary, err := GetRoutingNodeAlertUnreadSummary(user.Id)
	require.NoError(t, err)
	require.Equal(t, int64(1), summary.UnreadCount)
	require.Equal(t, triggered.Id, summary.LatestEventId)

	summary, err = MarkRoutingNodeAlertsRead(user.Id)
	require.NoError(t, err)
	require.Zero(t, summary.UnreadCount)
	require.Equal(t, triggered.Id, summary.LastReadEventId)

	escalated := &RoutingNodeAlertEvent{
		StateId: state.Id, NodeId: state.NodeId, RuleKey: state.RuleKey,
		EventType: RoutingNodeAlertEventEscalated, Severity: state.Severity,
		Value: 100, Threshold: 95, CreatedAt: 2,
	}
	require.NoError(t, db.Create(escalated).Error)
	summary, err = GetRoutingNodeAlertUnreadSummary(user.Id)
	require.NoError(t, err)
	require.Equal(t, int64(1), summary.UnreadCount)

	require.NoError(t, db.Model(state).Update("status", RoutingNodeAlertStatusResolved).Error)
	summary, err = GetRoutingNodeAlertUnreadSummary(user.Id)
	require.NoError(t, err)
	require.Zero(t, summary.UnreadCount)
}
