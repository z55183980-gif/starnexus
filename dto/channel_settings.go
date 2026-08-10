package dto

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/constant"
)

type ChannelSettings struct {
	ForceFormat                   bool                   `json:"force_format,omitempty"`
	ThinkingToContent             bool                   `json:"thinking_to_content,omitempty"`
	Proxy                         string                 `json:"proxy"`
	PassThroughBodyEnabled        bool                   `json:"pass_through_body_enabled,omitempty"`
	AccountPassThroughBodyEnabled bool                   `json:"-"`
	SystemPrompt                  string                 `json:"system_prompt,omitempty"`
	SystemPromptOverride          bool                   `json:"system_prompt_override,omitempty"`
	ZQBAPI                        *ZQBAPIChannelSettings `json:"zqbapi,omitempty"`
}

const (
	ZQBAPIMaterialModeOff           = "off"
	ZQBAPIMaterialModeRetryOnly     = "retry_only"
	ZQBAPIMaterialModeFacePreflight = "face_preflight"
	ZQBAPIMaterialModeAlways        = "always"
)

type ZQBAPIChannelSettings struct {
	MaterialMode   string `json:"material_mode,omitempty"`
	GroupID        string `json:"group_id,omitempty"`
	ProjectName    string `json:"project_name,omitempty"`
	AssetGroupType string `json:"asset_group_type,omitempty"`
	AutoNormalize  *bool  `json:"auto_normalize,omitempty"`
}

func (s *ZQBAPIChannelSettings) Normalize() {
	if s == nil {
		return
	}
	s.MaterialMode = strings.ToLower(strings.TrimSpace(s.MaterialMode))
	if s.MaterialMode == "" {
		s.MaterialMode = ZQBAPIMaterialModeFacePreflight
	}
	s.GroupID = strings.TrimSpace(s.GroupID)
	s.ProjectName = strings.TrimSpace(s.ProjectName)
	s.AssetGroupType = strings.ToLower(strings.TrimSpace(s.AssetGroupType))
	if s.AssetGroupType == "" {
		s.AssetGroupType = "virtual"
	}
}

func (s *ZQBAPIChannelSettings) Validate() error {
	if s == nil {
		return nil
	}
	s.Normalize()
	switch s.MaterialMode {
	case ZQBAPIMaterialModeOff, ZQBAPIMaterialModeRetryOnly, ZQBAPIMaterialModeFacePreflight, ZQBAPIMaterialModeAlways:
	default:
		return fmt.Errorf("invalid ZQBAPI material mode: %s", s.MaterialMode)
	}
	switch s.AssetGroupType {
	case "virtual", "real":
	default:
		return fmt.Errorf("invalid ZQBAPI asset group type: %s", s.AssetGroupType)
	}
	return nil
}

// ShouldPassThroughBody reports whether the request format is owned by either
// the channel or the selected local upstream account. Account passthrough is an
// internal transport capability and does not bypass channel field policies.
func (s ChannelSettings) ShouldPassThroughBody() bool {
	return s.PassThroughBodyEnabled || s.AccountPassThroughBodyEnabled
}

type VertexKeyType string

const (
	VertexKeyTypeJSON   VertexKeyType = "json"
	VertexKeyTypeAPIKey VertexKeyType = "api_key"
)

type AwsKeyType string

const (
	AwsKeyTypeAKSK   AwsKeyType = "ak_sk" // 默认
	AwsKeyTypeApiKey AwsKeyType = "api_key"
)

