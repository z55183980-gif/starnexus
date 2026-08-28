package service

import (
	"errors"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestRoutingNodeMonitorTokenAndReport(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	originalDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = originalDB })
	require.NoError(t, db.AutoMigrate(
		&model.RoutingNode{},
		&model.RoutingNodeMonitorStatus{},
		&model.RoutingNodeMonitorNetworkSample{},
		&model.RoutingNodeMonitorEnrollment{},
		&model.UserNodeBinding{},
	))

	node := &model.RoutingNode{
		Key: "s5", Name: "S5", Origin: "origin-s5.dkby.com", Enabled: true, MonitorEnabled: true,
	}
	require.NoError(t, model.CreateRoutingNode(node))
	token, err := RotateRoutingNodeMonitorToken(node.Id)
	require.NoError(t, err)
	require.NotEmpty(t, token)

	report := &RoutingNodeMonitorReport{
		NodeName:      "s5-host",
		CPUUsage:      31,
		CPUCores:      4,
		LoadOne:       0.68,
		LoadPercent:   17,
		MemoryUsed:    34,
		MemoryTotal:   78,
		MemoryPercent: 43.6,
		DiskUsed:      30,
		DiskTotal:     77,
		DiskPercent:   39,
		UptimeSeconds: 3600,
		AppVersion:    "v1.2.3",
	}
	require.ErrorIs(t, SubmitRoutingNodeMonitorReport("wrong-token", report), ErrRoutingNodeMonitorUnauthorized)
	require.NoError(t, SubmitRoutingNodeMonitorReport(token, report))

	nodes, err := model.ListRoutingNodes(true)
	require.NoError(t, err)
	require.Len(t, nodes, 1)
	require.True(t, nodes[0].MonitorConfigured)
	require.NotNil(t, nodes[0].MonitorStatus)
	require.Equal(t, "s5-host", nodes[0].MonitorStatus.NodeName)
	require.Equal(t, 31.0, nodes[0].MonitorStatus.CPUUsage)
}

func TestRoutingNodeMonitorSharedEnrollment(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	originalDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = originalDB })
	require.NoError(t, db.AutoMigrate(
		&model.RoutingNode{},
		&model.RoutingNodeMonitorEnrollment{},
	))

	node := &model.RoutingNode{
		Key: "s1", Name: "S1", Origin: "origin-s1.dkby.com", Enabled: true, MonitorEnabled: true,
	}
	require.NoError(t, model.CreateRoutingNode(node))

	enrollmentToken, err := RotateRoutingNodeMonitorEnrollmentToken()
	require.NoError(t, err)
	require.NotEmpty(t, enrollmentToken)
	storedEnrollmentToken, err := GetRoutingNodeMonitorEnrollmentToken()
	require.NoError(t, err)
	require.Equal(t, enrollmentToken, storedEnrollmentToken)
	require.ErrorIs(t, func() error {
		_, enrollErr := EnrollRoutingNodeMonitor("wrong-token", node.Key)
		return enrollErr
	}(), ErrRoutingNodeMonitorUnauthorized)

	enrollment, err := EnrollRoutingNodeMonitor(enrollmentToken, node.Key)
	require.NoError(t, err)
	require.Equal(t, node.Key, enrollment.NodeKey)
	require.NotEmpty(t, enrollment.Token)

	replacementEnrollmentToken, err := RotateRoutingNodeMonitorEnrollmentToken()
	require.NoError(t, err)
	require.NotEqual(t, enrollmentToken, replacementEnrollmentToken)
	require.ErrorIs(t, func() error {
		_, enrollErr := EnrollRoutingNodeMonitor(enrollmentToken, node.Key)
		return enrollErr
	}(), ErrRoutingNodeMonitorUnauthorized)
}

func TestRoutingNodeMonitorReportValidation(t *testing.T) {
	err := validateRoutingNodeMonitorReport(&RoutingNodeMonitorReport{CPUCores: 0})
	require.True(t, errors.Is(err, ErrInvalidRoutingNodeMonitorReport))
	require.Error(t, validateRoutingNodeMonitorReportURL("http://example.com/report"))
	require.NoError(t, validateRoutingNodeMonitorReportURL("https://example.com/report"))
	require.NoError(t, validateRoutingNodeMonitorReportURL("http://127.0.0.1:3000/report"))
}

func TestResolveRoutingNodeMonitorNodeKey(t *testing.T) {
	require.Equal(t, "s2", resolveRoutingNodeMonitorNodeKey("", "xingyuapi-prod-2"))
	require.Equal(t, "s12", resolveRoutingNodeMonitorNodeKey("", "XINGYUAPI-PROD-12"))
	require.Equal(t, "s3", resolveRoutingNodeMonitorNodeKey("s3", "xingyuapi-prod-2"))
	require.Equal(t, "custom-node", resolveRoutingNodeMonitorNodeKey("", "custom-node"))
}

func TestLoadRoutingNodeMonitorReporterConfig(t *testing.T) {
	t.Setenv("NODE_MONITOR_REPORT_URL", "https://origin-s2.dkby.com/api/node-monitor/report")
	t.Setenv("NODE_MONITOR_ENROLLMENT_TOKEN", "shared-token")
	t.Setenv("NODE_MONITOR_NODE_KEY", "spg")
	t.Setenv("NODE_MONITOR_INTERVAL_SECONDS", "2")

	config, interval, err := loadRoutingNodeMonitorReporterConfig()
	require.NoError(t, err)
	require.Equal(t, "spg", config.NodeKey)
	require.Equal(t, "shared-token", config.EnrollmentToken)
	require.Equal(t, 5*time.Second, interval)
}

func TestIsRoutingNodeVirtualNetworkInterface(t *testing.T) {
	for _, name := range []string{
		"lo", "br-10c4af1963eb", "docker0", "veth9085e70", "virbr0",
		"cni0", "flannel.1", "cali123", "tun0", "tap0", "wg0", "tailscale0",
	} {
		require.True(t, isRoutingNodeVirtualNetworkInterface(name), name)
	}

	for _, name := range []string{"eth0", "ens3", "enp1s0", "bond0"} {
		require.False(t, isRoutingNodeVirtualNetworkInterface(name), name)
	}
}

func TestCollectRoutingNodeNetworkCountersWithInterfaceOverride(t *testing.T) {
	t.Setenv("NODE_MONITOR_NETWORK_INTERFACE", "interface-that-does-not-exist")

	sent, received, source := collectRoutingNodeNetworkCounters()
	require.Zero(t, sent)
	require.Zero(t, received)
	require.Empty(t, source)
}
