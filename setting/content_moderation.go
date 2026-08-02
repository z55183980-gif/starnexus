package setting

import (
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
)

const (
	ContentModerationOptionKey = "ContentModerationConfig"

	ContentModerationModePreBlock = "pre_block"
	ContentModerationModeObserve  = "observe"

	defaultContentModerationBaseURL   = "https://api.openai.com"
	defaultContentModerationModel     = "omni-moderation-latest"
	defaultContentModerationTimeoutMS = 3000
	maxContentModerationTimeoutMS     = 30000
)

var contentModerationCategoryOrder = []string{
	"harassment",
	"harassment/threatening",
	"hate",
	"hate/threatening",
	"illicit",
	"illicit/violent",
	"self-harm",
	"self-harm/intent",
	"self-harm/instructions",
	"sexual",
	"sexual/minors",
	"violence",
	"violence/graphic",
}

// ContentModerationConfig is the global OpenAI Moderations gate configuration.
type ContentModerationConfig struct {
	Enabled    bool               `json:"enabled"`
	Mode       string             `json:"mode"`
	BaseURL    string             `json:"base_url"`
	Model      string             `json:"model"`
	APIKeys    []string           `json:"api_keys,omitempty"`
	TimeoutMS  int                `json:"timeout_ms"`
	Thresholds map[string]float64 `json:"thresholds"`
}

// ContentModerationConfigView is the admin-facing DTO with masked API keys.
type ContentModerationConfigView struct {
	Enabled        bool               `json:"enabled"`
	Mode           string             `json:"mode"`
	BaseURL        string             `json:"base_url"`
	Model          string             `json:"model"`
	APIKeyCount    int                `json:"api_key_count"`
	APIKeyMasks    []string           `json:"api_key_masks"`
	APIKeys        []string           `json:"api_keys"`
	TimeoutMS      int                `json:"timeout_ms"`
	Thresholds     map[string]float64 `json:"thresholds"`
	KeysConfigured bool               `json:"keys_configured"`
}

var (
	contentModerationMu     sync.RWMutex
	contentModerationConfig = defaultContentModerationConfig()
)

func ContentModerationDefaultThresholds() map[string]float64 {
	return map[string]float64{
		"harassment":             0.98,
		"harassment/threatening": 0.90,
		"hate":                   0.65,
		"hate/threatening":       0.65,
		"illicit":                0.95,
		"illicit/violent":        0.95,
		"self-harm":              0.65,
		"self-harm/intent":       0.85,
		"self-harm/instructions": 0.65,
		"sexual":                 0.65,
		"sexual/minors":          0.65,
		"violence":               0.95,
		"violence/graphic":       0.95,
	}
}

func ContentModerationCategories() []string {
	out := make([]string, len(contentModerationCategoryOrder))
	copy(out, contentModerationCategoryOrder)
	return out
}

func defaultContentModerationConfig() ContentModerationConfig {
	return ContentModerationConfig{
		Enabled:    false,
		Mode:       ContentModerationModePreBlock,
		BaseURL:    defaultContentModerationBaseURL,
		Model:      defaultContentModerationModel,
		APIKeys:    nil,
		TimeoutMS:  defaultContentModerationTimeoutMS,
		Thresholds: ContentModerationDefaultThresholds(),
	}
}

func GetContentModerationConfig() ContentModerationConfig {
	contentModerationMu.RLock()
	defer contentModerationMu.RUnlock()
	return cloneContentModerationConfig(contentModerationConfig)
}

func ContentModerationConfig2JsonString() string {
	return ContentModerationConfigJSON(GetContentModerationConfig())
}

func ContentModerationConfigViewJsonString() string {
	view := ContentModerationConfigToView(GetContentModerationConfig())
	bytes, err := common.Marshal(view)
	if err != nil {
		common.SysLog("error marshalling content moderation config view: " + err.Error())
		return "{}"
	}
	return string(bytes)
}

func UpdateContentModerationConfigByJsonString(jsonString string) error {
	cfg, err := ParseContentModerationConfigJSON(jsonString, GetContentModerationConfig().APIKeys)
	if err != nil {
		return err
	}
	contentModerationMu.Lock()
	contentModerationConfig = cfg
	contentModerationMu.Unlock()
	return nil
}

// ParseContentModerationConfigJSON parses admin/DB JSON, merges masked API keys
// against existingKeys, and returns a normalized config without mutating globals.
func ParseContentModerationConfigJSON(jsonString string, existingKeys []string) (ContentModerationConfig, error) {
	if strings.TrimSpace(jsonString) == "" {
		return defaultContentModerationConfig(), nil
	}

	var incoming ContentModerationConfig
	if err := common.UnmarshalJsonStr(jsonString, &incoming); err != nil {
		var view ContentModerationConfigView
		if viewErr := common.UnmarshalJsonStr(jsonString, &view); viewErr != nil {
			return ContentModerationConfig{}, err
		}
		incoming = contentModerationConfigFromView(view)
	}
	incoming.APIKeys = mergeContentModerationAPIKeys(existingKeys, incoming.APIKeys)
	incoming.normalize()
	return incoming, nil
}

