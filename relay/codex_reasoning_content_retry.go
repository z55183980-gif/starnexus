package relay

import (
	"net/http"
	"regexp"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	appconstant "github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

var codexReasoningContentArrayErrorPattern = regexp.MustCompile(`^Invalid 'input\[(\d+)\]\.content': array too long\. Expected an array with maximum length 0, but got an array with length (\d+) instead\.$`)

// TryArmCodexReasoningContentRetry schedules one compatibility retry for the
// exact ChatGPT Codex rejection of a non-empty reasoning.content array. The
// outbound converter applies the patch after HTTP continuation and input
// repair, so input[N] is resolved against the same shape the upstream saw.
func TryArmCodexReasoningContentRetry(c *gin.Context, info *relaycommon.RelayInfo, apiErr *types.NewAPIError) bool {
	if c == nil || info == nil || apiErr == nil ||
		apiErr.StatusCode != http.StatusBadRequest ||
		apiErr.GetErrorType() != types.ErrorTypeOpenAIError ||
		apiErr.GetErrorCode() != types.ErrorCode("array_above_max_length") ||
		info.RelayMode != relayconstant.RelayModeResponses ||
		info.SendResponseCount != 0 ||
		common.GetContextKeyInt(c, appconstant.ContextKeyChannelType) != appconstant.ChannelTypeCodex ||
		c.Request == nil || c.Request.Method != http.MethodPost || c.Request.URL == nil || c.Request.URL.Path != "/v1/responses" ||
		common.GetContextKeyBool(c, appconstant.ContextKeyResponsesWebSocketIngress) ||
		c.GetBool(codexResponsesValidationRetryContextKey) {
		return false
	}
	if request, ok := info.Request.(*dto.OpenAIResponsesRequest); !ok || request == nil {
		return false
	}

	matches := codexReasoningContentArrayErrorPattern.FindStringSubmatch(apiErr.Error())
	if len(matches) != 3 {
		return false
	}
	index, indexErr := strconv.Atoi(matches[1])
	length, lengthErr := strconv.Atoi(matches[2])
	if indexErr != nil || lengthErr != nil || index < 0 || length <= 0 {
		return false
	}

	c.Set(codexResponsesValidationRetryContextKey, true)
	common.SetContextKey(c, appconstant.ContextKeyCodexReasoningContentRetryArmed, true)
	common.SetContextKey(c, appconstant.ContextKeyCodexReasoningContentRetryIndex, index)
	common.SetContextKey(c, appconstant.ContextKeyCodexReasoningContentRetryLength, length)
	return true
}
