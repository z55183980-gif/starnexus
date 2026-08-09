package service

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupRoutingNodeAlertTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	originalDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = originalDB })
	require.NoError(t, db.AutoMigrate(
		&model.RoutingNode{},
		&model.RoutingNodeMonitorStatus{},
		&model.RoutingNodeAlertRule{},
		&model.RoutingNodeAlertState{},
		&model.RoutingNodeAlertEvent{},
	))
	return db
}

func TestRoutingNodeAlertLifecycle(t *testing.T) {
	setupRoutingNodeAlertTestDB(t)
	node := &model.RoutingNode{Key: "s4", Name: "S4", Origin: "origin-s4.example.com", Enabled: true, MonitorEnabled: true}
	require.NoError(t, model.CreateRoutingNode(node))
	rule := &model.RoutingNodeAlertRule{
		Key: "cpu_usage", Name: "CPU usage high", Metric: "cpu_usage_percent",
		WarningThreshold: 85, CriticalThreshold: 95, RecoveryThreshold: 75,
		TriggerCount: 2, RecoveryCount: 2, Enabled: true,
	}
	now := time.Now().Unix()

	state, event, err := evaluateRoutingNodeAlertValue(node, rule, 90, now)
	require.NoError(t, err)
	require.Nil(t, event)
	require.Equal(t, model.RoutingNodeAlertStatusNormal, state.Status)

	state, event, err = evaluateRoutingNodeAlertValue(node, rule, 91, now+15)
	require.NoError(t, err)
	require.Equal(t, model.RoutingNodeAlertEventTriggered, event.EventType)
	require.Equal(t, model.RoutingNodeAlertStatusFiring, state.Status)
	require.Equal(t, model.RoutingNodeAlertSeverityWarning, state.Severity)

	state, err = model.MutateRoutingNodeAlert(state.Id, 7, model.RoutingNodeAlertEventAcknowledged, 0)
	require.NoError(t, err)
	require.Equal(t, model.RoutingNodeAlertStatusAcknowledged, state.Status)

	state, event, err = evaluateRoutingNodeAlertValue(node, rule, 98, now+30)
	require.NoError(t, err)
	require.Equal(t, model.RoutingNodeAlertEventEscalated, event.EventType)
	require.Equal(t, model.RoutingNodeAlertStatusFiring, state.Status)
	require.Equal(t, model.RoutingNodeAlertSeverityCritical, state.Severity)

	state, event, err = evaluateRoutingNodeAlertValue(node, rule, 70, now+45)
	require.NoError(t, err)
	require.Nil(t, event)
	require.Equal(t, model.RoutingNodeAlertStatusFiring, state.Status)

	state, event, err = evaluateRoutingNodeAlertValue(node, rule, 70, now+60)
	require.NoError(t, err)
	require.Equal(t, model.RoutingNodeAlertEventRecovered, event.EventType)
	require.Equal(t, model.RoutingNodeAlertStatusResolved, state.Status)

	events, err := model.ListRoutingNodeAlertEvents(state.Id, 20)
	require.NoError(t, err)
	require.Len(t, events, 4)
}

func TestRoutingNodeHeartbeatAlert(t *testing.T) {
	db := setupRoutingNodeAlertTestDB(t)
	now := time.Now().Unix()
	node := &model.RoutingNode{
		Key: "s1", Name: "S1", Origin: "origin-s1.example.com", Enabled: true,
		MonitorEnabled: true, MonitorTokenHash: "configured", MonitorTokenUpdatedAt: now - 120,
	}
	require.NoError(t, model.CreateRoutingNode(node))
	rule := &model.RoutingNodeAlertRule{
		Key: "heartbeat_age", Name: "Monitor heartbeat overdue", Metric: "heartbeat_age_seconds",
		WarningThreshold: 45, CriticalThreshold: 90, RecoveryThreshold: 30,
		TriggerCount: 1, RecoveryCount: 1, Enabled: true,
	}
	require.NoError(t, db.Create(rule).Error)
	require.NoError(t, db.Create(&model.RoutingNodeAlertState{
		NodeId: node.Id, NodeKey: node.Key, NodeName: node.Name, RuleKey: rule.Key,
		Metric: rule.Metric, Status: model.RoutingNodeAlertStatusNormal,
		WarningThreshold: rule.WarningThreshold, CriticalThreshold: rule.CriticalThreshold,
		SilencedUntil: now + 3600, CreatedAt: now - 120, UpdatedAt: now - 120,
	}).Error)

	require.NoError(t, EvaluateRoutingNodeHeartbeatAlerts())
	alerts, err := model.ListRoutingNodeAlerts("", "", node.Id, 10)
	require.NoError(t, err)
	require.Len(t, alerts, 1)
	require.Equal(t, model.RoutingNodeAlertSeverityCritical, alerts[0].Severity)
	require.GreaterOrEqual(t, alerts[0].CurrentValue, 120.0)
}
