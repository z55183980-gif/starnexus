package codex

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestResponsesLiteSignals(t *testing.T) {
	t.Parallel()
	require.True(t, IsResponsesLiteHeader(" true "))
	require.True(t, IsResponsesLiteHeader("TRUE"))
	require.False(t, IsResponsesLiteHeader("false"))
	require.True(t, IsResponsesLiteWebSocketPayload([]byte(`{
		"client_metadata":{"ws_request_header_x_openai_internal_codex_responses_lite":"true"}
	}`)))
	require.False(t, IsResponsesLiteWebSocketPayload([]byte(`{"client_metadata":{}}`)))
}

func TestNormalizeResponsesLitePayloadEnsuresReasoningContract(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name string
		body string
	}{
		{name: "missing reasoning", body: `{"model":"gpt-5","input":"hello"}`},
		{name: "missing context", body: `{"model":"gpt-5","input":"hello","reasoning":{"effort":"high"}}`},
		{name: "wrong context", body: `{"model":"gpt-5","input":"hello","reasoning":{"effort":"medium","context":"current_turn"}}`},
	}
	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			normalized, changed, err := NormalizeResponsesLitePayload([]byte(testCase.body))
			require.NoError(t, err)
			require.True(t, changed)
			require.Equal(t, "all_turns", gjson.GetBytes(normalized, "reasoning.context").String())
			require.False(t, gjson.GetBytes(normalized, "parallel_tool_calls").Bool())
			require.Equal(t, "reasoning.encrypted_content", gjson.GetBytes(normalized, "include.0").String())
			if effort := gjson.Get(testCase.body, "reasoning.effort").String(); effort != "" {
				require.Equal(t, effort, gjson.GetBytes(normalized, "reasoning.effort").String())
			}
		})
	}
}

func TestNormalizeResponsesLitePayloadMovesNamespaceTools(t *testing.T) {
	t.Parallel()
	body := []byte(`{
		"model":"gpt-5.6-sol",
		"input":[
			{"type":"message","role":"user","content":"hello"},
			{"type":"additional_tools","role":"developer","tools":[{"type":"custom","name":"exec"}]}
		],
		"reasoning":{"effort":"max","context":"current_turn"},
		"tools":[
			{"type":"function","name":"shell","parameters":{"type":"object"}},
			{"type":"custom","name":"apply_patch"},
			{"type":"tool_search"},
			{"type":"namespace","name":"collaboration","tools":[{"type":"function","name":"spawn_agent"}]}
		]
	}`)

	normalized, changed, err := NormalizeResponsesLitePayload(body)
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, "all_turns", gjson.GetBytes(normalized, "reasoning.context").String())
	require.False(t, gjson.GetBytes(normalized, `tools.#(type=="namespace")`).Exists())
	require.Equal(t, "shell", gjson.GetBytes(normalized, `tools.#(type=="function").name`).String())
	require.Equal(t, "apply_patch", gjson.GetBytes(normalized, `tools.#(type=="custom").name`).String())
	require.True(t, gjson.GetBytes(normalized, `tools.#(type=="tool_search")`).Exists())
	require.Equal(t, "exec", gjson.GetBytes(normalized, `input.#(type=="additional_tools").tools.0.name`).String())
	require.Equal(t, "collaboration", gjson.GetBytes(normalized, `input.#(type=="additional_tools").tools.1.name`).String())

	second, changedAgain, err := NormalizeResponsesLitePayload(normalized)
	require.NoError(t, err)
	require.False(t, changedAgain)
	require.JSONEq(t, string(normalized), string(second))
}

func TestNormalizeResponsesLitePayloadForcesParallelToolCallsFalse(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"true", "false", "null"} {
		body := []byte(`{"model":"gpt-5.6-sol","input":"hello","parallel_tool_calls":` + value + `}`)
		normalized, changed, err := NormalizeResponsesLitePayload(body)
		require.NoError(t, err)
		require.True(t, changed)
		require.False(t, gjson.GetBytes(normalized, "parallel_tool_calls").Bool())
	}
}

func TestNormalizeResponsesLitePayloadRejectsUnsupportedToolShapes(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name string
		body string
		want string
	}{
		{name: "non array", body: `{"input":"hello","tools":{}}`, want: "tools to be an array"},
		{name: "unsupported hosted tool", body: `{"input":"hello","tools":[{"type":"web_search_preview"}]}`, want: `does not support top-level tool type "web_search_preview"`},
		{name: "non object reasoning", body: `{"input":"hello","reasoning":"high"}`, want: "reasoning to be an object"},
	}
	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := NormalizeResponsesLitePayload([]byte(testCase.body))
			require.ErrorContains(t, err, testCase.want)
		})
	}
}

func TestNormalizeResponsesLitePayloadRejectsConflictingAdditionalTools(t *testing.T) {
	t.Parallel()
	body := []byte(`{
		"input":[{"type":"additional_tools","tools":[{"type":"namespace","name":"collaboration","description":"old"}]}],
		"tools":[{"type":"namespace","name":"collaboration","description":"new"}]
	}`)
	_, _, err := NormalizeResponsesLitePayload(body)
	require.ErrorContains(t, err, "conflicts with migrated")
}
