package doubao

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	"github.com/samber/lo"
)

// ============================
// Request / Response structures
// ============================

type ContentItem struct {
	Type      string     `json:"type,omitempty"`
	Text      string     `json:"text,omitempty"`
	ImageURL  *MediaURL  `json:"image_url,omitempty"`
	VideoURL  *MediaURL  `json:"video_url,omitempty"`
	AudioURL  *MediaURL  `json:"audio_url,omitempty"`
	DraftTask *DraftTask `json:"draft_task,omitempty"`
	Role      string     `json:"role,omitempty"`
}

type MediaURL struct {
	URL string `json:"url,omitempty"`
}

type DraftTask struct {
	ID string `json:"id,omitempty"`
}

type ModerationOptions struct {
	IPs []string `json:"ips,omitempty"`
}

type requestPayload struct {
	Model                 string         `json:"model"`
	Content               []ContentItem  `json:"content,omitempty"`
	CallbackURL           *string        `json:"callback_url,omitempty"`
	ReturnLastFrame       *dto.BoolValue `json:"return_last_frame,omitempty"`
	ServiceTier           *string        `json:"service_tier,omitempty"`
	ExecutionExpiresAfter *dto.IntValue  `json:"execution_expires_after,omitempty"`
	GenerateAudio         *dto.BoolValue `json:"generate_audio,omitempty"`
	Draft                 *dto.BoolValue `json:"draft,omitempty"`
	Tools                 []struct {
		Type string `json:"type,omitempty"`
	} `json:"tools,omitempty"`
	Resolution        *string            `json:"resolution,omitempty"`
	Ratio             *string            `json:"ratio,omitempty"`
	Duration          *dto.IntValue      `json:"duration,omitempty"`
	Frames            *dto.IntValue      `json:"frames,omitempty"`
	Seed              *dto.IntValue      `json:"seed,omitempty"`
	CameraFixed       *dto.BoolValue     `json:"camera_fixed,omitempty"`
	Watermark         *dto.BoolValue     `json:"watermark,omitempty"`
	ModerationOptions *ModerationOptions `json:"moderation_options,omitempty"`
	SafetyIdentifier  *string            `json:"safety_identifier,omitempty"`
	BitrateMode       *string            `json:"bitrate_mode,omitempty"`
	OutputFormat      *string            `json:"output_format,omitempty"`
}

// The provider persists the complete request JSON in a MySQL request_body
// column. Production has returned MySQL 1406 once inline media pushes that
// value beyond the column capacity. Keep a small margin below the 64 KiB TEXT
// boundary because Base64/data URLs are embedded in the same JSON document.
const doubaoVideo2InlineRequestMaxBytes = 60 * 1024

const doubaoVideo2R2RequestCacheContextKey = "doubao_video2_r2_request_cache"

type responsePayload struct {
	ID     string `json:"id"` // task_id
	TaskID string `json:"task_id,omitempty"`
}

type responseTask struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Status  string `json:"status"`
	Content struct {
		VideoURL     string `json:"video_url"`
		KZVideoURL   string `json:"kz_video_url"`
		LastFrameURL string `json:"last_frame_url"`
	} `json:"content"`
	Seed            int    `json:"seed"`
	Resolution      string `json:"resolution"`
	Duration        int    `json:"duration"`
	Ratio           string `json:"ratio"`
	FramesPerSecond int    `json:"framespersecond"`
	ServiceTier     string `json:"service_tier"`
	Tools           []struct {
		Type string `json:"type"`
	} `json:"tools"`
	Usage struct {
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
		ToolUsage        struct {
			WebSearch int `json:"web_search"`
		} `json:"tool_usage"`
	} `json:"usage"`
	Error struct {
		Code    any    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	CreatedAt int64 `json:"created_at"`
	UpdatedAt int64 `json:"updated_at"`
}

// ============================
// Adaptor implementation
// ============================

type TaskAdaptor struct {
	taskcommon.BaseBilling
	ChannelType int
	apiKey      string
	baseURL     string
}

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	a.ChannelType = info.ChannelType
	a.baseURL = info.ChannelBaseUrl
	a.apiKey = info.ApiKey
}

// ValidateRequestAndSetAction parses body, validates fields and sets default action.
func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) (taskErr *dto.TaskError) {
	if a.ChannelType == constant.ChannelTypeZQBAPI || a.ChannelType == constant.ChannelTypeDoubaoVideo2 {
		if isZQBAPINativeSeedanceRequest(c) {
			return validateZQBAPINativeSeedanceRequest(c, info)
		}
	}
	if a.ChannelType == constant.ChannelTypeZQBAPI {
		if isZQBAPIOpenAIVideoRequest(c) {
			return validateZQBAPIOpenAIVideoRequest(c, info)
		}
	}
	if a.ChannelType == constant.ChannelTypeDoubaoVideo2 {
		return a.validateAndSetDoubaoVideo2Request(c, info)
	}
	// Accept only POST /v1/video/generations as "generate" action.
	if taskErr = relaycommon.ValidateBasicTaskRequest(c, info, constant.TaskActionGenerate); taskErr != nil {
		return taskErr
	}
	return nil
}

