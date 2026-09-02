package codex

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestNormalizeCodexResponsesRequestMatchesOAuthSchema(t *testing.T) {
	t.Parallel()
	topP := 0.8
	request := dto.OpenAIResponsesRequest{
		Input:                []byte(`"hello"`),
		Metadata:             []byte(`{"trace":"client"}`),
		PromptCacheRetention: []byte(`"24h"`),
		PromptCacheOptions:   []byte(`{"mode":"explicit"}`),
		SafetyIdentifier:     []byte(`"user-1"`),
		StreamOptions:        &dto.StreamOptions{},
		TopP:                 &topP,
		User:                 []byte(`"user-1"`),
		Reasoning:            &dto.Reasoning{},
	}
	require.NoError(t, normalizeCodexResponsesRequest(&request))
	require.Nil(t, request.Metadata)
	require.Nil(t, request.PromptCacheRetention)
	require.Nil(t, request.PromptCacheOptions)
	require.Nil(t, request.SafetyIdentifier)
	require.Nil(t, request.StreamOptions)
	require.Nil(t, request.TopP)
	require.Nil(t, request.User)
	require.True(t, gjson.GetBytes(request.Input, "#").Int() == 1)
	require.Equal(t, "hello", gjson.GetBytes(request.Input, "0.content").String())

	var include []string
	require.NoError(t, common.Unmarshal(request.Include, &include))
	require.Contains(t, include, "reasoning.encrypted_content")
}

func TestNormalizeCodexResponsesInputDropsOutputStatusFromReplayItems(t *testing.T) {
	t.Parallel()
	request := dto.OpenAIResponsesRequest{Input: json.RawMessage(`[
		{"type":"message","role":"assistant","status":"completed","content":"hello"},
		{"type":"function_call_output","call_id":"call_1","status":"completed","output":{"status":"keep"}}
	]`)}

	require.NoError(t, normalizeCodexResponsesRequest(&request))
	require.False(t, gjson.GetBytes(request.Input, "0.status").Exists())
	require.False(t, gjson.GetBytes(request.Input, "1.status").Exists())
	require.Equal(t, "keep", gjson.GetBytes(request.Input, "1.output.status").String())
}

func TestRepairAccountPassthroughResponsesBodyDropsOutputStatus(t *testing.T) {
	t.Parallel()
	body := []byte(`{
		"input":[
			{"type":"message","role":"assistant","status":"completed","content":"hello"},
			{"type":"function_call_output","call_id":"call_1","status":"completed","output":{"status":"keep"}}
		],
		"stream":true,
		"custom_zero":0,
		"custom_false":false
	}`)

	repaired, err := RepairAccountPassthroughResponsesBody(nil, body)
	require.NoError(t, err)
	require.False(t, gjson.GetBytes(repaired, "input.0.status").Exists())
	require.False(t, gjson.GetBytes(repaired, "input.1.status").Exists())
	require.Equal(t, "keep", gjson.GetBytes(repaired, "input.1.output.status").String())
	require.True(t, gjson.GetBytes(repaired, "stream").Bool())
	require.True(t, gjson.GetBytes(repaired, "custom_zero").Exists())
	require.Zero(t, gjson.GetBytes(repaired, "custom_zero").Int())
	require.True(t, gjson.GetBytes(repaired, "custom_false").Exists())
	require.False(t, gjson.GetBytes(repaired, "custom_false").Bool())
}

func TestNormalizeCodexResponsesRequestCompactsAllOverlongCallIDs(t *testing.T) {
	t.Parallel()
	longFunctionID := "call_" + strings.Repeat("a", 80)
	longCustomID := "custom_" + strings.Repeat("b", 80)
	boundaryID := strings.Repeat("c", codexCallIDMaxLength)
	request := dto.OpenAIResponsesRequest{
		Input: json.RawMessage(`[
			{"type":"function_call","call_id":"` + longFunctionID + `","name":"lookup","arguments":"{}"},
			{"type":"function_call_output","call_id":"` + longFunctionID + `","output":{"call_id":"` + longFunctionID + `"}},
			{"type":"custom_tool_call","call_id":"` + longCustomID + `","name":"exec","input":"pwd"},
			{"type":"custom_tool_call_output","call_id":"` + longCustomID + `","output":"ok"},
			{"type":"function_call","call_id":"` + boundaryID + `","name":"keep","arguments":"{}"}
		]`),
	}

	require.NoError(t, normalizeCodexResponsesRequest(&request))
	functionCallID := gjson.GetBytes(request.Input, "0.call_id").String()
	customCallID := gjson.GetBytes(request.Input, "2.call_id").String()
	require.Len(t, functionCallID, codexCallIDMaxLength)
	require.Len(t, customCallID, codexCallIDMaxLength)
	require.True(t, strings.HasPrefix(functionCallID, codexCallIDPrefix))
	require.True(t, strings.HasPrefix(customCallID, codexCallIDPrefix))
	require.Equal(t, functionCallID, gjson.GetBytes(request.Input, "1.call_id").String())
	require.Equal(t, customCallID, gjson.GetBytes(request.Input, "3.call_id").String())
	require.Equal(t, boundaryID, gjson.GetBytes(request.Input, "4.call_id").String())
	require.Equal(t, longFunctionID, gjson.GetBytes(request.Input, "1.output.call_id").String())

	first := append(json.RawMessage(nil), request.Input...)
	require.NoError(t, normalizeCodexResponsesRequest(&request))
	require.JSONEq(t, string(first), string(request.Input))
}

func TestNormalizeCodexResponsesCallIDsIgnoresNonObjectInputAndShortIDs(t *testing.T) {
	t.Parallel()
	request := dto.OpenAIResponsesRequest{Input: json.RawMessage(`[
		"plain input",
		null,
		{"type":"message","role":"user","content":{"call_id":"` + strings.Repeat("x", 80) + `"}},
		{"type":"message","role":"user","call_id":9007199254740993,"content":"keep numeric metadata"},
		{"type":"function_call","call_id":"call_short","name":"lookup","arguments":"{}"}
	]`)}

	require.NoError(t, normalizeCodexResponsesRequest(&request))
	require.Equal(t, "plain input", gjson.GetBytes(request.Input, "0").String())
	require.Equal(t, strings.Repeat("x", 80), gjson.GetBytes(request.Input, "2.content.call_id").String())
	require.Equal(t, int64(9007199254740993), gjson.GetBytes(request.Input, "3.call_id").Int())
	require.Equal(t, "call_short", gjson.GetBytes(request.Input, "4.call_id").String())
}

func TestNormalizeCodexResponsesToolItemsPromotesNamesWithoutFabrication(t *testing.T) {
	t.Parallel()
	request := dto.OpenAIResponsesRequest{Input: json.RawMessage(`[
		{"type":"function_call","call_id":"call_1","function":{"name":"lookup"},"arguments":"{}"},
		{"type":"custom_tool_call","call_id":"call_2","tool_name":"exec","input":"pwd"},
		{"type":"mcp_tool_call","call_id":"call_3","name":"remote","arguments":"{}"}
	]`)}

	require.NoError(t, normalizeCodexResponsesRequest(&request))
	require.Equal(t, "lookup", gjson.GetBytes(request.Input, "0.name").String())
	require.Equal(t, "exec", gjson.GetBytes(request.Input, "1.name").String())
	require.Equal(t, "remote", gjson.GetBytes(request.Input, "2.name").String())

	invalid := dto.OpenAIResponsesRequest{Input: json.RawMessage(`[
		{"type":"function_call","call_id":"call_1","arguments":"{}"}
	]`)}
	err := normalizeCodexResponsesRequest(&invalid)
	require.Error(t, err)
	require.Contains(t, err.Error(), "input[0]")
	require.Contains(t, err.Error(), "missing name")
}

