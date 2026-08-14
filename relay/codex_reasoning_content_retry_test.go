package relay

import (
	"net/http"
	"testing"

	appconstant "github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/types"

	"github.com/stretchr/testify/require"
)

func codexReasoningContentTestError(message string) *types.NewAPIError {
	return types.WithOpenAIError(types.OpenAIError{
		Message: message,
		Type:    "invalid_request_error",
		Code:    "array_above_max_length",
	}, http.StatusBadRequest)
}

func TestTryArmCodexReasoningContentRetry(t *testing.T) {
	t.Parallel()
	request := &dto.OpenAIResponsesRequest{Input: []byte(`[{"type":"reasoning","content":[{"type":"reasoning_text","text":"secret"}],"encrypted_content":"cipher"}]`)}
	ctx, info := codexInvalidMessageIDTestContext(t, request)
	apiErr := codexReasoningContentTestError("Invalid 'input[7].content': array too long. Expected an array with maximum length 0, but got an array with length 1 instead.")

	require.True(t, TryArmCodexReasoningContentRetry(ctx, info, apiErr))
	require.True(t, ctx.GetBool(string(appconstant.ContextKeyCodexReasoningContentRetryArmed)))
	require.Equal(t, 7, ctx.GetInt(string(appconstant.ContextKeyCodexReasoningContentRetryIndex)))
	require.Equal(t, 1, ctx.GetInt(string(appconstant.ContextKeyCodexReasoningContentRetryLength)))
	require.False(t, TryArmCodexReasoningContentRetry(ctx, info, apiErr), "shared validation retry budget must be one-shot")
}

func TestTryArmCodexReasoningContentRetryRejectsNearMatches(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name    string
		message string
		mutate  func(*types.NewAPIError)
	}{
		{name: "different maximum", message: "Invalid 'input[7].content': array too long. Expected an array with maximum length 1, but got an array with length 2 instead."},
		{name: "zero actual length", message: "Invalid 'input[7].content': array too long. Expected an array with maximum length 0, but got an array with length 0 instead."},
		{name: "different field", message: "Invalid 'input[7].summary': array too long. Expected an array with maximum length 0, but got an array with length 1 instead."},
		{name: "wrong code", message: "Invalid 'input[7].content': array too long. Expected an array with maximum length 0, but got an array with length 1 instead.", mutate: func(apiErr *types.NewAPIError) {
			*apiErr = *types.WithOpenAIError(types.OpenAIError{Message: apiErr.Error(), Type: "invalid_request_error", Code: "invalid_value"}, http.StatusBadRequest)
		}},
	}
	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			request := &dto.OpenAIResponsesRequest{Input: []byte(`[{"type":"reasoning"}]`)}
			ctx, info := codexInvalidMessageIDTestContext(t, request)
			apiErr := codexReasoningContentTestError(testCase.message)
			if testCase.mutate != nil {
				testCase.mutate(apiErr)
			}
			require.False(t, TryArmCodexReasoningContentRetry(ctx, info, apiErr))
		})
	}
}
