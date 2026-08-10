package doubao

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
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

func TestParseTaskResultAcceptsNumericErrorCode(t *testing.T) {
	result, err := (&TaskAdaptor{}).ParseTaskResult([]byte(`{
		"error":{"code":0,"message":"The request failed because the input image 'content[1]' may contain real person."}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "FAILURE" || result.Progress != "100%" {
		t.Fatalf("status/progress = %q/%q", result.Status, result.Progress)
	}
	if !strings.Contains(result.Reason, "may contain real person") {
		t.Fatalf("reason = %q", result.Reason)
	}
}

func TestDoubaoVideoBuildRequestDoesNotRunZQBAPIFaceFlow(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("task_request", relaycommon.TaskSubmitReq{
		Model:  "dreamina-seedance-2-0-260128",
		Prompt: "test",
		Images: []string{"https://private.invalid/portrait.jpg"},
	})
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeDoubaoVideo},
	}
	adaptor := &TaskAdaptor{}
	adaptor.Init(info)
	body, err := adaptor.BuildRequestBody(c, info)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte("https://private.invalid/portrait.jpg")) {
		t.Fatalf("ordinary DoubaoVideo image URL was changed: %s", data)
	}
}

func TestZQBAPIFaceDetectorIgnoresBlankImage(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 128, 128))
	for y := 0; y < 128; y++ {
		for x := 0; x < 128; x++ {
			img.Set(x, y, color.White)
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatal(err)
	}
	dataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(encoded.Bytes())
	inspection, err := inspectZQBAPIImage(nil, dataURL)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.HasFace {
		t.Fatal("blank image must not be classified as a face")
	}
}

func TestZQBAPIFaceDetectorConfiguredPortrait(t *testing.T) {
	path := strings.TrimSpace(os.Getenv("ZQBAPI_FACE_TEST_IMAGE"))
	if path == "" {
		t.Skip("ZQBAPI_FACE_TEST_IMAGE is not set")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := inspectZQBAPIImage(nil, "data:image/jpeg;base64,"+base64.StdEncoding.EncodeToString(data))
	if err != nil {
		t.Fatal(err)
	}
	if !inspection.HasFace {
		t.Fatal("configured portrait must be classified as containing a face")
	}
}