func TestNormalizeCodexResponsesRequestNormalizesEveryToolDeclaration(t *testing.T) {
	t.Parallel()
	request := dto.OpenAIResponsesRequest{Tools: json.RawMessage(`[
		{"type":"function","name":"canonical","parameters":{"type":"object"}},
		{"type":"function","function":{"name":"nested","description":"desc","parameters":{"type":"object"},"strict":false}},
		{"type":"custom","custom":{"name":"exec","description":"code","format":{"type":"text"}}},
		{"type":"web_search_preview"},
		{"type":"function","name":"f4"},
		{"type":"function","name":"f5"},
		{"type":"function","name":"f6"},
		{"type":"function","name":"f7"},
		{"type":"function","function":{"name":"ninth"}},
		{"type":"function","name":"tenth"}
	]`)}

	require.NoError(t, normalizeCodexResponsesRequest(&request))
	require.Equal(t, int64(10), gjson.GetBytes(request.Tools, "#").Int())
	require.Equal(t, "canonical", gjson.GetBytes(request.Tools, "0.name").String())
	require.Equal(t, "nested", gjson.GetBytes(request.Tools, "1.name").String())
	require.Equal(t, "desc", gjson.GetBytes(request.Tools, "1.description").String())
	require.False(t, gjson.GetBytes(request.Tools, "1.function").Exists())
	require.Equal(t, "exec", gjson.GetBytes(request.Tools, "2.name").String())
	require.Equal(t, "text", gjson.GetBytes(request.Tools, "2.format.type").String())
	require.False(t, gjson.GetBytes(request.Tools, "2.custom").Exists())
	require.Equal(t, "web_search_preview", gjson.GetBytes(request.Tools, "3.type").String())
	require.Equal(t, "ninth", gjson.GetBytes(request.Tools, "8.name").String())
}

func TestNormalizeCodexResponsesRequestRejectsUnnamedFunctionTool(t *testing.T) {
	t.Parallel()
	request := dto.OpenAIResponsesRequest{Tools: json.RawMessage(`[
		{"type":"function","name":"valid"},
		{"type":"function","function":{"parameters":{"type":"object"}}}
	]`)}

	err := normalizeCodexResponsesRequest(&request)
	require.Error(t, err)
	require.Contains(t, err.Error(), "tools[1]")
	require.Contains(t, err.Error(), "missing name")
}

func TestCodexAdaptorReturnsBadRequestForUnnamedFunctionTool(t *testing.T) {
	t.Parallel()
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	_, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(ctx, &relaycommon.RelayInfo{
		RelayMode:   relayconstant.RelayModeResponses,
		ChannelMeta: &relaycommon.ChannelMeta{},
	}, dto.OpenAIResponsesRequest{
		Input: json.RawMessage(`[{"type":"message","role":"user","content":"hello"}]`),
		Tools: json.RawMessage(`[{"type":"function","function":{"parameters":{"type":"object"}}}]`),
	})

	require.Error(t, err)
	var apiErr *types.NewAPIError
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
	require.True(t, types.IsSkipRetryError(apiErr))
}

func TestCodexChatCompatibilityNormalizesMixedToolsAndLongCallIDsEndToEnd(t *testing.T) {
	t.Parallel()
	longCallID := "toolu_" + strings.Repeat("z", 79)
	var chatRequest dto.GeneralOpenAIRequest
	require.NoError(t, common.UnmarshalJsonStr(`{
		"model":"gpt-5.6-sol",
		"messages":[
			{"role":"user","content":"use a tool"},
			{"role":"assistant","content":"","tool_calls":[{"id":"`+longCallID+`","type":"function","function":{"name":"ninth_tool","arguments":"{}"}}]},
			{"role":"tool","tool_call_id":"`+longCallID+`","content":"ok"}
		],
		"tools":[
			{"type":"function","function":{"name":"tool_0"}},
			{"type":"function","function":{"name":"tool_1"}},
			{"type":"function","function":{"name":"tool_2"}},
			{"type":"function","function":{"name":"tool_3"}},
			{"type":"function","function":{"name":"tool_4"}},
			{"type":"function","function":{"name":"tool_5"}},
			{"type":"function","function":{"name":"tool_6"}},
			{"type":"function","function":{"name":"tool_7"}},
			{"type":"custom","name":"ninth_tool","format":{"type":"text"}}
		]
	}`, &chatRequest))

	responsesRequest, err := service.ChatCompletionsRequestToResponsesRequest(&chatRequest)
	require.NoError(t, err)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(ctx, &relaycommon.RelayInfo{
		RelayMode:   relayconstant.RelayModeResponses,
		ChannelMeta: &relaycommon.ChannelMeta{},
	}, *responsesRequest)
	require.NoError(t, err)
	request := converted.(dto.OpenAIResponsesRequest)

	require.Equal(t, int64(9), gjson.GetBytes(request.Tools, "#").Int())
	require.Equal(t, "ninth_tool", gjson.GetBytes(request.Tools, "8.name").String())
	require.Equal(t, "custom", gjson.GetBytes(request.Tools, "8.type").String())
	callID := gjson.GetBytes(request.Input, `#(type=="function_call").call_id`).String()
	outputCallID := gjson.GetBytes(request.Input, `#(type=="function_call_output").call_id`).String()
	require.Len(t, callID, codexCallIDMaxLength)
	require.Equal(t, callID, outputCallID)
}

func TestNormalizeCodexResponsesInputStripsCacheBreakpointAndEmptyQuality(t *testing.T) {
	t.Parallel()
	input := []byte(`[
		{
			"type":"message",
			"role":"user",
			"content":[
				{"type":"input_text","text":"stable prefix","prompt_cache_breakpoint":{"mode":"explicit"}},
				{"type":"input_image","image_url":"https://example.com/a.png","prompt_cache_breakpoint":{"mode":"explicit"}}
			]
		},
		{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hi"}],"quality":"","size":""},
		{"type":"function_call_output","call_id":"call_1","output":{"quality":"keep-me","prompt_cache_breakpoint":{"mode":"explicit"}}},
		{"type":"image_generation_call","id":"img_1","quality":"high","size":"1024x1024"}
	]`)
	request := dto.OpenAIResponsesRequest{Input: input, PromptCacheOptions: []byte(`{"mode":"explicit"}`)}
	require.NoError(t, normalizeCodexResponsesRequest(&request))
	require.Nil(t, request.PromptCacheOptions)

	require.False(t, gjson.GetBytes(request.Input, "0.content.0.prompt_cache_breakpoint").Exists())
	require.False(t, gjson.GetBytes(request.Input, "0.content.1.prompt_cache_breakpoint").Exists())
	require.Equal(t, "stable prefix", gjson.GetBytes(request.Input, "0.content.0.text").String())

	require.False(t, gjson.GetBytes(request.Input, "1.quality").Exists())
	require.False(t, gjson.GetBytes(request.Input, "1.size").Exists())

	// Nested business JSON must not be scrubbed recursively.
	require.Equal(t, "keep-me", gjson.GetBytes(request.Input, "2.output.quality").String())
	require.True(t, gjson.GetBytes(request.Input, "2.output.prompt_cache_breakpoint").Exists())

	require.Equal(t, "high", gjson.GetBytes(request.Input, "3.quality").String())
	require.Equal(t, "1024x1024", gjson.GetBytes(request.Input, "3.size").String())
}

