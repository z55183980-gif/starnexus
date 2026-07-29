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

func setupUserNodeRouterTestDB(t *testing.T) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	originalDB := model.DB
	originalRedisEnabled := common.RedisEnabled
	model.DB = db
	common.RedisEnabled = false
	t.Cleanup(func() {
		model.DB = originalDB
		common.RedisEnabled = originalRedisEnabled
	})
	require.NoError(t, db.AutoMigrate(
		&model.Token{},
		&model.UserNodeBinding{},
		&model.UserNodeRoutingLock{},
		&model.RoutingNode{},
	))
}

func TestSyncUserRoutingTokensSendsOnlyTokenHashes(t *testing.T) {
	setupUserNodeRouterTestDB(t)
	require.NoError(t, model.DB.Create(&model.Token{UserId: 7, Key: "token-one"}).Error)
	require.NoError(t, model.DB.Create(&model.Token{UserId: 7, Key: "token-two"}).Error)

	var received userNodeRouterRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer test-secret", r.Header.Get("Authorization"))
		require.NoError(t, common.DecodeJson(r.Body, &received))
		response, marshalErr := common.Marshal(userNodeRouterResponse{
			Success: true, Version: 2, Action: "sync_tokens", UserId: 7, TokenCount: 2,
		})
		require.NoError(t, marshalErr)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(response)
	}))
	t.Cleanup(server.Close)
	t.Setenv("NODE_ROUTER_ADMIN_URL", server.URL)
	t.Setenv("NODE_ROUTER_ADMIN_SECRET", "test-secret")

	require.NoError(t, SyncUserRoutingTokens(context.Background(), 7))
	sort.Strings(received.TokenHashes)
	expected := []string{hashUserRoutingToken("token-one"), hashUserRoutingToken("token-two")}
	sort.Strings(expected)
	require.Equal(t, 2, received.Version)
	require.Equal(t, "sync_tokens", received.Action)
	require.Equal(t, 7, received.UserId)
	require.Equal(t, expected, received.TokenHashes)
	require.NotContains(t, received.TokenHashes, "token-one")
}

func TestUpdateUserNodeRouteSyncsTokensOnlyOnFirstMigration(t *testing.T) {
	setupUserNodeRouterTestDB(t)
	require.NoError(t, model.DB.Create(&model.Token{UserId: 7, Key: "token-one"}).Error)
	require.NoError(t, model.DB.Create(&model.RoutingNode{
		Key: "s1", Name: "S1", Origin: "origin-s1.dkby.com", Enabled: true,
	}).Error)
	require.NoError(t, model.DB.Create(&model.RoutingNode{
		Key: "s2", Name: "S2", Origin: "origin-s2.dkby.com", Enabled: true,
	}).Error)

	var actions []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request userNodeRouterRequest
		require.NoError(t, common.DecodeJson(r.Body, &request))
		actions = append(actions, request.Action)
		response := userNodeRouterResponse{
			Success: true, Version: 2, Action: request.Action, UserId: request.UserId,
			Node: request.Node, Revision: request.Revision, TokenCount: len(request.TokenHashes),
		}
		body, err := common.Marshal(response)
		require.NoError(t, err)
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)
	t.Setenv("NODE_ROUTER_ADMIN_URL", server.URL)
	t.Setenv("NODE_ROUTER_ADMIN_SECRET", "test-secret")

	first, err := UpdateUserNodeRoute(context.Background(), 7, "s1")
	require.NoError(t, err)
	require.Equal(t, []string{"set_route", "sync_tokens"}, actions)
	require.Positive(t, first.Revision)
	require.True(t, first.TokensSynced)

	actions = nil
	second, err := UpdateUserNodeRoute(context.Background(), 7, "s2")
	require.NoError(t, err)
	require.Equal(t, []string{"set_route"}, actions)
	require.Greater(t, second.Revision, first.Revision)
}

