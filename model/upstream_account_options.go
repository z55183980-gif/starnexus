package model

import (
	"errors"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
)

type TempUnschedulableRule struct {
	ErrorCode       int      `json:"error_code"`
	Keywords        []string `json:"keywords"`
	DurationMinutes int      `json:"duration_minutes"`
	Description     string   `json:"description"`
}

const (
	UpstreamOpenAIWSModeOff         = "off"
	UpstreamOpenAIWSModeContextPool = "ctx_pool"
	UpstreamOpenAIWSModePassthrough = "passthrough"
	UpstreamOpenAIWSModeHTTPBridge  = "http_bridge"

	UpstreamOpenAICompactModeAuto     = "auto"
	UpstreamOpenAICompactModeForceOn  = "force_on"
	UpstreamOpenAICompactModeForceOff = "force_off"

	UpstreamOpenAIResponsesModeAuto                 = "auto"
	UpstreamOpenAIResponsesModeForceResponses       = "force_responses"
	UpstreamOpenAIResponsesModeForceChatCompletions = "force_chat_completions"

	UpstreamAnthropicAuthSchemeAPIKey = "x_api_key"
	UpstreamAnthropicAuthSchemeBearer = "authorization_bearer"
)

type UpstreamAccountOptions struct {
	InterceptWarmupRequests         bool                    `json:"intercept_warmup_requests,omitempty"`
	OpenAIPassthrough               bool                    `json:"openai_passthrough,omitempty"`
	OpenAIOAuthWSMode               string                  `json:"openai_oauth_responses_websockets_v2_mode,omitempty"`
	OpenAIAPIKeyWSMode              string                  `json:"openai_apikey_responses_websockets_v2_mode,omitempty"`
	OpenAILongContextBillingEnabled bool                    `json:"openai_long_context_billing_enabled,omitempty"`
	CodexCLIOnly                    bool                    `json:"codex_cli_only,omitempty"`
	CodexCLIOnlyAllowAppServer      bool                    `json:"codex_cli_only_allow_app_server,omitempty"`
	OpenAICompactMode               string                  `json:"openai_compact_mode,omitempty"`
	OpenAICompactSupported          *bool                   `json:"openai_compact_supported,omitempty"`
	CompactModelMapping             map[string]string       `json:"compact_model_mapping,omitempty"`
	OpenAIResponsesMode             string                  `json:"openai_responses_mode,omitempty"`
	OpenAIResponsesSupported        *bool                   `json:"openai_responses_supported,omitempty"`
	OpenAIEndpointCapabilities      []string                `json:"openai_capabilities,omitempty"`
	AnthropicPassthrough            bool                    `json:"anthropic_passthrough,omitempty"`
	AnthropicAPIKeyAuthScheme       string                  `json:"anthropic_apikey_auth_scheme,omitempty"`
	TempUnschedulableEnabled        bool                    `json:"temp_unschedulable_enabled,omitempty"`
	TempUnschedulableRules          []TempUnschedulableRule `json:"temp_unschedulable_rules,omitempty"`
}

func ParseUpstreamAccountOptions(extra string) (UpstreamAccountOptions, error) {
	options := UpstreamAccountOptions{}
	if strings.TrimSpace(extra) == "" || strings.TrimSpace(extra) == "{}" {
		return options, nil
	}
	if err := common.UnmarshalJsonStr(extra, &options); err != nil {
		return UpstreamAccountOptions{}, errors.New("account extra contains invalid option values")
	}
	options.TempUnschedulableRules = NormalizeTempUnschedulableRules(options.TempUnschedulableRules)
	return options, nil
}