func TestNormalizeCodexResponsesInputPreservesLargeIntegers(t *testing.T) {
	t.Parallel()
	input := []byte(`[
		{"type":"function_call_output","call_id":"call_1","output":{"id":9007199254740993}},
		{"type":"message","role":"assistant","quality":"","content":[{"type":"output_text","text":"hi","metadata":{"id":9007199254740995}}],"metadata":{"id":9007199254740997}}
	]`)

	normalized, err := normalizeCodexResponsesInput(input)
	require.NoError(t, err)
	require.Contains(t, string(normalized), `"id":9007199254740993`)
	require.Contains(t, string(normalized), `"id":9007199254740995`)
	require.Contains(t, string(normalized), `"id":9007199254740997`)
	require.False(t, gjson.GetBytes(normalized, "1.quality").Exists())
}

func TestCodexStructuredOutputAddsJSONModeInstructionOnlyWhenMissing(t *testing.T) {
	t.Parallel()

	t.Run("missing JSON context", func(t *testing.T) {
		t.Parallel()
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		request := dto.OpenAIResponsesRequest{
			Instructions: json.RawMessage(`"Be concise."`),
			Input:        json.RawMessage(`[{"role":"user","content":"Summarize this."}]`),
			Text:         json.RawMessage(`{"format":{"type":"json_object"}}`),
		}

		applyCodexStructuredOutputCompatibility(ctx, &request)

		require.Equal(t, "Be concise.", gjson.ParseBytes(request.Instructions).String())
		require.Equal(t, codexJSONModeInstruction, gjson.GetBytes(request.Input, "1.content.0.text").String())
		value, exists := ctx.Get("codex_structured_output_compat")
		require.True(t, exists)
		require.True(t, value.(map[string]interface{})["json_instruction_added"].(bool))
	})

	t.Run("existing JSON context", func(t *testing.T) {
		t.Parallel()
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		request := dto.OpenAIResponsesRequest{
			Instructions: json.RawMessage(`"Be concise."`),
			Input:        json.RawMessage(`[{"role":"user","content":"Return JSON matching the requested shape."}]`),
			Text:         json.RawMessage(`{"format":{"type":"json_object"}}`),
		}
		original := append(json.RawMessage(nil), request.Input...)

		applyCodexStructuredOutputCompatibility(ctx, &request)

		require.Equal(t, original, request.Input)
		_, exists := ctx.Get("codex_structured_output_compat")
		require.False(t, exists)
	})

	t.Run("string input", func(t *testing.T) {
		t.Parallel()
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		request := dto.OpenAIResponsesRequest{
			Input: json.RawMessage(`"Summarize this."`),
			Text:  json.RawMessage(`{"format":{"type":"json_object"}}`),
		}

		applyCodexStructuredOutputCompatibility(ctx, &request)

		require.Equal(t, "Summarize this.\n\n"+codexJSONModeInstruction, gjson.ParseBytes(request.Input).String())
	})
}

func TestCodexStructuredOutputNormalizesStrictSchemaWithoutReorderingProperties(t *testing.T) {
	t.Parallel()
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	request := dto.OpenAIResponsesRequest{
		Text: json.RawMessage(`{"format":{"type":"json_schema","name":"response","strict":true,"schema":{"type":"object","properties":{"zeta":{"type":"string"},"alpha":{"type":"object","properties":{"summaryFieldName":{"type":"string"}}},"items":{"type":"array","items":{"type":"object","properties":{"item.value":{"type":"integer"}}}}},"required":["zeta"],"$defs":{"child.node":{"type":"object","properties":{"enabled":{"type":"boolean"}}}}}}}`),
	}

	applyCodexStructuredOutputCompatibility(ctx, &request)

	schema := gjson.GetBytes(request.Text, "format.schema")
	require.Less(t, strings.Index(schema.Raw, `"zeta"`), strings.Index(schema.Raw, `"alpha"`))
	require.Less(t, strings.Index(schema.Raw, `"alpha"`), strings.Index(schema.Raw, `"items"`))
	require.Equal(t, []string{"zeta", "alpha", "items"}, gjsonStringArray(schema.Get("required")))
	require.False(t, schema.Get("additionalProperties").Bool())
	require.Equal(t, []string{"summaryFieldName"}, gjsonStringArray(schema.Get("properties.alpha.required")))
	require.False(t, schema.Get("properties.alpha.additionalProperties").Bool())
	require.Equal(t, []string{"item.value"}, gjsonStringArray(schema.Get(`properties.items.items.required`)))
	require.False(t, schema.Get(`properties.items.items.additionalProperties`).Bool())
	require.Equal(t, []string{"enabled"}, gjsonStringArray(schema.Get(`$defs.child\.node.required`)))
	require.False(t, schema.Get(`$defs.child\.node.additionalProperties`).Bool())

	value, exists := ctx.Get("codex_structured_output_compat")
	require.True(t, exists)
	info := value.(map[string]interface{})
	require.Equal(t, 5, info["required_fields_added"])
	require.Equal(t, 4, info["additional_properties_added"])
}

func TestCodexStructuredOutputLeavesNonStrictAndConflictingSchemasUnchanged(t *testing.T) {
	t.Parallel()

	t.Run("strict false", func(t *testing.T) {
		t.Parallel()
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		request := dto.OpenAIResponsesRequest{Text: json.RawMessage(`{"format":{"type":"json_schema","strict":false,"schema":{"type":"object","properties":{"optional":{"type":"string"}}}}}`)}
		original := append(json.RawMessage(nil), request.Text...)

		applyCodexStructuredOutputCompatibility(ctx, &request)

		require.Equal(t, original, request.Text)
		_, exists := ctx.Get("codex_structured_output_compat")
		require.False(t, exists)
	})

	t.Run("explicit additional properties", func(t *testing.T) {
		t.Parallel()
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		request := dto.OpenAIResponsesRequest{Text: json.RawMessage(`{"format":{"type":"json_schema","strict":true,"schema":{"type":"object","properties":{"optional":{"type":"string"}},"additionalProperties":true}}}`)}
		original := append(json.RawMessage(nil), request.Text...)

		applyCodexStructuredOutputCompatibility(ctx, &request)

		require.Equal(t, original, request.Text)
		value, exists := ctx.Get("codex_structured_output_compat")
		require.True(t, exists)
		require.True(t, value.(map[string]interface{})["explicit_additional_properties_conflict"].(bool))
	})

	t.Run("root anyOf", func(t *testing.T) {
		t.Parallel()
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		request := dto.OpenAIResponsesRequest{Text: json.RawMessage(`{"format":{"type":"json_schema","strict":true,"schema":{"anyOf":[{"type":"object","properties":{"value":{"type":"string"}}}]}}}`)}
		original := append(json.RawMessage(nil), request.Text...)

		applyCodexStructuredOutputCompatibility(ctx, &request)

		require.Equal(t, original, request.Text)
		_, exists := ctx.Get("codex_structured_output_compat")
		require.False(t, exists)
	})
}

