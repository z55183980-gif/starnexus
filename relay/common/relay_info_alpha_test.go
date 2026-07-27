package common

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/dto"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGenRelayInfoAlphaSearchIncludesGuaranteedWebSearchCall(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/alpha/search", nil)

	info := GenRelayInfoAlphaSearch(c, &dto.AlphaSearchRequest{Model: "gpt-5.1"})

	tool := info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview]
	require.NotNil(t, tool)
	require.Equal(t, 1, tool.CallCount)
}
