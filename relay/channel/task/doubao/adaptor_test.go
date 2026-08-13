package doubao

import (
	"bytes"
	"context"
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

func TestDoubaoVideo2BuildRequestMapsEPAndForwardsSupportedFields(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("task_request", relaycommon.TaskSubmitReq{
		Model:  "doubao-seedance-2-0-260128",
		Prompt: "a calm lake at sunrise",
		Metadata: map[string]interface{}{
			"resolution":        "720p",
			"ratio":             "16:9",
			"duration":          5,
			"generate_audio":    false,
			"watermark":         false,
			"safety_identifier": "user-57",
			"bitrate_mode":      "standard",
			"output_format":     "mp4",
			"moderation_options": map[string]interface{}{
				"ips": []interface{}{"kz-ip-1"},
			},
		},
	})
	info := &relaycommon.RelayInfo{
		OriginModelName: "doubao-seedance-2-0-260128",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeDoubaoVideo2,
			UpstreamModelName: "kzep_pro",
			IsModelMapped:     true,
		},
	}
	adaptor := &TaskAdaptor{}
	adaptor.Init(info)
	if taskErr := adaptor.validateDoubaoVideo2Request(c, info); taskErr != nil {
		t.Fatalf("validation failed: %+v", taskErr)
	}
	body, err := adaptor.BuildRequestBody(c, info)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(body)
	if err != nil {
		t.Fatal(err)
	}
	var payload requestPayload
	if err := common.Unmarshal(data, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Model != "kzep_pro" {
		t.Fatalf("mapped model = %q", payload.Model)
	}
	if payload.GenerateAudio == nil || bool(*payload.GenerateAudio) || payload.Watermark == nil || bool(*payload.Watermark) {
		t.Fatalf("explicit false values were not preserved: generate_audio=%v watermark=%v", payload.GenerateAudio, payload.Watermark)
	}
	if payload.SafetyIdentifier == nil || *payload.SafetyIdentifier != "user-57" || payload.BitrateMode == nil || *payload.BitrateMode != "standard" || payload.OutputFormat == nil || *payload.OutputFormat != "mp4" {
		t.Fatalf("supported fields missing from payload: %+v", payload)
	}
	if payload.ModerationOptions == nil || len(payload.ModerationOptions.IPs) != 1 || payload.ModerationOptions.IPs[0] != "kz-ip-1" {
		t.Fatalf("moderation options missing from payload: %+v", payload.ModerationOptions)
	}
}

func TestDoubaoVideo2AcceptsInputReferenceCompatibilityForms(t *testing.T) {
	tests := []struct {
		name      string
		reference string
	}{
		{name: "string", reference: `"https://example.com/reference.png"`},
		{name: "compatibility object", reference: `{"image_url":"https://example.com/reference.png"}`},
		{name: "provider object", reference: `{"image_url":{"url":"https://example.com/reference.png"}}`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{
				"model":"doubao-seedance-2-0-260128",
				"prompt":"animate this image",
				"input_reference":`+test.reference+`,
				"seconds":"8",
				"size":"1280x720"
			}`))
			c.Request.Header.Set("Content-Type", "application/json")
			t.Cleanup(func() { common.CleanupBodyStorage(c) })

			info := &relaycommon.RelayInfo{
				OriginModelName: "doubao-seedance-2-0-260128",
				ChannelMeta: &relaycommon.ChannelMeta{
					ChannelType: constant.ChannelTypeDoubaoVideo2,
				},
			}
			adaptor := &TaskAdaptor{}
			adaptor.Init(info)
			if taskErr := adaptor.ValidateRequestAndSetAction(c, info); taskErr != nil {
				t.Fatalf("validation failed: %+v", taskErr)
			}
			body, err := adaptor.BuildRequestBody(c, info)
			if err != nil {
				t.Fatal(err)
			}
			data, err := io.ReadAll(body)
			if err != nil {
				t.Fatal(err)
			}
			var payload requestPayload
			if err := common.Unmarshal(data, &payload); err != nil {
				t.Fatal(err)
			}
			if len(payload.Content) != 2 {
				t.Fatalf("content count = %d, want 2: %s", len(payload.Content), data)
			}
			image := payload.Content[0]
			if image.Type != "image_url" || image.ImageURL == nil || image.ImageURL.URL != "https://example.com/reference.png" || image.Role != "first_frame" {
				t.Fatalf("reference was not normalized: %+v", image)
			}
			if payload.Content[1].Type != "text" || payload.Content[1].Text != "animate this image" {
				t.Fatalf("prompt content = %+v", payload.Content[1])
			}
			if payload.Duration == nil || int(*payload.Duration) != 8 || payload.Resolution == nil || *payload.Resolution != "720p" || payload.Ratio == nil || *payload.Ratio != "16:9" {
				t.Fatalf("duration/size = %v/%v/%v", payload.Duration, payload.Resolution, payload.Ratio)
			}
		})
	}
}

func TestDoubaoVideo2AcceptsProviderContentAndPreservesFalse(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{
		"model":"doubao-seedance-2-0-260128",
		"content":[
			{"type":"image_url","image_url":{"url":"https://example.com/reference.png"},"role":"first_frame"},
			{"type":"text","text":"move naturally"}
		],
		"duration":8,
		"resolution":"720p",
		"ratio":"adaptive",
		"generate_audio":false,
		"watermark":false
	}`))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })

	info := &relaycommon.RelayInfo{
		OriginModelName: "doubao-seedance-2-0-260128",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeDoubaoVideo2,
		},
	}
	adaptor := &TaskAdaptor{}
	adaptor.Init(info)
	if taskErr := adaptor.ValidateRequestAndSetAction(c, info); taskErr != nil {
		t.Fatalf("validation failed: %+v", taskErr)
	}
	body, err := adaptor.BuildRequestBody(c, info)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(body)
	if err != nil {
		t.Fatal(err)
	}
	var payload requestPayload
	if err := common.Unmarshal(data, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Content) != 2 || payload.Content[0].Role != "first_frame" || payload.Content[1].Text != "move naturally" {
		t.Fatalf("provider content was not preserved: %+v", payload.Content)
	}
	if payload.Duration == nil || int(*payload.Duration) != 8 || payload.Resolution == nil || *payload.Resolution != "720p" || payload.Ratio == nil || *payload.Ratio != "adaptive" {
		t.Fatalf("native options = duration:%v resolution:%v ratio:%v", payload.Duration, payload.Resolution, payload.Ratio)
	}
	if payload.GenerateAudio == nil || bool(*payload.GenerateAudio) || payload.Watermark == nil || bool(*payload.Watermark) {
		t.Fatalf("explicit false values were not preserved: generate_audio=%v watermark=%v", payload.GenerateAudio, payload.Watermark)
	}
}

