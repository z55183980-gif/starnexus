package service

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestNormalizeUpstreamCyberPolicyErrorRequiresStructuredCode(t *testing.T) {
	structured := types.WithOpenAIError(types.OpenAIError{
		Message: "This content was flagged for possible cybersecurity risk",
		Type:    "invalid_request_error",
		Code:    "cyber_policy",
	}, http.StatusInternalServerError)

	normalized := NormalizeUpstreamCyberPolicyError(structured)
	require.Equal(t, http.StatusBadRequest, normalized.StatusCode)
	require.Equal(t, types.ErrorCodeCyberPolicy, normalized.GetErrorCode())
	require.True(t, types.IsSkipRetryError(normalized))

	messageOnly := types.WithOpenAIError(types.OpenAIError{
		Message: "cyber_policy appeared only in message text",
		Code:    "server_error",
	}, http.StatusInternalServerError)
	require.False(t, IsUpstreamCyberPolicyError(messageOnly))
	require.Same(t, messageOnly, NormalizeUpstreamCyberPolicyError(messageOnly))
}

func TestSuspendUserAPIForUpstreamCyberPolicyRequiresConfiguredChannel(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.PromptAuditPolicy{}, &model.UserAPIAccessEvent{}))
	originalDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = originalDB })

	originalConfig := setting.GetSecurityAuditBanConfig()
	originalConfigJSON, _ := common.Marshal(originalConfig)
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateSecurityAuditBanConfigByJsonString(string(originalConfigJSON)))
	})
	require.NoError(t, setting.UpdateSecurityAuditBanConfigByJsonString(`{"channel_ids":[7],"duration_seconds":3600}`))

	user := &model.User{Username: "channel-scoped-ban", Password: "password", APIStatus: common.UserAPIStatusEnabled}
	require.NoError(t, db.Create(user).Error)
	apiErr := types.WithOpenAIError(types.OpenAIError{Code: "cyber_policy"}, http.StatusBadRequest)

	ctx, _ := gin.CreateTestContext(nil)
	common.SetContextKey(ctx, constant.ContextKeyUserId, user.Id)
	common.SetContextKey(ctx, constant.ContextKeyChannelId, 6)
	require.False(t, SuspendUserAPIForUpstreamCyberPolicy(ctx, apiErr, "gpt-test"))

	var stored model.User
	require.NoError(t, db.First(&stored, user.Id).Error)
	require.Equal(t, common.UserAPIStatusEnabled, stored.APIStatus)

	common.SetContextKey(ctx, constant.ContextKeyChannelId, 7)
	require.True(t, SuspendUserAPIForUpstreamCyberPolicy(ctx, apiErr, "gpt-test"))
	require.NoError(t, db.First(&stored, user.Id).Error)
	require.Equal(t, common.UserAPIStatusSuspended, stored.APIStatus)
	require.Greater(t, stored.APISuspendedUntil, stored.APISuspendedAt)
}
