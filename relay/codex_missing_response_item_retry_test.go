package relay

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/types"

	"github.com/stretchr/testify/require"
)

func codexMissingResponseItemTestError(message string) *types.NewAPIError {
	return types.WithOpenAIError(types.OpenAIError{
		Message: message,
		Type:    "invalid_request_error",
		Code:    "not_found",
	}, http.StatusNotFound)
}

func TestTryRepairCodexMissingResponseItemsForRetryAfterPortableReplayDetection(t *testing.T) {
	t.Parallel()
	request := &dto.OpenAIResponsesRequest{
		PreviousResponseID: "resp_previous",
		Input:              []byte(`[{"type":"reasoning","id":"rs_stale","encrypted_content":"cipher"},{"type":"message","role":"user","content":"continue"}]`),
	}
	ctx, info := codexInvalidMessageIDTestContext(t, request)
	apiErr := codexMissingResponseItemTestError("Item with id 'rs_stale' not found. Items are not persisted when 'store' is set to false. Try again with 'store' set to true, or remove this item from your input.")

	require.True(t, IsCodexResponsesMissingItemError(ctx, apiErr))
	require.True(t, TryRepairCodexMissingResponseItemsForRetry(ctx, info, apiErr))
	require.True(t, ctx.GetBool(codexMissingResponseItemRetryContextKey))
	require.False(t, TryRepairCodexMissingResponseItemsForRetry(ctx, info, apiErr))
	infoMap, exists := ctx.Get("codex_input_repair_admin_info")
	require.True(t, exists)
	require.Equal(t, true, infoMap.(map[string]interface{})["portable_replay_rebuild"])
}

func TestIsCodexResponsesMissingItemErrorIsNarrow(t *testing.T) {
	t.Parallel()
	ctx, info := codexInvalidMessageIDTestContext(t, &dto.OpenAIResponsesRequest{})
	_ = info
	valid := "Item with id 'rs_1' not found. Items are not persisted when 'store' is set to false."
	require.True(t, IsCodexResponsesMissingItemError(ctx, codexMissingResponseItemTestError(valid)))

	for _, message := range []string{
		"Item with id 'rs_1' not found",
		"resource not found while loading a model",
		"Item with id 'rs_1' not found. Items are persisted when store is true.",
	} {
		require.False(t, IsCodexResponsesMissingItemError(ctx, codexMissingResponseItemTestError(message)))
	}
}