// BuildRequestURL constructs the upstream URL.
func (a *TaskAdaptor) BuildRequestURL(_ *relaycommon.RelayInfo) (string, error) {
	return fmt.Sprintf("%s/api/v3/contents/generations/tasks", a.baseURL), nil
}

// BuildRequestHeader sets required headers.
func (a *TaskAdaptor) BuildRequestHeader(_ *gin.Context, req *http.Request, _ *relaycommon.RelayInfo) error {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	return nil
}

// EstimateBilling 根据请求 metadata 中的输出分辨率与是否包含视频输入，返回相对基准价的计费 OtherRatio。
func (a *TaskAdaptor) EstimateBilling(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64 {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil
	}
	hasVideo := hasVideoInMetadata(req.Metadata)
	if a.ChannelType == constant.ChannelTypeZQBAPI && info.Action == constant.TaskActionRemix {
		hasVideo = true
	}
	if c != nil {
		c.Set(contextKeySeedanceHasVideo, hasVideo)
	}
	resolution := seedanceRequestResolution(req)
	ratio, ok := GetVideoInputRatio(info.OriginModelName, resolution, hasVideo, info.PriceData.ModelRatio*2)
	if !ok || ratio == 1.0 {
		return nil
	}
	return map[string]float64{"video_input": ratio}
}

// EstimatePreAuthorization reserves a conservative video-token estimate for
// native Doubao Seedance tasks. Unlike the OpenAI-compatible Sora path, the
// native API reports video usage only (total_tokens == completion_tokens), so
// no synthetic text tokens are included in the reservation.
func (a *TaskAdaptor) EstimatePreAuthorization(c *gin.Context, info *relaycommon.RelayInfo) (int, *common.QuotaClamp, bool) {
	if info == nil || !ratio_setting.IsSeedanceVideoModel(info.OriginModelName) || info.PriceData.UsePrice || info.PriceData.ModelRatio <= 0 {
		return 0, nil, false
	}
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return 0, nil, false
	}
	hasVideo := hasVideoInMetadata(req.Metadata)
	if a.ChannelType == constant.ChannelTypeZQBAPI && info.Action == constant.TaskActionRemix {
		hasVideo = true
	}
	videoRatio := 1.0
	if ratio, ok := GetVideoInputRatio(info.OriginModelName, seedanceRequestResolution(req), hasVideo, info.PriceData.ModelRatio*2); ok && ratio > 0 {
		videoRatio = ratio
	}
	groupRatio := info.PriceData.GroupRatioInfo.GroupRatio
	c.Set(contextKeySeedanceHasVideo, hasVideo)
	c.Set(contextKeyEstimatedTextTokens, 0)
	c.Set(contextKeyEstimatedVideoTokens, seedanceEstimatedVideoTokens)
	quotaValue := float64(seedanceEstimatedVideoTokens) * videoRatio * info.PriceData.ModelRatio * groupRatio
	quota, clamp := common.QuotaFromFloatChecked(quotaValue)
	return quota, clamp, true
}

func seedanceRequestResolution(req relaycommon.TaskSubmitReq) string {
	if resolution, ok := req.Metadata["resolution"].(string); ok && strings.TrimSpace(resolution) != "" {
		return ratio_setting.NormalizeVideoResolution(resolution)
	}
	size := strings.ToLower(strings.TrimSpace(req.Size))
	switch size {
	case "854x480", "480x854":
		return "480p"
	case "1280x720", "720x1280":
		return "720p"
	case "1920x1080", "1080x1920", "1792x1024", "1024x1792":
		return "1080p"
	case "3840x2160", "2160x3840":
		return "4k"
	default:
		return ratio_setting.NormalizeVideoResolution(size)
	}
}

// AdjustBillingOnComplete updates OtherRatios from upstream-reported resolution, then
// returns 0 so the shared token recalculation path continues (no cross-channel formula change).
func (a *TaskAdaptor) AdjustBillingOnComplete(task *model.Task, taskResult *relaycommon.TaskInfo) int {
	if task == nil || taskResult == nil {
		return 0
	}
	res := strings.TrimSpace(taskResult.Resolution)
	if res == "" {
		return 0
	}
	bc := task.PrivateData.BillingContext
	if bc == nil || bc.OriginModelName == "" {
		return 0
	}
	hasVideo := false
	if bc.VideoHasInput != nil {
		hasVideo = *bc.VideoHasInput
	}
	ratio, ok := GetVideoInputRatio(bc.OriginModelName, res, hasVideo, bc.ModelRatio*2)
	if !ok {
		return 0
	}
	if bc.OtherRatios == nil {
		bc.OtherRatios = map[string]float64{}
	}
	if ratio == 1.0 {
		delete(bc.OtherRatios, "video_input")
	} else {
		bc.OtherRatios["video_input"] = ratio
	}
	return 0
}