func TestDoubaoVideo2MultipartInputReferenceIsForwardedDirectly(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	var pngData bytes.Buffer
	if err := png.Encode(&pngData, img); err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for key, value := range map[string]string{
		"model":   "doubao-seedance-2-0-260128",
		"prompt":  "animate directly",
		"seconds": "8",
		"size":    "1280x720",
	} {
		if err := writer.WriteField(key, value); err != nil {
			t.Fatal(err)
		}
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
	info := &relaycommon.RelayInfo{
		OriginModelName: "doubao-seedance-2-0-260128",
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeDoubaoVideo2},
	}
	adaptor := &TaskAdaptor{}
	adaptor.Init(info)
	if taskErr := adaptor.ValidateRequestAndSetAction(c, info); taskErr != nil {
		t.Fatalf("validation failed: %+v", taskErr)
	}
	requestBody, err := adaptor.BuildRequestBody(c, info)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(requestBody)
	if err != nil {
		t.Fatal(err)
	}
	var payload requestPayload
	if err := common.Unmarshal(data, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Content) != 2 || payload.Content[0].ImageURL == nil || !strings.HasPrefix(payload.Content[0].ImageURL.URL, "data:image/png;base64,") {
		t.Fatalf("multipart image was not directly encoded into upstream content: %s", data)
	}
	if payload.Content[0].Role != "first_frame" {
		t.Fatalf("multipart image role = %q", payload.Content[0].Role)
	}
}