type ChannelOtherSettings struct {
	AzureResponsesVersion                 string                `json:"azure_responses_version,omitempty"`
	ResponsesWebSocketV2Enabled           bool                  `json:"responses_websocket_v2_enabled,omitempty"`
	ResponsesWebSocketV2Mode              string                `json:"responses_websocket_v2_mode,omitempty"`
	ResponsesWebSocketV2ReplayEnabled     bool                  `json:"responses_websocket_v2_replay_enabled,omitempty"`
	AlphaSearchEnabled                    bool                  `json:"alpha_search_enabled,omitempty"` // 是否允许 OpenAI 兼容渠道处理 /v1/alpha/search（默认关闭）
	VertexKeyType                         VertexKeyType         `json:"vertex_key_type,omitempty"`      // "json" or "api_key"
	OpenRouterEnterprise                  *bool                 `json:"openrouter_enterprise,omitempty"`
	ClaudeBetaQuery                       bool                  `json:"claude_beta_query,omitempty"`         // Claude 渠道是否强制追加 ?beta=true
	AllowServiceTier                      bool                  `json:"allow_service_tier,omitempty"`        // 是否允许 service_tier 透传（默认过滤以避免额外计费）
	AllowInferenceGeo                     bool                  `json:"allow_inference_geo,omitempty"`       // 是否允许 inference_geo 透传（仅 Claude，默认过滤以满足数据驻留合规
	AllowSpeed                            bool                  `json:"allow_speed,omitempty"`               // 是否允许 speed 透传（仅 Claude，默认过滤以避免意外切换推理速度模式）
	AllowSafetyIdentifier                 bool                  `json:"allow_safety_identifier,omitempty"`   // 是否允许 safety_identifier 透传（默认过滤以保护用户隐私）
	DisableStore                          bool                  `json:"disable_store,omitempty"`             // 是否禁用 store 透传（默认允许透传，禁用后可能导致 Codex 无法使用）
	AllowIncludeObfuscation               bool                  `json:"allow_include_obfuscation,omitempty"` // 是否允许 stream_options.include_obfuscation 透传（默认过滤以避免关闭流混淆保护）
	AwsKeyType                            AwsKeyType            `json:"aws_key_type,omitempty"`
	UpstreamModelUpdateCheckEnabled       bool                  `json:"upstream_model_update_check_enabled,omitempty"`        // 是否检测上游模型更新
	UpstreamModelUpdateAutoSyncEnabled    bool                  `json:"upstream_model_update_auto_sync_enabled,omitempty"`    // 是否自动同步上游模型更新
	UpstreamModelUpdateLastCheckTime      int64                 `json:"upstream_model_update_last_check_time,omitempty"`      // 上次检测时间
	UpstreamModelUpdateLastDetectedModels []string              `json:"upstream_model_update_last_detected_models,omitempty"` // 上次检测到的可加入模型
	UpstreamModelUpdateLastRemovedModels  []string              `json:"upstream_model_update_last_removed_models,omitempty"`  // 上次检测到的可删除模型
	UpstreamModelUpdateIgnoredModels      []string              `json:"upstream_model_update_ignored_models,omitempty"`       // 手动忽略的模型
	AdvancedCustom                        *AdvancedCustomConfig `json:"advanced_custom,omitempty"`
}

// SupportsAlphaSearch reports whether a channel is explicitly capable of
// handling /v1/alpha/search. OpenAI-compatible channels must opt in because
// most upstreams do not implement this Codex-specific endpoint.
func (s ChannelOtherSettings) SupportsAlphaSearch(channelType int, modelName string) bool {
	switch channelType {
	case constant.ChannelTypeCodex, constant.ChannelTypeSub2API, constant.ChannelTypeNewAPI:
		return true
	case constant.ChannelTypeOpenAI:
		return s.AlphaSearchEnabled
	case constant.ChannelTypeAdvancedCustom:
		return s.AdvancedCustom != nil && s.AdvancedCustom.SupportsPathForModel("/v1/alpha/search", modelName)
	default:
		return false
	}
}

func (s *ChannelOtherSettings) IsOpenRouterEnterprise() bool {
	if s == nil || s.OpenRouterEnterprise == nil {
		return false
	}
	return *s.OpenRouterEnterprise
}

const (
	AdvancedCustomConverterNone  = "none"
	AdvancedCustomAuthTypeNone   = "none"
	AdvancedCustomAuthTypeHeader = "header"
	AdvancedCustomAuthTypeQuery  = "query"

	advancedCustomModelPlaceholder = "{model}"
	advancedCustomModelRegexPrefix = "re:"
)

type AdvancedCustomConfig struct {
	Routes []AdvancedCustomRoute `json:"advanced_routes,omitempty"`
}

type AdvancedCustomRoute struct {
	IncomingPath string                   `json:"incoming_path,omitempty"`
	UpstreamPath string                   `json:"upstream_path,omitempty"`
	Converter    string                   `json:"converter,omitempty"`
	Models       []string                 `json:"models,omitempty"`
	Auth         *AdvancedCustomRouteAuth `json:"auth,omitempty"`
}

type AdvancedCustomRouteAuth struct {
	Type  string `json:"type,omitempty"`
	Name  string `json:"name,omitempty"`
	Value string `json:"value,omitempty"`
}