// hasVideoInMetadata 检查 metadata 的 content 是否包含非空 video_url，
// 避免空壳 type=video_url 骗取含视频输入折扣。
func hasVideoInMetadata(metadata map[string]interface{}) bool {
	if metadata == nil {
		return false
	}
	contentRaw, ok := metadata["content"]
	if !ok {
		return false
	}
	contentSlice, ok := contentRaw.([]interface{})
	if !ok {
		return false
	}
	for _, item := range contentSlice {
		itemMap, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		if url := videoURLFromContentItem(itemMap); url != "" {
			return true
		}
	}
	return false
}

func videoURLFromContentItem(itemMap map[string]interface{}) string {
	if raw, ok := itemMap["video_url"]; ok {
		switch v := raw.(type) {
		case string:
			return strings.TrimSpace(v)
		case map[string]interface{}:
			if u, ok := v["url"].(string); ok {
				return strings.TrimSpace(u)
			}
		}
	}
	if itemMap["type"] == "video_url" {
		if u, ok := itemMap["url"].(string); ok {
			return strings.TrimSpace(u)
		}
	}
	return ""
}

// BuildRequestBody converts request into Doubao specific format.
func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil, err
	}

	var body *requestPayload
	nativeRequest, nativeRequestOK := getZQBAPINativeSeedanceRequest(c)
	if (a.ChannelType == constant.ChannelTypeZQBAPI || a.ChannelType == constant.ChannelTypeDoubaoVideo2) && nativeRequestOK {
		body = nativeRequest.Payload
		originalModel := body.Model
		originalMediaURLs := zqbapiNativeSeedanceMediaURLs(body)
		if info.IsModelMapped {
			body.Model = info.UpstreamModelName
		}
		if err := externalizeInlineMediaToR2(c, info, body); err != nil {
			return nil, err
		}
		if a.ChannelType == constant.ChannelTypeZQBAPI {
			if err := a.prepareZQBAPIImages(c, info, body); err != nil {
				return nil, err
			}
		} else {
			if err := a.prepareDoubaoVideo2Images(c, info, body); err != nil {
				return nil, err
			}
		}
		data, err := mergeZQBAPINativeSeedancePayload(
			nativeRequest.Raw,
			body,
			body.Model != originalModel,
			!zqbapiNativeSeedanceMediaEqual(originalMediaURLs, zqbapiNativeSeedanceMediaURLs(body)),
		)
		if err != nil {
			return nil, errors.Wrap(err, "marshal native Seedance request payload failed")
		}
		info.UpstreamRequestBodySize = int64(len(data))
		return bytes.NewReader(data), nil
	}
	if videoCtx, ok := getZQBAPIOpenAIVideoContext(c); a.ChannelType == constant.ChannelTypeZQBAPI && ok {
		if strings.TrimSpace(req.Model) == "" {
			req.Model = info.OriginModelName
		}
		originVideoURL := ""
		if info.Action == constant.TaskActionRemix {
			originTask, exists, getErr := model.GetByTaskId(info.UserId, info.OriginTaskID)
			if getErr != nil {
				return nil, errors.Wrap(getErr, "get remix origin task failed")
			}
			if !exists || originTask == nil || (originTask.Status != model.TaskStatusSuccess && originTask.Status != model.TaskStatusPendingSettlement) {
				return nil, newZQBAPIBuildError(zqbapiErrorInvalidVideo, "remix", fmt.Errorf("origin video is not completed"))
			}
			originVideoURL = strings.TrimSpace(originTask.GetUpstreamResultURL())
			if originVideoURL == "" {
				return nil, newZQBAPIBuildError(zqbapiErrorInvalidVideo, "remix", fmt.Errorf("origin video URL is empty"))
			}
		}
		body, err = a.convertToZQBAPIOpenAIRequestPayload(&req, originVideoURL, videoCtx.FrameReferences)
		if err != nil {
			err = newZQBAPIBuildError(zqbapiErrorInvalidVideo, "protocol conversion", err)
		}
		if err == nil {
			imageCount, videoCount := countZQBAPIMedia(body.Content)
			if videoCtx.ReferenceDeclared && imageCount == 0 {
				err = newZQBAPIBuildError(zqbapiErrorInvalidVideo, "protocol conversion", fmt.Errorf("input_reference was declared but no image reached the provider payload"))
			}
			if info.Action == constant.TaskActionRemix && videoCount == 0 {
				err = newZQBAPIBuildError(zqbapiErrorInvalidVideo, "protocol conversion", fmt.Errorf("remix origin was not included in the provider payload"))
			}
		}
	} else {
		body, err = a.convertToRequestPayload(&req)
	}
	if err != nil {
		return nil, errors.Wrap(err, "convert request payload failed")
	}
	if info.IsModelMapped {
		body.Model = info.UpstreamModelName
	} else {
		info.UpstreamModelName = body.Model
	}
	if a.ChannelType == constant.ChannelTypeZQBAPI {
		// OpenAI-compatible multipart/data media also needs a public URL before
		// it can participate in video-material recovery. Native Seedance requests
		// take the same step above; this keeps both protocol paths consistent.
		if err := externalizeInlineMediaToR2(c, info, body); err != nil {
			return nil, err
		}
		if err := a.prepareZQBAPIImages(c, info, body); err != nil {
			return nil, err
		}
		if videoCtx, ok := getZQBAPIOpenAIVideoContext(c); ok {
			logger.LogInfo(c.Request.Context(), fmt.Sprintf("ZQBAPI OpenAI video payload prepared: action=%s references=%d source=%s seconds=%s size=%s", info.Action, videoCtx.ReferenceCount, videoCtx.ReferenceSource, videoCtx.Seconds, videoCtx.Size))
		}
	}
	if a.ChannelType == constant.ChannelTypeDoubaoVideo2 {
		if err := externalizeInlineMediaToR2(c, info, body); err != nil {
			return nil, err
		}
		if err := a.prepareDoubaoVideo2Images(c, info, body); err != nil {
			return nil, err
		}
	}
	data, err := common.Marshal(body)
	if err != nil {
		return nil, err
	}
	info.UpstreamRequestBodySize = int64(len(data))
	if a.ChannelType == constant.ChannelTypeDoubaoVideo2 && doubaoVideo2HasInlineMedia(body) && len(data) > doubaoVideo2InlineRequestMaxBytes {
		return nil, &doubaoVideo2BuildError{
			Kind:  doubaoVideo2ErrorRequestTooLarge,
			Stage: "request size validation",
			Err: fmt.Errorf(
				"inline media makes the upstream JSON request %d bytes, exceeding the safe %d-byte limit; use a public HTTP(S) media URL or material-library asset instead of Base64/data URL input",
				len(data), doubaoVideo2InlineRequestMaxBytes,
			),
		}
	}
	return bytes.NewReader(data), nil
}