func TestCodexStructuredOutputCompatibilityCoversChatCompletionsConversion(t *testing.T) {
	t.Parallel()
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	chatRequest := &dto.GeneralOpenAIRequest{
		Model: "gpt-5.6-luna",
		Messages: []dto.Message{{
			Role:    "user",
			Content: "Summarize this.",
		}},
		ResponseFormat: &dto.ResponseFormat{Type: "json_object"},
	}
	responsesRequest, err := service.ChatCompletionsRequestToResponsesRequest(chatRequest)
	require.NoError(t, err)

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(ctx, &relaycommon.RelayInfo{
		RelayMode:   relayconstant.RelayModeResponses,
		ChannelMeta: &relaycommon.ChannelMeta{},
	}, *responsesRequest)
	require.NoError(t, err)
	request := converted.(dto.OpenAIResponsesRequest)
	require.Equal(t, codexJSONModeInstruction, gjson.GetBytes(request.Input, "1.content.0.text").String())
	require.Equal(t, "json_object", gjson.GetBytes(request.Text, "format.type").String())
}

func gjsonStringArray(result gjson.Result) []string {
	values := result.Array()
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, value.String())
	}
	return out
}

func TestRepairCodexInvalidLocalReasoningItemsIsTopLevelAndNarrow(t *testing.T) {
	t.Parallel()
	input := json.RawMessage(`[
		{"type":"message","role":"user","content":{"id":"item_nested","type":"reasoning"}},
		{"type":"reasoning","id":"rs_valid","encrypted_content":"valid"},
		{"type":"reasoning","id":"item_9ccf846e881144534dd7408d","encrypted_content":"invalid"},
		{"type":"message","id":"item_message","role":"assistant","content":"keep"},
		{"type":"function_call","id":"item_call","call_id":"call_1","name":"lookup","arguments":"{}"},
		{"type":"function_call_output","call_id":"call_1","output":{"id":"item_tool_payload"}}
	]`)

	repaired, result, err := repairCodexInvalidLocalItemIDs(input)
	require.NoError(t, err)
	require.Equal(t, 1, result.DroppedReasoningItems)
	require.Zero(t, result.DroppedItemReferences)
	require.Equal(t, 1, result.RemovedMessageIDs)
	require.Equal(t, 2, result.FirstDroppedIndex)
	require.Equal(t, 3, result.FirstRemovedIndex)
	require.Equal(t, 5, result.RemainingItems)
	require.False(t, result.HasOrphanToolOutput)
	require.Equal(t, int64(5), gjson.GetBytes(repaired, "#").Int())
	require.Equal(t, "item_nested", gjson.GetBytes(repaired, "0.content.id").String())
	require.Equal(t, "rs_valid", gjson.GetBytes(repaired, "1.id").String())
	require.False(t, gjson.GetBytes(repaired, "2.id").Exists())
	require.Equal(t, "keep", gjson.GetBytes(repaired, "2.content").String())
	require.Equal(t, "item_call", gjson.GetBytes(repaired, "3.id").String())
	require.Equal(t, "item_tool_payload", gjson.GetBytes(repaired, "4.output.id").String())
}

func TestRepairCodexMessageIDsKeepsValidPrefixAndArrayShape(t *testing.T) {
	t.Parallel()
	input := json.RawMessage(`[
		{"type":"message","id":"item_invalid","role":"assistant","content":"strip id only"},
		{"type":"message","id":"msg_valid","role":"assistant","content":"keep id"},
		{"type":"function_call","id":"item_call","call_id":"call_1","name":"lookup","arguments":"{}"}
	]`)

	repaired, result, err := repairCodexInvalidLocalItemIDs(input)
	require.NoError(t, err)
	require.Equal(t, 1, result.RemovedMessageIDs)
	require.Equal(t, int64(3), gjson.GetBytes(repaired, "#").Int())
	require.False(t, gjson.GetBytes(repaired, "0.id").Exists())
	require.Equal(t, "strip id only", gjson.GetBytes(repaired, "0.content").String())
	require.Equal(t, "msg_valid", gjson.GetBytes(repaired, "1.id").String())
	require.Equal(t, "item_call", gjson.GetBytes(repaired, "2.id").String())
}

