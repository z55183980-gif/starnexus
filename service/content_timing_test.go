package service

import (
	"net/http/httptest"
	"testing"
	"time"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestContentTimingDisplayDoesNotFallBackToHeartbeat(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
	start := time.Now().Add(-time.Minute)
	info := &relaycommon.RelayInfo{StartTime: start, FirstResponseTime: start.Add(30 * time.Second), ChannelMeta: &relaycommon.ChannelMeta{}, ContentTimingTracked: true}
	other := GenerateTextOtherInfo(c, info, 1, 1, 1, 2259, .05, -1, -1)
	require.Nil(t, other["frt"])
	require.Equal(t, "content_unobserved", other["frt_source"])
	first, generation := int64(45000), int64(20000)
	info.FirstContentMs, info.GenerationFirstTokenMs = &first, &generation
	other = GenerateTextOtherInfo(c, info, 1, 1, 1, 2259, .05, -1, -1)
	require.Equal(t, float64(45000), other["frt"])
	require.Equal(t, "downstream_content", other["frt_source"])
	require.Equal(t, generation, other["generation_first_token_ms"])
	require.Equal(t, 2259, other["cache_tokens"])
	require.Equal(t, start.Add(30*time.Second), info.FirstResponseTime)
}
