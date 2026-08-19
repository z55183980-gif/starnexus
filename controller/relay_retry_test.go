package controller

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestShouldRetrySkipsUpstreamContextLengthExceeded(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	apiErr := types.WithOpenAIError(types.OpenAIError{
		Message: "Your input exceeds the context window of this model.",
		Type:    "invalid_request_error",
		Code:    string(types.ErrorCodeContextLengthExceeded),
	}, http.StatusBadGateway)

	require.Equal(t, types.ErrorCodeContextLengthExceeded, apiErr.GetErrorCode())
	require.False(t, shouldRetry(c, apiErr, 3))
}

func TestShouldRetryKeepsOrdinaryBadGatewayRetryable(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	apiErr := types.WithOpenAIError(types.OpenAIError{
		Message: "temporary upstream failure",
		Type:    "upstream_error",
		Code:    "temporary_upstream_error",
	}, http.StatusBadGateway)

	require.True(t, shouldRetry(c, apiErr, 3))
}

func TestShouldRetryStopsCapacityAfterAccountFailover(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	apiErr := types.WithOpenAIError(types.OpenAIError{
		Message: "Our servers are currently overloaded. Please try again later.",
		Type:    "upstream_error",
		Code:    "server_is_overloaded",
	}, 529)

	require.False(t, shouldRetry(c, apiErr, 3))
}

func TestContentModerationTeamExclusionIsReevaluatedAfterFailover(t *testing.T) {
	logDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, logDB.AutoMigrate(&model.PromptAuditLog{}, &model.ContentModerationKeyUsage{}))
	originalLogDB := model.LOG_DB
	model.LOG_DB = logDB
	t.Cleanup(func() { model.LOG_DB = originalLogDB })

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"results":[{"flagged":false,"category_scores":{}}]}`))
	}))
	defer server.Close()

	original := setting.GetContentModerationConfig()
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateContentModerationConfigByJsonString(setting.ContentModerationConfigJSON(original)))
	})
	require.NoError(t, setting.UpdateContentModerationConfigByJsonString(setting.ContentModerationConfigJSON(setting.ContentModerationConfig{
		Enabled: true, Mode: setting.ContentModerationModePreBlock,
		BaseURL: server.URL, Model: "omni-moderation-latest", APIKeys: []string{"sk-test"}, TimeoutMS: 3000,
		AllGroups: true, ExcludeOpenAIOAuthTeam: true, Thresholds: setting.ContentModerationDefaultThresholds(),
	})))

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	common.SetContextKey(c, constant.ContextKeyUpstreamAccountPlatform, constant.UpstreamPlatformOpenAI)
	common.SetContextKey(c, constant.ContextKeyUpstreamAccountType, constant.UpstreamAccountTypeOAuth)
	common.SetContextKey(c, constant.ContextKeyUpstreamAccountPlanType, "team")
	request := &dto.GeneralOpenAIRequest{Messages: []dto.Message{{Role: "user", Content: "hello"}}}
	completed := false

	require.Nil(t, applyContentModerationForSelectedAccount(c, request, types.RelayFormatOpenAI, "gpt-test", &completed))
	require.False(t, completed)
	require.Zero(t, calls.Load())

	common.SetContextKey(c, constant.ContextKeyUpstreamAccountPlanType, "pro")
	require.Nil(t, applyContentModerationForSelectedAccount(c, request, types.RelayFormatOpenAI, "gpt-test", &completed))
	require.True(t, completed)
	require.Equal(t, int32(1), calls.Load())

	require.Nil(t, applyContentModerationForSelectedAccount(c, request, types.RelayFormatOpenAI, "gpt-test", &completed))
	require.Equal(t, int32(1), calls.Load())
}
