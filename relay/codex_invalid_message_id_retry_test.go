package relay

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	appconstant "github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func codexInvalidMessageIDTestError(message string) *types.NewAPIError {
	return types.WithOpenAIError(types.OpenAIError{
		Message: message,
		Type:    "invalid_request_error",
		Code:    "invalid_value",
	}, http.StatusBadRequest)
}

func codexInvalidMessageIDTestContext(t *testing.T, request *dto.OpenAIResponsesRequest) (*gin.Context, *relaycommon.RelayInfo) {
	t.Helper()
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	common.SetContextKey(ctx, appconstant.ContextKeyChannelType, appconstant.ChannelTypeCodex)
	return ctx, &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeResponses,
		Request:   request,
	}
}

func TestTryRepairCodexInvalidMessageIDForRetry(t *testing.T) {
	t.Parallel()
	request := &dto.OpenAIResponsesRequest{Input: []byte(`[
		{"type":"message","role":"user","content":"earlier"},
		{"type":"message","id":"resp_chatcmpl-example_msg","role":"assistant","content":[{"type":"output_text","text":"kept"}]},
		{"type":"message","role":"user","content":"continue"}
	]`)}
	ctx, info := codexInvalidMessageIDTestContext(t, request)
	apiErr := codexInvalidMessageIDTestError("Invalid 'input[1].id': 'resp_chatcmpl-example_msg'. Expected an ID that begins with 'msg'.")

	repaired := TryRepairCodexInvalidMessageIDForRetry(ctx, info, apiErr)

	require.True(t, repaired)
	require.False(t, gjson.GetBytes(request.Input, "1.id").Exists())
	require.Equal(t, "kept", gjson.GetBytes(request.Input, "1.content.0.text").String())
	repairInfo, exists := ctx.Get("codex_input_repair_admin_info")
	require.True(t, exists)
	require.Equal(t, 1, repairInfo.(map[string]interface{})["invalid_message_ids_removed"])
	require.Equal(t, 1, repairInfo.(map[string]interface{})["first_removed_index"])
	require.Equal(t, true, repairInfo.(map[string]interface{})["upstream_validation_retry"])

	// A request can be reactively repaired at most once, even if another
	// matching upstream error is returned by the retry.
	require.False(t, TryRepairCodexInvalidMessageIDForRetry(ctx, info, apiErr))
}

