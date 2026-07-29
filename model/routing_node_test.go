package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestRoutingNodeLifecycle(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	originalDB := DB
	DB = db
	t.Cleanup(func() { DB = originalDB })
	require.NoError(t, db.AutoMigrate(&RoutingNode{}, &RoutingNodeMonitorStatus{}, &RoutingNodeMonitorNetworkSample{}, &UserNodeBinding{}))

	node := &RoutingNode{
		Key: "S5-East", Name: "S5 East", Origin: "origin-s5.dkby.com", Enabled: false, Sort: 5,
	}
	require.NoError(t, CreateRoutingNode(node))
	require.Equal(t, "s5-east", node.Key)

	all, err := ListRoutingNodes(true)
	require.NoError(t, err)
	require.Len(t, all, 1)
	require.False(t, all[0].Enabled)
	require.False(t, all[0].MonitorConfigured)
	require.Nil(t, all[0].MonitorStatus)

	node.Enabled = true
	require.NoError(t, UpdateRoutingNode(node))
	enabled, err := ListRoutingNodes(false)
	require.NoError(t, err)
	require.Len(t, enabled, 1)

	require.NoError(t, SaveUserNodeBindingWithRevision(9, node.Key, 10))
	require.Error(t, DeleteRoutingNode(node.Id))
	require.NoError(t, DeleteUserNodeBinding(9))
	require.NoError(t, DeleteRoutingNode(node.Id))
}

func TestRoutingNodeMonitorState(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	originalDB := DB
	DB = db
	t.Cleanup(func() { DB = originalDB })
	require.NoError(t, db.AutoMigrate(&RoutingNode{}, &RoutingNodeMonitorStatus{}, &RoutingNodeMonitorNetworkSample{}, &UserNodeBinding{}))

	node := &RoutingNode{
		Key: "s5", Name: "S5", Origin: "origin-s5.dkby.com", Enabled: true, MonitorEnabled: true,
	}
	require.NoError(t, CreateRoutingNode(node))
	require.NoError(t, SaveRoutingNodeMonitorToken(node.Id, "abc123"))
	require.NoError(t, SaveRoutingNodeMonitorStatus(&RoutingNodeMonitorStatus{
		NodeId:               node.Id,
		NodeName:             "node-s5",
		CPUUsage:             12.5,
		CPUCores:             4,
		LoadOne:              0.5,
		LoadPercent:          12.5,
		MemoryUsed:           4,
		MemoryTotal:          8,
		MemoryPercent:        50,
		DiskUsed:             25,
		DiskTotal:            100,
		DiskPercent:          25,
		NetworkBytesSent:     1024,
		NetworkBytesReceived: 2048,
		NetworkUploadBps:     128,
		NetworkDownloadBps:   256,
		ReportedAt:           common.GetTimestamp(),
	}))

	nodes, err := ListRoutingNodes(true)
	require.NoError(t, err)
	require.Len(t, nodes, 1)
	require.True(t, nodes[0].MonitorConfigured)
	require.NotNil(t, nodes[0].MonitorStatus)
	require.Equal(t, "node-s5", nodes[0].MonitorStatus.NodeName)
	require.Len(t, nodes[0].MonitorStatus.NetworkSamples, 1)
	require.Equal(t, 128.0, nodes[0].MonitorStatus.NetworkSamples[0].UploadBps)

	require.NoError(t, DeleteRoutingNode(node.Id))
	var count int64
	require.NoError(t, db.Model(&RoutingNodeMonitorStatus{}).Count(&count).Error)
	require.Zero(t, count)
	require.NoError(t, db.Model(&RoutingNodeMonitorNetworkSample{}).Count(&count).Error)
	require.Zero(t, count)
}

func TestListRoutingNodeBoundUsers(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	originalDB := DB
	DB = db
	t.Cleanup(func() { DB = originalDB })
	require.NoError(t, db.AutoMigrate(&User{}, &UserNodeBinding{}))
	require.NoError(t, db.Create(&[]User{
		{Id: 1, Username: "alice", DisplayName: "Alice", AffCode: "alice-code"},
		{Id: 2, Username: "bob", DisplayName: "Bob", AffCode: "bob-code"},
		{Id: 3, Username: "carol", DisplayName: "Carol", AffCode: "carol-code"},
	}).Error)
	require.NoError(t, db.Create(&[]UserNodeBinding{
		{UserId: 1, Node: "s1"},
		{UserId: 2, Node: "s1"},
		{UserId: 3, Node: "s2"},
	}).Error)

	users, total, err := ListRoutingNodeBoundUsers("S1", &common.PageInfo{Page: 2, PageSize: 1})
	require.NoError(t, err)
	require.Equal(t, int64(2), total)
	require.Equal(t, []RoutingNodeBoundUser{{UserId: 2, Username: "bob", DisplayName: "Bob"}}, users)
}

func TestUserNodeRoutingDatabaseLock(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	originalDB := DB
	DB = db
	t.Cleanup(func() { DB = originalDB })
	require.NoError(t, db.AutoMigrate(&UserNodeRoutingLock{}))

	expiresAt := time.Now().Unix() + 100
	acquired, err := TryAcquireUserNodeRoutingLock(7, "owner-a", expiresAt)
	require.NoError(t, err)
	require.True(t, acquired)

	acquired, err = TryAcquireUserNodeRoutingLock(7, "owner-b", expiresAt)
	require.NoError(t, err)
	require.False(t, acquired)

	require.NoError(t, ReleaseUserNodeRoutingLock(7, "owner-a"))
	acquired, err = TryAcquireUserNodeRoutingLock(7, "owner-b", expiresAt)
	require.NoError(t, err)
	require.True(t, acquired)
}
