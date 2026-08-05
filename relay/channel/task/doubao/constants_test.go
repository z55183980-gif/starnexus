package doubao

import (
	"math"
	"testing"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

func almostEqual(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}

func TestGetVideoInputRatioDreaminaAliases(t *testing.T) {
	proBase, ok := GetVideoInputRatio("dreamina-seedance-2-0-260128", "720p", false)
	if !ok || !almostEqual(proBase, 1.0) {
		t.Fatalf("pro base ratio = %v, ok=%v; want 1.0, true", proBase, ok)
	}

	proWithVideo, ok := GetVideoInputRatio("dreamina-seedance-2-0-ep", "", true)
	if !ok || !almostEqual(proWithVideo, 28.0/46.0) {
		t.Fatalf("pro+video = %v, ok=%v; want %v", proWithVideo, ok, 28.0/46.0)
	}

	pro1080, ok := GetVideoInputRatio("dreamina-seedance-2-0-hc", "1080p", false)
	if !ok || !almostEqual(pro1080, 51.0/46.0) {
		t.Fatalf("pro 1080p = %v, ok=%v; want %v", pro1080, ok, 51.0/46.0)
	}

	pro4kVideo, ok := GetVideoInputRatio("dreamina-seedance-2-0-260128", "2K", true)
	if !ok || !almostEqual(pro4kVideo, 16.0/46.0) {
		t.Fatalf("pro 2K+video = %v, ok=%v; want %v", pro4kVideo, ok, 16.0/46.0)
	}

	fastWithVideo, ok := GetVideoInputRatio("dreamina-seedance-2-0-fast-hc", "720p", true)
	if !ok || !almostEqual(fastWithVideo, 22.0/37.0) {
		t.Fatalf("fast+video = %v, ok=%v; want %v", fastWithVideo, ok, 22.0/37.0)
	}

	miniWithVideo, ok := GetVideoInputRatio("dreamina-seedance-2-0-mini-hc", "", true)
	if !ok || !almostEqual(miniWithVideo, 22.0/37.0) {
		t.Fatalf("mini+video = %v, ok=%v; want %v", miniWithVideo, ok, 22.0/37.0)
	}

	// Fast has no 1080p row → fall back to base (ratio 1.0).
	fast1080, ok := GetVideoInputRatio("dreamina-seedance-2-0-fast-hc", "1080p", false)
	if !ok || !almostEqual(fast1080, 1.0) {
		t.Fatalf("fast 1080p fallback = %v, ok=%v; want 1.0", fast1080, ok)
	}

	if _, ok := GetVideoInputRatio("unknown-model", "720p", false); ok {
		t.Fatal("unknown model should miss price table")
	}
}

func TestGetVideoInputRatioConfigMissFallsBackToHardcoded(t *testing.T) {
	raw := `{
		"dreamina-seedance-2-0-260128": {
			"4k": {"base": 26, "with_video": 16}
		}
	}`
	if err := ratio_setting.UpdateVideoTokenPriceByJSONString(raw); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = ratio_setting.UpdateVideoTokenPriceByJSONString("{}")
	})

	r, ok := GetVideoInputRatio("dreamina-seedance-2-0-260128", "1080p", false)
	if !ok || !almostEqual(r, 51.0/46.0) {
		t.Fatalf("incomplete config must use hardcoded 1080p; got %v ok=%v", r, ok)
	}
}

func TestHasVideoInMetadataRequiresNonEmptyURL(t *testing.T) {
	if hasVideoInMetadata(map[string]interface{}{
		"content": []interface{}{
			map[string]interface{}{"type": "video_url"},
		},
	}) {
		t.Fatal("empty video_url must not count")
	}
	if !hasVideoInMetadata(map[string]interface{}{
		"content": []interface{}{
			map[string]interface{}{
				"type": "video_url",
				"video_url": map[string]interface{}{
					"url": "https://example.com/a.mp4",
				},
			},
		},
	}) {
		t.Fatal("non-empty nested video_url must count")
	}
}

func TestAdjustBillingOnCompleteUpdatesVideoInputRatio(t *testing.T) {
	hasVideo := false
	task := &model.Task{
		PrivateData: model.TaskPrivateData{
			BillingContext: &model.TaskBillingContext{
				OriginModelName: "dreamina-seedance-2-0-260128",
				VideoHasInput:   &hasVideo,
				OtherRatios:     map[string]float64{"video_input": 1.1},
			},
		},
	}
	adaptor := &TaskAdaptor{}
	quota := adaptor.AdjustBillingOnComplete(task, &relaycommon.TaskInfo{
		Resolution:  "1080p",
		TotalTokens: 1000,
	})
	if quota != 0 {
		t.Fatalf("must return 0 to keep shared token settle path, got %d", quota)
	}
	got := task.PrivateData.BillingContext.OtherRatios["video_input"]
	want := 51.0 / 46.0
	if !almostEqual(got, want) {
		t.Fatalf("video_input=%v want %v", got, want)
	}
}
