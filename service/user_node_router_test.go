package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestSyncUserNodeBindingSendsOnlyTokenHashes(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	originalDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = originalDB })
	require.NoError(t, db.AutoMigrate(&model.Token{}))
	require.NoError(t, db.Create(&model.Token{UserId: 7, Key: "token-one"}).Error)
	require.NoError(t, db.Create(&model.Token{UserId: 7, Key: "token-two"}).Error)

	var received userNodeRouterSyncRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer test-secret", r.Header.Get("Authorization"))
		require.NoError(t, common.DecodeJson(r.Body, &received))
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	t.Setenv("NODE_ROUTER_ADMIN_URL", server.URL)
	t.Setenv("NODE_ROUTER_ADMIN_SECRET", "test-secret")

	require.NoError(t, SyncUserNodeBinding(context.Background(), 7, model.UserNodeS3))
	sort.Strings(received.TokenHashes)
	expected := []string{hashUserRoutingToken("token-one"), hashUserRoutingToken("token-two")}
	sort.Strings(expected)
	require.Equal(t, 7, received.UserId)
	require.Equal(t, model.UserNodeS3, received.Node)
	require.Equal(t, expected, received.TokenHashes)
	require.NotContains(t, received.TokenHashes, "token-one")
}