func TestDoubaoVideo2TopLevelVideoContentParticipatesInBilling(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{
		"model":"doubao-seedance-2-0-260128",
		"prompt":"use the reference video",
		"content":[{"type":"video_url","video_url":{"url":"https://example.com/reference.mp4"},"role":"reference_video"}]
	}`))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	info := &relaycommon.RelayInfo{
		OriginModelName: "doubao-seedance-2-0-260128",
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeDoubaoVideo2},
	}
	adaptor := &TaskAdaptor{}
	adaptor.Init(info)
	if taskErr := adaptor.ValidateRequestAndSetAction(c, info); taskErr != nil {
		t.Fatalf("validation failed: %+v", taskErr)
	}
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		t.Fatal(err)
	}
	if !hasVideoInMetadata(req.Metadata) {
		t.Fatalf("normalized video content was not visible to billing: %#v", req.Metadata["content"])
	}
}

func TestDoubaoVideo2RejectsInvalidFrameRoleCombinations(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{
			name:    "last frame without first frame",
			content: `[{"type":"image_url","image_url":{"url":"https://example.com/last.png"},"role":"last_frame"}]`,
		},
		{
			name:    "duplicate first frame",
			content: `[{"type":"image_url","image_url":{"url":"https://example.com/a.png"},"role":"first_frame"},{"type":"image_url","image_url":{"url":"https://example.com/b.png"},"role":"first_frame"}]`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{"model":"doubao-seedance-2-0-260128","prompt":"animate","content":`+test.content+`}`))
			c.Request.Header.Set("Content-Type", "application/json")
			t.Cleanup(func() { common.CleanupBodyStorage(c) })
			info := &relaycommon.RelayInfo{
				OriginModelName: "doubao-seedance-2-0-260128",
				ChannelMeta:     &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeDoubaoVideo2},
			}
			adaptor := &TaskAdaptor{}
			adaptor.Init(info)
			taskErr := adaptor.ValidateRequestAndSetAction(c, info)
			if taskErr == nil || taskErr.Code != "invalid_reference_role" {
				t.Fatalf("validation error = %+v", taskErr)
			}
		})
	}
}

