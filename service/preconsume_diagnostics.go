package service

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

const preConsumeEstimateKey = "preconsume_estimate_diagnostics"
const preConsumeWalletQuotaKey = "preconsume_checked_wallet_quota"

// Capture only scalar estimation/pricing facts, before billing can fail. Never
// retain prompts, media bytes/URLs, tools, headers, API keys or error messages.
func CapturePreConsumeEstimate(c *gin.Context, info *relaycommon.RelayInfo, meta *types.TokenCountMeta) {
	if c == nil || info == nil {
		return
	}
	group := info.UsingGroup
	if group == "" {
		group = info.UserGroup
	}
	pricingContext := billing_setting.TokenPricingContext{Model: info.OriginModelName, Group: group, UserId: info.UserId}
	pricing := billing_setting.GetEffectiveTokenPricing(pricingContext)
	facts := map[string]any{
		"estimated_input_tokens": info.GetEstimatePromptTokens(),
		"billing_input_tokens":   billing_setting.ApplyInputTokenPricingForContext(info.GetEstimatePromptTokens(), pricingContext),
		"input_token_ratio":      pricing.InputRatio, "output_token_ratio": pricing.OutputRatio,
		"model_ratio": info.PriceData.ModelRatio, "group_ratio": info.PriceData.GroupRatioInfo.GroupRatio,
		"model_price": info.PriceData.ModelPrice, "per_call": info.PriceData.UsePrice,
		"token_count_enabled": constant.CountToken, "minimum_preconsume_tokens": common.PreConsumedQuota,
	}
	if meta != nil {
		facts["max_tokens"] = meta.MaxTokens
		facts["billing_output_tokens"] = billing_setting.ApplyOutputTokenPricingForContext(meta.MaxTokens, pricingContext)
		facts["message_count"], facts["tool_count"] = meta.MessagesCount, meta.ToolsCount
		facts["combined_text_bytes"] = len(meta.CombineText)
		images, base64Chars := 0, 0
		for _, file := range meta.Files {
			if file == nil || file.FileType != types.FileTypeImage {
				continue
			}
			images++
			if source, ok := file.Source.(*types.Base64Source); ok {
				base64Chars += len(source.Base64Data)
			}
		}
		facts["image_count"], facts["image_base64_chars"] = images, base64Chars
	}
	if snap := info.TieredBillingSnapshot; snap != nil {
		facts["billing_mode"] = snap.BillingMode
		facts["billing_input_tokens"] = snap.EstimatedPromptTokens
		facts["billing_output_tokens"] = snap.EstimatedCompletionTokens
		facts["group_ratio"] = snap.GroupRatio
	}
	c.Set(preConsumeEstimateKey, facts)
}

func logPreConsumeFailure(c *gin.Context, info *relaycommon.RelayInfo, requiredQuota int, apiErr *types.NewAPIError) {
	if apiErr == nil || c == nil || info == nil {
		return
	}
	facts := map[string]any{
		"event": "preconsume_failed", "request_id": info.RequestId,
		"user_id": info.UserId, "token_id": info.TokenId,
		"model": info.OriginModelName, "group": info.UsingGroup,
		"required_quota": requiredQuota, "quota_per_unit": common.QuotaPerUnit,
		"error_code": apiErr.GetErrorCode(), "status": apiErr.StatusCode,
	}
	if value, ok := c.Get(preConsumeEstimateKey); ok {
		if estimate, ok := value.(map[string]any); ok {
			for key, value := range estimate {
				facts[key] = value
			}
		}
	}
	if quota, ok := c.Get(preConsumeWalletQuotaKey); ok {
		facts["available_wallet_quota"] = quota
	}
	if data, err := common.Marshal(facts); err == nil {
		logger.LogWarn(c, "preconsume_failed "+string(data))
	}
}
