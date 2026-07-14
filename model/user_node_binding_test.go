package model

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestUserNodeBindingLifecycle(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	originalDB := DB
	DB = db
	t.Cleanup(func() { DB = originalDB })
	require.NoError(t, db.AutoMigrate(&UserNodeBinding{}, &UserNodeRoutingLock{}))

	node, err := GetUserNodeBinding(42)
	require.NoError(t, err)
	require.Equal(t, UserNodeAuto, node)

	require.NoError(t, SaveUserNodeBinding(42, "S2"))
	node, err = GetUserNodeBinding(42)
	require.NoError(t, err)
	require.Equal(t, UserNodeS2, node)

	require.NoError(t, SaveUserNodeBinding(42, UserNodeAuto))
	node, err = GetUserNodeBinding(42)
	require.NoError(t, err)
	require.Equal(t, UserNodeAuto, node)

	node, err = NormalizeUserNode("S5-East")
	require.NoError(t, err)
	require.Equal(t, "s5-east", node)

	require.NoError(t, SaveUserNodeBindingWithRevision(42, "s5-east", 123))
	binding, err := GetUserNodeBindingRecord(42)
	require.NoError(t, err)
	require.Equal(t, "s5-east", binding.Node)
	require.Equal(t, int64(123), binding.Revision)

	_, err = NormalizeUserNode("invalid/node")
	require.Error(t, err)
}