type zqbapiNativeSeedanceMedia struct {
	ImageURL string
	VideoURL string
	AudioURL string
}

func zqbapiNativeSeedanceMediaURLs(body *requestPayload) []zqbapiNativeSeedanceMedia {
	if body == nil {
		return nil
	}
	urls := make([]zqbapiNativeSeedanceMedia, 0, len(body.Content))
	for _, item := range body.Content {
		media := zqbapiNativeSeedanceMedia{}
		if item.ImageURL != nil {
			media.ImageURL = item.ImageURL.URL
		}
		if item.VideoURL != nil {
			media.VideoURL = item.VideoURL.URL
		}
		if item.AudioURL != nil {
			media.AudioURL = item.AudioURL.URL
		}
		urls = append(urls, media)
	}
	return urls
}

func zqbapiNativeSeedanceMediaEqual(left, right []zqbapiNativeSeedanceMedia) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func mergeZQBAPINativeSeedancePayload(raw []byte, body *requestPayload, modelChanged, contentChanged bool) ([]byte, error) {
	var fields map[string]json.RawMessage
	if err := common.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	if !modelChanged && !contentChanged {
		return append([]byte(nil), raw...), nil
	}
	if modelChanged {
		modelData, err := common.Marshal(body.Model)
		if err != nil {
			return nil, err
		}
		fields["model"] = modelData
	}
	if contentChanged {
		var rawContent []map[string]json.RawMessage
		if err := common.Unmarshal(fields["content"], &rawContent); err != nil {
			return nil, err
		}
		for index, item := range body.Content {
			if index >= len(rawContent) {
				continue
			}
			for field, media := range map[string]*MediaURL{
				"image_url": item.ImageURL,
				"video_url": item.VideoURL,
				"audio_url": item.AudioURL,
			} {
				if media == nil {
					continue
				}
				mediaFields := map[string]json.RawMessage{}
				if rawMedia, exists := rawContent[index][field]; exists {
					if err := common.Unmarshal(rawMedia, &mediaFields); err != nil {
						return nil, err
					}
				}
				urlData, err := common.Marshal(media.URL)
				if err != nil {
					return nil, err
				}
				mediaFields["url"] = urlData
				mediaData, err := common.Marshal(mediaFields)
				if err != nil {
					return nil, err
				}
				rawContent[index][field] = mediaData
			}
		}
		contentData, err := common.Marshal(rawContent)
		if err != nil {
			return nil, err
		}
		fields["content"] = contentData
	}
	return common.Marshal(fields)
}

func doubaoVideo2HasInlineMedia(body *requestPayload) bool {
	if body == nil {
		return false
	}
	for _, item := range body.Content {
		for _, media := range []*MediaURL{item.ImageURL, item.VideoURL, item.AudioURL} {
			if media == nil {
				continue
			}
			if doubaoVideo2IsInlineMediaSource(media.URL) {
				return true
			}
		}
	}
	return false
}

