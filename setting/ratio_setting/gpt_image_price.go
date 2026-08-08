package ratio_setting

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"
)

const (
	GPTImageTier1K = "1k"
	GPTImageTier2K = "2k"
	GPTImageTier4K = "4k"
)

// GPTImageModelPrice stores independent USD-per-image prices by output tier.
// A tier must be explicitly present; zero is a valid free price.
type GPTImageModelPrice map[string]float64

var gptImagePriceMap = types.NewRWMap[string, GPTImageModelPrice]()

var datedGPTImageModelPattern = regexp.MustCompile(`^(gpt-image-.+)-\d{4}-\d{2}-\d{2}$`)

func GPTImagePrice2JSONString() string {
	return gptImagePriceMap.MarshalJSONString()
}

func UpdateGPTImagePriceByJSONString(jsonStr string) error {
	var raw map[string]GPTImageModelPrice
	if err := common.UnmarshalJsonStr(jsonStr, &raw); err != nil {
		return err
	}
	for modelName, prices := range raw {
		if !IsGPTImageModel(modelName) {
			return fmt.Errorf("model %q is not a gpt-image model", modelName)
		}
		if modelName != strings.ToLower(strings.TrimSpace(modelName)) {
			return fmt.Errorf("gpt-image model name %q must be lowercase without surrounding spaces", modelName)
		}
		if len(prices) != 3 {
			return fmt.Errorf("model %q must configure all 1k, 2k, and 4k prices", modelName)
		}
		for _, requiredTier := range []string{GPTImageTier1K, GPTImageTier2K, GPTImageTier4K} {
			if _, ok := prices[requiredTier]; !ok {
				return fmt.Errorf("model %q is missing the %s price", modelName, requiredTier)
			}
		}
		for tier, price := range prices {
			if NormalizeGPTImageTier(tier) != strings.ToLower(strings.TrimSpace(tier)) {
				return fmt.Errorf("invalid gpt-image price tier %q for model %q", tier, modelName)
			}
			if price < 0 {
				return fmt.Errorf("gpt-image price cannot be negative for model %q tier %q", modelName, tier)
			}
		}
	}
	return types.LoadFromJsonStringWithCallback(gptImagePriceMap, jsonStr, InvalidateExposedDataCache)
}

func GetGPTImagePriceCopy() map[string]GPTImageModelPrice {
	return gptImagePriceMap.ReadAll()
}

func IsGPTImageModel(modelName string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(modelName)), "gpt-image-")
}

func NormalizeGPTImageTier(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch normalized {
	case "1k":
		return GPTImageTier1K
	case "", "auto", "default", "2k":
		return GPTImageTier2K
	case "4k":
		return GPTImageTier4K
	}

	parts := strings.Split(normalized, "x")
	if len(parts) != 2 {
		return ""
	}
	width, widthErr := strconv.Atoi(strings.TrimSpace(parts[0]))
	height, heightErr := strconv.Atoi(strings.TrimSpace(parts[1]))
	if widthErr != nil || heightErr != nil || width <= 0 || height <= 0 {
		return ""
	}
	maxDimension := width
	if height > maxDimension {
		maxDimension = height
	}
	if maxDimension <= 1024 {
		return GPTImageTier1K
	}
	if maxDimension <= 2048 {
		return GPTImageTier2K
	}
	return GPTImageTier4K
}

func GPTImageTierRank(tier string) int {
	if strings.TrimSpace(tier) == "" {
		return 0
	}
	switch NormalizeGPTImageTier(tier) {
	case GPTImageTier1K:
		return 1
	case GPTImageTier2K:
		return 2
	case GPTImageTier4K:
		return 3
	default:
		return 0
	}
}

func GetGPTImagePrice(modelName, tier string) (float64, bool) {
	if !IsGPTImageModel(modelName) {
		return 0, false
	}
	normalizedTier := NormalizeGPTImageTier(tier)
	if normalizedTier == "" {
		return 0, false
	}

	modelName = strings.ToLower(strings.TrimSpace(modelName))
	candidates := []string{modelName}
	if match := datedGPTImageModelPattern.FindStringSubmatch(modelName); len(match) == 2 {
		candidates = append(candidates, match[1])
	}
	for _, candidate := range candidates {
		prices, ok := gptImagePriceMap.Get(candidate)
		if !ok {
			continue
		}
		price, ok := prices[normalizedTier]
		if ok {
			return price, true
		}
	}
	return 0, false
}