func ContentModerationConfigJSON(cfg ContentModerationConfig) string {
	cfg.normalize()
	bytes, err := common.Marshal(cfg)
	if err != nil {
		common.SysLog("error marshalling content moderation config: " + err.Error())
		return "{}"
	}
	return string(bytes)
}

func ContentModerationConfigToView(cfg ContentModerationConfig) ContentModerationConfigView {
	cfg.normalize()
	masks := make([]string, 0, len(cfg.APIKeys))
	placeholders := make([]string, 0, len(cfg.APIKeys))
	for _, key := range cfg.APIKeys {
		masks = append(masks, maskSecretTail(key))
		placeholders = append(placeholders, maskSecretPlaceholder(key))
	}
	return ContentModerationConfigView{
		Enabled:        cfg.Enabled,
		Mode:           cfg.Mode,
		BaseURL:        cfg.BaseURL,
		Model:          cfg.Model,
		APIKeyCount:    len(cfg.APIKeys),
		APIKeyMasks:    masks,
		APIKeys:        placeholders,
		TimeoutMS:      cfg.TimeoutMS,
		Thresholds:     cloneFloatMap(cfg.Thresholds),
		KeysConfigured: len(cfg.APIKeys) > 0,
	}
}

func (cfg *ContentModerationConfig) normalize() {
	if cfg == nil {
		return
	}
	cfg.Mode = strings.ToLower(strings.TrimSpace(cfg.Mode))
	switch cfg.Mode {
	case ContentModerationModeObserve, ContentModerationModePreBlock:
	default:
		cfg.Mode = ContentModerationModePreBlock
	}
	cfg.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultContentModerationBaseURL
	}
	cfg.Model = strings.TrimSpace(cfg.Model)
	if cfg.Model == "" {
		cfg.Model = defaultContentModerationModel
	}
	if cfg.TimeoutMS <= 0 {
		cfg.TimeoutMS = defaultContentModerationTimeoutMS
	}
	if cfg.TimeoutMS > maxContentModerationTimeoutMS {
		cfg.TimeoutMS = maxContentModerationTimeoutMS
	}
	cfg.APIKeys = normalizeContentModerationAPIKeys(cfg.APIKeys)
	cfg.Thresholds = mergeContentModerationThresholds(ContentModerationDefaultThresholds(), cfg.Thresholds)
}

func contentModerationConfigFromView(view ContentModerationConfigView) ContentModerationConfig {
	return ContentModerationConfig{
		Enabled:    view.Enabled,
		Mode:       view.Mode,
		BaseURL:    view.BaseURL,
		Model:      view.Model,
		APIKeys:    view.APIKeys,
		TimeoutMS:  view.TimeoutMS,
		Thresholds: view.Thresholds,
	}
}

func mergeContentModerationAPIKeys(existing, incoming []string) []string {
	existing = normalizeContentModerationAPIKeys(existing)
	incoming = stringsTrimList(incoming)
	if len(incoming) == 0 {
		return nil
	}

	out := make([]string, 0, len(incoming))
	existingIdx := 0
	for _, key := range incoming {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if isMaskedAPIKeyPlaceholder(key) {
			if existingIdx < len(existing) {
				out = append(out, existing[existingIdx])
				existingIdx++
			}
			continue
		}
		out = append(out, key)
		existingIdx++
	}
	return normalizeContentModerationAPIKeys(out)
}

func normalizeContentModerationAPIKeys(keys []string) []string {
	out := make([]string, 0, len(keys))
	seen := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" || isMaskedAPIKeyPlaceholder(key) {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, key)
	}
	return out
}

func mergeContentModerationThresholds(defaults, overrides map[string]float64) map[string]float64 {
	out := cloneFloatMap(defaults)
	for _, category := range contentModerationCategoryOrder {
		if overrides == nil {
			continue
		}
		if value, ok := overrides[category]; ok {
			if value < 0 {
				value = 0
			}
			if value > 1 {
				value = 1
			}
			out[category] = value
		}
	}
	return out
}

func cloneContentModerationConfig(cfg ContentModerationConfig) ContentModerationConfig {
	return ContentModerationConfig{
		Enabled:    cfg.Enabled,
		Mode:       cfg.Mode,
		BaseURL:    cfg.BaseURL,
		Model:      cfg.Model,
		APIKeys:    append([]string(nil), cfg.APIKeys...),
		TimeoutMS:  cfg.TimeoutMS,
		Thresholds: cloneFloatMap(cfg.Thresholds),
	}
}

func cloneFloatMap(in map[string]float64) map[string]float64 {
	out := make(map[string]float64, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func stringsTrimList(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, strings.TrimSpace(value))
	}
	return out
}

func maskSecretTail(secret string) string {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return ""
	}
	runes := []rune(secret)
	if len(runes) <= 4 {
		return "****"
	}
	return "****" + string(runes[len(runes)-4:])
}

func maskSecretPlaceholder(secret string) string {
	masked := maskSecretTail(secret)
	if masked == "" {
		return ""
	}
	return masked
}

func isMaskedAPIKeyPlaceholder(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	if strings.HasPrefix(value, "****") {
		return true
	}
	return strings.Contains(value, "***") && !strings.HasPrefix(strings.ToLower(value), "sk-") &&
		!strings.HasPrefix(strings.ToLower(value), "rk-")
}