func TestResolveRoutingNodeRejectsDatabaseNode(t *testing.T) {
	setupUserNodeRouterTestDB(t)
	require.NoError(t, model.CreateRoutingNode(&model.RoutingNode{
		Key: "spg", Name: "SPG", Type: model.RoutingNodeTypeDatabase, Enabled: true,
	}))

	_, _, err := resolveRoutingNode("spg")
	require.ErrorContains(t, err, "does not participate in user routing")
}

func TestUpdateUserNodeRouteCompensatesUnknownSetRouteResult(t *testing.T) {
	setupUserNodeRouterTestDB(t)
	require.NoError(t, model.DB.Create(&model.RoutingNode{
		Key: "s1", Name: "S1", Origin: "origin-s1.dkby.com", Enabled: true,
	}).Error)
	require.NoError(t, model.DB.Create(&model.RoutingNode{
		Key: "s2", Name: "S2", Origin: "origin-s2.dkby.com", Enabled: true,
	}).Error)
	require.NoError(t, model.SaveUserNodeBindingWithRevision(7, "s1", 100))

	var requests []userNodeRouterRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request userNodeRouterRequest
		require.NoError(t, common.DecodeJson(r.Body, &request))
		requests = append(requests, request)
		if len(requests) == 1 {
			_, _ = w.Write([]byte("committed but response was corrupted"))
			return
		}
		body, err := common.Marshal(userNodeRouterResponse{
			Success: true, Version: 2, Action: request.Action, UserId: request.UserId,
			Node: request.Node, Revision: request.Revision,
		})
		require.NoError(t, err)
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)
	t.Setenv("NODE_ROUTER_ADMIN_URL", server.URL)
	t.Setenv("NODE_ROUTER_ADMIN_SECRET", "test-secret")

	_, err := UpdateUserNodeRoute(context.Background(), 7, "s2")
	require.ErrorContains(t, err, "invalid user node router response")
	require.Len(t, requests, 2)
	require.Equal(t, "s2", requests[0].Node)
	require.Equal(t, "s1", requests[1].Node)
	require.Greater(t, requests[1].Revision, requests[0].Revision)

	binding, err := model.GetUserNodeBindingRecord(7)
	require.NoError(t, err)
	require.Equal(t, "s1", binding.Node)
	require.Equal(t, requests[1].Revision, binding.Revision)
	require.False(t, binding.TokensSynced)
}

func TestUserNodeRouterRejectsNonJSONSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(server.Close)
	t.Setenv("NODE_ROUTER_ADMIN_URL", server.URL)
	t.Setenv("NODE_ROUTER_ADMIN_SECRET", "test-secret")

	_, err := postUserNodeRouter(context.Background(), userNodeRouterRequest{
		Version: 2, Action: "delete_user", UserId: 8,
	})
	require.ErrorContains(t, err, "invalid user node router response")
}

func TestShouldCompensateUserRoute(t *testing.T) {
	require.False(t, shouldCompensateUserRoute(&userNodeRouterHTTPError{StatusCode: http.StatusConflict}))
	require.False(t, shouldCompensateUserRoute(&userNodeRouterHTTPError{StatusCode: http.StatusBadRequest}))
	require.True(t, shouldCompensateUserRoute(&userNodeRouterHTTPError{StatusCode: http.StatusInternalServerError}))
	require.True(t, shouldCompensateUserRoute(context.DeadlineExceeded))
}

func TestRefreshAutoBindingMarksTokensPending(t *testing.T) {
	setupUserNodeRouterTestDB(t)
	require.NoError(t, model.SaveUserNodeBindingState(7, model.UserNodeAuto, 100, true))
	t.Setenv("NODE_ROUTER_ADMIN_URL", "https://router.invalid/sync")
	t.Setenv("NODE_ROUTER_ADMIN_SECRET", "test-secret")

	require.NoError(t, RefreshStoredUserNodeBinding(context.Background(), 7))
	binding, err := model.GetUserNodeBindingRecord(7)
	require.NoError(t, err)
	require.False(t, binding.TokensSynced)
}
