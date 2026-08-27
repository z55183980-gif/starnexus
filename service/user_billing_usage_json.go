package service

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// RewriteOpenAIUsageJSON replaces only public numeric usage fields and keeps
// all unrelated upstream response fields intact. prefix is usually "usage" or
// "response.usage" for a Responses API stream event.
func RewriteOpenAIUsageJSON(data []byte, prefix string, usage *dto.Usage, responsesStyle bool) ([]byte, error) {
	if len(data) == 0 || prefix == "" || usage == nil {
		return data, nil
	}

	var err error
	set := func(path string, value int) {
		if err != nil {
			return
		}
		data, err = sjson.SetBytes(data, prefix+"."+path, value)
	}
	setIfPresent := func(path string, value int) {
		if gjson.GetBytes(data, prefix+"."+path).Exists() {
			set(path, value)
		}
	}

	if responsesStyle {
		set("input_tokens", usage.PromptTokens)
		set("output_tokens", usage.CompletionTokens)
		set("total_tokens", usage.TotalTokens)
		set("input_tokens_details.cached_tokens", usage.PromptTokensDetails.CachedTokens)
		setIfPresent("input_tokens_details.cached_creation_tokens", usage.PromptTokensDetails.CachedCreationTokens)
		setIfPresent("input_tokens_details.cache_write_tokens", usage.PromptTokensDetails.CacheWriteTokens)
		setIfPresent("input_tokens_details.text_tokens", usage.PromptTokensDetails.TextTokens)
		setIfPresent("input_tokens_details.audio_tokens", usage.PromptTokensDetails.AudioTokens)
		setIfPresent("input_tokens_details.image_tokens", usage.PromptTokensDetails.ImageTokens)
		setIfPresent("prompt_tokens", usage.PromptTokens)
		setIfPresent("completion_tokens", usage.CompletionTokens)
		setIfPresent("prompt_tokens_details.cached_tokens", usage.PromptTokensDetails.CachedTokens)
		setIfPresent("prompt_tokens_details.cached_creation_tokens", usage.PromptTokensDetails.CachedCreationTokens)
		setIfPresent("prompt_tokens_details.cache_write_tokens", usage.PromptTokensDetails.CacheWriteTokens)
		setIfPresent("prompt_tokens_details.text_tokens", usage.PromptTokensDetails.TextTokens)
		setIfPresent("prompt_tokens_details.audio_tokens", usage.PromptTokensDetails.AudioTokens)
		setIfPresent("prompt_tokens_details.image_tokens", usage.PromptTokensDetails.ImageTokens)
	} else {
		set("prompt_tokens", usage.PromptTokens)
		set("completion_tokens", usage.CompletionTokens)
		set("total_tokens", usage.TotalTokens)
		set("prompt_tokens_details.cached_tokens", usage.PromptTokensDetails.CachedTokens)
		setIfPresent("prompt_tokens_details.cached_creation_tokens", usage.PromptTokensDetails.CachedCreationTokens)
		setIfPresent("prompt_tokens_details.cache_write_tokens", usage.PromptTokensDetails.CacheWriteTokens)
		setIfPresent("prompt_tokens_details.text_tokens", usage.PromptTokensDetails.TextTokens)
		setIfPresent("prompt_tokens_details.audio_tokens", usage.PromptTokensDetails.AudioTokens)
		setIfPresent("prompt_tokens_details.image_tokens", usage.PromptTokensDetails.ImageTokens)
		setIfPresent("input_tokens", usage.PromptTokens)
		setIfPresent("output_tokens", usage.CompletionTokens)
		setIfPresent("input_tokens_details.cached_tokens", usage.PromptTokensDetails.CachedTokens)
		setIfPresent("input_tokens_details.cached_creation_tokens", usage.PromptTokensDetails.CachedCreationTokens)
		setIfPresent("input_tokens_details.cache_write_tokens", usage.PromptTokensDetails.CacheWriteTokens)
		setIfPresent("input_tokens_details.text_tokens", usage.PromptTokensDetails.TextTokens)
		setIfPresent("input_tokens_details.audio_tokens", usage.PromptTokensDetails.AudioTokens)
		setIfPresent("input_tokens_details.image_tokens", usage.PromptTokensDetails.ImageTokens)
		setIfPresent("prompt_cache_hit_tokens", usage.PromptTokensDetails.CachedTokens)
	}
	setIfPresent("completion_tokens_details.text_tokens", usage.CompletionTokenDetails.TextTokens)
	setIfPresent("completion_tokens_details.audio_tokens", usage.CompletionTokenDetails.AudioTokens)
	setIfPresent("completion_tokens_details.image_tokens", usage.CompletionTokenDetails.ImageTokens)
	setIfPresent("completion_tokens_details.reasoning_tokens", usage.CompletionTokenDetails.ReasoningTokens)
	setIfPresent("claude_cache_creation_5_m_tokens", usage.ClaudeCacheCreation5mTokens)
	setIfPresent("claude_cache_creation_1_h_tokens", usage.ClaudeCacheCreation1hTokens)

	// Some compatible upstreams add Anthropic-style aliases to OpenAI usage.
	// If they are present, rewrite them too so no raw cache count wins parser
	// precedence in clients such as CC Switch.
	setIfPresent("cache_read_input_tokens", usage.PromptTokensDetails.CachedTokens)
	setIfPresent("cache_creation_input_tokens", usage.PromptTokensDetails.CacheCreationTokensTotal())
	if err != nil {
		return nil, err
	}
	return stripInternalUsagePolicyFields(data, prefix)
}