// ParseUpstreamAccountOptionsWithCredentials reads the credential-backed
// account settings used by sub2api while retaining support for older
// StarNexus records that stored the same settings in extra.
func ParseUpstreamAccountOptionsWithCredentials(extra string, credentials map[string]any) (UpstreamAccountOptions, error) {
	options, err := ParseUpstreamAccountOptions(extra)
	if err != nil {
		return UpstreamAccountOptions{}, err
	}
	if raw, ok := credentials["intercept_warmup_requests"]; ok {
		if raw == nil {
			options.InterceptWarmupRequests = false
		} else {
			enabled, valid := raw.(bool)
			if !valid {
				return UpstreamAccountOptions{}, errors.New("intercept_warmup_requests must be a boolean")
			}
			options.InterceptWarmupRequests = enabled
		}
	}
	if raw, ok := credentials["compact_model_mapping"]; ok {
		if raw == nil {
			options.CompactModelMapping = nil
		} else {
			mapping, parseErr := parseUpstreamStringMapping(raw)
			if parseErr != nil {
				return UpstreamAccountOptions{}, errors.New("compact_model_mapping must contain string model pairs")
			}
			options.CompactModelMapping = mapping
		}
	}
	if raw, ok := credentials["openai_capabilities"]; ok {
		if raw == nil {
			options.OpenAIEndpointCapabilities = nil
		} else {
			capabilities, parseErr := parseOpenAIEndpointCapabilities(raw)
			if parseErr != nil {
				return UpstreamAccountOptions{}, errors.New("openai_capabilities must be a string array or boolean map")
			}
			options.OpenAIEndpointCapabilities = capabilities
		}
	}
	if raw, ok := credentials["temp_unschedulable_enabled"]; ok {
		if raw == nil {
			options.TempUnschedulableEnabled = false
		} else {
			enabled, valid := raw.(bool)
			if !valid {
				return UpstreamAccountOptions{}, errors.New("temp_unschedulable_enabled must be a boolean")
			}
			options.TempUnschedulableEnabled = enabled
		}
	}
	if raw, ok := credentials["temp_unschedulable_rules"]; ok {
		if raw == nil {
			options.TempUnschedulableRules = nil
		} else {
			rules, parseErr := ParseTempUnschedulableRules(raw)
			if parseErr != nil {
				return UpstreamAccountOptions{}, parseErr
			}
			options.TempUnschedulableRules = rules
		}
	}
	if err := validateUpstreamCompactModelMapping(options.CompactModelMapping); err != nil {
		return UpstreamAccountOptions{}, err
	}
	return options, nil
}

func ParseTempUnschedulableRules(raw any) ([]TempUnschedulableRule, error) {
	if raw == nil {
		return nil, nil
	}
	switch rules := raw.(type) {
	case []TempUnschedulableRule:
		return NormalizeTempUnschedulableRules(rules), nil
	case []any:
		parsed := make([]TempUnschedulableRule, 0, len(rules))
		for _, item := range rules {
			entry, ok := item.(map[string]any)
			if !ok || entry == nil {
				return nil, errors.New("temp_unschedulable_rules entries must be objects")
			}
			parsed = append(parsed, TempUnschedulableRule{
				ErrorCode:       parseTempUnschedInt(entry["error_code"]),
				Keywords:        parseTempUnschedStrings(entry["keywords"]),
				DurationMinutes: parseTempUnschedInt(entry["duration_minutes"]),
				Description:     parseTempUnschedString(entry["description"]),
			})
		}
		return NormalizeTempUnschedulableRules(parsed), nil
	default:
		encoded, err := common.Marshal(raw)
		if err != nil {
			return nil, errors.New("temp_unschedulable_rules must be an array")
		}
		var typed []TempUnschedulableRule
		if err := common.Unmarshal(encoded, &typed); err != nil {
			return nil, errors.New("temp_unschedulable_rules must be an array")
		}
		return NormalizeTempUnschedulableRules(typed), nil
	}
}

func NormalizeTempUnschedulableRules(rules []TempUnschedulableRule) []TempUnschedulableRule {
	if len(rules) == 0 {
		return nil
	}
	normalized := make([]TempUnschedulableRule, 0, len(rules))
	for _, rule := range rules {
		keywords := make([]string, 0, len(rule.Keywords))
		for _, keyword := range rule.Keywords {
			keyword = strings.TrimSpace(keyword)
			if keyword != "" {
				keywords = append(keywords, keyword)
			}
		}
		if rule.ErrorCode < 100 || rule.ErrorCode > 599 || rule.DurationMinutes <= 0 || len(keywords) == 0 {
			continue
		}
		normalized = append(normalized, TempUnschedulableRule{
			ErrorCode:       rule.ErrorCode,
			Keywords:        keywords,
			DurationMinutes: rule.DurationMinutes,
			Description:     strings.TrimSpace(rule.Description),
		})
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

func parseTempUnschedString(value any) string {
	s, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(s)
}

func parseTempUnschedStrings(value any) []string {
	if value == nil {
		return nil
	}
	var raw []string
	switch v := value.(type) {
	case []string:
		raw = v
	case []any:
		raw = make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				raw = append(raw, s)
			}
		}
	case string:
		for _, part := range strings.Split(v, ",") {
			raw = append(raw, part)
		}
	default:
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		s := strings.TrimSpace(item)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func parseTempUnschedInt(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int32:
		return int(v)
	case int64:
		return int(v)
	case float64:
		return int(v)
	case float32:
		return int(v)
	case string:
		if i, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return i
		}
	}
	return 0
}