func doubaoVideo2IsInlineMediaSource(source string) bool {
	source = strings.ToLower(strings.TrimSpace(source))
	return source != "" &&
		!strings.HasPrefix(source, "http://") &&
		!strings.HasPrefix(source, "https://") &&
		!strings.HasPrefix(source, "asset://")
}

func externalizeInlineMediaToR2(c *gin.Context, info *relaycommon.RelayInfo, body *requestPayload) error {
	if body == nil || !doubaoVideo2HasInlineMedia(body) {
		return nil
	}
	data, err := common.Marshal(body)
	if err != nil {
		return err
	}
	info.UpstreamRequestBodySize = int64(len(data))
	// Once R2 is configured, every inline payload uses the stable externalized
	// path. Without R2, preserve legacy behavior for small payloads.
	if len(data) <= doubaoVideo2InlineRequestMaxBytes && !service.DoubaoVideo2R2Configured() {
		return nil
	}
	for index := range body.Content {
		item := &body.Content[index]
		for _, media := range []*MediaURL{item.ImageURL, item.VideoURL, item.AudioURL} {
			if media == nil {
				continue
			}
			if !doubaoVideo2IsInlineMediaSource(media.URL) {
				continue
			}
			digest := sha256.Sum256([]byte(media.URL))
			cacheKey := hex.EncodeToString(digest[:])
			if cached, exists := getDoubaoVideo2R2RequestCache(c)[cacheKey]; exists {
				media.URL = cached
				continue
			}
			publicURL, uploadErr := service.StoreDoubaoVideo2InlineMedia(c.Request.Context(), media.URL)
			if uploadErr != nil {
				kind := doubaoVideo2ErrorTemporaryMedia
				if !service.DoubaoVideo2R2Configured() {
					kind = doubaoVideo2ErrorRequestTooLarge
				}
				return &doubaoVideo2BuildError{
					Kind:  kind,
					Stage: "R2 compatibility upload",
					Err:   fmt.Errorf("content[%d] could not be converted to a temporary public URL: %w", index, uploadErr),
				}
			}
			getDoubaoVideo2R2RequestCache(c)[cacheKey] = publicURL
			media.URL = publicURL
		}
	}
	converted, err := common.Marshal(body)
	if err != nil {
		return err
	}
	info.UpstreamRequestBodySize = int64(len(converted))
	logger.LogInfo(c.Request.Context(), fmt.Sprintf("inline video media externalized to R2: upstream_bytes=%d", len(converted)))
	return nil
}

func getDoubaoVideo2R2RequestCache(c *gin.Context) map[string]string {
	if c != nil {
		if cached, exists := c.Get(doubaoVideo2R2RequestCacheContextKey); exists {
			if values, ok := cached.(map[string]string); ok {
				return values
			}
		}
	}
	values := map[string]string{}
	if c != nil {
		c.Set(doubaoVideo2R2RequestCacheContextKey, values)
	}
	return values
}

// DoRequest delegates to common helper.
func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	return channel.DoTaskApiRequest(a, c, info, requestBody)
}

