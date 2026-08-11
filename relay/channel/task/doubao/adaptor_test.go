package doubao

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
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

func TestZQBAPIOpenAIVideoMapsInputReferenceAfterMetadata(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{
		"model":"dreamina-seedance-2-0-260128",
		"prompt":"animate this image",
		"input_reference":{"image_url":"https://example.com/reference.png"},
		"seconds":"5",
		"size":"1280x720",
		"metadata":{"content":[]}
	}`))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })

	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeZQBAPI}}
	adaptor := &TaskAdaptor{}
	adaptor.Init(info)
	if taskErr := adaptor.ValidateRequestAndSetAction(c, info); taskErr != nil {
		t.Fatalf("validation failed: %+v", taskErr)
	}
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := adaptor.convertToZQBAPIOpenAIRequestPayload(&req, "")
	if err != nil {
		t.Fatal(err)
	}
	images, videos := countZQBAPIMedia(payload.Content)
	if images != 1 || videos != 0 {
		t.Fatalf("media counts = images:%d videos:%d", images, videos)
	}
	if payload.Content[0].ImageURL == nil || payload.Content[0].ImageURL.URL != "https://example.com/reference.png" {
		t.Fatalf("reference image missing from payload: %+v", payload.Content)
	}
	if payload.Duration == nil || int(*payload.Duration) != 5 || payload.Resolution != "720p" || payload.Ratio != "16:9" {
		t.Fatalf("duration/size mapping failed: duration=%v resolution=%q ratio=%q", payload.Duration, payload.Resolution, payload.Ratio)
	}
}

func TestZQBAPIOpenAIVideoAcceptsMultipartInputReference(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 300, 300))
	var pngData bytes.Buffer
	if err := png.Encode(&pngData, img); err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("model", "dreamina-seedance-2-0-260128"); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteField("prompt", "animate this image"); err != nil {
		t.Fatal(err)
	}
	part, err := writer.CreateFormFile("input_reference", "reference.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(pngData.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", &body)
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeZQBAPI}}
	adaptor := &TaskAdaptor{}
	adaptor.Init(info)
	if taskErr := adaptor.ValidateRequestAndSetAction(c, info); taskErr != nil {
		t.Fatalf("validation failed: %+v", taskErr)
	}
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Images) != 1 || !strings.HasPrefix(req.Images[0], "data:image/png;base64,") {
		t.Fatalf("multipart reference was not converted to an image data URL: %#v", req.Images)
	}
}

func TestZQBAPIOpenAIVideoRejectsUnsupportedFileID(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{
		"prompt":"animate this image",
		"input_reference":{"file_id":"file_123"}
	}`))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeZQBAPI}}
	adaptor := &TaskAdaptor{}
	adaptor.Init(info)
	if taskErr := adaptor.ValidateRequestAndSetAction(c, info); taskErr == nil {
		t.Fatal("file_id must fail closed for ZQBAPI")
	}
}

func TestZQBAPIOpenAIVideoRemixAddsOriginVideo(t *testing.T) {
	req := relaycommon.TaskSubmitReq{Prompt: "make it brighter"}
	payload, err := (&TaskAdaptor{}).convertToZQBAPIOpenAIRequestPayload(&req, "https://example.com/origin.mp4")
	if err != nil {
		t.Fatal(err)
	}
	images, videos := countZQBAPIMedia(payload.Content)
	if images != 0 || videos != 1 {
		t.Fatalf("media counts = images:%d videos:%d", images, videos)
	}
}

func TestConvertToOpenAIVideoAddsZQBAPIFieldsOnlyForZQBAPI(t *testing.T) {
	data := []byte(`{"status":"running","duration":5,"resolution":"720p","ratio":"9:16"}`)
	zqbapiTask := &model.Task{
		TaskID:    "task_zqbapi",
		Platform:  constant.TaskPlatform("61"),
		Status:    model.TaskStatusInProgress,
		UpdatedAt: 123,
		Data:      data,
		Properties: model.Properties{
			OriginModelName:    "dreamina-seedance-2-0-260128",
			RemixedFromVideoID: "task_origin",
		},
	}
	converted, err := (&TaskAdaptor{}).ConvertToOpenAIVideo(zqbapiTask)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(converted, []byte(`"completed_at"`)) {
		t.Fatalf("in-progress ZQBAPI video must not have completed_at: %s", converted)
	}
	if !bytes.Contains(converted, []byte(`"seconds":"5"`)) || !bytes.Contains(converted, []byte(`"size":"720x1280"`)) || !bytes.Contains(converted, []byte(`"remixed_from_video_id":"task_origin"`)) {
		t.Fatalf("ZQBAPI compatibility fields missing: %s", converted)
	}

	otherTask := *zqbapiTask
	otherTask.Platform = constant.TaskPlatform("15")
	converted, err = (&TaskAdaptor{}).ConvertToOpenAIVideo(&otherTask)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(converted, []byte(`"completed_at":123`)) {
		t.Fatalf("non-ZQBAPI converter behavior changed: %s", converted)
	}
	if bytes.Contains(converted, []byte(`"seconds"`)) || bytes.Contains(converted, []byte(`"size"`)) || bytes.Contains(converted, []byte(`"remixed_from_video_id"`)) {
		t.Fatalf("ZQBAPI fields leaked into another channel: %s", converted)
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