// RewriteClaudeUsageJSON rewrites an Anthropic usage object at prefix without
// adding any internal billing metadata.
func RewriteClaudeUsageJSON(data []byte, prefix string, usage *dto.ClaudeUsage) ([]byte, error) {
	if len(data) == 0 || prefix == "" || usage == nil {
		return data, nil
	}

	var err error
	set := func(path string, value int) {
		if err != nil {
			return
		}
		data, err = sjson.SetBytes(data, prefix+"."+path, value)
	}
	set("input_tokens", usage.InputTokens)
	set("output_tokens", usage.OutputTokens)
	set("cache_read_input_tokens", usage.CacheReadInputTokens)
	set("cache_creation_input_tokens", usage.CacheCreationInputTokens)
	if usage.CacheCreation != nil {
		set("cache_creation.ephemeral_5m_input_tokens", usage.CacheCreation.Ephemeral5mInputTokens)
		set("cache_creation.ephemeral_1h_input_tokens", usage.CacheCreation.Ephemeral1hInputTokens)
	}
	if err != nil {
		return nil, err
	}
	return stripInternalUsagePolicyFields(data, prefix)
}

func stripInternalUsagePolicyFields(data []byte, prefix string) ([]byte, error) {
	for _, field := range []string{
		"admin_info",
		"cost",
		"cost_details",
		"upstream_cost",
		"upstream_inference_cost",
		"usage_semantic",
		"usage_source",
		"token_pricing_enabled",
		"token_pricing_input_ratio",
		"token_pricing_output_ratio",
		"token_pricing_rules",
		"raw_prompt_tokens",
		"raw_completion_tokens",
		"raw_total_tokens",
		"billing_prompt_tokens",
		"billing_completion_tokens",
		"billing_total_tokens",
		"cache_billing",
		"cache_billing_offset_bps",
		"offset_bps",
		"reclassified_tokens",
	} {
		var err error
		data, err = sjson.DeleteBytes(data, prefix+"."+field)
		if err != nil {
			return nil, err
		}
	}
	return data, nil
}

// RewriteGeminiUsageJSON replaces Gemini's public usageMetadata object while
// preserving the rest of the response verbatim.
func RewriteGeminiUsageJSON(data []byte, metadata dto.GeminiUsageMetadata) ([]byte, error) {
	if len(data) == 0 || !gjson.GetBytes(data, "usageMetadata").Exists() {
		return data, nil
	}
	metadataJSON, err := common.Marshal(metadata)
	if err != nil {
		return nil, err
	}
	return sjson.SetRawBytes(data, "usageMetadata", metadataJSON)
}