// DoResponse handles upstream response, returns taskID etc.
func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (taskID string, taskData []byte, taskErr *dto.TaskError) {
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		taskErr = service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusInternalServerError)
		return
	}
	_ = resp.Body.Close()

	// Parse Doubao response
	var dResp responsePayload
	if err := common.Unmarshal(responseBody, &dResp); err != nil {
		taskErr = service.TaskErrorWrapper(errors.Wrapf(err, "body: %s", responseBody), "unmarshal_response_body_failed", http.StatusInternalServerError)
		return
	}

	if dResp.ID == "" {
		dResp.ID = dResp.TaskID
	}
	if dResp.ID == "" {
		taskErr = service.TaskErrorWrapper(fmt.Errorf("task_id is empty"), "invalid_response", http.StatusInternalServerError)
		return
	}

	ov := dto.NewOpenAIVideo()
	ov.ID = info.PublicTaskID
	ov.TaskID = info.PublicTaskID
	ov.CreatedAt = time.Now().Unix()
	ov.Model = info.OriginModelName
	if a.ChannelType == constant.ChannelTypeDoubaoVideo2 {
		if videoCtx, ok := getDoubaoVideo2OpenAIVideoContext(c); ok {
			ov.TaskID = ""
			ov.StandardFields = true
			if taskReq, reqErr := relaycommon.GetTaskRequest(c); reqErr == nil {
				ov.Prompt = taskReq.Prompt
			}
			ov.Seconds = videoCtx.Seconds
			ov.Size = videoCtx.Size
		}
	}
	if a.ChannelType == constant.ChannelTypeZQBAPI {
		if videoCtx, ok := getZQBAPIOpenAIVideoContext(c); ok {
			ov.TaskID = ""
			ov.StandardFields = true
			if taskReq, reqErr := relaycommon.GetTaskRequest(c); reqErr == nil {
				ov.Prompt = taskReq.Prompt
			}
			ov.Seconds = videoCtx.Seconds
			ov.Size = videoCtx.Size
			ov.RemixedFromVideoID = videoCtx.RemixedFromID
		}
	}

	if a.ChannelType == constant.ChannelTypeZQBAPI {
		if _, ok := getZQBAPIOpenAIVideoContext(c); ok {
			responseData, marshalErr := common.Marshal(ov)
			if marshalErr != nil {
				return "", nil, service.TaskErrorWrapper(marshalErr, "marshal_response_failed", http.StatusInternalServerError)
			}
			// The controller writes this response only after the local task row is
			// durable, preventing successful-but-unqueryable public task IDs.
			c.Set(string(constant.ContextKeyZQBAPIOpenAIVideoResponse), responseData)
			return dResp.ID, responseBody, nil
		}
	}
	if a.ChannelType == constant.ChannelTypeZQBAPI || a.ChannelType == constant.ChannelTypeDoubaoVideo2 {
		if _, ok := getZQBAPINativeSeedanceRequest(c); ok {
			responseData, marshalErr := NormalizeZQBAPINativeSeedanceResponse(responseBody, info.PublicTaskID)
			if marshalErr != nil {
				return "", nil, service.TaskErrorWrapper(marshalErr, "marshal_response_failed", http.StatusInternalServerError)
			}
			// The controller writes this body only after the local task is durable.
			c.Set(string(constant.ContextKeyZQBAPINativeSeedanceResponse), responseData)
			return dResp.ID, responseBody, nil
		}
	}
	if a.ChannelType == constant.ChannelTypeDoubaoVideo2 {
		if _, ok := getDoubaoVideo2OpenAIVideoContext(c); ok {
			responseData, marshalErr := common.Marshal(ov)
			if marshalErr != nil {
				return "", nil, service.TaskErrorWrapper(marshalErr, "marshal_response_failed", http.StatusInternalServerError)
			}
			c.Set(string(constant.ContextKeyOpenAIVideoResponse), responseData)
			return dResp.ID, responseBody, nil
		}
	}
	c.JSON(http.StatusOK, ov)
	return dResp.ID, responseBody, nil
}

// FetchTask fetch task status
func (a *TaskAdaptor) FetchTask(baseUrl, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid task_id")
	}

	uri := fmt.Sprintf("%s/api/v3/contents/generations/tasks/%s", baseUrl, taskID)

	ctx := context.Background()
	if requestContext, ok := body["context"].(context.Context); ok && requestContext != nil {
		ctx = requestContext
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)

	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, fmt.Errorf("new proxy http client failed: %w", err)
	}
	return client.Do(req)
}

func (a *TaskAdaptor) DeleteOpenAIVideo(ctx context.Context, baseURL, key, upstreamTaskID, proxy string) (*http.Response, error) {
	upstreamTaskID = strings.TrimSpace(upstreamTaskID)
	if upstreamTaskID == "" {
		return nil, fmt.Errorf("upstream task id is required")
	}
	uri := fmt.Sprintf("%s/api/v3/contents/generations/tasks/%s", strings.TrimRight(baseURL, "/"), upstreamTaskID)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, uri, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	client, err := service.NewProxyHttpClient(proxy)
	if err != nil {
		return nil, fmt.Errorf("new proxy http client failed: %w", err)
	}
	return client.Do(req)
}

func (a *TaskAdaptor) GetModelList() []string {
	if a.ChannelType == constant.ChannelTypeDoubaoVideo2 {
		return DoubaoVideo2ModelList
	}
	return ModelList
}

func (a *TaskAdaptor) GetChannelName() string {
	if a.ChannelType == constant.ChannelTypeDoubaoVideo2 {
		return "doubao-video-2.0"
	}
	return ChannelName
}

func (a *TaskAdaptor) convertToRequestPayload(req *relaycommon.TaskSubmitReq) (*requestPayload, error) {
	r := requestPayload{
		Model:   req.Model,
		Content: []ContentItem{},
	}

	// Add images if present
	if req.HasImage() {
		for _, imgURL := range req.Images {
			r.Content = append(r.Content, ContentItem{
				Type: "image_url",
				ImageURL: &MediaURL{
					URL: imgURL,
				},
			})
		}
	}

	metadata := req.Metadata
	if err := taskcommon.UnmarshalMetadata(metadata, &r); err != nil {
		return nil, errors.Wrap(err, "unmarshal metadata failed")
	}

	if sec, _ := strconv.Atoi(req.Seconds); sec > 0 {
		r.Duration = lo.ToPtr(dto.IntValue(sec))
	}

	r.Content = lo.Reject(r.Content, func(c ContentItem, _ int) bool { return c.Type == "text" })
	r.Content = append(r.Content, ContentItem{
		Type: "text",
		Text: req.Prompt,
	})

	return &r, nil
}

