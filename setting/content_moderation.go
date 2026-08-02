package setting

import (
	"encoding/json"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
)

const (
	ContentModerationOptionKey = "ContentModerationConfig"

	ContentModerationModePreBlock = "pre_block"
	ContentModerationModeObserve  = "observe"

	ContentModerationModelFilterAll     = "all"
	ContentModerationModelFilterInclude = "include"
	ContentModerationModelFilterExclude = "exclude"

	defaultContentModerationBaseURL   = "https://api.openai.com"
	defaultContentModerationModel     = "omni-moderation-latest"
	defaultContentModerationTimeoutMS = 3000
	maxContentModerationTimeoutMS     = 30000
	maxContentModerationScopeGroups   = 1000
	maxContentModerationScopeModels   = 1000
	maxContentModerationScopeItemLen  = 200
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

// ContentModerationModelFilter selects which requested models are audited.
// Type is one of: all | include | exclude.
type ContentModerationModelFilter struct {
	Type   string   `json:"type"`
	Models []string `json:"models"`
}

// ContentModerationConfig is the global OpenAI Moderations gate configuration.
// Audit scope (all_groups / groups / model_filter) mirrors sub2api risk-control
// 「审计范围」: which request groups and models are subject to Moderations.
// Groups are StarNexus string channel/request groups (not numeric IDs).
type ContentModerationConfig struct {
	Enabled     bool                         `json:"enabled"`
	Mode        string                       `json:"mode"`
	BaseURL     string                       `json:"base_url"`
	Model       string                       `json:"model"`
	APIKeys     []string                     `json:"api_keys,omitempty"`
	TimeoutMS   int                          `json:"timeout_ms"`
	AllGroups   bool                         `json:"all_groups"`
	Groups      []string                     `json:"groups"`
	ModelFilter ContentModerationModelFilter `json:"model_filter"`
	Thresholds  map[string]float64           `json:"thresholds"`
}

// ContentModerationConfigView is the admin-facing DTO with masked API keys.
type ContentModerationConfigView struct {
	Enabled        bool                         `json:"enabled"`
	Mode           string                       `json:"mode"`
	BaseURL        string                       `json:"base_url"`
	Model          string                       `json:"model"`
	APIKeyCount    int                          `json:"api_key_count"`
	APIKeyMasks    []string                     `json:"api_key_masks"`
	APIKeys        []string                     `json:"api_keys"`
	TimeoutMS      int                          `json:"timeout_ms"`
	AllGroups      bool                         `json:"all_groups"`
	Groups         []string                     `json:"groups"`
	ModelFilter    ContentModerationModelFilter `json:"model_filter"`
	Thresholds     map[string]float64           `json:"thresholds"`
	KeysConfigured bool                         `json:"keys_configured"`
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
		Enabled:   false,
		Mode:      ContentModerationModePreBlock,
		BaseURL:   defaultContentModerationBaseURL,
		Model:     defaultContentModerationModel,
		APIKeys:   nil,
		TimeoutMS: defaultContentModerationTimeoutMS,
		AllGroups: true,
		Groups:    nil,
		ModelFilter: ContentModerationModelFilter{
			Type:   ContentModerationModelFilterAll,
			Models: nil,
		},
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

	// Legacy configs predate audit scope; missing all_groups must stay "all groups".
	var raw map[string]json.RawMessage
	if rawErr := common.UnmarshalJsonStr(jsonString, &raw); rawErr == nil {
		if _, ok := raw["all_groups"]; !ok {
			incoming.AllGroups = true
		}
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
		AllGroups:      cfg.AllGroups,
		Groups:         append([]string(nil), cfg.Groups...),
		ModelFilter:    cloneContentModerationModelFilter(cfg.ModelFilter),
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
	cfg.Groups = normalizeContentModerationScopeList(cfg.Groups, maxContentModerationScopeGroups)
	if cfg.AllGroups {
		cfg.Groups = nil
	}
	cfg.ModelFilter = normalizeContentModerationModelFilter(cfg.ModelFilter)
	cfg.Thresholds = mergeContentModerationThresholds(ContentModerationDefaultThresholds(), cfg.Thresholds)
}

// IncludesGroup reports whether the request channel/using group is in audit scope.
// Empty group is out of scope when AllGroups is false (same semantics as sub2api nil GroupID).
func (cfg *ContentModerationConfig) IncludesGroup(group string) bool {
	if cfg == nil || cfg.AllGroups {
		return true
	}
	group = strings.TrimSpace(group)
	if group == "" {
		return false
	}
	for _, item := range cfg.Groups {
		if strings.EqualFold(item, group) {
			return true
		}
	}
	return false
}

// IncludesModel reports whether the client-requested model is in audit scope.
func (cfg *ContentModerationConfig) IncludesModel(modelName string) bool {
	if cfg == nil {
		return true
	}
	filter := normalizeContentModerationModelFilter(cfg.ModelFilter)
	switch filter.Type {
	case ContentModerationModelFilterInclude:
		return contentModerationModelListContains(filter.Models, modelName)
	case ContentModerationModelFilterExclude:
		return !contentModerationModelListContains(filter.Models, modelName)
	default:
		return true
	}
}

func contentModerationConfigFromView(view ContentModerationConfigView) ContentModerationConfig {
	return ContentModerationConfig{
		Enabled:     view.Enabled,
		Mode:        view.Mode,
		BaseURL:     view.BaseURL,
		Model:       view.Model,
		APIKeys:     view.APIKeys,
		TimeoutMS:   view.TimeoutMS,
		AllGroups:   view.AllGroups,
		Groups:      view.Groups,
		ModelFilter: view.ModelFilter,
		Thresholds:  view.Thresholds,
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
		Enabled:     cfg.Enabled,
		Mode:        cfg.Mode,
		BaseURL:     cfg.BaseURL,
		Model:       cfg.Model,
		APIKeys:     append([]string(nil), cfg.APIKeys...),
		TimeoutMS:   cfg.TimeoutMS,
		AllGroups:   cfg.AllGroups,
		Groups:      append([]string(nil), cfg.Groups...),
		ModelFilter: cloneContentModerationModelFilter(cfg.ModelFilter),
		Thresholds:  cloneFloatMap(cfg.Thresholds),
	}
}

func normalizeContentModerationModelFilter(filter ContentModerationModelFilter) ContentModerationModelFilter {
	out := ContentModerationModelFilter{
		Type:   normalizeContentModerationModelFilterType(filter.Type),
		Models: normalizeContentModerationScopeList(filter.Models, maxContentModerationScopeModels),
	}
	if out.Type == ContentModerationModelFilterAll {
		out.Models = nil
	} else if len(out.Models) == 0 {
		// Include/exclude with an empty list is invalid; fall back to all models.
		out.Type = ContentModerationModelFilterAll
		out.Models = nil
	}
	return out
}

func cloneContentModerationModelFilter(filter ContentModerationModelFilter) ContentModerationModelFilter {
	normalized := normalizeContentModerationModelFilter(filter)
	return ContentModerationModelFilter{
		Type:   normalized.Type,
		Models: append([]string(nil), normalized.Models...),
	}
}

func normalizeContentModerationModelFilterType(filterType string) string {
	switch strings.ToLower(strings.TrimSpace(filterType)) {
	case ContentModerationModelFilterInclude:
		return ContentModerationModelFilterInclude
	case ContentModerationModelFilterExclude:
		return ContentModerationModelFilterExclude
	default:
		return ContentModerationModelFilterAll
	}
}

func normalizeContentModerationScopeList(values []string, maxItems int) []string {
	if maxItems <= 0 {
		maxItems = maxContentModerationScopeGroups
	}
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, raw := range values {
		item := strings.TrimSpace(raw)
		if item == "" {
			continue
		}
		runes := []rune(item)
		if len(runes) > maxContentModerationScopeItemLen {
			item = string(runes[:maxContentModerationScopeItemLen])
		}
		key := strings.ToLower(item)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
		if len(out) >= maxItems {
			break
		}
	}
	return out
}

func contentModerationModelListContains(models []string, modelName string) bool {
	modelName = strings.TrimSpace(modelName)
	if modelName == "" {
		return false
	}
	for _, item := range models {
		if strings.EqualFold(item, modelName) {
			return true
		}
	}
	return false
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

// MaskSecretTail returns a short masked form of a secret for admin UI/API responses.
func MaskSecretTail(secret string) string {
	return maskSecretTail(secret)
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

// IsMaskedAPIKeyPlaceholder reports whether value is a masked key placeholder
// from ContentModerationConfigToView (not a real secret).
func IsMaskedAPIKeyPlaceholder(value string) bool {
	return isMaskedAPIKeyPlaceholder(value)
}
