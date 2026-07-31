package relay

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestApplyResponsesPassthroughAccountModel(t *testing.T) {
	original := []byte(`{"model":"gpt-5.6-sol","input":"hello","custom":{"keep":true}}`)

	mapped, changed, err := applyResponsesPassthroughAccountModel(original, "gpt-5.4-compact")
	require.NoError(t, err)
	require.True(t, changed)

	var payload map[string]any
	require.NoError(t, common.Unmarshal(mapped, &payload))
	require.Equal(t, "gpt-5.4-compact", payload["model"])
	require.Equal(t, "hello", payload["input"])
	require.Equal(t, map[string]any{"keep": true}, payload["custom"])
}

func TestApplyResponsesPassthroughAccountModelLeavesMatchingModelUntouched(t *testing.T) {
	original := []byte(`{"model":"gpt-5.4-compact","input":"hello"}`)

	mapped, changed, err := applyResponsesPassthroughAccountModel(original, "gpt-5.4-compact")
	require.NoError(t, err)
	require.False(t, changed)
	require.Equal(t, original, mapped)
}

func TestApplyResponsesPassthroughAccountModelPatchesWithoutLossyReencode(t *testing.T) {
	original := []byte("{\n  \"model\": \"gpt-client\",\n  \"large_integer\": 9007199254740993,\n  \"input\": \"hello\"\n}")

	mapped, changed, err := applyResponsesPassthroughAccountModel(original, "gpt-upstream")
	require.NoError(t, err)
	require.True(t, changed)
	require.Contains(t, string(mapped), `"large_integer": 9007199254740993`)
	require.Contains(t, string(mapped), `"input": "hello"`)
	require.Contains(t, string(mapped), `"model": "gpt-upstream"`)
}
