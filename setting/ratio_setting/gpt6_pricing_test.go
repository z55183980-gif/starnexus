package ratio_setting

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGPT6PricingDefaults(t *testing.T) {
	InitRatioSettings()
	for _, tc := range []struct {
		model string
		input float64
	}{
		{"gpt-6.1-sol", 2},
		{"gpt-6.1-terra", 1},
		{"gpt-6.2-astra", 8},
		{"gpt-6.2-sol", 2},
	} {
		t.Run(tc.model, func(t *testing.T) {
			ratio, ok := modelRatioMap.Get(tc.model)
			require.True(t, ok)
			require.Equal(t, tc.input, ratio*2)
			require.Equal(t, 5.0, GetCompletionRatio(tc.model))
			cache, ok := GetCacheRatio(tc.model)
			require.True(t, ok)
			require.InDelta(t, tc.input*0.05, ratio*2*cache, 1e-9)
		})
	}
}
