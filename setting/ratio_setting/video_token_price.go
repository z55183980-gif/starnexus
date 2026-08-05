package ratio_setting

import (
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"
)

// VideoTokenTierPrice is a relative unit price for one resolution tier.
// Values share the same display unit as the model's input (ModelRatio) price;
// only tier/base ratios are used for billing.
type VideoTokenTierPrice struct {
	Base      float64 `json:"base"`       // no video input
	WithVideo float64 `json:"with_video"` // has video input
}

// VideoTokenModelPrice maps resolution keys (default/480p/720p/1080p/4k) to prices.
type VideoTokenModelPrice map[string]VideoTokenTierPrice

var videoTokenPriceMap = types.NewRWMap[string, VideoTokenModelPrice]()

var (
	defaultVideoTokenPriceOnce sync.Once
	defaultVideoTokenPriceJSON string
)

func VideoTokenPrice2JSONString() string {
	return videoTokenPriceMap.MarshalJSONString()
}

func UpdateVideoTokenPriceByJSONString(jsonStr string) error {
	return types.LoadFromJsonStringWithCallback(videoTokenPriceMap, jsonStr, InvalidateExposedDataCache)
}

func GetVideoTokenPriceCopy() map[string]VideoTokenModelPrice {
	return videoTokenPriceMap.ReadAll()
}

func GetVideoTokenModelPrice(modelName string) (VideoTokenModelPrice, bool) {
	return videoTokenPriceMap.Get(modelName)
}

// IsSeedanceVideoModel reports whether model uses Seedance 2.x video token billing UI.
func IsSeedanceVideoModel(modelName string) bool {
	n := strings.ToLower(strings.TrimSpace(modelName))
	if !strings.Contains(n, "seedance") {
		return false
	}
	if !strings.HasPrefix(n, "dreamina-") && !strings.HasPrefix(n, "doubao-") {
		return false
	}
	return strings.Contains(n, "seedance-2") || strings.Contains(n, "seedance2")
}

// NormalizeVideoResolution maps upstream/client aliases to price-table keys.
// Empty input stays empty (caller selects default/base tier).
func NormalizeVideoResolution(resolution string) string {
	res := strings.ToLower(strings.TrimSpace(resolution))
	switch res {
	case "", "default":
		return res
	case "480p", "480":
		return "480p"
	case "720p", "720":
		return "720p"
	case "1080p", "1080":
		return "1080p"
	case "4k", "2160p", "2160", "2k":
		// Upstream accepts 2K as the 4k billing tier alias.
		return "4k"
	default:
		return res
	}
}

func baseTierPrice(price VideoTokenModelPrice) (VideoTokenTierPrice, bool) {
	for _, k := range []string{"default", "720p", "480p"} {
		if tier, ok := price[k]; ok && tier.Base > 0 {
			return tier, true
		}
	}
	return VideoTokenTierPrice{}, false
}

// GetConfiguredVideoTokenRatio returns OtherRatio = tierPrice / configuredBase
// when VideoTokenPrice fully covers this request. ok=false means caller must use
// hardcoded fallback (incomplete/missing config must not shadow it).
func GetConfiguredVideoTokenRatio(modelName, resolution string, hasVideo bool) (float64, bool) {
	price, ok := videoTokenPriceMap.Get(modelName)
	if !ok || len(price) == 0 {
		return 0, false
	}
	baseTier, ok := baseTierPrice(price)
	if !ok || baseTier.Base <= 0 {
		return 0, false
	}
	tier, ok := pickConfiguredTierExact(price, resolution)
	if !ok {
		// Missing this resolution in config → hard fallback, do not pretend ratio=1.
		return 0, false
	}
	unit := tier.Base
	if hasVideo {
		if tier.WithVideo <= 0 {
			// with-video cell missing → hard fallback for this combo
			return 0, false
		}
		unit = tier.WithVideo
	} else if unit <= 0 {
		return 0, false
	}
	return unit / baseTier.Base, true
}

// pickConfiguredTierExact requires an explicit tier for the resolved key
// (default/720p/480p may alias each other). Unknown keys do not invent prices.
func pickConfiguredTierExact(price VideoTokenModelPrice, resolution string) (VideoTokenTierPrice, bool) {
	key := NormalizeVideoResolution(resolution)
	switch key {
	case "", "default":
		for _, k := range []string{"default", "720p", "480p"} {
			if tier, ok := price[k]; ok && (tier.Base > 0 || tier.WithVideo > 0) {
				return tier, true
			}
		}
		return VideoTokenTierPrice{}, false
	case "480p", "720p", "1080p", "4k":
		tier, ok := price[key]
		if !ok || (tier.Base <= 0 && tier.WithVideo <= 0) {
			return VideoTokenTierPrice{}, false
		}
		return tier, true
	default:
		return VideoTokenTierPrice{}, false
	}
}

// DefaultVideoTokenPriceJSON returns {} (admins configure per model; hardcoded fallback remains in doubao).
func DefaultVideoTokenPriceJSON() string {
	defaultVideoTokenPriceOnce.Do(func() {
		b, err := common.Marshal(map[string]VideoTokenModelPrice{})
		if err != nil {
			defaultVideoTokenPriceJSON = "{}"
			return
		}
		defaultVideoTokenPriceJSON = string(b)
	})
	return defaultVideoTokenPriceJSON
}
