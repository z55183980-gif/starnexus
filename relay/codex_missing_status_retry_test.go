package relay

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel/codex"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestTryRepairCodexMissingStatusForRetry(t *testing.T) {
	request := &dto.OpenAIResponsesRequest{Input: []byte(`[{"type":"message","role":"assistant","content":"hi","status":"completed"}]`)}
	ctx, info := codexInvalidMessageIDTestContext(t, request)
	body, err := codex.RepairAccountPassthroughResponsesBody(ctx, []byte(`{"input":[{"type":"message","role":"assistant","content":"hi","status":"completed"}]}`))
	require.NoError(t, err)
	_, err = (&codex.Adaptor{}).FinalizeOutboundJSONBody(ctx, &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeResponses}, body)
	require.NoError(t, err)
	apiErr := types.WithOpenAIError(types.OpenAIError{Message: "Missing required parameter: 'input[0].status'.", Type: "invalid_request_error", Code: "missing_required_parameter"}, http.StatusBadRequest)
	require.True(t, TryRepairCodexMissingStatusForRetry(ctx, info, apiErr))
	require.False(t, TryRepairCodexMissingStatusForRetry(ctx, info, apiErr))
	repairInfo, exists := ctx.Get("codex_input_repair_admin_info")
	require.True(t, exists)
	require.Equal(t, 0, repairInfo.(map[string]interface{})["missing_status_retried_index"])
	require.Equal(t, "message", repairInfo.(map[string]interface{})["missing_status_retried_type"])
}

func TestTryRepairCodexMissingStatusRejectsOtherErrors(t *testing.T) {
	request := &dto.OpenAIResponsesRequest{Input: []byte(`[{"type":"message","role":"assistant","content":"hi","status":"completed"}]`)}
	ctx, info := codexInvalidMessageIDTestContext(t, request)
	body, err := codex.RepairAccountPassthroughResponsesBody(ctx, []byte(`{"input":[{"type":"message","role":"assistant","content":"hi","status":"completed"}]}`))
	require.NoError(t, err)
	_, err = (&codex.Adaptor{}).FinalizeOutboundJSONBody(ctx, &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeResponses}, body)
	require.NoError(t, err)
	for _, test := range []struct{ message, code string }{
		{"Missing required parameter: 'input[1].status'.", "missing_required_parameter"},
		{"Missing required parameter: 'input[0].status'.", "invalid_value"},
		{"Missing required parameter: 'input[0].content'.", "missing_required_parameter"},
	} {
		apiErr := types.WithOpenAIError(types.OpenAIError{Message: test.message, Type: "invalid_request_error", Code: test.code}, http.StatusBadRequest)
		require.False(t, TryRepairCodexMissingStatusForRetry(ctx, info, apiErr))
	}
	info.RelayMode = relayconstant.RelayModeResponsesCompact
	apiErr := types.WithOpenAIError(types.OpenAIError{Message: "Missing required parameter: 'input[0].status'.", Type: "invalid_request_error", Code: "missing_required_parameter"}, http.StatusBadRequest)
	require.False(t, TryRepairCodexMissingStatusForRetry(ctx, info, apiErr))
	info.RelayMode = relayconstant.RelayModeResponses
	ctx.Set(string(constant.ContextKeyResponsesWebSocketIngress), true)
	require.False(t, TryRepairCodexMissingStatusForRetry(ctx, info, apiErr))
}
