package controller

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseResponsesWSClientEvent(t *testing.T) {
	t.Parallel()

	event, request, envelope, apiErr := parseResponsesWSClientEvent([]byte(`{
		"type":"response.create",
		"model":"gpt-5",
		"input":"hello",
		"previous_response_id":"resp_1",
		"generate":false
	}`))
	require.Nil(t, apiErr)
	require.Equal(t, "response.create", event.Type)
	require.Equal(t, "gpt-5", request.Model)
	require.Equal(t, "resp_1", request.PreviousResponseID)
	require.Contains(t, envelope, "generate")
}

func TestBuildResponsesWSOutboundEvent(t *testing.T) {
	t.Parallel()

	_, _, original, apiErr := parseResponsesWSClientEvent([]byte(`{
		"type":"response.create",
		"model":"client-model",
		"previous_response_id":null,
		"generate":false,
		"background":true
	}`))
	require.Nil(t, apiErr)

	outbound, err := buildResponsesWSOutboundEvent(original, []byte(`{"model":"upstream-model","store":false}`))
	require.NoError(t, err)
	require.JSONEq(t, `{
		"type":"response.create",
		"model":"upstream-model",
		"store":false,
		"previous_response_id":null,
		"generate":false
	}`, string(outbound))
}
