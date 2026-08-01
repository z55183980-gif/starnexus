package sub2api

import (
	"net/http"

	"github.com/QuantumNous/new-api/relay/channel/codex"
	"github.com/QuantumNous/new-api/relay/channel/newapi"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"

	"github.com/gin-gonic/gin"
)

type Adaptor struct {
	newapi.Adaptor
}

func (a *Adaptor) GetModelList() []string { return ModelList }

func (a *Adaptor) GetChannelName() string { return ChannelName }

func (a *Adaptor) SetupRequestHeader(c *gin.Context, header *http.Header, info *relaycommon.RelayInfo) error {
	if err := a.Adaptor.SetupRequestHeader(c, header, info); err != nil {
		return err
	}
	if info != nil && info.RelayMode == relayconstant.RelayModeResponses &&
		!codex.ResponsesUsesNativeWebSocket(c, info) && codex.IsResponsesLiteHeader(c.GetHeader(codex.ResponsesLiteHeader)) {
		header.Set(codex.ResponsesLiteHeader, "true")
	}
	return nil
}
