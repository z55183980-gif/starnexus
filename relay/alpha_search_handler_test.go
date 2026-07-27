package relay

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAlphaSearchHelperRejectsUnsupportedChannelWithoutRetry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/alpha/search", nil)
	common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeOpenAI)

	err := AlphaSearchHelper(c, &relaycommon.RelayInfo{Request: &dto.AlphaSearchRequest{Model: "gpt-5.1"}})

	require.NotNil(t, err)
	assert.Equal(t, http.StatusBadRequest, err.StatusCode)
	assert.True(t, types.IsSkipRetryError(err))
}

func TestAlphaSearchHelperAcceptsSupportedChannelTypes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, channelType := range []int{
		constant.ChannelTypeCodex,
		constant.ChannelTypeSub2API,
		constant.ChannelTypeNewAPI,
		constant.ChannelTypeAdvancedCustom,
	} {
		t.Run(constant.GetChannelTypeName(channelType), func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/alpha/search", nil)
			common.SetContextKey(c, constant.ContextKeyChannelType, channelType)

			err := AlphaSearchHelper(c, &relaycommon.RelayInfo{Request: &dto.AlphaSearchRequest{Model: "gpt-5.6-sol"}})

			require.NotNil(t, err)
			assert.Contains(t, err.Error(), "empty alpha search request body")
		})
	}
}

func TestBuildAlphaSearchRequestBodyPreservesUnknownFields(t *testing.T) {
	raw := []byte(`{
		"id":"req_1",
		"model":"gpt-5.1",
		"commands":{"search_query":[{"q":"weather","recency":1}]},
		"future_field":{"nested":true}
	}`)

	out, err := buildAlphaSearchRequestBody(raw, "gpt-5.1", "gpt-5.1-mapped")
	require.NoError(t, err)

	var body map[string]any
	require.NoError(t, common.Unmarshal(out, &body))
	assert.Equal(t, "gpt-5.1-mapped", body["model"])
	assert.Equal(t, "req_1", body["id"])
	require.Contains(t, body, "commands")
	require.Contains(t, body, "future_field")
}

func TestBuildAlphaSearchRequestBodyNoMappingKeepsRawBytes(t *testing.T) {
	raw := []byte(`{"model":"gpt-5.1","commands":{"search_query":[{"q":"x"}]},"future_field":1}`)
	out, err := buildAlphaSearchRequestBody(raw, "gpt-5.1", "gpt-5.1")
	require.NoError(t, err)
	assert.Equal(t, raw, out)
}