func parseUpstreamStringMapping(raw any) (map[string]string, error) {
	result := map[string]string{}
	switch mapping := raw.(type) {
	case map[string]string:
		for source, target := range mapping {
			source = strings.TrimSpace(source)
			target = strings.TrimSpace(target)
			if source == "" || target == "" {
				return nil, errors.New("mapping entries require source and target models")
			}
			result[source] = target
		}
	case map[string]any:
		for source, value := range mapping {
			target, ok := value.(string)
			source = strings.TrimSpace(source)
			target = strings.TrimSpace(target)
			if !ok || source == "" || target == "" {
				return nil, errors.New("mapping entries require string source and target models")
			}
			result[source] = target
		}
	default:
		return nil, errors.New("mapping must be an object")
	}
	return result, nil
}

func parseOpenAIEndpointCapabilities(raw any) ([]string, error) {
	selected := map[string]bool{}
	add := func(value string) {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "chat_completions" || value == "embeddings" {
			selected[value] = true
		}
	}
	switch capabilities := raw.(type) {
	case []string:
		for _, value := range capabilities {
			add(value)
		}
	case []any:
		for _, item := range capabilities {
			value, ok := item.(string)
			if !ok {
				return nil, errors.New("capability entries must be strings")
			}
			add(value)
		}
	case map[string]bool:
		for value, enabled := range capabilities {
			if enabled {
				add(value)
			}
		}
	case map[string]any:
		for value, rawEnabled := range capabilities {
			enabled, ok := rawEnabled.(bool)
			if !ok {
				return nil, errors.New("capability map values must be booleans")
			}
			if enabled {
				add(value)
			}
		}
	default:
		return nil, errors.New("capabilities must be an array or object")
	}
	result := make([]string, 0, 2)
	for _, capability := range []string{"chat_completions", "embeddings"} {
		if selected[capability] {
			result = append(result, capability)
		}
	}
	return result, nil
}

func ValidateUpstreamAccountOptions(account *UpstreamAccount) error {
	options, err := ParseUpstreamAccountOptions(account.Extra)
	if err != nil {
		return err
	}
	if account.Platform != constant.UpstreamPlatformOpenAI {
		if options.OpenAIPassthrough || options.OpenAIOAuthWSMode != "" || options.OpenAIAPIKeyWSMode != "" ||
			options.OpenAILongContextBillingEnabled || options.CodexCLIOnly || options.CodexCLIOnlyAllowAppServer ||
			options.OpenAICompactMode != "" || options.OpenAICompactSupported != nil || len(options.CompactModelMapping) > 0 || options.OpenAIResponsesMode != "" || options.OpenAIResponsesSupported != nil ||
			len(options.OpenAIEndpointCapabilities) > 0 {
			return errors.New("OpenAI account options require the OpenAI platform")
		}
	}
	if account.Platform != constant.UpstreamPlatformAnthropic &&
		(options.AnthropicPassthrough || options.AnthropicAPIKeyAuthScheme != "") {
		return errors.New("Anthropic account options require the Anthropic platform")
	}
	if options.CodexCLIOnlyAllowAppServer && !options.CodexCLIOnly {
		return errors.New("Codex app-server access requires Codex-only mode")
	}
	if options.CodexCLIOnly && account.Type != constant.UpstreamAccountTypeOAuth {
		return errors.New("Codex-only mode requires an OpenAI OAuth account")
	}
	if account.Platform == constant.UpstreamPlatformOpenAI {
		switch account.Type {
		case constant.UpstreamAccountTypeOAuth:
			if options.OpenAIAPIKeyWSMode != "" || options.OpenAIResponsesMode != "" || options.OpenAIResponsesSupported != nil || len(options.OpenAIEndpointCapabilities) > 0 {
				return errors.New("OpenAI API key options require an API key account")
			}
		case constant.UpstreamAccountTypeAPIKey:
			if options.OpenAIOAuthWSMode != "" || options.CodexCLIOnly || options.CodexCLIOnlyAllowAppServer {
				return errors.New("OpenAI OAuth options require an OAuth account")
			}
		}
	}
	if account.Platform == constant.UpstreamPlatformAnthropic && account.Type != constant.UpstreamAccountTypeAPIKey &&
		(options.AnthropicPassthrough || options.AnthropicAPIKeyAuthScheme != "") {
		return errors.New("Anthropic API key options require an API key account")
	}
	if err := validateUpstreamOptionValue(options.OpenAIOAuthWSMode, UpstreamOpenAIWSModeOff, UpstreamOpenAIWSModeContextPool, UpstreamOpenAIWSModePassthrough, UpstreamOpenAIWSModeHTTPBridge); err != nil {
		return errors.New("unsupported OpenAI OAuth WebSocket mode")
	}
	if err := validateUpstreamOptionValue(options.OpenAIAPIKeyWSMode, UpstreamOpenAIWSModeOff, UpstreamOpenAIWSModeContextPool, UpstreamOpenAIWSModePassthrough, UpstreamOpenAIWSModeHTTPBridge); err != nil {
		return errors.New("unsupported OpenAI API key WebSocket mode")
	}
	if err := validateUpstreamOptionValue(options.OpenAICompactMode, UpstreamOpenAICompactModeAuto, UpstreamOpenAICompactModeForceOn, UpstreamOpenAICompactModeForceOff); err != nil {
		return errors.New("unsupported OpenAI compact mode")
	}
	if err := validateUpstreamOptionValue(options.OpenAIResponsesMode, UpstreamOpenAIResponsesModeAuto, UpstreamOpenAIResponsesModeForceResponses, UpstreamOpenAIResponsesModeForceChatCompletions); err != nil {
		return errors.New("unsupported OpenAI responses mode")
	}
	if err := validateUpstreamOptionValue(options.AnthropicAPIKeyAuthScheme, UpstreamAnthropicAuthSchemeAPIKey, UpstreamAnthropicAuthSchemeBearer); err != nil {
		return errors.New("unsupported Anthropic API key authentication scheme")
	}
	if err := validateUpstreamCompactModelMapping(options.CompactModelMapping); err != nil {
		return err
	}
	seenCapabilities := make(map[string]struct{}, len(options.OpenAIEndpointCapabilities))
	for _, capability := range options.OpenAIEndpointCapabilities {
		capability = strings.ToLower(strings.TrimSpace(capability))
		if capability != "chat_completions" && capability != "embeddings" {
			return errors.New("unsupported OpenAI endpoint capability")
		}
		if _, exists := seenCapabilities[capability]; exists {
			return errors.New("OpenAI endpoint capabilities must be unique")
		}
		seenCapabilities[capability] = struct{}{}
	}
	if options.TempUnschedulableEnabled && len(options.TempUnschedulableRules) == 0 {
		return errors.New("temporary unschedulable rules require at least one valid rule")
	}
	return nil
}

