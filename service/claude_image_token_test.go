package service

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestClaudeNestedImageEstimateMatchesTopLevel(t *testing.T) {
	previous := constant.CountToken
	constant.CountToken = true
	t.Cleanup(func() { constant.CountToken = previous })
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(ctx, constant.ContextKeyOriginalModel, "claude-opus-5-5")
	info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatClaude}
	text := "read result"
	image := dto.ClaudeMediaMessage{Type: "image", Source: &dto.ClaudeMessageSource{Type: "base64", MediaType: "image/png", Data: strings.Repeat("Ab12+/Z7", 131072)}}
	blocks := []dto.ClaudeMediaMessage{{Type: "text", Text: &text}, image}
	top := dto.ClaudeRequest{Messages: []dto.ClaudeMessage{{Role: "user", Content: blocks}}}
	nested := dto.ClaudeRequest{Messages: []dto.ClaudeMessage{{Role: "user", Content: []dto.ClaudeMediaMessage{{Type: "tool_result", Content: blocks}}}}}
	topTokens, err := EstimateRequestToken(ctx, top.GetTokenCountMeta(), info)
	require.NoError(t, err)
	nestedTokens, err := EstimateRequestToken(ctx, nested.GetTokenCountMeta(), info)
	require.NoError(t, err)
	require.Equal(t, topTokens, nestedTokens)
	require.GreaterOrEqual(t, nestedTokens, 520)
	require.Less(t, nestedTokens, 550)
}
