package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
)

// Body the S4 gateway returned to S1 for a non-streamed refusal on 2026-11-17:
// the refusal type was rewritten to upstream_error, status and message kept.
const s4NonStreamRefusalBody = `{"error":{"message":"Upstream model declined the request (stop_reason=refusal)","type":"upstream_error"},"type":"error"}`

func enableRefusalNoRetry(t *testing.T, value string) {
	original := setting.UpstreamRefusalNoRetryConfig2JsonString()
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateUpstreamRefusalNoRetryConfigByJsonString(original))
	})
	require.NoError(t, setting.UpdateUpstreamRefusalNoRetryConfigByJsonString(value))
}

func upstreamHTTPError(t *testing.T, status int, body string) *types.NewAPIError {
	t.Helper()
	resp := &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	return RelayErrorHandler(context.Background(), resp, false)
}

func TestUpstreamRefusalSource(t *testing.T) {
	refusalMessage := "Upstream model declined the request (stop_reason=refusal)"
	bodyOnly := types.WithOpenAIError(types.OpenAIError{Message: "x", Type: "upstream_error"}, http.StatusBadGateway)
	bodyOnly.SetUpstreamResponse(nil, []byte(`{"error":{"type":"upstream_refusal_error","message":"x","retry_safe":false},"type":"error"}`))

	cases := []struct {
		name string
		err  *types.NewAPIError
		want string
	}{
		// Streamed refusal: the first SSE error event is the Wrapper's own,
		// and the Claude stream handler assigns status 500.
		{"stream structured 500", types.WithClaudeError(types.ClaudeError{Type: "upstream_refusal_error", Message: refusalMessage}, http.StatusInternalServerError), UpstreamRefusalSourceStructured},
		{"non-stream structured type", upstreamHTTPError(t, 422, `{"error":{"message":"`+refusalMessage+`","type":"upstream_refusal_error","retry_safe":false},"type":"error"}`), UpstreamRefusalSourceStructured},
		{"structured type only in upstream body", bodyOnly, UpstreamRefusalSourceStructured},
		{"S4 rewritten 422", upstreamHTTPError(t, 422, s4NonStreamRefusalBody), UpstreamRefusalSourceCompat},
		{"ordinary 422", upstreamHTTPError(t, 422, `{"error":{"message":"messages: text content blocks must be non-empty","type":"invalid_request_error"},"type":"error"}`), ""},
		{"refusal text on 502 without type", upstreamHTTPError(t, 502, s4NonStreamRefusalBody), ""},
		{"generic stream failure", types.WithClaudeError(types.ClaudeError{Type: "upstream_error", Message: "Upstream request failed"}, http.StatusInternalServerError), ""},
		{"declined without stop reason", upstreamHTTPError(t, 422, `{"error":{"message":"request declined","type":"upstream_error"},"type":"error"}`), ""},
		// Validation errors that echo client input must not pass as refusals.
		{"validation echo of stop reason", upstreamHTTPError(t, 422, `{"error":{"message":"messages.0.content.0.text: invalid value \"stop_reason=refusal\"","type":"invalid_request_error"},"type":"error"}`), ""},
		{"echo with gateway type", upstreamHTTPError(t, 422, `{"error":{"message":"metadata.tag must not contain stop_reason=refusal","type":"upstream_error"},"type":"error"}`), ""},
		{"refusal message as prefix only", upstreamHTTPError(t, 422, `{"error":{"message":"Upstream model declined the request (stop_reason=refusal) and more","type":"upstream_error"},"type":"error"}`), ""},
		{"rate limit", upstreamHTTPError(t, 429, `{"error":{"message":"rate limited","type":"rate_limit_error"},"type":"error"}`), ""},
		{"cyber policy keeps its own path", types.WithOpenAIError(types.OpenAIError{Message: refusalMessage, Type: "policy_error", Code: "cyber_policy"}, 422), ""},
		{"nil", nil, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, UpstreamRefusalSource(tc.err))
		})
	}
}

func TestNormalizeUpstreamRefusalRequiresChannelOptIn(t *testing.T) {
	enableRefusalNoRetry(t, `{"channel_ids":[92]}`)
	original := upstreamHTTPError(t, 422, s4NonStreamRefusalBody)

	unchanged, source := NormalizeUpstreamRefusalError(15, original)
	require.Empty(t, source)
	require.Same(t, original, unchanged)
	require.False(t, types.IsSkipRetryError(unchanged))
}

func TestNormalizeUpstreamRefusalIsTerminalAndNotPolicy(t *testing.T) {
	enableRefusalNoRetry(t, `{"channel_ids":[92]}`)
	for _, original := range []*types.NewAPIError{
		upstreamHTTPError(t, 422, s4NonStreamRefusalBody),
		types.WithClaudeError(types.ClaudeError{Type: "upstream_refusal_error", Message: "Upstream model declined the request (stop_reason=refusal)"}, http.StatusInternalServerError),
	} {
		normalized, source := NormalizeUpstreamRefusalError(92, original)
		require.NotEmpty(t, source)
		require.Equal(t, http.StatusUnprocessableEntity, normalized.StatusCode)
		require.Equal(t, types.ErrorCodeUpstreamRefusal, normalized.GetErrorCode())
		require.True(t, types.IsSkipRetryError(normalized))
		require.Contains(t, normalized.Error(), "stop_reason=refusal")
		require.Equal(t, "upstream_refusal_error", normalized.ToClaudeError().Type)
		require.Equal(t, "upstream_refusal_error", normalized.ToOpenAIError().Type)

		// Never routed into the user-suspension path.
		require.False(t, IsUpstreamCyberPolicyError(normalized))
		require.Same(t, normalized, NormalizeUpstreamCyberPolicyError(normalized))
		// Never treated as a channel failure.
		require.False(t, ShouldDisableChannel(normalized))

		_, wantBody := original.UpstreamResponse()
		_, gotBody := normalized.UpstreamResponse()
		require.Equal(t, wantBody, gotBody)
	}
}