func TestCodexResponsesRepairsInvalidItemReferenceWithPairedToolContext(t *testing.T) {
	t.Parallel()
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(ctx, &relaycommon.RelayInfo{
		RelayMode:   relayconstant.RelayModeResponses,
		ChannelMeta: &relaycommon.ChannelMeta{},
	}, dto.OpenAIResponsesRequest{
		Model: "gpt-5.6-sol",
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":"continue"},
			{"type":"item_reference","id":"item_local_reasoning"},
			{"type":"function_call","id":"fc_1","call_id":"call_1","name":"lookup","arguments":"{}"},
			{"type":"function_call_output","call_id":"call_1","output":"ok"}
		]`),
	})
	require.NoError(t, err)
	request := converted.(dto.OpenAIResponsesRequest)
	require.Equal(t, int64(3), gjson.GetBytes(request.Input, "#").Int())
	require.False(t, gjson.GetBytes(request.Input, `#(type=="item_reference")`).Exists())
	require.True(t, gjson.GetBytes(request.Input, `#(id=="fc_1")`).Exists())
	value, exists := ctx.Get("codex_input_repair_admin_info")
	require.True(t, exists)
	require.Equal(t, 1, value.(map[string]interface{})["dropped_item_references"])
}

func TestCodexResponsesRejectsRepairThatWouldOrphanToolOutput(t *testing.T) {
	t.Parallel()
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	_, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(ctx, &relaycommon.RelayInfo{
		RelayMode:   relayconstant.RelayModeResponses,
		ChannelMeta: &relaycommon.ChannelMeta{},
	}, dto.OpenAIResponsesRequest{
		Model: "gpt-5.6-sol",
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":"continue"},
			{"type":"item_reference","id":"item_local_call"},
			{"type":"function_call_output","call_id":"call_1","output":"ok"}
		]`),
	})
	require.Error(t, err)
	var apiErr *types.NewAPIError
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, http.StatusConflict, apiErr.StatusCode)
	require.True(t, types.IsSkipRetryError(apiErr))
}

func TestCodexResponsesRejectsInputContainingOnlyInvalidLocalReasoning(t *testing.T) {
	t.Parallel()
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	_, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(ctx, &relaycommon.RelayInfo{
		RelayMode:   relayconstant.RelayModeResponses,
		ChannelMeta: &relaycommon.ChannelMeta{},
	}, dto.OpenAIResponsesRequest{
		Model: "gpt-5.6-sol",
		Input: json.RawMessage(`{"type":"reasoning","id":"item_local_reasoning","encrypted_content":"invalid"}`),
	})
	require.Error(t, err)
	var apiErr *types.NewAPIError
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
	require.True(t, types.IsSkipRetryError(apiErr))
}

func TestCodexCompactDoesNotApplyOrdinaryResponsesHistoryRepair(t *testing.T) {
	t.Parallel()
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(ctx, &relaycommon.RelayInfo{
		RelayMode:   relayconstant.RelayModeResponsesCompact,
		ChannelMeta: &relaycommon.ChannelMeta{},
	}, dto.OpenAIResponsesRequest{
		Model: "gpt-5.6-sol",
		Input: json.RawMessage(`[
			{"type":"reasoning","id":"item_compact_owned"},
			{"type":"message","role":"user","content":"compact"}
		]`),
	})
	require.NoError(t, err)
	request := converted.(dto.OpenAIResponsesRequest)
	require.Equal(t, "item_compact_owned", gjson.GetBytes(request.Input, "0.id").String())
	_, exists := ctx.Get("codex_input_repair_admin_info")
	require.False(t, exists)
}

func TestNormalizeCodexResponsesPromotesSystemMessages(t *testing.T) {
	t.Parallel()
	request := dto.OpenAIResponsesRequest{
		Instructions: []byte(`"Existing."`),
		Input: []byte(`[
			{"type":"message","role":"system","content":"First."},
			{"type":"message","role":"system","content":[
				{"type":"input_text","text":"Second "},
				{"type":"output_text","text":"and third."},
				{"type":"input_image","image_url":"https://example.com/a.png","metadata":{"id":9007199254740993}}
			]},
			{"type":"message","role":"user","content":"hello"},
			{"type":"function_call_output","call_id":"call_1","output":{"role":"system","id":9007199254740995}}
		]`),
	}

	require.NoError(t, normalizeCodexResponsesRequest(&request))
	require.Equal(t, "First.\n\nSecond and third.\n\nExisting.", gjson.ParseBytes(request.Instructions).String())
	require.Equal(t, "developer", gjson.GetBytes(request.Input, "0.role").String())
	require.Equal(t, "developer", gjson.GetBytes(request.Input, "1.role").String())
	require.Equal(t, "user", gjson.GetBytes(request.Input, "2.role").String())
	require.Equal(t, "system", gjson.GetBytes(request.Input, "3.output.role").String())
	require.Contains(t, string(request.Input), `"id":9007199254740993`)
	require.Contains(t, string(request.Input), `"id":9007199254740995`)
}

func TestPromoteCodexSystemMessagesIsIdempotent(t *testing.T) {
	t.Parallel()
	request := dto.OpenAIResponsesRequest{
		Instructions: []byte(`"Existing."`),
		Input:        []byte(`[{"role":"system","content":"System."},{"role":"user","content":"hello"}]`),
	}
	require.NoError(t, promoteCodexSystemMessages(&request))
	firstInput := append([]byte(nil), request.Input...)
	firstInstructions := append([]byte(nil), request.Instructions...)
	require.NoError(t, promoteCodexSystemMessages(&request))
	require.JSONEq(t, string(firstInput), string(request.Input))
	require.JSONEq(t, string(firstInstructions), string(request.Instructions))
	require.Equal(t, 1, strings.Count(gjson.ParseBytes(request.Instructions).String(), "System."))
}

func TestCodexSystemPromptMergeModes(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name         string
		instructions json.RawMessage
		override     bool
		want         string
	}{
		{name: "missing client instructions keeps channel prefix", want: "Channel.\nClient."},
		{name: "client instructions without override", instructions: json.RawMessage(`"Existing."`), want: "Client.\n\nExisting."},
		{name: "client instructions with override", instructions: json.RawMessage(`"Existing."`), override: true, want: "Channel.\nClient.\n\nExisting."},
	}
	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(ctx, &relaycommon.RelayInfo{
				RelayMode: relayconstant.RelayModeResponses,
				ChannelMeta: &relaycommon.ChannelMeta{ChannelSetting: dto.ChannelSettings{
					SystemPrompt:         "Channel.",
					SystemPromptOverride: testCase.override,
				}},
			}, dto.OpenAIResponsesRequest{
				Model:        "gpt-5.6-sol",
				Instructions: testCase.instructions,
				Input:        []byte(`[{"role":"system","content":"Client."},{"role":"user","content":"hello"}]`),
			})
			require.NoError(t, err)
			request := converted.(dto.OpenAIResponsesRequest)
			require.Equal(t, testCase.want, gjson.ParseBytes(request.Instructions).String())
			require.Equal(t, "developer", gjson.GetBytes(request.Input, "0.role").String())
		})
	}
}

func TestCodexCompactPromotesSystemWithoutBroadNormalization(t *testing.T) {
	t.Parallel()
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	falseValue := false
	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(ctx, &relaycommon.RelayInfo{
		RelayMode:   relayconstant.RelayModeResponsesCompact,
		ChannelMeta: &relaycommon.ChannelMeta{},
	}, dto.OpenAIResponsesRequest{
		Model:    "gpt-5.6-sol",
		Input:    []byte(`{"role":"system","content":"Compact guidance.","metadata":{"id":9007199254740993}}`),
		Stream:   &falseValue,
		Metadata: []byte(`{"keep":"compact"}`),
	})
	require.NoError(t, err)
	request := converted.(dto.OpenAIResponsesRequest)
	require.Equal(t, byte('{'), bytes.TrimSpace(request.Input)[0])
	require.Equal(t, "developer", gjson.GetBytes(request.Input, "role").String())
	require.Equal(t, "Compact guidance.", gjson.ParseBytes(request.Instructions).String())
	require.Contains(t, string(request.Input), `"id":9007199254740993`)
	require.NotNil(t, request.Stream)
	require.False(t, *request.Stream)
	require.JSONEq(t, `{"keep":"compact"}`, string(request.Metadata))
}

func TestChatCompletionsSystemIsNotDuplicatedByCodexNormalization(t *testing.T) {
	t.Parallel()
	var chatRequest dto.GeneralOpenAIRequest
	require.NoError(t, common.UnmarshalJsonStr(`{
		"model":"gpt-5.6-sol",
		"messages":[
			{"role":"system","content":"Chat guidance."},
			{"role":"user","content":"hello"}
		]
	}`, &chatRequest))
	responsesRequest, err := service.ChatCompletionsRequestToResponsesRequest(&chatRequest)
	require.NoError(t, err)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(ctx, &relaycommon.RelayInfo{
		RelayMode:   relayconstant.RelayModeResponses,
		ChannelMeta: &relaycommon.ChannelMeta{},
	}, *responsesRequest)
	require.NoError(t, err)
	request := converted.(dto.OpenAIResponsesRequest)
	instructions := gjson.ParseBytes(request.Instructions).String()
	require.Equal(t, "Chat guidance.", instructions)
	require.Equal(t, 1, strings.Count(instructions, "Chat guidance."))
	require.False(t, gjson.GetBytes(request.Input, `#(role=="system")`).Exists())
}

func TestCodexResponsesForcesOnlyOrdinaryRequestsToStream(t *testing.T) {
	t.Parallel()
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	falseValue := false

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(ctx, &relaycommon.RelayInfo{
		RelayMode:   relayconstant.RelayModeResponses,
		ChannelMeta: &relaycommon.ChannelMeta{},
	}, dto.OpenAIResponsesRequest{Model: "gpt-5", Input: []byte(`[]`), Stream: &falseValue})
	require.NoError(t, err)
	request := converted.(dto.OpenAIResponsesRequest)
	require.NotNil(t, request.Stream)
	require.True(t, *request.Stream)

	converted, err = (&Adaptor{}).ConvertOpenAIResponsesRequest(ctx, &relaycommon.RelayInfo{
		RelayMode:   relayconstant.RelayModeResponsesCompact,
		ChannelMeta: &relaycommon.ChannelMeta{},
	}, dto.OpenAIResponsesRequest{Model: "gpt-5", Input: []byte(`[]`), Stream: &falseValue})
	require.NoError(t, err)
	compactRequest := converted.(dto.OpenAIResponsesRequest)
	require.NotNil(t, compactRequest.Stream)
	require.False(t, *compactRequest.Stream)
}

func TestIsolateCodexSessionHeaderByUserAndToken(t *testing.T) {
	t.Parallel()
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(ctx, constant.ContextKeyUserId, 7)
	common.SetContextKey(ctx, constant.ContextKeyTokenId, 11)

	first := isolateCodexSessionHeader(ctx, "client-session")
	require.NotEqual(t, "client-session", first)
	require.Equal(t, first, isolateCodexSessionHeader(ctx, "client-session"))

	common.SetContextKey(ctx, constant.ContextKeyTokenId, 12)
	require.NotEqual(t, first, isolateCodexSessionHeader(ctx, "client-session"))
}

func TestCodexPreviousResponseIDOnlyUsesNativeResponsesWebSocket(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name     string
		ingress  bool
		mode     string
		wantPrev bool
	}{
		{name: "ordinary HTTP", wantPrev: false},
		{name: "HTTP bridge", ingress: true, mode: model.UpstreamOpenAIWSModeHTTPBridge, wantPrev: false},
		{name: "context pool", ingress: true, mode: model.UpstreamOpenAIWSModeContextPool, wantPrev: true},
		{name: "passthrough", ingress: true, mode: model.UpstreamOpenAIWSModePassthrough, wantPrev: true},
	}
	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			if testCase.ingress {
				common.SetContextKey(ctx, constant.ContextKeyResponsesWebSocketIngress, true)
			}
			info := &relaycommon.RelayInfo{}
			info.ChannelMeta = &relaycommon.ChannelMeta{}
			info.ChannelOtherSettings.ResponsesWebSocketV2Mode = testCase.mode
			converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(ctx, info, dto.OpenAIResponsesRequest{
				Model: "gpt-5", Input: []byte(`[]`), PreviousResponseID: "resp_previous",
			})
			require.NoError(t, err)
			request, ok := converted.(dto.OpenAIResponsesRequest)
			require.True(t, ok)
			if testCase.wantPrev {
				require.Equal(t, "resp_previous", request.PreviousResponseID)
			} else {
				require.Empty(t, request.PreviousResponseID)
			}
		})
	}
}

