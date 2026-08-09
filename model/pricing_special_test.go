package model

import (
	"testing"

	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/require"
)

func TestAttachSpecialPricingUsesGPTImageTiers(t *testing.T) {
	saved := ratio_setting.GPTImagePrice2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateGPTImagePriceByJSONString(saved))
	})
	require.NoError(t, ratio_setting.UpdateGPTImagePriceByJSONString(
		`{"gpt-image-2":{"1k":0.04,"2k":0.08,"4k":0.16}}`,
	))

	pricing := Pricing{}
	require.True(t, attachSpecialPricing(&pricing, "gpt-image-2-2026-04-21"))
	require.Equal(t, ratio_setting.GPTImageModelPrice{
		"1k": 0.04,
		"2k": 0.08,
		"4k": 0.16,
	}, pricing.GPTImagePrice)
}

func TestAttachSpecialPricingRequiresCompleteGPTImageTiers(t *testing.T) {
	saved := ratio_setting.GPTImagePrice2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateGPTImagePriceByJSONString(saved))
	})
	require.NoError(t, ratio_setting.UpdateGPTImagePriceByJSONString(`{}`))

	pricing := Pricing{}
	require.False(t, attachSpecialPricing(&pricing, "gpt-image-2"))
	require.Empty(t, pricing.GPTImagePrice)
}

func TestAttachSpecialPricingIncludesVideoTokenMatrix(t *testing.T) {
	saved := ratio_setting.VideoTokenPrice2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateVideoTokenPriceByJSONString(saved))
	})
	require.NoError(t, ratio_setting.UpdateVideoTokenPriceByJSONString(
		`{"dreamina-seedance-2-0-260128":{"720p":{"base":46,"with_video":28}}}`,
	))

	pricing := Pricing{}
	require.False(t, attachSpecialPricing(&pricing, "dreamina-seedance-2-0-260128"))
	require.Equal(t, ratio_setting.VideoTokenModelPrice{
		"720p": {Base: 46, WithVideo: 28},
	}, pricing.VideoTokenPrice)
}