func TestDoubaoVideo2RejectsContentAndInputReferenceTogether(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{
		"model":"doubao-seedance-2-0-260128",
		"prompt":"animate",
		"content":[{"type":"image_url","image_url":{"url":"https://example.com/content.png"}}],
		"input_reference":{"image_url":"https://example.com/reference.png"}
	}`))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })

	info := &relaycommon.RelayInfo{
		OriginModelName: "doubao-seedance-2-0-260128",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeDoubaoVideo2,
		},
	}
	adaptor := &TaskAdaptor{}
	adaptor.Init(info)
	taskErr := adaptor.ValidateRequestAndSetAction(c, info)
	if taskErr == nil || taskErr.Code != "invalid_request" || !strings.Contains(taskErr.Message, "content and input_reference") {
		t.Fatalf("validation error = %+v", taskErr)
	}
}

func TestDoubaoVideo2ValidatesOfficialModelBeforeEPMapping(t *testing.T) {
	tests := []struct {
		name     string
		model    string
		metadata map[string]interface{}
		code     string
	}{
		{
			name:  "seedance 2.5 rejects 1080p",
			model: "doubao-seedance-2-5-260628",
			metadata: map[string]interface{}{
				"resolution": "1080p",
			},
			code: "unsupported_resolution",
		},
		{
			name:  "seedance 2.5 first frame requires adaptive ratio",
			model: "doubao-seedance-2-5-260628",
			metadata: map[string]interface{}{
				"ratio": "16:9",
				"content": []interface{}{
					map[string]interface{}{
						"type":      "image_url",
						"image_url": map[string]interface{}{"url": "https://example.com/first.png"},
						"role":      "first_frame",
					},
				},
			},
			code: "unsupported_ratio",
		},
		{
			name:  "pro rejects duration above 15",
			model: "doubao-seedance-2-0-260128",
			metadata: map[string]interface{}{
				"duration": 16,
			},
			code: "unsupported_duration",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Set("task_request", relaycommon.TaskSubmitReq{Model: test.model, Prompt: "test", Metadata: test.metadata})
			info := &relaycommon.RelayInfo{
				OriginModelName: test.model,
				ChannelMeta: &relaycommon.ChannelMeta{
					ChannelType:       constant.ChannelTypeDoubaoVideo2,
					UpstreamModelName: "kzep_mapped",
					IsModelMapped:     true,
				},
			}
			adaptor := &TaskAdaptor{}
			adaptor.Init(info)
			taskErr := adaptor.validateDoubaoVideo2Request(c, info)
			if taskErr == nil || taskErr.Code != test.code {
				t.Fatalf("validation error = %+v, want code %q", taskErr, test.code)
			}
		})
	}
}

func TestDoubaoVideo2UsesPersistentResultAndTerminalStatuses(t *testing.T) {
	adaptor := &TaskAdaptor{ChannelType: constant.ChannelTypeDoubaoVideo2}
	responseData := []byte(`{
		"status":"succeeded",
		"content":{
			"video_url":"https://example.com/temporary.mp4",
			"kz_video_url":"https://example.com/persistent.mp4",
			"last_frame_url":"https://example.com/last.png"
		}
	}`)
	result, err := adaptor.ParseTaskResult(responseData)
	if err != nil {
		t.Fatal(err)
	}
	if result.Url != "https://example.com/persistent.mp4" {
		t.Fatalf("result URL = %q", result.Url)
	}
	converted, err := adaptor.ConvertToOpenAIVideo(&model.Task{
		TaskID:   "task_video2",
		Platform: constant.TaskPlatform("62"),
		Status:   model.TaskStatusSuccess,
		Data:     responseData,
		Properties: model.Properties{
			OriginModelName: "doubao-seedance-2-0-260128",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(converted, []byte(`"url":"https://example.com/persistent.mp4"`)) || !bytes.Contains(converted, []byte(`"kz_video_url":"https://example.com/persistent.mp4"`)) || !bytes.Contains(converted, []byte(`"last_frame_url":"https://example.com/last.png"`)) {
		t.Fatalf("persistent result metadata missing: %s", converted)
	}

	for _, status := range []string{"expired", "cancelled", "canceled"} {
		terminal, err := adaptor.ParseTaskResult([]byte(`{"status":"` + status + `"}`))
		if err != nil {
			t.Fatal(err)
		}
		if terminal.Status != string(model.TaskStatusFailure) || terminal.Progress != "100%" || !strings.Contains(terminal.Reason, status) {
			t.Fatalf("terminal mapping for %s = %+v", status, terminal)
		}
	}
}

func TestDoubaoVideo2ModelMetadata(t *testing.T) {
	adaptor := &TaskAdaptor{ChannelType: constant.ChannelTypeDoubaoVideo2}
	models := adaptor.GetModelList()
	if len(models) != 4 || models[2] != "doubao-seedance-2-0-mini-260615" || models[3] != "doubao-seedance-2-5-260628" {
		t.Fatalf("DoubaoVideo2.0 models = %#v", models)
	}
	if adaptor.GetChannelName() != "doubao-video-2.0" {
		t.Fatalf("channel name = %q", adaptor.GetChannelName())
	}
}

func TestZQBAPIOpenAIVideoMapsInputReferenceAfterMetadata(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{
		"model":"dreamina-seedance-2-0-260128",
		"prompt":"animate this image",
		"input_reference":{"image_url":"https://example.com/reference.png"},
		"seconds":"8",
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
	videoCtx, ok := getZQBAPIOpenAIVideoContext(c)
	if !ok {
		t.Fatal("ZQBAPI OpenAI video context is missing")
	}
	payload, err := adaptor.convertToZQBAPIOpenAIRequestPayload(&req, "", videoCtx.FrameReferences)
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
	if payload.Content[0].Role != "first_frame" {
		t.Fatalf("input_reference role = %q, want first_frame", payload.Content[0].Role)
	}
	if payload.Duration == nil || int(*payload.Duration) != 8 || payload.Resolution == nil || *payload.Resolution != "720p" || payload.Ratio == nil || *payload.Ratio != "16:9" {
		t.Fatalf("duration/size mapping failed: duration=%v resolution=%v ratio=%v", payload.Duration, payload.Resolution, payload.Ratio)
	}
}

func TestZQBAPIOpenAIVideoAppliesOpenAIDefaults(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{
		"model":"dreamina-seedance-2-0-260128",
		"prompt":"a paper boat on a river"
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
	if req.Seconds != "4" || req.Duration != 4 || req.Size != "720x1280" {
		t.Fatalf("defaults = seconds:%q duration:%d size:%q", req.Seconds, req.Duration, req.Size)
	}
}

func TestZQBAPIOpenAIVideoMapsFirstAndLastFrames(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{
		"model":"dreamina-seedance-2-0-260128",
		"prompt":"transition naturally",
		"first_frame":{"image_url":"https://example.com/first.png"},
		"last_frame":"https://example.com/last.png"
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
	videoCtx, ok := getZQBAPIOpenAIVideoContext(c)
	if !ok {
		t.Fatal("ZQBAPI OpenAI video context is missing")
	}
	payload, err := adaptor.convertToZQBAPIOpenAIRequestPayload(&req, "", videoCtx.FrameReferences)
	if err != nil {
		t.Fatal(err)
	}
	if len(payload.Content) != 3 || payload.Content[0].Role != "first_frame" || payload.Content[1].Role != "last_frame" {
		t.Fatalf("first/last frame roles were not preserved: %+v", payload.Content)
	}
}

func TestZQBAPIOpenAIVideoRejectsNonOpenAISeconds(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{
		"model":"dreamina-seedance-2-0-260128",
		"prompt":"test",
		"seconds":"5"
	}`))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeZQBAPI}}
	adaptor := &TaskAdaptor{}
	adaptor.Init(info)
	if taskErr := adaptor.ValidateRequestAndSetAction(c, info); taskErr == nil || taskErr.Code != "invalid_seconds" {
		t.Fatalf("seconds=5 must fail with invalid_seconds, got %+v", taskErr)
	}
}

