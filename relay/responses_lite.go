package relay

import (
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relay/channel/codex"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
)

func normalizeSelectedResponsesLitePayload(c *gin.Context, info *relaycommon.RelayInfo, body []byte, websocketPayload bool) ([]byte, bool, error) {
	websocketLite := websocketPayload && codex.IsResponsesLiteWebSocketPayload(body)
	if !selectedUpstreamIsOpenAIOAuth(c, info) {
		// A Sub2API upstream owns its account selection and performs the actual
		// normalization. Preserve the per-turn Lite signal so an HTTP bridge can
		// restore the equivalent request header for that downstream gateway.
		if websocketLite && relayInfoMatchesChannel(info, constant.APITypeSub2API, constant.ChannelTypeSub2API) {
			return body, true, nil
		}
		return body, false, nil
	}

	isLite := selectedResponsesLiteHTTP(c, info)
	if websocketLite {
		isLite = true
	}
	if !isLite {
		return body, false, nil
	}

	normalized, _, err := codex.NormalizeResponsesLitePayload(body)
	if err != nil {
		return body, true, err
	}
	return normalized, true, nil
}

func selectedResponsesLiteHTTP(c *gin.Context, info *relaycommon.RelayInfo) bool {
	return selectedUpstreamIsOpenAIOAuth(c, info) && c != nil &&
		codex.IsResponsesLiteHeader(c.GetHeader(codex.ResponsesLiteHeader))
}

func markResponsesLiteHTTPHeader(c *gin.Context) {
	if c == nil || c.Request == nil {
		return
	}
	if c.Request.Header == nil {
		c.Request.Header = make(http.Header)
	}
	c.Request.Header.Set(codex.ResponsesLiteHeader, "true")
}

func selectedUpstreamIsOpenAIOAuth(c *gin.Context, info *relaycommon.RelayInfo) bool {
	platform := ""
	accountType := ""
	if c != nil {
		platform = strings.ToLower(strings.TrimSpace(common.GetContextKeyString(c, constant.ContextKeyUpstreamAccountPlatform)))
		accountType = strings.ToLower(strings.TrimSpace(common.GetContextKeyString(c, constant.ContextKeyUpstreamAccountType)))
	}
	if platform != "" || accountType != "" {
		return platform == constant.UpstreamPlatformOpenAI && accountType == constant.UpstreamAccountTypeOAuth
	}
	return relayInfoMatchesChannel(info, constant.APITypeCodex, constant.ChannelTypeCodex)
}

func relayInfoMatchesChannel(info *relaycommon.RelayInfo, apiType int, channelType int) bool {
	return info != nil && info.ChannelMeta != nil && (info.ApiType == apiType || info.ChannelType == channelType)
}