func (a *TaskAdaptor) convertToZQBAPIOpenAIRequestPayload(req *relaycommon.TaskSubmitReq, originVideoURL string, frameReferences []zqbapiOpenAIFrameReference) (*requestPayload, error) {
	r := requestPayload{Model: req.Model, Content: []ContentItem{}}
	if err := taskcommon.UnmarshalMetadata(req.Metadata, &r); err != nil {
		return nil, errors.Wrap(err, "unmarshal metadata failed")
	}

	// Apply canonical protocol fields after metadata so metadata cannot silently
	// erase a declared reference, remix source, size, duration, or prompt.
	r.Content = lo.Reject(r.Content, func(c ContentItem, _ int) bool { return c.Type == "text" })
	if originVideoURL != "" {
		r.Content = append(r.Content, ContentItem{Type: "video_url", VideoURL: &MediaURL{URL: originVideoURL}, Role: "reference_video"})
	}
	for _, imageURL := range req.Images {
		if imageURL = strings.TrimSpace(imageURL); imageURL != "" {
			r.Content = append(r.Content, ContentItem{Type: "image_url", ImageURL: &MediaURL{URL: imageURL}})
		}
	}
	for _, reference := range frameReferences {
		if reference.URL = strings.TrimSpace(reference.URL); reference.URL != "" {
			r.Content = append(r.Content, ContentItem{
				Type:     "image_url",
				ImageURL: &MediaURL{URL: reference.URL},
				Role:     reference.Role,
			})
		}
	}
	if sec, _ := strconv.Atoi(req.Seconds); sec > 0 {
		r.Duration = lo.ToPtr(dto.IntValue(sec))
	}
	if req.Size != "" {
		resolution, ratio, ok := zqbapiOpenAISizeToProvider(req.Size)
		if !ok {
			return nil, fmt.Errorf("unsupported size %q", req.Size)
		}
		r.Resolution = lo.ToPtr(resolution)
		r.Ratio = lo.ToPtr(ratio)
	}
	r.Content = append(r.Content, ContentItem{Type: "text", Text: req.Prompt})
	return &r, nil
}

func countZQBAPIMedia(content []ContentItem) (images, videos int) {
	for _, item := range content {
		if item.ImageURL != nil && strings.TrimSpace(item.ImageURL.URL) != "" {
			images++
		}
		if item.VideoURL != nil && strings.TrimSpace(item.VideoURL.URL) != "" {
			videos++
		}
	}
	return images, videos
}

func (a *TaskAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	resTask := responseTask{}
	if err := common.Unmarshal(respBody, &resTask); err != nil {
		return nil, errors.Wrap(err, "unmarshal task result failed")
	}

	taskResult := relaycommon.TaskInfo{
		Code: 0,
	}
	if strings.TrimSpace(resTask.Status) == "" && strings.TrimSpace(resTask.Error.Message) != "" {
		taskResult.Status = model.TaskStatusFailure
		taskResult.Progress = "100%"
		taskResult.Reason = resTask.Error.Message
		return &taskResult, nil
	}

	// Map Doubao status to internal status
	switch resTask.Status {
	case "pending", "queued":
		taskResult.Status = model.TaskStatusQueued
		taskResult.Progress = "10%"
	case "processing", "running":
		taskResult.Status = model.TaskStatusInProgress
		taskResult.Progress = "50%"
	case "succeeded":
		taskResult.Status = model.TaskStatusSuccess
		taskResult.Progress = "100%"
		taskResult.Url = preferredDoubaoVideoURL(resTask)
		// 解析 usage 信息用于按倍率计费
		taskResult.CompletionTokens = resTask.Usage.CompletionTokens
		taskResult.TotalTokens = resTask.Usage.TotalTokens
		taskResult.Resolution = resTask.Resolution
	case "failed":
		taskResult.Status = model.TaskStatusFailure
		taskResult.Progress = "100%"
		taskResult.Reason = resTask.Error.Message
	case "expired", "cancelled", "canceled":
		if a.ChannelType == constant.ChannelTypeZQBAPI || a.ChannelType == constant.ChannelTypeDoubaoVideo2 {
			taskResult.Status = model.TaskStatusFailure
			taskResult.Progress = "100%"
			taskResult.Reason = resTask.Error.Message
			if strings.TrimSpace(taskResult.Reason) == "" {
				taskResult.Reason = fmt.Sprintf("upstream video task %s", resTask.Status)
			}
		} else {
			taskResult.Status = model.TaskStatusInProgress
			taskResult.Progress = "30%"
		}
	default:
		// Unknown status, treat as processing
		taskResult.Status = model.TaskStatusInProgress
		taskResult.Progress = "30%"
	}

	return &taskResult, nil
}

