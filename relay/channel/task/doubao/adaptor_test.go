package doubao

import (
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

func TestEstimatePreAuthorizationUsesNativeVideoOnlyEstimate(t *testing.T) {
	if err := ratio_setting.UpdateVideoTokenPriceByJSONString(`{
		"dreamina-seedance-2-0-260128": {
			"default": {"base": 7.7, "with_video": 4.7},
			"720p": {"base": 7.0, "with_video": 4.7}
		}
	}`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = ratio_setting.UpdateVideoTokenPriceByJSONString("{}")
	})

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("task_request", relaycommon.TaskSubmitReq{
		Metadata: map[string]interface{}{"resolution": "720p"},
	})
	info := &relaycommon.RelayInfo{
		OriginModelName: "dreamina-seedance-2-0-260128",
		PriceData: types.PriceData{
			ModelRatio: 3.85,
			GroupRatioInfo: types.GroupRatioInfo{
				GroupRatio: 1,
			},
		},
	}

	quota, clamp, ok := (&TaskAdaptor{}).EstimatePreAuthorization(c, info)
	if !ok || clamp != nil || quota != 875_000 {
		t.Fatalf("pre-authorization = %d clamp=%v ok=%v; want 875000 nil true", quota, clamp, ok)
	}
	if got := c.GetInt(contextKeyEstimatedTextTokens); got != 0 {
		t.Fatalf("estimated text tokens = %d, want 0", got)
	}
	if got := c.GetInt(contextKeyEstimatedVideoTokens); got != seedanceEstimatedVideoTokens {
		t.Fatalf("estimated video tokens = %d, want %d", got, seedanceEstimatedVideoTokens)
	}
}

func TestEstimatePreAuthorizationSkipsFixedPriceAndNonSeedance(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("task_request", relaycommon.TaskSubmitReq{})
	adaptor := &TaskAdaptor{}

	for name, info := range map[string]*relaycommon.RelayInfo{
		"fixed price": {
			OriginModelName: "dreamina-seedance-2-0-260128",
			PriceData:       types.PriceData{UsePrice: true, ModelRatio: 3.85},
		},
		"non seedance": {
			OriginModelName: "other-video-model",
			PriceData:       types.PriceData{ModelRatio: 3.85},
		},
	} {
		t.Run(name, func(t *testing.T) {
			quota, clamp, ok := adaptor.EstimatePreAuthorization(c, info)
			if ok || quota != 0 || clamp != nil {
				t.Fatalf("pre-authorization = %d clamp=%v ok=%v; want skipped", quota, clamp, ok)
			}
		})
	}
}

func TestParseTaskResultReadsNativeSeedanceUsage(t *testing.T) {
	result, err := (&TaskAdaptor{}).ParseTaskResult([]byte(`{
		"id":"task_upstream",
		"status":"succeeded",
		"resolution":"720p",
		"created_at":100,
		"updated_at":235,
		"content":{"video_url":"https://example.com/video.mp4"},
		"usage":{"completion_tokens":108900,"total_tokens":108900}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "SUCCESS" || result.CompletionTokens != 108_900 || result.TotalTokens != 108_900 {
		t.Fatalf("result status/tokens = %q %d/%d", result.Status, result.CompletionTokens, result.TotalTokens)
	}
	if result.Resolution != "720p" || result.Url != "https://example.com/video.mp4" {
		t.Fatalf("result resolution/url = %q %q", result.Resolution, result.Url)
	}
}
