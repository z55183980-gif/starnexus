package relay

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/require"
)

func TestForceResponsesRequestStreamOverridesFalseAndPreservesFields(t *testing.T) {
	t.Parallel()

	data, err := forceResponsesRequestStream([]byte(`{"model":"gpt-5","stream":false,"store":false}`))
	require.NoError(t, err)
	var payload map[string]json.RawMessage
	require.NoError(t, common.Unmarshal(data, &payload))
	require.JSONEq(t, `true`, string(payload["stream"]))
	require.JSONEq(t, `"gpt-5"`, string(payload["model"]))
	require.JSONEq(t, `false`, string(payload["store"]))
}