func TestZQBAPIOpenAIVideoRejectsHighResolutionForFastModel(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{
		"model":"dreamina-seedance-2-0-fast-hc",
		"prompt":"test",
		"size":"1792x1024"
	}`))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeZQBAPI}}
	adaptor := &TaskAdaptor{}
	adaptor.Init(info)
	if taskErr := adaptor.ValidateRequestAndSetAction(c, info); taskErr == nil || taskErr.Code != "unsupported_size" {
		t.Fatalf("Fast 1080p must fail with unsupported_size, got %+v", taskErr)
	}
}

func TestZQBAPIOpenAIVideoRejectsUnsupportedMediaFields(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{
		"model":"dreamina-seedance-2-0-260128",
		"prompt":"test",
		"content":[{"type":"image_url","image_url":{"url":"https://example.com/ignored.png"}}]
	}`))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeZQBAPI}}
	adaptor := &TaskAdaptor{}
	adaptor.Init(info)
	if taskErr := adaptor.ValidateRequestAndSetAction(c, info); taskErr == nil || taskErr.Code != "unsupported_input" {
		t.Fatalf("unsupported media field must fail closed, got %+v", taskErr)
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
	videoCtx, ok := getZQBAPIOpenAIVideoContext(c)
	if !ok || len(videoCtx.FrameReferences) != 1 || !strings.HasPrefix(videoCtx.FrameReferences[0].URL, "data:image/png;base64,") || videoCtx.FrameReferences[0].Role != "first_frame" {
		t.Fatalf("multipart reference was not converted to a first-frame data URL: %#v", videoCtx.FrameReferences)
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
	payload, err := (&TaskAdaptor{}).convertToZQBAPIOpenAIRequestPayload(&req, "https://example.com/origin.mp4", nil)
	if err != nil {
		t.Fatal(err)
	}
	images, videos := countZQBAPIMedia(payload.Content)
	if images != 0 || videos != 1 {
		t.Fatalf("media counts = images:%d videos:%d", images, videos)
	}
	if payload.Content[0].Role != "reference_video" {
		t.Fatalf("remix source role = %q, want reference_video", payload.Content[0].Role)
	}
}

func TestParseTaskResultMapsZQBAPITerminalStatuses(t *testing.T) {
	for _, status := range []string{"expired", "cancelled", "canceled"} {
		t.Run(status, func(t *testing.T) {
			result, err := (&TaskAdaptor{ChannelType: constant.ChannelTypeZQBAPI}).ParseTaskResult([]byte(`{"status":"` + status + `"}`))
			if err != nil {
				t.Fatal(err)
			}
			if result.Status != string(model.TaskStatusFailure) || result.Progress != "100%" || !strings.Contains(result.Reason, status) {
				t.Fatalf("terminal mapping = %+v", result)
			}
		})
	}
	result, err := (&TaskAdaptor{ChannelType: constant.ChannelTypeDoubaoVideo}).ParseTaskResult([]byte(`{"status":"expired"}`))
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != string(model.TaskStatusInProgress) || result.Progress != "30%" {
		t.Fatalf("non-ZQBAPI status mapping changed: %+v", result)
	}
}

func TestZQBAPIOpenAIVideoDeleteUsesNativeTaskEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/api/v3/contents/generations/tasks/upstream-1" {
			t.Fatalf("delete request = %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	resp, err := (&TaskAdaptor{}).DeleteOpenAIVideo(context.Background(), server.URL, "secret", "upstream-1", "")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete status = %d", resp.StatusCode)
	}
}