func (a *TaskAdaptor) ConvertToOpenAIVideo(originTask *model.Task) ([]byte, error) {
	var dResp responseTask
	if err := common.Unmarshal(originTask.Data, &dResp); err != nil {
		return nil, errors.Wrap(err, "unmarshal doubao task data failed")
	}

	openAIVideo := dto.NewOpenAIVideo()
	openAIVideo.ID = originTask.TaskID
	openAIVideo.TaskID = originTask.TaskID
	openAIVideo.Status = originTask.Status.ToVideoStatus()
	openAIVideo.SetProgressStr(originTask.Progress)
	openAIVideo.CreatedAt = originTask.CreatedAt
	openAIVideo.Model = originTask.Properties.OriginModelName

	isZQBAPI := originTask.Platform == constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeZQBAPI))
	isDoubaoVideo2 := originTask.Platform == constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeDoubaoVideo2))
	isZQBAPIOpenAI := isZQBAPI && originTask.Properties.OpenAIVideo
	isDoubaoVideo2OpenAI := isDoubaoVideo2 && originTask.Properties.OpenAIVideo
	isOpenAICompatible := isZQBAPIOpenAI || isDoubaoVideo2OpenAI
	if !isOpenAICompatible {
		openAIVideo.SetMetadata("url", preferredDoubaoVideoURL(dResp))
		if dResp.Content.KZVideoURL != "" {
			openAIVideo.SetMetadata("kz_video_url", dResp.Content.KZVideoURL)
		}
		if dResp.Content.LastFrameURL != "" {
			openAIVideo.SetMetadata("last_frame_url", dResp.Content.LastFrameURL)
		}
	}
	if isZQBAPI {
		if isZQBAPIOpenAI {
			openAIVideo.TaskID = ""
			openAIVideo.StandardFields = true
			openAIVideo.Prompt = originTask.Properties.VideoPrompt
		}
		if originTask.Status == model.TaskStatusSuccess || originTask.Status == model.TaskStatusFailure || originTask.Status == model.TaskStatusPendingSettlement {
			openAIVideo.CompletedAt = originTask.FinishTime
			if openAIVideo.CompletedAt == 0 {
				openAIVideo.CompletedAt = originTask.UpdatedAt
			}
		}
		if dResp.Duration > 0 {
			openAIVideo.Seconds = strconv.Itoa(dResp.Duration)
		} else {
			openAIVideo.Seconds = originTask.Properties.VideoSeconds
		}
		openAIVideo.Size = zqbapiProviderSizeToOpenAI(dResp.Resolution, dResp.Ratio)
		if openAIVideo.Size == "" {
			openAIVideo.Size = originTask.Properties.VideoSize
		}
		openAIVideo.RemixedFromVideoID = originTask.Properties.RemixedFromVideoID
	} else if isDoubaoVideo2OpenAI {
		openAIVideo.TaskID = ""
		openAIVideo.StandardFields = true
		openAIVideo.Prompt = originTask.Properties.VideoPrompt
		if originTask.Status == model.TaskStatusSuccess || originTask.Status == model.TaskStatusFailure || originTask.Status == model.TaskStatusPendingSettlement {
			openAIVideo.CompletedAt = originTask.FinishTime
			if openAIVideo.CompletedAt == 0 {
				openAIVideo.CompletedAt = originTask.UpdatedAt
			}
		}
		if dResp.Duration > 0 {
			openAIVideo.Seconds = strconv.Itoa(dResp.Duration)
		} else {
			openAIVideo.Seconds = originTask.Properties.VideoSeconds
		}
		openAIVideo.Size = doubaoVideo2ProviderSizeToOpenAI(dResp.Resolution, dResp.Ratio)
		if openAIVideo.Size == "" {
			openAIVideo.Size = originTask.Properties.VideoSize
		}
	} else {
		openAIVideo.CompletedAt = originTask.UpdatedAt
	}

	isFailedResponse := dResp.Status == "failed" || (originTask.Status == model.TaskStatusFailure && dResp.Error.Message != "")
	if isZQBAPIOpenAI && (dResp.Status == "expired" || dResp.Status == "cancelled" || dResp.Status == "canceled" || originTask.Status == model.TaskStatusFailure) {
		isFailedResponse = true
	}
	if isDoubaoVideo2OpenAI && originTask.Status == model.TaskStatusFailure {
		isFailedResponse = true
	}
	if isFailedResponse {
		message := strings.TrimSpace(dResp.Error.Message)
		if message == "" {
			message = strings.TrimSpace(originTask.FailReason)
		}
		if message == "" {
			message = "Video generation failed"
		}
		publicMessage, publicCode := zqbapiPublicVideoFailure(message)
		if isDoubaoVideo2OpenAI {
			publicMessage, publicCode = doubaoVideo2PublicVideoFailure(message)
		}
		openAIVideo.Error = &dto.OpenAIVideoError{
			Message: publicMessage,
			Code:    publicCode,
		}
	}

	return common.Marshal(openAIVideo)
}
