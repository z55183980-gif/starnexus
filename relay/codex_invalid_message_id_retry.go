package relay

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	appconstant "github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/sjson"
)

const codexInvalidMessageIDRetryContextKey = "codex_invalid_message_id_retry_applied"

var codexInvalidMessageIDErrorPattern = regexp.MustCompile(`^Invalid 'input\[(\d+)\]\.id': '([^']+)'\. Expected an ID that begins with 'msg'\.$`)

// TryRepairCodexInvalidMessageIDForRetry applies a single, reactive repair
// only after the Codex Responses backend has rejected one exact top-level
// message ID. It deliberately does not pre-normalize requests that upstream
// would otherwise accept.
func TryRepairCodexInvalidMessageIDForRetry(c *gin.Context, info *relaycommon.RelayInfo, apiErr *types.NewAPIError) bool {
	if c == nil || info == nil || apiErr == nil ||
		apiErr.StatusCode != http.StatusBadRequest ||
		apiErr.GetErrorType() != types.ErrorTypeOpenAIError ||
		apiErr.GetErrorCode() != types.ErrorCode("invalid_value") ||
		info.RelayMode != relayconstant.RelayModeResponses ||
		info.SendResponseCount != 0 ||
		common.GetContextKeyInt(c, appconstant.ContextKeyChannelType) != appconstant.ChannelTypeCodex ||
		c.Request == nil || c.Request.URL == nil || c.Request.URL.Path != "/v1/responses" ||
		common.GetContextKeyBool(c, appconstant.ContextKeyResponsesWebSocketIngress) ||
		c.GetBool(codexInvalidMessageIDRetryContextKey) {
		return false
	}

	matches := codexInvalidMessageIDErrorPattern.FindStringSubmatch(apiErr.Error())
	if len(matches) != 3 {
		return false
	}
	index, err := strconv.Atoi(matches[1])
	if err != nil || index < 0 {
		return false
	}
	rejectedID := matches[2]
	if strings.HasPrefix(rejectedID, "msg") {
		return false
	}

	request, ok := info.Request.(*dto.OpenAIResponsesRequest)
	if !ok || request == nil || strings.TrimSpace(request.PreviousResponseID) != "" {
		return false
	}
	trimmedInput := bytes.TrimSpace(request.Input)
	if len(trimmedInput) == 0 || trimmedInput[0] != '[' {
		return false
	}

	var items []json.RawMessage
	if common.Unmarshal(trimmedInput, &items) != nil || index >= len(items) {
		return false
	}
	var target struct {
		Type string `json:"type"`
		ID   string `json:"id"`
	}
	if common.Unmarshal(items[index], &target) != nil || target.Type != "message" || target.ID != rejectedID {
		return false
	}

	// If any other item contains the rejected ID, leave the request unchanged.
	// This favors the original deterministic 400 over creating an orphaned
	// item_reference or tool relationship on retry.
	encodedID, err := common.Marshal(rejectedID)
	if err != nil {
		return false
	}
	for itemIndex, item := range items {
		if itemIndex != index && bytes.Contains(item, encodedID) {
			return false
		}
	}

	repairedInput, err := sjson.DeleteBytes(request.Input, fmt.Sprintf("%d.id", index))
	if err != nil {
		return false
	}
	request.Input = repairedInput
	c.Set(codexInvalidMessageIDRetryContextKey, true)

	repairInfo := map[string]interface{}{}
	if existing, exists := c.Get("codex_input_repair_admin_info"); exists {
		if existingMap, mapOK := existing.(map[string]interface{}); mapOK {
			for key, value := range existingMap {
				repairInfo[key] = value
			}
		}
	}
	repairInfo["invalid_message_ids_removed"] = 1
	repairInfo["first_removed_index"] = index
	repairInfo["upstream_validation_retry"] = true
	c.Set("codex_input_repair_admin_info", repairInfo)
	return true
}
