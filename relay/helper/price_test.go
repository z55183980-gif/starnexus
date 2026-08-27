package helper

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestModelPriceHelperTieredUsesPreloadedRequestInput(t *testing.T) {
	gin.SetMode(gin.TestMode)

	saved := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(key, value string) error {
		saved[key] = value
		return nil
	}))
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.LoadFromDB(saved))
	})

	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"billing_setting.billing_mode":             `{"tiered-test-model":"tiered_expr"}`,
		"billing_setting.billing_expr":             `{"tiered-test-model":"param(\"stream\") == true ? tier(\"stream\", p * 3) : tier(\"base\", p * 2)"}`,
		"billing_setting.cache_billing_offset_bps": `{"tiered-test-model":300}`,
	}))

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest(http.MethodPost, "/api/channel/test/1", nil)
	req.Body = nil
	req.ContentLength = 0
	req.Header.Set("Content-Type", "application/json")
	ctx.Request = req
	ctx.Set("group", "default")

	info := &relaycommon.RelayInfo{
		OriginModelName: "tiered-test-model",
		UserGroup:       "default",
		UsingGroup:      "default",
		RequestHeaders:  map[string]string{"Content-Type": "application/json"},
		BillingRequestInput: &billingexpr.RequestInput{
			Headers: map[string]string{"Content-Type": "application/json"},
			Body:    []byte(`{"stream":true}`),
		},
	}

	priceData, err := ModelPriceHelper(ctx, info, 1000, &types.TokenCountMeta{})
	require.NoError(t, err)
	require.Equal(t, 1500, priceData.QuotaToPreConsume)
	require.NotNil(t, info.TieredBillingSnapshot)
	require.Equal(t, "stream", info.TieredBillingSnapshot.EstimatedTier)
	require.Equal(t, billing_setting.BillingModeTieredExpr, info.TieredBillingSnapshot.BillingMode)
	require.Equal(t, common.QuotaPerUnit, info.TieredBillingSnapshot.QuotaPerUnit)
	require.Equal(t, 300, info.TieredBillingSnapshot.CacheBillingOffsetBps)
}

func TestModelPriceHelperRequestBillingRatiosOnlyApplyToFixedPrice(t *testing.T) {
	gin.SetMode(gin.TestMode)
	savedModelPrices := ratio_setting.ModelPrice2JSONString()
	savedModelRatios := ratio_setting.ModelRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(savedModelPrices))
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(savedModelRatios))
	})

	modelPrices, err := common.Marshal(map[string]float64{
		"fixed-image-price": 0.04,
	})
	require.NoError(t, err)
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(string(modelPrices)))
	modelRatios, err := common.Marshal(map[string]float64{"ratio-image-price": 15})
	require.NoError(t, err)
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(string(modelRatios)))

	tests := []struct {
		name           string
		model          string
		wantQuota      int
		wantUsePrice   bool
		wantImageCount bool
	}{
		{
			name:           "fixed price applies image count",
			model:          "fixed-image-price",
			wantQuota:      180000,
			wantUsePrice:   true,
			wantImageCount: true,
		},
		{
			name:         "ratio price ignores request billing ratios",
			model:        "ratio-image-price",
			wantQuota:    15000,
			wantUsePrice: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Set("group", "default")
			info := &relaycommon.RelayInfo{
				OriginModelName: tt.model,
				UserGroup:       "default",
				UsingGroup:      "default",
			}
			meta := &types.TokenCountMeta{
				ImagePriceRatio: 3,
				BillingRatios:   map[string]float64{"n": 3},
			}

			priceData, err := ModelPriceHelper(ctx, info, 1000, meta)

			require.NoError(t, err)
			require.Equal(t, tt.wantQuota, priceData.QuotaToPreConsume)
			require.Equal(t, tt.wantUsePrice, priceData.UsePrice)
			require.Equal(t, tt.wantImageCount, priceData.HasOtherRatio("n"))
			require.Equal(t, priceData.OtherRatios, info.PriceData.OtherRatios)
		})
	}
}

func TestModelPriceHelperGPTImageUsesConfiguredResolutionTier(t *testing.T) {
	gin.SetMode(gin.TestMode)
	saved := ratio_setting.GPTImagePrice2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateGPTImagePriceByJSONString(saved))
	})
	require.NoError(t, ratio_setting.UpdateGPTImagePriceByJSONString(
		`{"gpt-image-2":{"1k":0.04,"2k":0.08,"4k":0.16}}`,
	))

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("group", "default")
	info := &relaycommon.RelayInfo{
		OriginModelName: "gpt-image-2",
		UserGroup:       "default",
		UsingGroup:      "default",
	}
	priceData, err := ModelPriceHelper(ctx, info, 100, &types.TokenCountMeta{
		ImageSizeTier: "2k",
		BillingRatios: map[string]float64{"n": 2},
	})

	require.NoError(t, err)
	require.True(t, priceData.UsePrice)
	require.Equal(t, 0.08, priceData.ModelPrice)
	require.Equal(t, 80000, priceData.QuotaToPreConsume)
	require.Equal(t, 2.0, priceData.OtherRatios["n"])
}

func TestModelPriceHelperGPTImageRejectsUnconfiguredModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	saved := ratio_setting.GPTImagePrice2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateGPTImagePriceByJSONString(saved))
	})
	require.NoError(t, ratio_setting.UpdateGPTImagePriceByJSONString(
		`{"gpt-image-1":{"1k":0.04,"2k":0.08,"4k":0.16}}`,
	))

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("group", "default")
	_, err := ModelPriceHelper(ctx, &relaycommon.RelayInfo{
		OriginModelName: "gpt-image-2",
		UserGroup:       "default",
		UsingGroup:      "default",
	}, 100, &types.TokenCountMeta{ImageSizeTier: "4k"})

	require.Error(t, err)
	require.Contains(t, err.Error(), "4k")
}
