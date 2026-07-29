package service

import (
	"errors"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestUpstreamRateLimitStateUsesOpenAIResponseBodyFallback(t *testing.T) {
	apiErr := types.NewErrorWithStatusCode(errors.New("rate limited"), types.ErrorCodeBadResponseStatusCode, http.StatusTooManyRequests)
	apiErr.SetUpstreamResponse(nil, []byte(`{"error":{"type":"usage_limit_reached","resets_at":2000}}`))
	resetAt, windowStart, windowEnd := upstreamRateLimitState(apiErr, 1000)
	require.EqualValues(t, 2000, resetAt)
	require.Nil(t, windowStart)
	require.Nil(t, windowEnd)
}

func TestUpstreamRateLimitStateUsesRetryAfter(t *testing.T) {
	apiErr := types.NewErrorWithStatusCode(errors.New("rate limited"), types.ErrorCodeBadResponseStatusCode, http.StatusTooManyRequests)
	header := http.Header{}
	header.Set("Retry-After", "90")
	apiErr.SetUpstreamResponse(header, nil)
	resetAt, _, _ := upstreamRateLimitState(apiErr, 1000)
	require.EqualValues(t, 1090, resetAt)
}
