package billing_setting

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/samber/lo"
)

const (
	BillingModeRatio           = "ratio"
	BillingModeTieredExpr      = "tiered_expr"
	BillingModeField           = "billing_mode"
	BillingExprField           = "billing_expr"
	TokenPricingField          = "token_pricing"
	CacheBillingOffsetBpsField = "cache_billing_offset_bps"
)

type TokenPricingSetting struct {
	Enabled     bool               `json:"enabled"`
	InputRatio  float64            `json:"input_ratio,omitempty"`
	OutputRatio float64            `json:"output_ratio,omitempty"`
	Rules       []TokenPricingRule `json:"rules,omitempty"`
}

type TokenPricingRule struct {
	Name        string   `json:"name,omitempty"`
	Enabled     bool     `json:"enabled"`
	InputRatio  float64  `json:"input_ratio"`
	OutputRatio float64  `json:"output_ratio"`
	Models      []string `json:"models,omitempty"`
	Groups      []string `json:"groups,omitempty"`
	UserIds     []int    `json:"user_ids,omitempty"`
}

type TokenPricingContext struct {
	Model  string
	Group  string
	UserId int
}

type EffectiveTokenPricing struct {
	Enabled     bool
	InputRatio  float64
	OutputRatio float64
	RuleNames   []string
}

// BillingSetting is managed by config.GlobalConfig.Register.
// DB keys: billing_setting.billing_mode, billing_setting.billing_expr,
// billing_setting.token_pricing, billing_setting.cache_billing_offset_bps
type BillingSetting struct {
	BillingMode           map[string]string   `json:"billing_mode"`
	BillingExpr           map[string]string   `json:"billing_expr"`
	TokenPricing          TokenPricingSetting `json:"token_pricing"`
	CacheBillingOffsetBps map[string]int      `json:"cache_billing_offset_bps"`
}

var billingSetting = BillingSetting{
	BillingMode:           make(map[string]string),
	BillingExpr:           make(map[string]string),
	CacheBillingOffsetBps: make(map[string]int),
	TokenPricing: TokenPricingSetting{
		Enabled:     false,
		InputRatio:  1,
		OutputRatio: 1,
	},
}

func init() {
	config.GlobalConfig.Register("billing_setting", &billingSetting)
}

// ---------------------------------------------------------------------------
// Read accessors (hot path, must be fast)
// ---------------------------------------------------------------------------

func GetBillingMode(model string) string {
	if mode, ok := billingSetting.BillingMode[model]; ok {
		return mode
	}
	return BillingModeRatio
}

func GetBillingExpr(model string) (string, bool) {
	expr, ok := billingSetting.BillingExpr[model]
	return expr, ok
}

func GetBillingModeCopy() map[string]string {
	return lo.Assign(billingSetting.BillingMode)
}

func GetBillingExprCopy() map[string]string {
	return lo.Assign(billingSetting.BillingExpr)
}

// GetCacheBillingOffsetBps returns the model-specific cache billing reduction
// in basis points. Invalid persisted values are treated as disabled so the hot
// billing path remains safe even if an older database contains malformed data.
func GetCacheBillingOffsetBps(model string) int {
	offsetBps, ok := billingSetting.CacheBillingOffsetBps[model]
	if !ok || offsetBps <= 0 || offsetBps > 10000 {
		return 0
	}
	return offsetBps
}

func setCacheBillingOffsetBpsForTest(offsets map[string]int) func() {
	previous := billingSetting.CacheBillingOffsetBps
	billingSetting.CacheBillingOffsetBps = lo.Assign(offsets)
	return func() {
		billingSetting.CacheBillingOffsetBps = previous
	}
}

func GetTokenPricingSetting() TokenPricingSetting {
	setting := billingSetting.TokenPricing
	setting.InputRatio = normalizeRatio(setting.InputRatio, !setting.Enabled)
	setting.OutputRatio = normalizeRatio(setting.OutputRatio, !setting.Enabled)
	for i := range setting.Rules {
		setting.Rules[i].InputRatio = normalizeRatio(setting.Rules[i].InputRatio, false)
		setting.Rules[i].OutputRatio = normalizeRatio(setting.Rules[i].OutputRatio, false)
		setting.Rules[i].Models = normalizeStringList(setting.Rules[i].Models)
		setting.Rules[i].Groups = normalizeStringList(setting.Rules[i].Groups)
	}
	return setting
}

func SetTokenPricingSettingForTest(setting TokenPricingSetting) func() {
	previous := billingSetting.TokenPricing
	billingSetting.TokenPricing = setting
	return func() {
		billingSetting.TokenPricing = previous
	}
}

func IsTokenPricingEnabled() bool {
	return GetEffectiveTokenPricing(TokenPricingContext{}).Enabled
}