func TestCodexNativeResponsesWebSocketRepairsOnlyInvalidLocalReasoning(t *testing.T) {
	t.Parallel()
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(ctx, constant.ContextKeyResponsesWebSocketIngress, true)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}
	info.ChannelOtherSettings.ResponsesWebSocketV2Mode = model.UpstreamOpenAIWSModePassthrough
	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(ctx, info, dto.OpenAIResponsesRequest{
		Model:              "gpt-5.6-sol",
		PreviousResponseID: "resp_previous",
		Input: json.RawMessage(`[
			{"type":"reasoning","id":"item_local_reasoning"},
			{"type":"reasoning","id":"rs_valid","encrypted_content":"valid"},
			{"type":"message","role":"user","content":"continue"}
		]`),
	})
	require.NoError(t, err)
	request := converted.(dto.OpenAIResponsesRequest)
	require.Equal(t, "resp_previous", request.PreviousResponseID)
	require.Equal(t, int64(2), gjson.GetBytes(request.Input, "#").Int())
	require.Equal(t, "rs_valid", gjson.GetBytes(request.Input, "0.id").String())
	require.Equal(t, "user", gjson.GetBytes(request.Input, "1.role").String())
}

func TestCodexHTTPExpandsGatewayContinuationBeforeDroppingPreviousResponseID(t *testing.T) {
	firstCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
	firstCtx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	common.SetContextKey(firstCtx, constant.ContextKeyUserId, 8201)
	common.SetContextKey(firstCtx, constant.ContextKeyTokenId, 9201)
	common.SetContextKey(firstCtx, constant.ContextKeyChannelId, 42)
	common.SetContextKey(firstCtx, constant.ContextKeyChannelType, constant.ChannelTypeCodex)
	common.SetContextKey(firstCtx, constant.ContextKeyUpstreamAccountPoolId, 3)
	common.SetContextKey(firstCtx, constant.ContextKeyUpstreamAccountId, 9)
	common.SetContextKey(firstCtx, constant.ContextKeyUpstreamAccountPlatform, constant.UpstreamPlatformOpenAI)
	common.SetContextKey(firstCtx, constant.ContextKeyUpstreamAccountType, constant.UpstreamAccountTypeOAuth)
	service.PrepareResponsesHTTPContinuation(firstCtx, &dto.OpenAIResponsesRequest{Model: "gpt-5", Input: []byte(`"lookup"`)})
	service.MarkResponsesHTTPContinuationPersistTarget(firstCtx)
	service.CommitResponsesHTTPContinuation(firstCtx, "resp_codex_http", []json.RawMessage{
		json.RawMessage(`{"type":"reasoning","id":"item_cached_local_reasoning","encrypted_content":"invalid"}`),
		json.RawMessage(`{"type":"function_call","id":"fc_1","call_id":"call_1","name":"lookup","arguments":"{}"}`),
	})

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	common.SetContextKey(ctx, constant.ContextKeyUserId, 8201)
	common.SetContextKey(ctx, constant.ContextKeyTokenId, 9201)
	common.SetContextKey(ctx, constant.ContextKeyChannelId, 42)
	common.SetContextKey(ctx, constant.ContextKeyChannelType, constant.ChannelTypeCodex)
	common.SetContextKey(ctx, constant.ContextKeyUpstreamAccountPoolId, 3)
	common.SetContextKey(ctx, constant.ContextKeyUpstreamAccountId, 9)
	common.SetContextKey(ctx, constant.ContextKeyUpstreamAccountPlatform, constant.UpstreamPlatformOpenAI)
	common.SetContextKey(ctx, constant.ContextKeyUpstreamAccountType, constant.UpstreamAccountTypeOAuth)
	request := dto.OpenAIResponsesRequest{
		Model:              "gpt-5",
		PreviousResponseID: "resp_codex_http",
		Input:              []byte(`[{"type":"function_call_output","call_id":"call_1","output":"ok"}]`),
	}
	service.PrepareResponsesHTTPContinuation(ctx, &request)
	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(ctx, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}, request)
	require.NoError(t, err)
	prepared := converted.(dto.OpenAIResponsesRequest)
	require.Empty(t, prepared.PreviousResponseID)
	require.Equal(t, int64(3), gjson.GetBytes(prepared.Input, "#").Int())
	require.False(t, gjson.GetBytes(prepared.Input, `#(id=="item_cached_local_reasoning")`).Exists())
}

