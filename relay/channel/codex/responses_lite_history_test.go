package codex

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestResponsesLitePreservesOrderedToolHistory(t *testing.T) {
	for _, later := range []string{
		`[{"type":"function","name":"write_file"}]`,
		`[{"type":"function","name":"read_file","parameters":{"type":"object","properties":{"changed":{"type":"boolean"}}}}]`,
		`[{"type":"function","name":"write_file"},{"type":"function","name":"read_file"}]`,
		`[{"type":"function","name":"read_file"},{"type":"function","name":"write_file"}]`,
	} {
		body := []byte(`{"input":[
		 {"type":"message","role":"user","content":"First turn"},
		 {"type":"additional_tools","id":"tools_1","role":"developer","tools":[{"type":"namespace","name":"functions","tools":[{"type":"function","name":"read_file"},{"type":"function","name":"write_file"}]}]},
		 {"type":"message","role":"assistant","content":"Ready"},
		 {"type":"additional_tools","id":"tools_2","role":"developer","extension":{"number":9007199254740993,"flag":false},"tools":[{"type":"namespace","name":"functions","tools":` + later + `}]}
		]}`)
		got, _, diagnostics, err := NormalizeResponsesLitePayloadWithDiagnostics(body)
		require.NoError(t, err)
		require.Empty(t, diagnostics.FallbackReason)
		require.Equal(t, 2, diagnostics.AdditionalToolItems)
		// Compare raw JSON, not float64-decoded values: schema integers must
		// remain exact even when another root field needs normalization.
		require.Equal(t, gjson.GetBytes(body, "input").Array()[3].Get("extension.number").Raw, gjson.GetBytes(got, "input.3.extension.number").Raw)
		require.JSONEq(t, gjson.GetBytes(body, "input").Raw, gjson.GetBytes(got, "input").Raw)
		second, changed, err := NormalizeResponsesLitePayload(got)
		require.NoError(t, err)
		require.False(t, changed)
		require.Equal(t, got, second)
	}
}

func TestResponsesLiteToolFallbackIsAtomic(t *testing.T) {
	for _, tools := range []string{
		`{}`, `42`,
		`[{"type":"function","name":"known"},{"type":"future_tool","version":9007199254740993}]`,
		`[{"type":"web_search_preview"}]`, `[null]`, `[""]`, `[{}]`,
	} {
		body := []byte(`{"input":[{"type":"message","role":"user","content":"hello"}],"tools":` + tools + `,"custom_zero":0,"custom_false":false}`)
		got, _, diagnostics, err := NormalizeResponsesLitePayloadWithDiagnostics(body)
		require.NoError(t, err)
		require.NotEmpty(t, diagnostics.FallbackReason)
		require.Equal(t, gjson.GetBytes(body, "tools").Raw, gjson.GetBytes(got, "tools").Raw)
		require.JSONEq(t, gjson.GetBytes(body, "input").Raw, gjson.GetBytes(got, "input").Raw)
		require.Equal(t, "all_turns", gjson.GetBytes(got, "reasoning.context").String())
		require.Equal(t, "false", gjson.GetBytes(got, "parallel_tool_calls").Raw)
		require.Equal(t, "false", gjson.GetBytes(got, "store").Raw)
		require.Equal(t, "0", gjson.GetBytes(got, "custom_zero").Raw)
		require.Equal(t, "false", gjson.GetBytes(got, "custom_false").Raw)
	}
}

func TestResponsesLiteMigrationPreservesNumbersAndInstructions(t *testing.T) {
	body := []byte(`{"instructions":"Keep instructions","input":[{"type":"message","role":"user","content":"hello"},{"type":"additional_tools","role":"developer","tools":[{"type":"namespace","name":"functions","tools":[{"type":"function","name":"later"}]}]}],"tools":[{"type":"namespace","name":"functions","tools":[{"type":"function","name":"first","parameters":{"type":"object","properties":{"id":{"const":9007199254740993}}},"strict":false}]}],"extension":9007199254740993}`)
	got, _, err := NormalizeResponsesLitePayload(body)
	require.NoError(t, err)
	require.False(t, gjson.GetBytes(got, "tools").Exists())
	require.Equal(t, "9007199254740993", gjson.GetBytes(got, "input.0.tools.0.tools.0.parameters.properties.id.const").Raw)
	require.Equal(t, "9007199254740993", gjson.GetBytes(got, "extension").Raw)
	require.Equal(t, "Keep instructions", gjson.GetBytes(got, "input.1.content.0.text").String())
	require.Equal(t, "user", gjson.GetBytes(got, "input.2.role").String())
	require.JSONEq(t, gjson.GetBytes(body, "input.1").Raw, gjson.GetBytes(got, "input.3").Raw)
	second, changed, err := NormalizeResponsesLitePayload(got)
	require.NoError(t, err)
	require.False(t, changed)
	require.Equal(t, got, second)
}

func TestResponsesLiteExistingUnknownDeclarationsArePreserved(t *testing.T) {
	body := []byte(`{"input":[{"type":"message","role":"user","content":"hello"},{"type":"additional_tools","role":"developer","tools":{"future_shape":true}}]}`)
	got, _, err := NormalizeResponsesLitePayload(body)
	require.NoError(t, err)
	require.JSONEq(t, gjson.GetBytes(body, "input").Raw, gjson.GetBytes(got, "input").Raw)
}

func TestResponsesLiteFailureReturnsOriginalBody(t *testing.T) {
	for _, body := range []string{`{"tools":[{"type":"function","name":"a"}],"instructions":42}`, `{"reasoning":42}`, `[]`, `null`, `{`} {
		got, changed, err := NormalizeResponsesLitePayload([]byte(body))
		require.Error(t, err)
		require.False(t, changed)
		require.Equal(t, body, string(got))
	}
}

func TestResponsesLiteToolPassthroughEscapeHatch(t *testing.T) {
	t.Setenv("RESPONSES_LITE_TOOLS_PASSTHROUGH", "true")
	body := []byte(`{"tools":[{"type":"function","name":"a"}],"input":"hello"}`)
	got, _, diagnostics, err := NormalizeResponsesLitePayloadWithDiagnostics(body)
	require.NoError(t, err)
	require.Equal(t, "tools_passthrough_enabled", diagnostics.FallbackReason)
	require.Equal(t, gjson.GetBytes(body, "input").Raw, gjson.GetBytes(got, "input").Raw)
	require.Equal(t, gjson.GetBytes(body, "tools").Raw, gjson.GetBytes(got, "tools").Raw)
	require.Equal(t, "false", gjson.GetBytes(got, "store").Raw)
}
