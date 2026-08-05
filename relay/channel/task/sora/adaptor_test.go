package sora

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

func TestParseTaskResultIncludesDurationAndTimestamps(t *testing.T) {
	adaptor := &TaskAdaptor{}
	result, err := adaptor.ParseTaskResult([]byte(`{"id":"upstream-task","status":"completed","progress":100,"seconds":"5","created_at":1700000000000,"completed_at":1700000123000}`))
	if err != nil {
		t.Fatalf("ParseTaskResult() error = %v", err)
	}
	if result.DurationSeconds != 5 {
		t.Fatalf("DurationSeconds = %d, want 5", result.DurationSeconds)
	}
	if result.CreatedAt != 1_700_000_000 || result.CompletedAt != 1_700_000_123 {
		t.Fatalf("timestamps = %d/%d, want normalized Unix seconds", result.CreatedAt, result.CompletedAt)
	}
}

func TestParseTaskResultIncludesUpstreamResolutionAndVideoTokens(t *testing.T) {
	adaptor := &TaskAdaptor{}
	result, err := adaptor.ParseTaskResult([]byte(`{"id":"upstream-task","status":"completed","metadata":{"resolution":"720p"},"usage":{"text_tokens":0,"video_tokens":87300}}`))
	if err != nil {
		t.Fatalf("ParseTaskResult() error = %v", err)
	}
	if result.Resolution != "720p" {
		t.Fatalf("Resolution = %q, want 720p", result.Resolution)
	}
	if result.CompletionTokens != 87_300 || result.TotalTokens != 87_300 {
		t.Fatalf("tokens = %d/%d, want 87300/87300", result.CompletionTokens, result.TotalTokens)
	}
}

func TestAdjustBillingOnCompleteReconcilesDuration(t *testing.T) {
	task := &model.Task{
		Quota: 1_487_500,
		PrivateData: model.TaskPrivateData{
			BillingContext: &model.TaskBillingContext{
				OtherRatios: map[string]float64{"seconds": 4, "size": 1},
			},
		},
	}
	adaptor := &TaskAdaptor{}

	actualQuota := adaptor.AdjustBillingOnComplete(task, &relaycommon.TaskInfo{DurationSeconds: 5})
	if actualQuota != 1_859_375 {
		t.Fatalf("actual quota = %d, want 1859375", actualQuota)
	}
	if got := task.PrivateData.BillingContext.OtherRatios["seconds"]; got != 5 {
		t.Fatalf("seconds ratio = %v, want 5", got)
	}
}

func TestAdjustBillingOnCompleteKeepsMatchingDuration(t *testing.T) {
	task := &model.Task{
		Quota: 1_487_500,
		PrivateData: model.TaskPrivateData{
			BillingContext: &model.TaskBillingContext{
				OtherRatios: map[string]float64{"seconds": 4},
			},
		},
	}
	adaptor := &TaskAdaptor{}

	if actualQuota := adaptor.AdjustBillingOnComplete(task, &relaycommon.TaskInfo{DurationSeconds: 4}); actualQuota != 0 {
		t.Fatalf("actual quota = %d, want no adjustment", actualQuota)
	}
}

func TestEstimatePreAuthorizationUsesLocalSeedanceTokenEstimate(t *testing.T) {
	if err := ratio_setting.UpdateVideoTokenPriceByJSONString(`{
		"dreamina-seedance-2-0-mini-hc": {
			"default": {"base": 3.5, "with_video": 0.21},
			"720p": {"base": 3.5, "with_video": 2.1}
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
		OriginModelName: "dreamina-seedance-2-0-mini-hc",
		PriceData: types.PriceData{
			ModelRatio: 1.75,
			GroupRatioInfo: types.GroupRatioInfo{
				GroupRatio: 0.85,
			},
		},
	}
	adaptor := &TaskAdaptor{}
	quota, clamp, ok := adaptor.EstimatePreAuthorization(c, info)
	if !ok || clamp != nil || quota != 372_618 {
		t.Fatalf("pre-authorization = %d clamp=%v ok=%v; want 372618 nil true", quota, clamp, ok)
	}
}
