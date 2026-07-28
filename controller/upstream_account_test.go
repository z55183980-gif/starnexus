package controller

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestCreateUpstreamAccountAndProxyReturnPersistedIds(t *testing.T) {
	t.Setenv("UPSTREAM_ACCOUNT_CREDENTIAL_KEYS", `{"1":"`+base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))+`"}`)
	t.Setenv("UPSTREAM_ACCOUNT_ACTIVE_KEY_VERSION", "1")
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.User{}, &model.Log{}, &model.Channel{}, &model.UpstreamProxy{}, &model.UpstreamAccountPool{},
		&model.UpstreamAccount{}, &model.UpstreamAccountPoolMember{},
	))
	originalDB := model.DB
	originalLogDB := model.LOG_DB
	originalMemoryCache := common.MemoryCacheEnabled
	originalRedisEnabled := common.RedisEnabled
	model.DB = db
	model.LOG_DB = db
	common.MemoryCacheEnabled = false
	common.RedisEnabled = false
	t.Cleanup(func() {
		model.DB = originalDB
		model.LOG_DB = originalLogDB
		common.MemoryCacheEnabled = originalMemoryCache
		common.RedisEnabled = originalRedisEnabled
	})
	gin.SetMode(gin.TestMode)

	accountResponse := invokeUpstreamHandler(t, CreateUpstreamAccount, `{
		"name":"api account","platform":"openai","type":"apikey",
		"credentials":{"api_key":"sk-test"},"extra":"{}","concurrency":1,
		"priority":50,"weight":1,"status":"active","schedulable":true
	}`)
	require.True(t, accountResponse.Success)
	var account model.UpstreamAccount
	require.NoError(t, common.Unmarshal(accountResponse.Data, &account))
	require.Positive(t, account.Id)

	proxyResponse := invokeUpstreamHandler(t, CreateUpstreamProxy, `{
		"name":"proxy","protocol":"http","host":"127.0.0.1","port":8080,
		"auth":{"username":"user","password":"pass"},"status":"active",
		"fallback_mode":"none","expiry_warn_days":7
	}`)
	require.True(t, proxyResponse.Success)
	var proxy model.UpstreamProxy
	require.NoError(t, common.Unmarshal(proxyResponse.Data, &proxy))
	require.Positive(t, proxy.Id)
}

func TestValidBatchIdsRejectsNonPositiveValues(t *testing.T) {
	require.True(t, validBatchIds([]int{1, 2, 2}))
	require.False(t, validBatchIds(nil))
	require.False(t, validBatchIds([]int{0}))
	require.False(t, validBatchIds([]int{-1, 2}))
	require.False(t, validBatchIds(make([]int, 101)))
}

func TestUpstreamPatchRequestsCanClearNullableFields(t *testing.T) {
	notes := "old"
	proxyID := 9
	loadFactor := 3
	rateMultiplier := 2.5
	expiresAt := int64(123)
	account := model.UpstreamAccount{
		Notes: &notes, ProxyId: &proxyID, LoadFactor: &loadFactor,
		RateMultiplier: &rateMultiplier, ExpiresAt: &expiresAt, ProxyFallbackOriginId: &proxyID,
	}
	var accountPatch upstreamAccountRequest
	require.NoError(t, common.UnmarshalJsonStr(`{
		"notes":null,"proxy_id":null,"load_factor":null,
		"rate_multiplier":0,"expires_at":null
	}`, &accountPatch))
	applyAccountRequest(&account, accountPatch)
	require.Nil(t, account.Notes)
	require.Nil(t, account.ProxyId)
	require.Nil(t, account.ProxyFallbackOriginId)
	require.Nil(t, account.LoadFactor)
	require.NotNil(t, account.RateMultiplier)
	require.Zero(t, *account.RateMultiplier)
	require.Nil(t, account.ExpiresAt)

	pool := model.UpstreamAccountPool{DefaultProxyId: &proxyID}
	var poolPatch upstreamAccountPoolRequest
	require.NoError(t, common.UnmarshalJsonStr(`{"default_proxy_id":null}`, &poolPatch))
	applyAccountPoolRequest(&pool, poolPatch)
	require.Nil(t, pool.DefaultProxyId)

	proxy := model.UpstreamProxy{ExpiresAt: &expiresAt, BackupProxyId: &proxyID}
	var proxyPatch upstreamProxyRequest
	require.NoError(t, common.UnmarshalJsonStr(`{"expires_at":null,"backup_proxy_id":null}`, &proxyPatch))
	applyProxyRequest(&proxy, proxyPatch)
	require.Nil(t, proxy.ExpiresAt)
	require.Nil(t, proxy.BackupProxyId)
}

type upstreamHandlerResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    []byte `json:"data"`
}

func invokeUpstreamHandler(t *testing.T, handler gin.HandlerFunc, body string) upstreamHandlerResponse {
	t.Helper()
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	context.Request.Header.Set("Content-Type", "application/json")
	context.Set("id", 1)
	handler(context)
	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool            `json:"success"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	return upstreamHandlerResponse{Success: response.Success, Message: response.Message, Data: response.Data}
}
