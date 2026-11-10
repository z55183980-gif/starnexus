package service

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func capturePreConsumeLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var output bytes.Buffer
	common.LogWriterMu.Lock()
	previous := gin.DefaultErrorWriter
	gin.DefaultErrorWriter = &output
	common.LogWriterMu.Unlock()
	t.Cleanup(func() {
		common.LogWriterMu.Lock()
		gin.DefaultErrorWriter = previous
		common.LogWriterMu.Unlock()
	})
	return &output
}

func TestPreConsumeFailureDiagnostics(t *testing.T) {
	output := capturePreConsumeLog(t)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/messages?secret=query-secret", nil)
	ctx.Request.Header.Set("Authorization", "Bearer auth-secret")
	ctx.Set(common.RequestIdKey, "req-estimate-test")
	info := &relaycommon.RelayInfo{UserId: 584, TokenId: 695, TokenKey: "api-key-secret", RequestId: "req-estimate-test", OriginModelName: "claude-opus-5-5", UsingGroup: "discount"}
	info.SetEstimatePromptTokens(200000)
	info.PriceData = types.PriceData{ModelRatio: 2, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 0.8}}
	restore := billing_setting.SetTokenPricingSettingForTest(billing_setting.TokenPricingSetting{Enabled: true, InputRatio: 1.05, OutputRatio: 1.05})
	t.Cleanup(restore)
	CapturePreConsumeEstimate(ctx, info, &types.TokenCountMeta{CombineText: "private prompt", MaxTokens: 128000, MessagesCount: 111, ToolsCount: 4, Files: []*types.FileMeta{
		types.NewFileMeta(types.FileTypeImage, types.NewBase64FileSource("image-secret", "image/png")),
		types.NewFileMeta(types.FileTypeImage, types.NewURLFileSource("https://private.invalid/url-secret")),
	}})
	restore() // A later configuration change must not change captured pricing.
	ctx.Set(preConsumeWalletQuotaKey, 9224468)
	apiErr := types.NewErrorWithStatusCode(errors.New("error-secret"), types.ErrorCodeInsufficientUserQuota, http.StatusForbidden, types.ErrOptionWithNoRecordErrorLog())
	logPreConsumeFailure(ctx, info, 10059473, apiErr)
	line := output.String()
	for _, secret := range []string{"auth-secret", "query-secret", "api-key-secret", "private prompt", "image-secret", "url-secret", "error-secret"} {
		require.NotContains(t, line, secret)
	}
	_, data, ok := strings.Cut(line, "preconsume_failed ")
	require.True(t, ok)
	var facts map[string]any
	require.NoError(t, common.Unmarshal([]byte(data), &facts))
	for key, expected := range map[string]any{
		"event": "preconsume_failed", "request_id": "req-estimate-test", "user_id": float64(584),
		"estimated_input_tokens": float64(200000), "billing_input_tokens": float64(210000),
		"max_tokens": float64(128000), "billing_output_tokens": float64(134400),
		"input_token_ratio": 1.05, "output_token_ratio": 1.05, "model_ratio": float64(2), "group_ratio": 0.8,
		"image_count": float64(2), "image_base64_chars": float64(len("image-secret")),
		"required_quota": float64(10059473), "available_wallet_quota": float64(9224468),
		"status": float64(403), "error_code": "insufficient_user_quota",
	} {
		require.Equal(t, expected, facts[key], key)
	}
	require.False(t, types.IsRecordErrorLog(apiErr), "diagnostics must not change public error/log policy")
}

func TestPreConsumeBillingFailureEmitsDiagnosticsOnce(t *testing.T) {
	output := capturePreConsumeLog(t)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{UserId: 584, QuotaClamp: &common.QuotaClamp{Kind: common.QuotaClampOverflow}}
	err := PreConsumeBilling(ctx, 100, info)
	require.NotNil(t, err)
	require.Equal(t, types.ErrorCodeModelPriceError, err.GetErrorCode())
	require.Contains(t, output.String(), "preconsume_failed")
	require.Equal(t, 1, strings.Count(output.String(), "\n"))
	output.Reset()
	logPreConsumeFailure(ctx, info, 100, nil)
	logPreConsumeFailure(nil, info, 100, err)
	logPreConsumeFailure(ctx, nil, 100, err)
	require.Empty(t, output.String())
}

func TestPreConsumeWalletFailureLogsCheckedBalanceWithoutDeduction(t *testing.T) {
	truncate(t)
	seedUser(t, 584, 9224468)
	output := capturePreConsumeLog(t)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{UserId: 584, RequestId: "wallet-rejection-test"}
	info.UserSetting.BillingPreference = "wallet_only"
	CapturePreConsumeEstimate(ctx, info, &types.TokenCountMeta{MaxTokens: 128000})
	err := PreConsumeBilling(ctx, 10059473, info)
	require.NotNil(t, err)
	require.Equal(t, http.StatusForbidden, err.StatusCode)
	require.Equal(t, types.ErrorCodeInsufficientUserQuota, err.GetErrorCode())
	require.Contains(t, output.String(), `"available_wallet_quota":9224468`)
	require.Contains(t, output.String(), `"required_quota":10059473`)
	require.Equal(t, 1, strings.Count(output.String(), "preconsume_failed "))
	var user model.User
	require.NoError(t, model.DB.First(&user, 584).Error)
	require.Equal(t, 9224468, user.Quota)
	require.Nil(t, info.Billing)
}

func TestPreConsumeDiagnosticsTieredSnapshot(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{TieredBillingSnapshot: &billingexpr.BillingSnapshot{
		BillingMode: "tiered_expr", ExprString: "private expression", EstimatedPromptTokens: 123,
		EstimatedCompletionTokens: 8192, GroupRatio: 0.5,
	}}
	CapturePreConsumeEstimate(ctx, info, &types.TokenCountMeta{})
	value, ok := ctx.Get(preConsumeEstimateKey)
	require.True(t, ok)
	facts := value.(map[string]any)
	require.Equal(t, 123, facts["billing_input_tokens"])
	require.Equal(t, 8192, facts["billing_output_tokens"])
	require.Equal(t, 0.5, facts["group_ratio"])
	data, err := common.Marshal(facts)
	require.NoError(t, err)
	require.NotContains(t, string(data), "private expression")
}
