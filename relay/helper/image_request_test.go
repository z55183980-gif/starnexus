package helper

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGetAndValidOpenAIImageRequestStreamAndN(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("POST", "/v1/images/generations", strings.NewReader(`{"model":"gpt-image-1","prompt":"draw","stream":true,"n":2}`))
	c.Request.Header.Set("Content-Type", "application/json")

	got, err := GetAndValidOpenAIImageRequest(c, relayconstant.RelayModeImagesGenerations)
	require.NoError(t, err)
	require.NotNil(t, got.Stream)
	require.True(t, *got.Stream)
	require.EqualValues(t, 2, *got.N)
	require.True(t, got.IsStream(c))
}

func TestGetAndValidOpenAIImageRequestRejectsTooManyImages(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("POST", "/v1/images/generations", strings.NewReader(`{"model":"gpt-image-1","prompt":"draw","n":129}`))
	c.Request.Header.Set("Content-Type", "application/json")

	_, err := GetAndValidOpenAIImageRequest(c, relayconstant.RelayModeImagesGenerations)
	require.Error(t, err)
	require.Contains(t, err.Error(), "between 1 and 128")
}

func TestImageRequestTokenMetaResolvesGPTImagePriceTier(t *testing.T) {
	tests := map[string]string{
		"":          "2k",
		"auto":      "2k",
		"1024x1024": "1k",
		"1024x1536": "2k",
		"2048x2048": "2k",
		"3840x2160": "4k",
	}
	for size, want := range tests {
		t.Run(size, func(t *testing.T) {
			request := dto.ImageRequest{Model: "gpt-image-2", Size: size}
			require.Equal(t, want, request.GetTokenCountMeta().ImageSizeTier)
		})
	}
}
