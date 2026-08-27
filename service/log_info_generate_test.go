package service

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestInjectTieredBillingInfoKeepsCacheAdjustmentUnderAdminInfo(t *testing.T) {
	other := map[string]interface{}{
		"cache_tokens": 9391,
	}
	info := &relaycommon.RelayInfo{
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{ExprString: `tier("base", p + cr * 0.1)`},
	}
	result := &billingexpr.TieredResult{
		MatchedTier: "base",
		CacheBilling: &billingexpr.CacheBillingAdjustment{
			OffsetBps:              300,
			TotalInputTokens:       10000,
			RawCacheReadTokens:     9391,
			BillingCacheReadTokens: 9091,
			ReclassifiedTokens:     300,
		},
	}

	InjectTieredBillingInfo(other, info, result)

	require.Equal(t, 9391, other["cache_tokens"])
	require.NotContains(t, other, "cache_billing")
	adminInfo := other["admin_info"].(map[string]interface{})
	require.Equal(t, map[string]interface{}{
		"offset_bps":                300,
		"total_input_tokens":        int64(10000),
		"raw_cache_read_tokens":     int64(9391),
		"billing_cache_read_tokens": int64(9091),
		"reclassified_tokens":       int64(300),
	}, adminInfo["cache_billing"])
}

func TestGenerateTextOtherInfoIncludesStreamTransport(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	ctx.Set("use_channel", []string{"44"})
	SetStreamTransportInfo(ctx, "websocket", "websocket", "responses_websocket_v2")

	started := time.Now().Add(-time.Second)
	status := relaycommon.NewStreamStatus()
	status.SetEndReason(relaycommon.StreamEndReasonDone, nil)
	info := &relaycommon.RelayInfo{
		StartTime:         started,
		FirstResponseTime: started.Add(500 * time.Millisecond),
		IsStream:          true,
		StreamStatus:      status,
		ChannelMeta:       &relaycommon.ChannelMeta{},
	}

	other := GenerateTextOtherInfo(ctx, info, 1, 1, 1, 0, 0, -1, -1)
	require.Equal(t, []string{"44"}, other["admin_info"].(map[string]interface{})["use_channel"])
	require.Equal(t, map[string]string{
		"downstream": "websocket",
		"upstream":   "websocket",
		"mode":       "responses_websocket_v2",
	}, other["stream_transport"])
	require.Equal(t, map[string]interface{}{
		"status":     "ok",
		"end_reason": "done",
	}, other["stream_status"])
}

func TestGenerateTextOtherInfoIncludesCodexStructuredOutputCompatibility(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	ctx.Set("codex_structured_output_compat", map[string]interface{}{
		"required_fields_added":       2,
		"additional_properties_added": 1,
	})

	started := time.Now().Add(-time.Second)
	info := &relaycommon.RelayInfo{
		StartTime:         started,
		FirstResponseTime: started.Add(500 * time.Millisecond),
		ChannelMeta:       &relaycommon.ChannelMeta{},
	}

	other := GenerateTextOtherInfo(ctx, info, 1, 1, 1, 0, 0, -1, -1)
	adminInfo := other["admin_info"].(map[string]interface{})
	require.Equal(t, map[string]interface{}{
		"required_fields_added":       2,
		"additional_properties_added": 1,
	}, adminInfo["codex_structured_output_compat"])
}

func TestGenerateTextOtherInfoIncludesCodexInputRepair(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	ctx.Set("codex_input_repair_admin_info", map[string]interface{}{
		"invalid_message_ids_removed": 1,
		"first_removed_index":         178,
		"upstream_validation_retry":   true,
	})

	started := time.Now().Add(-time.Second)
	info := &relaycommon.RelayInfo{
		StartTime:         started,
		FirstResponseTime: started.Add(500 * time.Millisecond),
		ChannelMeta:       &relaycommon.ChannelMeta{},
	}

	other := GenerateTextOtherInfo(ctx, info, 1, 1, 1, 0, 0, -1, -1)
	adminInfo := other["admin_info"].(map[string]interface{})
	require.Equal(t, map[string]interface{}{
		"invalid_message_ids_removed": 1,
		"first_removed_index":         178,
		"upstream_validation_retry":   true,
	}, adminInfo["codex_input_repair"])
}
