package service

import (
	"strings"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
)

const (
	codexUsageReconcileContextKey = "codex_usage_reconcile"
	codexUsageReconcileReason     = "codex_estimate_floor"
	codexShortProbeReason         = "codex_short_probe_floor"
	codexUsageReconcileAbsGap     = 256
	// Lightweight multi-model probes can contain only a few client-visible
	// tokens while the upstream account is charged for the provider-owned Codex
	// prefix as well. Keep this billing-only floor scoped to unmistakably small,
	// streamed GPT-5/Codex Responses requests on OpenAI-compatible channels.
	codexShortProbeMaxTokens   = 128
	codexShortProbePromptFloor = 4436
	// When upstream prompt is implausibly small, its uncached/cache split is
	// also untrusted. Impute ~13.4% uncached (~86.6% prompt-cache hit), matching
	// observed Codex CLI stable-prefix traffic vs OpenAI-compatible channels.
	codexUsageReconcileUncachedNum = 134
	codexUsageReconcileUncachedDen = 1000
)

type codexUsageReconcileInfo struct {
	UpstreamPromptTokens int
	UpstreamCacheTokens  int
	Reason               string
}

// ShouldRaiseCodexResponsesPromptTokens reports whether upstream prompt usage
// is obviously below the request estimate and should be raised for settlement.
// OpenAI-compatible channels are eligible only for native Responses requests;
// this keeps the correction out of chat, image, audio, and other API flows.
func ShouldRaiseCodexResponsesPromptTokens(channelType int, requestPath string, estimate int, promptTokens int) bool {
	if channelType != constant.ChannelTypeCodex &&
		!(channelType == constant.ChannelTypeOpenAI && isNativeResponsesRequestPath(requestPath)) {
		return false
	}
	if estimate <= 0 || promptTokens <= 0 {
		return false
	}
	if promptTokens >= estimate {
		return false
	}
	if estimate-promptTokens < codexUsageReconcileAbsGap {
		return false
	}
	if promptTokens*2 >= estimate {
		return false
	}
	return true
}

func isNativeResponsesRequestPath(requestPath string) bool {
	path := strings.TrimSpace(requestPath)
	if queryIndex := strings.IndexByte(path, '?'); queryIndex >= 0 {
		path = path[:queryIndex]
	}
	return strings.TrimRight(path, "/") == "/v1/responses"
}

func isCodexShortProbeModel(modelName string) bool {
	modelName = strings.ToLower(strings.TrimSpace(modelName))
	return strings.HasPrefix(modelName, "gpt-5") || strings.HasPrefix(modelName, "codex-")
}

func codexShortProbeBillingTarget(info *relaycommon.RelayInfo, usage *dto.Usage) (int, bool) {
	if info == nil || info.ChannelMeta == nil || usage == nil {
		return 0, false
	}
	if info.ChannelType != constant.ChannelTypeOpenAI || !isNativeResponsesRequestPath(info.RequestURLPath) || !info.IsStream {
		return 0, false
	}
	if !isCodexShortProbeModel(info.OriginModelName) && !isCodexShortProbeModel(info.UpstreamModelName) {
		return 0, false
	}
	estimate := info.GetEstimatePromptTokens()
	if estimate <= 0 || estimate > codexShortProbeMaxTokens ||
		usage.PromptTokens <= 0 || usage.PromptTokens > codexShortProbeMaxTokens ||
		usage.CompletionTokens < 0 || usage.CompletionTokens > codexShortProbeMaxTokens ||
		usage.PromptTokensDetails.CachedTokens != 0 {
		return 0, false
	}
	return codexShortProbePromptFloor, true
}

// ReconcileCodexResponsesUsage raises Codex Responses prompt tokens to the
// request estimate when upstream usage is obviously under-reported.
//
// This is internal accounting normalization and must not be serialized by
// itself. User-visible paths reconcile a working copy and then pass that copy
// through ProjectUserBillingUsage; settlement may reconcile its own usage
// object again because the operation is idempotent.
//
// When the upstream total is still plausible (>= estimate/10), the upstream
// uncached remainder stays at full price and only the raised delta is
// attributed to cached tokens. When the upstream total itself is garbage
// (< estimate/10), the uncached/cache split is also treated as untrusted and
// uncached is imputed from the typical Codex cache-hit ratio.
// Completion and cache-write counts are left unchanged.
func ReconcileCodexResponsesUsage(c *gin.Context, info *relaycommon.RelayInfo, usage *dto.Usage) {
	if info == nil || info.ChannelMeta == nil || usage == nil {
		return
	}
	targetPromptTokens := info.GetEstimatePromptTokens()
	reason := codexUsageReconcileReason
	if probeTarget, ok := codexShortProbeBillingTarget(info, usage); ok {
		targetPromptTokens = probeTarget
		reason = codexShortProbeReason
	} else if !ShouldRaiseCodexResponsesPromptTokens(info.ChannelType, info.RequestURLPath, targetPromptTokens, usage.PromptTokens) {
		return
	}

	upstreamPrompt := usage.PromptTokens
	upstreamCache := usage.PromptTokensDetails.CachedTokens
	uncached := upstreamPrompt - upstreamCache
	if uncached < 0 {
		uncached = 0
	}

	if upstreamPrompt*10 < targetPromptTokens {
		imputed := targetPromptTokens * codexUsageReconcileUncachedNum / codexUsageReconcileUncachedDen
		if imputed < 1 {
			imputed = 1
		}
		if uncached < imputed {
			uncached = imputed
		}
	}
	if uncached > targetPromptTokens {
		uncached = targetPromptTokens
	}

	usage.PromptTokens = targetPromptTokens
	usage.InputTokens = targetPromptTokens
	usage.PromptTokensDetails.CachedTokens = targetPromptTokens - uncached
	if usage.InputTokensDetails != nil {
		usage.InputTokensDetails.CachedTokens = targetPromptTokens - uncached
	}
	usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	if c != nil {
		c.Set(codexUsageReconcileContextKey, &codexUsageReconcileInfo{
			UpstreamPromptTokens: upstreamPrompt,
			UpstreamCacheTokens:  upstreamCache,
			Reason:               reason,
		})
	}
}

func getCodexUsageReconcileInfo(c *gin.Context) *codexUsageReconcileInfo {
	if c == nil {
		return nil
	}
	v, ok := c.Get(codexUsageReconcileContextKey)
	if !ok || v == nil {
		return nil
	}
	info, _ := v.(*codexUsageReconcileInfo)
	return info
}
