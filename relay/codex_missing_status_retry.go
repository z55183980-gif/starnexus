package relay

import (
	"net/http"
	"regexp"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	appconstant "github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel/codex"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

var codexMissingStatusErrorPattern = regexp.MustCompile(`^Missing required parameter: 'input\[(\d+)\]\.status'\.$`)

// TryRepairCodexMissingStatusForRetry retries once only when the outbound
// scrubber removed an explicit client status from this exact input item.
func TryRepairCodexMissingStatusForRetry(c *gin.Context, info *relaycommon.RelayInfo, apiErr *types.NewAPIError) bool {
	if c == nil || info == nil || apiErr == nil || apiErr.StatusCode != http.StatusBadRequest ||
		apiErr.GetErrorType() != types.ErrorTypeOpenAIError ||
		apiErr.GetErrorCode() != types.ErrorCode("missing_required_parameter") ||
		info.RelayMode != relayconstant.RelayModeResponses || info.SendResponseCount != 0 ||
		common.GetContextKeyInt(c, appconstant.ContextKeyChannelType) != appconstant.ChannelTypeCodex ||
		c.Request == nil || c.Request.Method != http.MethodPost || c.Request.URL == nil || c.Request.URL.Path != "/v1/responses" ||
		common.GetContextKeyBool(c, appconstant.ContextKeyResponsesWebSocketIngress) ||
		c.GetBool(codexResponsesValidationRetryContextKey) {
		return false
	}
	if _, ok := info.Request.(*dto.OpenAIResponsesRequest); !ok {
		return false
	}
	matches := codexMissingStatusErrorPattern.FindStringSubmatch(apiErr.Error())
	if len(matches) != 2 {
		return false
	}
	index, err := strconv.Atoi(matches[1])
	if err != nil {
		return false
	}
	itemType, removed, observed := codex.DescribeCodexMissingStatusItem(c, index)
	if observed {
		repairInfo := map[string]interface{}{}
		if existing, exists := c.Get("codex_input_repair_admin_info"); exists {
			if existingMap, mapOK := existing.(map[string]interface{}); mapOK {
				for key, value := range existingMap {
					repairInfo[key] = value
				}
			}
		}
		repairInfo["missing_status_index"] = index
		repairInfo["missing_status_item_type"] = itemType
		repairInfo["missing_status_removed_by_gateway"] = removed
		c.Set("codex_input_repair_admin_info", repairInfo)
	}
	var ok bool
	itemType, ok = codex.ArmCodexMissingStatusRetry(c, index)
	if !ok {
		return false
	}
	c.Set(codexResponsesValidationRetryContextKey, true)
	repairInfo := map[string]interface{}{}
	if existing, exists := c.Get("codex_input_repair_admin_info"); exists {
		if existingMap, mapOK := existing.(map[string]interface{}); mapOK {
			for key, value := range existingMap {
				repairInfo[key] = value
			}
		}
	}
	repairInfo["missing_status_retried_index"] = index
	repairInfo["missing_status_retried_type"] = itemType
	repairInfo["upstream_validation_retry"] = true
	c.Set("codex_input_repair_admin_info", repairInfo)
	return true
}
