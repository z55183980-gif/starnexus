package relay

import (
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	appconstant "github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

const codexMissingResponseItemRetryContextKey = "codex_missing_response_item_retry_applied"

// IsCodexResponsesMissingItemError identifies the semantic 404 returned when
// the stateless Codex endpoint is asked to resolve a provider-owned item that
// was created by a store=false request. It is intentionally narrow: ordinary
// 404s must retain their existing retry and channel-error behavior.
func IsCodexResponsesMissingItemError(c *gin.Context, apiErr *types.NewAPIError) bool {
	if c == nil || apiErr == nil || apiErr.StatusCode != http.StatusNotFound ||
		common.GetContextKeyInt(c, appconstant.ContextKeyChannelType) != appconstant.ChannelTypeCodex ||
		common.GetContextKeyBool(c, appconstant.ContextKeyResponsesWebSocketIngress) ||
		c.Request == nil || c.Request.Method != http.MethodPost || c.Request.URL == nil ||
		c.Request.URL.Path != "/v1/responses" {
		return false
	}
	message := apiErr.Error()
	return strings.Contains(message, "Item with id '") &&
		strings.Contains(message, "not found") &&
		strings.Contains(message, "not persisted") &&
		strings.Contains(message, "store") &&
		strings.Contains(message, "false")
}

// TryRepairCodexMissingResponseItemsForRetry performs one same-account
// request rebuild for legacy or client-supplied history. It forces the next
// conversion to the portable replay representation, then lets the normal
// relay retry the same selected account. No new API error is created here; if
// the input cannot be safely rebuilt, the original upstream 404 is preserved.
func TryRepairCodexMissingResponseItemsForRetry(c *gin.Context, info *relaycommon.RelayInfo, apiErr *types.NewAPIError) bool {
	if c == nil || info == nil || !IsCodexResponsesMissingItemError(c, apiErr) ||
		info.RelayMode != relayconstant.RelayModeResponses || info.SendResponseCount != 0 ||
		c.GetBool(codexMissingResponseItemRetryContextKey) {
		return false
	}
	request, ok := info.Request.(*dto.OpenAIResponsesRequest)
	if !ok || request == nil {
		return false
	}
	if !service.ResponsesHTTPContinuationHasNonPortableInput(c, request.Input) {
		return false
	}
	c.Set(codexMissingResponseItemRetryContextKey, true)

	repairInfo := map[string]interface{}{}
	if existing, exists := c.Get("codex_input_repair_admin_info"); exists {
		if existingInfo, ok := existing.(map[string]interface{}); ok {
			for key, value := range existingInfo {
				repairInfo[key] = value
			}
		}
	}
	repairInfo["provider_reasoning_items_removed"] = true
	repairInfo["portable_replay_rebuild"] = true
	repairInfo["upstream_validation_retry"] = true
	c.Set("codex_input_repair_admin_info", repairInfo)
	return true
}