func TestCodexHTTPDoesNotReplayProviderOwnedReasoningFromClientInput(t *testing.T) {
	t.Parallel()
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	request := dto.OpenAIResponsesRequest{
		Model: "gpt-5.6-sol",
		Input: json.RawMessage(`[
			{"type":"reasoning","id":"rs_client_stale","encrypted_content":"cipher"},
			{"type":"item_reference","id":"rs_client_stale"},
			{"type":"message","role":"user","content":"continue"}
		]`),
	}
	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(ctx, &relaycommon.RelayInfo{
		RelayMode:   relayconstant.RelayModeResponses,
		ChannelMeta: &relaycommon.ChannelMeta{},
	}, request)
	require.NoError(t, err)
	prepared := converted.(dto.OpenAIResponsesRequest)
	require.False(t, gjson.GetBytes(prepared.Input, `#(id=="rs_client_stale")`).Exists())
	require.Equal(t, "continue", gjson.GetBytes(prepared.Input, "0.content").String())
	canonicalInfo, exists := ctx.Get("responses_http_replay_canonicalization")
	require.True(t, exists)
	require.Equal(t, 1, canonicalInfo.(map[string]interface{})["reasoning_items_dropped"])
	require.Equal(t, 1, canonicalInfo.(map[string]interface{})["item_references_dropped"])
}

func TestCodexHTTPContinuationPromotesReplayedSystemOnce(t *testing.T) {
	firstCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
	firstCtx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	common.SetContextKey(firstCtx, constant.ContextKeyUserId, 8301)
	common.SetContextKey(firstCtx, constant.ContextKeyTokenId, 9301)
	common.SetContextKey(firstCtx, constant.ContextKeyChannelId, 43)
	common.SetContextKey(firstCtx, constant.ContextKeyChannelType, constant.ChannelTypeCodex)
	common.SetContextKey(firstCtx, constant.ContextKeyUpstreamAccountPoolId, 4)
	common.SetContextKey(firstCtx, constant.ContextKeyUpstreamAccountId, 10)
	common.SetContextKey(firstCtx, constant.ContextKeyUpstreamAccountPlatform, constant.UpstreamPlatformOpenAI)
	common.SetContextKey(firstCtx, constant.ContextKeyUpstreamAccountType, constant.UpstreamAccountTypeOAuth)
	service.PrepareResponsesHTTPContinuation(firstCtx, &dto.OpenAIResponsesRequest{
		Model: "gpt-5.6-sol",
		Input: []byte(`[
			{"role":"system","content":"Replayed guidance."},
			{"role":"user","content":"first turn"}
		]`),
	})
	service.MarkResponsesHTTPContinuationPersistTarget(firstCtx)
	service.CommitResponsesHTTPContinuation(firstCtx, "resp_codex_system", []json.RawMessage{
		json.RawMessage(`{"type":"message","role":"assistant","content":[{"type":"output_text","text":"first answer"}]}`),
	})

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	common.SetContextKey(ctx, constant.ContextKeyUserId, 8301)
	common.SetContextKey(ctx, constant.ContextKeyTokenId, 9301)
	common.SetContextKey(ctx, constant.ContextKeyChannelId, 43)
	common.SetContextKey(ctx, constant.ContextKeyChannelType, constant.ChannelTypeCodex)
	common.SetContextKey(ctx, constant.ContextKeyUpstreamAccountPoolId, 4)
	common.SetContextKey(ctx, constant.ContextKeyUpstreamAccountId, 10)
	common.SetContextKey(ctx, constant.ContextKeyUpstreamAccountPlatform, constant.UpstreamPlatformOpenAI)
	common.SetContextKey(ctx, constant.ContextKeyUpstreamAccountType, constant.UpstreamAccountTypeOAuth)
	request := dto.OpenAIResponsesRequest{
		Model:              "gpt-5.6-sol",
		PreviousResponseID: "resp_codex_system",
		Input:              []byte(`[{"role":"user","content":"second turn"}]`),
	}
	service.PrepareResponsesHTTPContinuation(ctx, &request)
	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(ctx, &relaycommon.RelayInfo{
		RelayMode:   relayconstant.RelayModeResponses,
		ChannelMeta: &relaycommon.ChannelMeta{},
	}, request)
	require.NoError(t, err)
	prepared := converted.(dto.OpenAIResponsesRequest)
	require.Empty(t, prepared.PreviousResponseID)
	require.Equal(t, "Replayed guidance.", gjson.ParseBytes(prepared.Instructions).String())
	require.Equal(t, 1, strings.Count(gjson.ParseBytes(prepared.Instructions).String(), "Replayed guidance."))
	require.Equal(t, "developer", gjson.GetBytes(prepared.Input, "0.role").String())
	require.Equal(t, "second turn", gjson.GetBytes(prepared.Input, "3.content").String())
}

func TestCodexHTTPRejectsOrphanFunctionCallOutput(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	common.SetContextKey(ctx, constant.ContextKeyUserId, 8202)
	common.SetContextKey(ctx, constant.ContextKeyTokenId, 9202)
	request := dto.OpenAIResponsesRequest{
		Model:              "gpt-5",
		PreviousResponseID: "resp_missing_codex_http",
		Input:              []byte(`[{"type":"function_call_output","call_id":"call_1","output":"ok"}]`),
	}
	service.PrepareResponsesHTTPContinuation(ctx, &request)
	_, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(ctx, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}, request)
	require.Error(t, err)
	var apiErr *types.NewAPIError
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, http.StatusConflict, apiErr.StatusCode)
}

func TestNormalizeCodexReasoningSummaryContent(t *testing.T) {
	t.Parallel()
	input := []byte(`[
		{"type":"message","role":"user","content":"keep"},
		{"type":"reasoning","summary":[{"type":"summary_text","text":"existing"}],"content":[
			{"type":"summary_text","text":"existing"},
			{"type":"summary_text","text":"new"},
			{"type":"reasoning_text","text":"private"}
		],"encrypted_content":"cipher"},
		{"type":"reasoning","content":null}
	]`)

	normalized, result, err := normalizeCodexReasoningSummaryContent(input)
	require.NoError(t, err)
	require.Equal(t, 2, result.MovedSummaryBlocks)
	require.Equal(t, 1, result.RemovedNullContent)
	require.Equal(t, int64(2), gjson.GetBytes(normalized, "1.summary.#").Int())
	require.Equal(t, "existing", gjson.GetBytes(normalized, "1.summary.0.text").String())
	require.Equal(t, "new", gjson.GetBytes(normalized, "1.summary.1.text").String())
	require.Equal(t, int64(1), gjson.GetBytes(normalized, "1.content.#").Int())
	require.Equal(t, "reasoning_text", gjson.GetBytes(normalized, "1.content.0.type").String())
	require.Equal(t, "cipher", gjson.GetBytes(normalized, "1.encrypted_content").String())
	require.False(t, gjson.GetBytes(normalized, "2.content").Exists())
	require.Equal(t, "keep", gjson.GetBytes(normalized, "0.content").String())
}