func (c *AdvancedCustomConfig) MatchPathForModel(requestPath string, model string) (AdvancedCustomRoute, bool) {
	if c == nil {
		return AdvancedCustomRoute{}, false
	}
	requestPath = strings.SplitN(strings.TrimSpace(requestPath), "?", 2)[0]
	model = strings.TrimSpace(model)
	for _, route := range c.Routes {
		if matchAdvancedCustomIncomingPath(strings.TrimSpace(route.IncomingPath), requestPath) &&
			matchAdvancedCustomRouteModel(route.Models, model) {
			return route, true
		}
	}
	return AdvancedCustomRoute{}, false
}

func (c *AdvancedCustomConfig) SupportsPathForModel(requestPath string, model string) bool {
	_, ok := c.MatchPathForModel(requestPath, model)
	return ok
}

var advancedCustomModelRegexCache sync.Map

func matchAdvancedCustomRouteModel(models []string, model string) bool {
	matchedRule := false
	for _, rule := range models {
		rule = strings.TrimSpace(rule)
		if rule == "" {
			continue
		}
		matchedRule = true
		if !strings.HasPrefix(rule, advancedCustomModelRegexPrefix) {
			if rule == model {
				return true
			}
			continue
		}
		pattern := strings.TrimPrefix(rule, advancedCustomModelRegexPrefix)
		if pattern == "" {
			continue
		}
		cached, ok := advancedCustomModelRegexCache.Load(pattern)
		if !ok {
			re, err := regexp.Compile(pattern)
			if err != nil {
				advancedCustomModelRegexCache.Store(pattern, false)
				continue
			}
			advancedCustomModelRegexCache.Store(pattern, re)
			cached = re
		}
		if re, ok := cached.(*regexp.Regexp); ok && re.MatchString(model) {
			return true
		}
	}
	return !matchedRule
}

func matchAdvancedCustomIncomingPath(configuredPath string, requestPath string) bool {
	if !strings.Contains(configuredPath, advancedCustomModelPlaceholder) {
		return configuredPath == requestPath
	}
	parts := strings.Split(configuredPath, advancedCustomModelPlaceholder)
	if len(parts) != 2 || !strings.HasPrefix(requestPath, parts[0]) || !strings.HasSuffix(requestPath, parts[1]) {
		return false
	}
	value := strings.TrimSuffix(strings.TrimPrefix(requestPath, parts[0]), parts[1])
	return value != "" && !strings.Contains(value, "/")
}

func (c *AdvancedCustomConfig) Validate() error {
	if c == nil || len(c.Routes) == 0 {
		return fmt.Errorf("advanced_custom requires at least one route")
	}
	for i, route := range c.Routes {
		incomingPath := strings.TrimSpace(route.IncomingPath)
		upstreamPath := strings.TrimSpace(route.UpstreamPath)
		if incomingPath == "" || !strings.HasPrefix(incomingPath, "/") || strings.Contains(incomingPath, "?") {
			return fmt.Errorf("advanced_custom.advanced_routes[%d].incoming_path must be a path without query", i)
		}
		if upstreamPath == "" {
			return fmt.Errorf("advanced_custom.advanced_routes[%d].upstream_path is required", i)
		}
		if !strings.HasPrefix(upstreamPath, "/") {
			parsed, err := url.Parse(upstreamPath)
			if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
				return fmt.Errorf("advanced_custom.advanced_routes[%d].upstream_path must be an http(s) URL or absolute path", i)
			}
		}
		converter := strings.TrimSpace(route.Converter)
		if converter != "" && converter != AdvancedCustomConverterNone {
			return fmt.Errorf("advanced_custom.advanced_routes[%d].converter is not supported: %s", i, converter)
		}
		for _, modelRule := range route.Models {
			modelRule = strings.TrimSpace(modelRule)
			if strings.HasPrefix(modelRule, advancedCustomModelRegexPrefix) {
				if _, err := regexp.Compile(strings.TrimPrefix(modelRule, advancedCustomModelRegexPrefix)); err != nil {
					return fmt.Errorf("advanced_custom.advanced_routes[%d].models contains invalid regex: %s", i, modelRule)
				}
			}
		}
		if route.Auth == nil {
			continue
		}
		switch strings.TrimSpace(route.Auth.Type) {
		case "", AdvancedCustomAuthTypeNone:
		case AdvancedCustomAuthTypeHeader, AdvancedCustomAuthTypeQuery:
			if strings.TrimSpace(route.Auth.Name) == "" || strings.TrimSpace(route.Auth.Value) == "" {
				return fmt.Errorf("advanced_custom.advanced_routes[%d].auth requires name and value", i)
			}
		default:
			return fmt.Errorf("advanced_custom.advanced_routes[%d].auth.type is invalid", i)
		}
	}
	return nil
}
