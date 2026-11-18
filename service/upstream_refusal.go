package service

import (
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/tidwall/gjson"
)

const (
	UpstreamRefusalSourceStructured = "structured"
	UpstreamRefusalSourceCompat     = "compat"

	upstreamRefusalDefaultMessage = "Upstream model declined the request (stop_reason=refusal)"
)

// UpstreamRefusalSource reports how an upstream error is known to be an
// explicit model refusal, or "" when it is not one.
//
// Structured: the gateway kept the refusal error type. This is how streamed
// refusals reach the relay today (the first SSE error event carries
// error.type=upstream_refusal_error, whatever HTTP status the stream handler
// assigns), and how non-streamed ones will arrive once the gateway stops
// rewriting the type.
//
// Compat: the gateway rewrote a non-streamed refusal to a generic
// upstream_error but kept HTTP 422 and the Wrapper's fixed refusal message.
// Only that exact body matches; generic 422s, validation errors that merely
// mention the stop reason, and bare 5xx do not.
func UpstreamRefusalSource(apiErr *types.NewAPIError) string {
	if apiErr == nil {
		return ""
	}
	code := apiErr.GetErrorCode()
	if code == types.ErrorCodeCyberPolicy {
		return ""
	}
	if code == types.ErrorCodeUpstreamRefusal {
		return UpstreamRefusalSourceStructured
	}
	switch relayError := apiErr.RelayError.(type) {
	case types.OpenAIError:
		if relayError.Type == string(types.ErrorCodeUpstreamRefusal) {
			return UpstreamRefusalSourceStructured
		}
	case types.ClaudeError:
		if relayError.Type == string(types.ErrorCodeUpstreamRefusal) {
			return UpstreamRefusalSourceStructured
		}
	}
	if _, body := apiErr.UpstreamResponse(); len(body) > 0 &&
		gjson.GetBytes(body, "error.type").String() == string(types.ErrorCodeUpstreamRefusal) {
		return UpstreamRefusalSourceStructured
	}
	if apiErr.StatusCode == http.StatusUnprocessableEntity && isGatewayRewrittenRefusal(apiErr) {
		return UpstreamRefusalSourceCompat
	}
	return ""
}

// isGatewayRewrittenRefusal matches only the exact body the gateway produces
// when it rewrites a Wrapper refusal: type upstream_error and the Wrapper's
// fixed refusal message, verbatim. A substring match would also accept
// ordinary validation errors that echo client input such as
// "stop_reason=refusal".
func isGatewayRewrittenRefusal(apiErr *types.NewAPIError) bool {
	_, body := apiErr.UpstreamResponse()
	if len(body) == 0 {
		return false
	}
	return gjson.GetBytes(body, "error.type").String() == "upstream_error" &&
		strings.TrimSpace(gjson.GetBytes(body, "error.message").String()) == upstreamRefusalDefaultMessage
}

// NormalizeUpstreamRefusalError turns an explicit upstream refusal on an
// opted-in channel into a terminal, non-retryable 422. It returns the error
// unchanged and an empty source when the channel is not opted in or the error
// is not a refusal.
//
// The result deliberately uses its own code instead of cyber_policy: a
// refusal is a per-request content outcome and must never suspend the user,
// fail the account over, or penalize the channel.
func NormalizeUpstreamRefusalError(channelId int, apiErr *types.NewAPIError) (*types.NewAPIError, string) {
	if !setting.UpstreamRefusalNoRetryAppliesToChannel(channelId) {
		return apiErr, ""
	}
	source := UpstreamRefusalSource(apiErr)
	if source == "" {
		return apiErr, ""
	}
	message := strings.TrimSpace(apiErr.Error())
	if message == "" || message == string(apiErr.GetErrorCode()) {
		message = upstreamRefusalDefaultMessage
	}
	// Built like other upstream HTTP errors so the relay's request-id suffix
	// reaches Claude-format clients, both as JSON and as a terminal SSE event.
	normalized := types.WithOpenAIError(
		types.OpenAIError{
			Message: message,
			Type:    string(types.ErrorCodeUpstreamRefusal),
			Code:    string(types.ErrorCodeUpstreamRefusal),
		},
		http.StatusUnprocessableEntity,
		types.ErrOptionWithSkipRetry(),
	)
	header, body := apiErr.UpstreamResponse()
	normalized.SetUpstreamResponse(header, body)
	return normalized, source
}