func GetEffectiveTokenPricing(ctx TokenPricingContext) EffectiveTokenPricing {
	setting := GetTokenPricingSetting()
	effective := EffectiveTokenPricing{InputRatio: 1, OutputRatio: 1}
	if !setting.Enabled {
		return effective
	}

	if len(setting.Rules) == 0 {
		effective.InputRatio = setting.InputRatio
		effective.OutputRatio = setting.OutputRatio
		effective.Enabled = effective.InputRatio != 1 || effective.OutputRatio != 1
		return effective
	}

	matched := false
	for _, rule := range setting.Rules {
		if !rule.Enabled || !ruleMatchesContext(rule, ctx) {
			continue
		}
		if !matched {
			effective.InputRatio = rule.InputRatio
			effective.OutputRatio = rule.OutputRatio
			matched = true
		}
		if rule.InputRatio > effective.InputRatio {
			effective.InputRatio = rule.InputRatio
		}
		if rule.OutputRatio > effective.OutputRatio {
			effective.OutputRatio = rule.OutputRatio
		}
		if strings.TrimSpace(rule.Name) != "" {
			effective.RuleNames = append(effective.RuleNames, strings.TrimSpace(rule.Name))
		}
	}
	effective.Enabled = matched && (effective.InputRatio != 1 || effective.OutputRatio != 1)
	return effective
}

func ApplyInputTokenPricing(tokens int) int {
	return ApplyInputTokenPricingForContext(tokens, TokenPricingContext{})
}

func ApplyOutputTokenPricing(tokens int) int {
	return ApplyOutputTokenPricingForContext(tokens, TokenPricingContext{})
}

func ApplyInputTokenPricingForContext(tokens int, ctx TokenPricingContext) int {
	pricing := GetEffectiveTokenPricing(ctx)
	if !pricing.Enabled {
		return tokens
	}
	return roundTokenCount(float64(tokens) * pricing.InputRatio)
}

func ApplyOutputTokenPricingForContext(tokens int, ctx TokenPricingContext) int {
	pricing := GetEffectiveTokenPricing(ctx)
	if !pricing.Enabled {
		return tokens
	}
	return roundTokenCount(float64(tokens) * pricing.OutputRatio)
}

func roundTokenCount(value float64) int {
	if value <= 0 {
		return 0
	}
	return int(math.Round(value))
}

func normalizeRatio(ratio float64, defaultOne bool) float64 {
	if ratio < 0 {
		return 0
	}
	if ratio == 0 && defaultOne {
		return 1
	}
	return ratio
}

func normalizeStringList(values []string) []string {
	normalized := make([]string, 0, len(values))
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		normalized = append(normalized, value)
	}
	return normalized
}

func ruleMatchesContext(rule TokenPricingRule, ctx TokenPricingContext) bool {
	if len(rule.Models) > 0 && !stringListContains(rule.Models, ctx.Model) {
		return false
	}
	if len(rule.Groups) > 0 && !stringListContains(rule.Groups, ctx.Group) {
		return false
	}
	if len(rule.UserIds) > 0 && !intListContains(rule.UserIds, ctx.UserId) {
		return false
	}
	return true
}

func stringListContains(values []string, target string) bool {
	target = strings.TrimSpace(target)
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), target) {
			return true
		}
	}
	return false
}

func intListContains(values []int, target int) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func ParseUserIdList(raw string) ([]int, error) {
	parts := strings.Split(raw, ",")
	userIds := make([]int, 0, len(parts))
	seen := map[int]bool{}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		userId, err := strconv.Atoi(part)
		if err != nil || userId <= 0 {
			return nil, fmt.Errorf("invalid user id: %s", part)
		}
		if seen[userId] {
			continue
		}
		seen[userId] = true
		userIds = append(userIds, userId)
	}
	return userIds, nil
}

func GetPricingSyncData(base map[string]any) map[string]any {
	extra := make(map[string]any, 2)
	if modes := GetBillingModeCopy(); len(modes) > 0 {
		extra[BillingModeField] = modes
	}
	if exprs := GetBillingExprCopy(); len(exprs) > 0 {
		extra[BillingExprField] = exprs
	}
	return lo.Assign(base, extra)
}

// ---------------------------------------------------------------------------
// Smoke test (called externally for validation before save)
// ---------------------------------------------------------------------------

func SmokeTestExpr(exprStr string) error {
	return smokeTestExpr(exprStr)
}

func smokeTestExpr(exprStr string) error {
	vectors := []billingexpr.TokenParams{
		{P: 0, C: 0, Len: 0},
		{P: 1000, C: 1000, Len: 1000},
		{P: 100000, C: 100000, Len: 100000},
		{P: 1000000, C: 1000000, Len: 1000000},
	}
	requests := []billingexpr.RequestInput{
		{},
		{
			Headers: map[string]string{
				"anthropic-beta": "fast-mode-2026-02-01",
			},
			Body: []byte(`{"service_tier":"fast","stream_options":{"include_usage":true},"messages":[1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21]}`),
		},
	}

	for _, v := range vectors {
		for _, request := range requests {
			result, _, err := billingexpr.RunExprWithRequest(exprStr, v, request)
			if err != nil {
				return fmt.Errorf("vector {p=%g, c=%g}: run failed: %w", v.P, v.C, err)
			}
			if result < 0 {
				return fmt.Errorf("vector {p=%g, c=%g}: result %f < 0", v.P, v.C, result)
			}
		}
	}
	return nil
}