func TestZQBAPIOpenAIVideoSubmitResponseIsDeferred(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
	c.Set("task_request", relaycommon.TaskSubmitReq{Prompt: "test prompt"})
	c.Set(zqbapiOpenAIVideoContextKey, zqbapiOpenAIVideoContext{Seconds: "4", Size: "720x1280"})

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"id":"upstream-task"}`)),
	}
	info := &relaycommon.RelayInfo{
		OriginModelName: "dreamina-seedance-2-0-260128",
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{PublicTaskID: "task_public"},
	}
	taskID, _, taskErr := (&TaskAdaptor{ChannelType: constant.ChannelTypeZQBAPI}).DoResponse(c, resp, info)
	if taskErr != nil {
		t.Fatal(taskErr)
	}
	if taskID != "upstream-task" {
		t.Fatalf("taskID = %q", taskID)
	}
	if recorder.Body.Len() != 0 {
		t.Fatalf("response was written before persistence: %s", recorder.Body.String())
	}
	value, exists := c.Get(string(constant.ContextKeyZQBAPIOpenAIVideoResponse))
	responseBody, ok := value.([]byte)
	if !exists || !ok || !bytes.Contains(responseBody, []byte(`"id":"task_public"`)) {
		t.Fatalf("deferred response missing or invalid: %q", responseBody)
	}
}

func TestDoubaoVideo2OpenAIVideoResponseAndPropertiesAreStable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
	c.Set("task_request", relaycommon.TaskSubmitReq{Prompt: "stored prompt"})
	c.Set(doubaoVideo2OpenAIContextKey, doubaoVideo2OpenAIVideoContext{Seconds: "8", Size: "1280x720"})
	c.Set(string(constant.ContextKeyOpenAIVideoRequest), true)

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"id":"upstream-task"}`)),
	}
	info := &relaycommon.RelayInfo{
		OriginModelName: "doubao-seedance-2-0-260128",
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{PublicTaskID: "task_public"},
	}
	taskID, _, taskErr := (&TaskAdaptor{ChannelType: constant.ChannelTypeDoubaoVideo2}).DoResponse(c, resp, info)
	if taskErr != nil {
		t.Fatal(taskErr)
	}
	if taskID != "upstream-task" || recorder.Body.Len() != 0 {
		t.Fatalf("submission response must be deferred until persistence: id=%q body=%s", taskID, recorder.Body.String())
	}
	value, exists := c.Get(string(constant.ContextKeyOpenAIVideoResponse))
	responseBody, ok := value.([]byte)
	if !exists || !ok || !bytes.Contains(responseBody, []byte(`"id":"task_public"`)) || bytes.Contains(responseBody, []byte(`"task_id"`)) {
		t.Fatalf("deferred OpenAI response missing or invalid: %s", responseBody)
	}

	task := &model.Task{}
	ApplyDoubaoVideo2OpenAIVideoTaskProperties(c, task)
	if !task.Properties.OpenAIVideo || task.Properties.VideoPrompt != "stored prompt" || task.Properties.VideoSeconds != "8" || task.Properties.VideoSize != "1280x720" {
		t.Fatalf("compatibility properties = %+v", task.Properties)
	}

	task.TaskID = "task_public"
	task.Platform = constant.TaskPlatform("62")
	task.Status = model.TaskStatusFailure
	task.CreatedAt = 100
	task.UpdatedAt = 200
	task.FailReason = "provider stack at https://internal.example/image"
	task.Data = []byte(`{"status":"failed","error":{"message":"resource download failed at https://internal.example/image"}}`)
	converted, err := (&TaskAdaptor{}).ConvertToOpenAIVideo(task)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(converted, []byte(`"completed_at":200`)) || !bytes.Contains(converted, []byte(`"seconds":"8"`)) || !bytes.Contains(converted, []byte(`"size":"1280x720"`)) {
		t.Fatalf("persisted compatibility fields missing: %s", converted)
	}
	if bytes.Contains(converted, []byte(`"task_id"`)) || bytes.Contains(converted, []byte("internal.example")) || !bytes.Contains(converted, []byte(`"code":"reference_image_download_failed"`)) {
		t.Fatalf("public failure response is not sanitized: %s", converted)
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
			OpenAIVideo:        true,
			VideoPrompt:        "stored prompt",
			RemixedFromVideoID: "task_origin",
		},
	}
	converted, err := (&TaskAdaptor{}).ConvertToOpenAIVideo(zqbapiTask)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(converted, []byte(`"completed_at":null`)) || !bytes.Contains(converted, []byte(`"expires_at":null`)) || !bytes.Contains(converted, []byte(`"error":null`)) {
		t.Fatalf("in-progress ZQBAPI video must include nullable standard fields: %s", converted)
	}
	if !bytes.Contains(converted, []byte(`"seconds":"5"`)) || !bytes.Contains(converted, []byte(`"size":"720x1280"`)) || !bytes.Contains(converted, []byte(`"remixed_from_video_id":"task_origin"`)) {
		t.Fatalf("ZQBAPI compatibility fields missing: %s", converted)
	}
	if !bytes.Contains(converted, []byte(`"prompt":"stored prompt"`)) || bytes.Contains(converted, []byte(`"task_id"`)) || bytes.Contains(converted, []byte(`"metadata"`)) {
		t.Fatalf("ZQBAPI standard response fields are incorrect: %s", converted)
	}

	nativeZQBAPI := *zqbapiTask
	nativeZQBAPI.Properties.OpenAIVideo = false
	converted, err = (&TaskAdaptor{}).ConvertToOpenAIVideo(&nativeZQBAPI)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(converted, []byte(`"task_id":"task_zqbapi"`)) || !bytes.Contains(converted, []byte(`"metadata"`)) || bytes.Contains(converted, []byte(`"completed_at":null`)) {
		t.Fatalf("native ZQBAPI converter behavior changed: %s", converted)
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

func TestZQBAPIPublicVideoFailureDoesNotExposeProviderMessage(t *testing.T) {
	message, code := zqbapiPublicVideoFailure(`The parameter content[1].image_url is invalid: resource download failed at https://provider.example/file`)
	if code != "reference_image_download_failed" || strings.Contains(message, "provider.example") || strings.Contains(message, "content[1]") {
		t.Fatalf("message=%q code=%q", message, code)
	}

	message, code = zqbapiPublicVideoFailure("unrecognized provider stack trace")
	if message != "Video generation failed." || code != "video_generation_failed" {
		t.Fatalf("fallback message=%q code=%q", message, code)
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
