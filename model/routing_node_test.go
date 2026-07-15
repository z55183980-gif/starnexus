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
	require.NoError(t, db.AutoMigrate(&RoutingNode{}, &UserNodeBinding{}))

	node := &RoutingNode{
		Key: "S5-East", Name: "S5 East", Origin: "origin-s5.dkby.com", Enabled: false, Sort: 5,
	}
	require.NoError(t, CreateRoutingNode(node))
	require.Equal(t, "s5-east", node.Key)

	all, err := ListRoutingNodes(true)
	require.NoError(t, err)
	require.Len(t, all, 1)
	require.False(t, all[0].Enabled)

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
