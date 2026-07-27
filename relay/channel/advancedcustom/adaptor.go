package advancedcustom

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/newapi"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"

	"github.com/gin-gonic/gin"
)

const ChannelName = "advanced_custom"

var ModelList = []string{}

type Adaptor struct {
	newapi.Adaptor
}

func (a *Adaptor) resolveRoute(info *relaycommon.RelayInfo) (dto.AdvancedCustomRoute, error) {
	config := info.ChannelOtherSettings.AdvancedCustom
	requestPath := strings.SplitN(info.RequestURLPath, "?", 2)[0]
	if info.RelayMode == relayconstant.RelayModeAlphaSearch {
		requestPath = "/v1/alpha/search"
	}
	route, ok := config.MatchPathForModel(requestPath, info.OriginModelName)
	if !ok {
		return dto.AdvancedCustomRoute{}, fmt.Errorf("advanced custom channel has no route for %s and model %s", requestPath, info.OriginModelName)
	}
	return route, nil
}

func (a *Adaptor) GetRequestURL(info *relaycommon.RelayInfo) (string, error) {
	route, err := a.resolveRoute(info)
	if err != nil {
		return "", err
	}
	modelName := info.UpstreamModelName
	if modelName == "" {
		modelName = info.OriginModelName
	}
	target := strings.ReplaceAll(strings.TrimSpace(route.UpstreamPath), "{model}", url.PathEscape(modelName))
	if strings.HasPrefix(target, "/") {
		target = relaycommon.GetFullRequestURL(info.ChannelBaseUrl, target, info.ChannelType)
	}
	if route.Auth != nil && strings.TrimSpace(route.Auth.Type) == dto.AdvancedCustomAuthTypeQuery {
		parsed, err := url.Parse(target)
		if err != nil {
			return "", err
		}
		query := parsed.Query()
		query.Set(strings.TrimSpace(route.Auth.Name), expandAuthValue(route.Auth.Value, info.ApiKey))
		parsed.RawQuery = query.Encode()
		target = parsed.String()
	}
	return target, nil
}

func (a *Adaptor) SetupRequestHeader(c *gin.Context, req *http.Header, info *relaycommon.RelayInfo) error {
	route, err := a.resolveRoute(info)
	if err != nil {
		return err
	}
	if route.Auth == nil {
		return a.Adaptor.SetupRequestHeader(c, req, info)
	}
	channel.SetupApiRequestHeader(info, c, req)
	switch strings.TrimSpace(route.Auth.Type) {
	case "", dto.AdvancedCustomAuthTypeNone, dto.AdvancedCustomAuthTypeQuery:
		return nil
	case dto.AdvancedCustomAuthTypeHeader:
		req.Set(strings.TrimSpace(route.Auth.Name), expandAuthValue(route.Auth.Value, info.ApiKey))
		return nil
	default:
		return fmt.Errorf("unsupported advanced custom auth type: %s", route.Auth.Type)
	}
}

func expandAuthValue(value string, apiKey string) string {
	value = strings.ReplaceAll(value, "{key}", apiKey)
	return strings.ReplaceAll(value, "{api_key}", apiKey)
}

func (a *Adaptor) GetModelList() []string { return ModelList }

func (a *Adaptor) GetChannelName() string { return ChannelName }