func TestTryRepairCodexInvalidMessageIDForRetryRejectsNearMatches(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name    string
		input   string
		message string
		mutate  func(*gin.Context, *relaycommon.RelayInfo, *types.NewAPIError)
	}{
		{
			name:    "different upstream message",
			input:   `[{"type":"message","id":"resp_example_msg","role":"assistant","content":"kept"}]`,
			message: "Invalid 'input[0].id': 'resp_example_msg'.",
		},
		{
			name:    "index mismatch",
			input:   `[{"type":"message","id":"resp_example_msg","role":"assistant","content":"kept"}]`,
			message: "Invalid 'input[1].id': 'resp_example_msg'. Expected an ID that begins with 'msg'.",
		},
		{
			name:    "id mismatch",
			input:   `[{"type":"message","id":"resp_other_msg","role":"assistant","content":"kept"}]`,
			message: "Invalid 'input[0].id': 'resp_example_msg'. Expected an ID that begins with 'msg'.",
		},
		{
			name:    "non message item",
			input:   `[{"type":"reasoning","id":"resp_example_msg","summary":[]}]`,
			message: "Invalid 'input[0].id': 'resp_example_msg'. Expected an ID that begins with 'msg'.",
		},
		{
			name:    "referenced id",
			input:   `[{"type":"message","id":"resp_example_msg","role":"assistant","content":"kept"},{"type":"item_reference","id":"resp_example_msg"}]`,
			message: "Invalid 'input[0].id': 'resp_example_msg'. Expected an ID that begins with 'msg'.",
		},
		{
			name:    "native message id",
			input:   `[{"type":"message","id":"msg_example","role":"assistant","content":"kept"}]`,
			message: "Invalid 'input[0].id': 'msg_example'. Expected an ID that begins with 'msg'.",
		},
		{
			name:    "previous response continuation",
			input:   `[{"type":"message","id":"resp_example_msg","role":"assistant","content":"kept"}]`,
			message: "Invalid 'input[0].id': 'resp_example_msg'. Expected an ID that begins with 'msg'.",
			mutate: func(_ *gin.Context, info *relaycommon.RelayInfo, _ *types.NewAPIError) {
				info.Request.(*dto.OpenAIResponsesRequest).PreviousResponseID = "resp_previous"
			},
		},
		{
			name:    "non Codex channel",
			input:   `[{"type":"message","id":"resp_example_msg","role":"assistant","content":"kept"}]`,
			message: "Invalid 'input[0].id': 'resp_example_msg'. Expected an ID that begins with 'msg'.",
			mutate: func(ctx *gin.Context, _ *relaycommon.RelayInfo, _ *types.NewAPIError) {
				common.SetContextKey(ctx, appconstant.ContextKeyChannelType, appconstant.ChannelTypeOpenAI)
			},
		},
		{
			name:    "wrong error code",
			input:   `[{"type":"message","id":"resp_example_msg","role":"assistant","content":"kept"}]`,
			message: "Invalid 'input[0].id': 'resp_example_msg'. Expected an ID that begins with 'msg'.",
			mutate: func(_ *gin.Context, _ *relaycommon.RelayInfo, apiErr *types.NewAPIError) {
				*apiErr = *types.WithOpenAIError(types.OpenAIError{Message: apiErr.Error(), Code: "invalid_request_error"}, http.StatusBadRequest)
			},
		},
		{
			name:    "wrong status code",
			input:   `[{"type":"message","id":"resp_example_msg","role":"assistant","content":"kept"}]`,
			message: "Invalid 'input[0].id': 'resp_example_msg'. Expected an ID that begins with 'msg'.",
			mutate: func(_ *gin.Context, _ *relaycommon.RelayInfo, apiErr *types.NewAPIError) {
				apiErr.StatusCode = http.StatusUnprocessableEntity
			},
		},
		{
			name:    "response already started",
			input:   `[{"type":"message","id":"resp_example_msg","role":"assistant","content":"kept"}]`,
			message: "Invalid 'input[0].id': 'resp_example_msg'. Expected an ID that begins with 'msg'.",
			mutate: func(_ *gin.Context, info *relaycommon.RelayInfo, _ *types.NewAPIError) {
				info.SendResponseCount = 1
			},
		},
		{
			name:    "websocket ingress",
			input:   `[{"type":"message","id":"resp_example_msg","role":"assistant","content":"kept"}]`,
			message: "Invalid 'input[0].id': 'resp_example_msg'. Expected an ID that begins with 'msg'.",
			mutate: func(ctx *gin.Context, _ *relaycommon.RelayInfo, _ *types.NewAPIError) {
				common.SetContextKey(ctx, appconstant.ContextKeyResponsesWebSocketIngress, true)
			},
		},
		{
			name:    "different request path",
			input:   `[{"type":"message","id":"resp_example_msg","role":"assistant","content":"kept"}]`,
			message: "Invalid 'input[0].id': 'resp_example_msg'. Expected an ID that begins with 'msg'.",
			mutate: func(ctx *gin.Context, _ *relaycommon.RelayInfo, _ *types.NewAPIError) {
				ctx.Request.URL.Path = "/v1/chat/completions"
			},
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			request := &dto.OpenAIResponsesRequest{Input: []byte(testCase.input)}
			original := append([]byte(nil), request.Input...)
			ctx, info := codexInvalidMessageIDTestContext(t, request)
			apiErr := codexInvalidMessageIDTestError(testCase.message)
			if testCase.mutate != nil {
				testCase.mutate(ctx, info, apiErr)
			}

			require.False(t, TryRepairCodexInvalidMessageIDForRetry(ctx, info, apiErr))
			require.Equal(t, string(original), string(request.Input))
		})
	}
}