func validateUpstreamCompactModelMapping(mapping map[string]string) error {
	for source, target := range mapping {
		source = strings.TrimSpace(source)
		target = strings.TrimSpace(target)
		if source == "" || target == "" {
			return errors.New("compact model mapping entries require source and target models")
		}
		wildcardIndex := strings.Index(source, "*")
		if wildcardIndex >= 0 && (wildcardIndex != len(source)-1 || strings.LastIndex(source, "*") != wildcardIndex) {
			return errors.New("compact model mapping wildcard can only appear once at the end")
		}
		if strings.Contains(target, "*") {
			return errors.New("compact model mapping target cannot contain a wildcard")
		}
	}
	return nil
}

func (options UpstreamAccountOptions) OpenAIWSMode(accountType string) string {
	mode := options.OpenAIAPIKeyWSMode
	if accountType == constant.UpstreamAccountTypeOAuth {
		mode = options.OpenAIOAuthWSMode
	}
	if mode == "" {
		return UpstreamOpenAIWSModeOff
	}
	return mode
}

func (options UpstreamAccountOptions) EffectiveOpenAIResponsesMode() string {
	if options.OpenAIResponsesMode == UpstreamOpenAIResponsesModeForceResponses || options.OpenAIResponsesMode == UpstreamOpenAIResponsesModeForceChatCompletions {
		return options.OpenAIResponsesMode
	}
	if options.OpenAIResponsesSupported != nil && !*options.OpenAIResponsesSupported {
		return UpstreamOpenAIResponsesModeForceChatCompletions
	}
	return options.OpenAIResponsesMode
}

func (options UpstreamAccountOptions) AllowsOpenAICompact() bool {
	switch options.OpenAICompactMode {
	case UpstreamOpenAICompactModeForceOn:
		return true
	case UpstreamOpenAICompactModeForceOff:
		return false
	}
	return options.OpenAICompactSupported == nil || *options.OpenAICompactSupported
}

func (options UpstreamAccountOptions) SupportsOpenAIEndpoint(requestPath string) bool {
	if len(options.OpenAIEndpointCapabilities) == 0 {
		return true
	}
	wanted := "chat_completions"
	if strings.Contains(strings.ToLower(requestPath), "embedding") {
		wanted = "embeddings"
	}
	for _, capability := range options.OpenAIEndpointCapabilities {
		if strings.ToLower(strings.TrimSpace(capability)) == wanted {
			return true
		}
	}
	return false
}

func validateUpstreamOptionValue(value string, allowed ...string) error {
	if value == "" {
		return nil
	}
	for _, candidate := range allowed {
		if value == candidate {
			return nil
		}
	}
	return errors.New("unsupported option value")
}