func TestNormalizeCodexReasoningSummaryContentDeletesContentWhenFullyMigrated(t *testing.T) {
	t.Parallel()
	input := []byte(`[{"type":"reasoning","content":[{"type":"summary_text","text":"thinking"}],"encrypted_content":"cipher"}]`)

	normalized, result, err := normalizeCodexReasoningSummaryContent(input)
	require.NoError(t, err)
	require.Equal(t, 1, result.MovedSummaryBlocks)
	require.False(t, gjson.GetBytes(normalized, "0.content").Exists())
	require.Equal(t, "thinking", gjson.GetBytes(normalized, "0.summary.0.text").String())
}

func TestApplyCodexReasoningContentRetry(t *testing.T) {
	t.Parallel()
	input := []byte(`[
		{"type":"message","role":"user","content":"keep"},
		{"type":"reasoning","content":[{"type":"reasoning_text","text":"private"}],"encrypted_content":"cipher"}
	]`)

	repaired, result, err := applyCodexReasoningContentRetry(input, 1, 1)
	require.NoError(t, err)
	require.Equal(t, 1, result.RemovedContentArrays)
	require.Zero(t, result.RemovedWithoutEncryptedContent)
	require.False(t, gjson.GetBytes(repaired, "1.content").Exists())
	require.Equal(t, "cipher", gjson.GetBytes(repaired, "1.encrypted_content").String())
	require.True(t, gjson.GetBytes(repaired, "1.summary").IsArray())
	require.Equal(t, "keep", gjson.GetBytes(repaired, "0.content").String())
}

func TestApplyCodexReasoningContentRetryWithoutEncryptedContent(t *testing.T) {
	t.Parallel()
	input := []byte(`[{"type":"reasoning","content":[{"type":"reasoning_text","text":"private"}],"summary":[{"type":"summary_text","text":"keep"}]}]`)

	repaired, result, err := applyCodexReasoningContentRetry(input, 0, 1)
	require.NoError(t, err)
	require.Equal(t, 1, result.RemovedContentArrays)
	require.Equal(t, 1, result.RemovedWithoutEncryptedContent)
	require.False(t, gjson.GetBytes(repaired, "0.content").Exists())
	require.Equal(t, "keep", gjson.GetBytes(repaired, "0.summary.0.text").String())
}

func TestApplyCodexReasoningContentRetryRepairsAllCompatibleHistoryItems(t *testing.T) {
	t.Parallel()
	input := []byte(`[
		{"type":"reasoning","content":[{"type":"reasoning_text","text":"first"}],"summary":[]},
		{"type":"message","role":"user","content":"keep"},
		{"type":"reasoning","content":[{"type":"reasoning_text","text":"second"}],"encrypted_content":"cipher"},
		{"type":"reasoning","content":[{"type":"unknown","text":"untouched"}],"encrypted_content":"cipher"}
	]`)

	repaired, result, err := applyCodexReasoningContentRetry(input, 0, 1)
	require.NoError(t, err)
	require.Equal(t, 2, result.RemovedContentArrays)
	require.Equal(t, 1, result.RemovedWithoutEncryptedContent)
	require.False(t, gjson.GetBytes(repaired, "0.content").Exists())
	require.False(t, gjson.GetBytes(repaired, "2.content").Exists())
	require.Equal(t, "keep", gjson.GetBytes(repaired, "1.content").String())
	require.Equal(t, "unknown", gjson.GetBytes(repaired, "3.content.0.type").String())
}

func TestApplyCodexReasoningContentRetryFailsClosed(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name  string
		input string
	}{
		{name: "unsupported block", input: `[{"type":"reasoning","content":[{"type":"unknown","text":"private"}],"encrypted_content":"cipher"}]`},
		{name: "wrong item type", input: `[{"type":"message","content":[{"type":"reasoning_text","text":"private"}],"encrypted_content":"cipher"}]`},
	}
	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := applyCodexReasoningContentRetry([]byte(testCase.input), 0, 1)
			require.Error(t, err)
		})
	}
}

func TestCodexOrdinaryHTTPNormalizesMisplacedReasoningSummary(t *testing.T) {
	t.Parallel()
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	request := dto.OpenAIResponsesRequest{
		Model: "gpt-5.6-sol",
		Input: []byte(`[{"type":"reasoning","content":[{"type":"summary_text","text":"thinking"}],"encrypted_content":"cipher"},{"role":"user","content":"continue"}]`),
	}

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(ctx, &relaycommon.RelayInfo{
		RelayMode:   relayconstant.RelayModeResponses,
		ChannelMeta: &relaycommon.ChannelMeta{},
	}, request)
	require.NoError(t, err)
	prepared := converted.(dto.OpenAIResponsesRequest)
	require.False(t, gjson.GetBytes(prepared.Input, "0.content").Exists())
	require.Equal(t, "thinking", gjson.GetBytes(prepared.Input, "0.summary.0.text").String())
	require.Equal(t, "cipher", gjson.GetBytes(prepared.Input, "0.encrypted_content").String())
}

func TestCodexReasoningContentRetryAppliesAfterOutboundNormalization(t *testing.T) {
	t.Parallel()
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	common.SetContextKey(ctx, constant.ContextKeyCodexReasoningContentRetryArmed, true)
	common.SetContextKey(ctx, constant.ContextKeyCodexReasoningContentRetryIndex, 1)
	common.SetContextKey(ctx, constant.ContextKeyCodexReasoningContentRetryLength, 1)
	request := dto.OpenAIResponsesRequest{
		Model: "gpt-5.6-sol",
		Input: []byte(`[{"role":"user","content":"continue"},{"type":"reasoning","content":[{"type":"reasoning_text","text":"private"}],"encrypted_content":"cipher"}]`),
	}

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(ctx, &relaycommon.RelayInfo{
		RelayMode:   relayconstant.RelayModeResponses,
		ChannelMeta: &relaycommon.ChannelMeta{},
	}, request)
	require.NoError(t, err)
	prepared := converted.(dto.OpenAIResponsesRequest)
	require.False(t, gjson.GetBytes(prepared.Input, "1.content").Exists())
	require.Equal(t, "cipher", gjson.GetBytes(prepared.Input, "1.encrypted_content").String())
	require.True(t, gjson.GetBytes(prepared.Input, "1.summary").IsArray())
}

func TestCodexWebSocketIngressPreservesReasoningContent(t *testing.T) {
	t.Parallel()
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	common.SetContextKey(ctx, constant.ContextKeyResponsesWebSocketIngress, true)
	request := dto.OpenAIResponsesRequest{
		Model: "gpt-5.6-sol",
		Input: []byte(`[{"type":"reasoning","content":[{"type":"summary_text","text":"keep websocket unchanged"}],"encrypted_content":"cipher"}]`),
	}

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(ctx, &relaycommon.RelayInfo{
		RelayMode:   relayconstant.RelayModeResponses,
		ChannelMeta: &relaycommon.ChannelMeta{},
	}, request)
	require.NoError(t, err)
	prepared := converted.(dto.OpenAIResponsesRequest)
	require.Equal(t, "summary_text", gjson.GetBytes(prepared.Input, "0.content.0.type").String())
	require.False(t, gjson.GetBytes(prepared.Input, "0.summary").Exists())
}
