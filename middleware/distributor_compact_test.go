package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestCompactModelEndpointMismatch(t *testing.T) {
	tests := []struct {
		name      string
		path      string
		modelName string
		want      bool
	}{
		{name: "compact endpoint accepts virtual model", path: "/v1/responses/compact", modelName: "gpt-5.6-luna-openai-compact", want: false},
		{name: "compact endpoint accepts base model", path: "/v1/responses/compact", modelName: "gpt-5.6-luna", want: false},
		{name: "responses rejects virtual model", path: "/v1/responses", modelName: "gpt-5.6-luna-openai-compact", want: true},
		{name: "chat rejects virtual model", path: "/v1/chat/completions", modelName: "gpt-5.6-luna-openai-compact", want: true},
		{name: "messages rejects virtual model", path: "/v1/messages", modelName: "gpt-5.6-luna-openai-compact", want: true},
		{name: "regular model remains valid", path: "/v1/responses", modelName: "gpt-5.6-luna", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, compactModelEndpointMismatch(tt.path, tt.modelName))
		})
	}
}

func TestGetModelRequestAddsCompactVirtualSuffix(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, modelName := range []string{"gpt-5.6-luna", "gpt-5.6-luna-openai-compact"} {
		t.Run(modelName, func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", strings.NewReader(`{"model":"`+modelName+`"}`))
			ctx.Request.Header.Set("Content-Type", "application/json")

			request, shouldSelectChannel, err := getModelRequest(ctx)
			require.NoError(t, err)
			require.True(t, shouldSelectChannel)
			require.Equal(t, "gpt-5.6-luna-openai-compact", request.Model)
		})
	}
}

func TestDistributeRejectsCompactVirtualModelBeforeChannelLookup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	require.NoError(t, i18n.Init())
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5.6-luna-openai-compact"}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	common.SetContextKey(ctx, constant.ContextKeyTokenSpecificChannelId, "not-a-channel-id")

	Distribute()(ctx)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Contains(t, recorder.Body.String(), "compact_model_endpoint_mismatch")
	require.NotContains(t, recorder.Body.String(), "invalid_channel")
}

func TestGetModelRequestVideoFetchSkipsChannelSelection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/videos/task_abc", nil)

	request, shouldSelectChannel, err := getModelRequest(ctx)
	require.NoError(t, err)
	require.NotNil(t, request)
	require.False(t, shouldSelectChannel)
	relayMode, ok := ctx.Get("relay_mode")
	require.True(t, ok)
	require.Equal(t, relayconstant.RelayModeVideoFetchByID, relayMode)
}

func TestDistributeAllowsVideoFetchWithoutChannel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	require.NoError(t, i18n.Init())

	recorder := httptest.NewRecorder()
	r := gin.New()
	r.Use(Distribute())
	r.GET("/v1/videos/:task_id", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/videos/task_abc", nil)
	r.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"ok":true`)
	require.NotContains(t, recorder.Body.String(), "channel is nil")
}
