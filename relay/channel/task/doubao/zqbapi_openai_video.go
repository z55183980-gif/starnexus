package doubao

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

const zqbapiOpenAIVideoContextKey = "zqbapi_openai_video_context"

const (
	zqbapiOpenAIDefaultSeconds = 4
	zqbapiOpenAIDefaultSize    = "720x1280"
)

type zqbapiOpenAIFrameReference struct {
	URL  string
	Role string
}

type zqbapiOpenAIVideoContext struct {
	ReferenceDeclared bool
	ReferenceSource   string
	ReferenceCount    int
	FrameReferences   []zqbapiOpenAIFrameReference
	Seconds           string
	Size              string
	RemixedFromID     string
}

type zqbapiOpenAIVideoRequest struct {
	Prompt         string          `json:"prompt"`
	Model          string          `json:"model,omitempty"`
	Mode           string          `json:"mode,omitempty"`
	Image          string          `json:"image,omitempty"`
	Images         []string        `json:"images,omitempty"`
	Size           string          `json:"size,omitempty"`
	Seconds        json.RawMessage `json:"seconds,omitempty"`
	Duration       json.RawMessage `json:"duration,omitempty"`
	InputReference json.RawMessage `json:"input_reference,omitempty"`
	FirstFrame     json.RawMessage `json:"first_frame,omitempty"`
	LastFrame      json.RawMessage `json:"last_frame,omitempty"`
	Metadata       json.RawMessage `json:"metadata,omitempty"`
}

type zqbapiOpenAIReferenceObject struct {
	ImageURL *string `json:"image_url,omitempty"`
	FileID   *string `json:"file_id,omitempty"`
}

func isZQBAPIOpenAIVideoRequest(c *gin.Context) bool {
	if c == nil || c.Request == nil {
		return false
	}
	path := strings.TrimSuffix(c.Request.URL.Path, "/")
	return path == "/v1/videos" || (strings.HasPrefix(path, "/v1/videos/") && strings.HasSuffix(path, "/remix"))
}

func validateZQBAPIOpenAIVideoRequest(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	if info == nil {
		return service.TaskErrorWrapperLocal(fmt.Errorf("relay info is required"), "invalid_request", http.StatusBadRequest)
	}
	if info.TaskRelayInfo == nil {
		info.TaskRelayInfo = &relaycommon.TaskRelayInfo{}
	}
	c.Set(string(constant.ContextKeyZQBAPIOpenAIVideoRequest), true)
	var body zqbapiOpenAIVideoRequest
	if err := common.UnmarshalBodyReusable(c, &body); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}
	if field, err := unsupportedZQBAPIOpenAIMediaField(c); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	} else if field != "" {
		return service.TaskErrorWrapperLocal(fmt.Errorf("field %q is not supported by the OpenAI Videos compatibility endpoint", field), "unsupported_input", http.StatusBadRequest)
	}

	req := relaycommon.TaskSubmitReq{
		Prompt: body.Prompt,
		Model:  body.Model,
		Mode:   body.Mode,
		Image:  strings.TrimSpace(body.Image),
		Size:   strings.TrimSpace(body.Size),
	}
	for _, source := range body.Images {
		if source = strings.TrimSpace(source); source != "" {
			req.Images = append(req.Images, source)
		}
	}
	if req.Image != "" {
		req.Images = append(req.Images, req.Image)
	}

	metadata, err := parseZQBAPIOpenAIMetadata(body.Metadata)
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_metadata", http.StatusBadRequest)
	}
	req.Metadata = metadata

	seconds, secondsSet, err := parseZQBAPIOpenAIPositiveInt(body.Seconds, "seconds")
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_seconds", http.StatusBadRequest)
	}
	if secondsSet && seconds != 4 && seconds != 8 && seconds != 12 {
		return service.TaskErrorWrapperLocal(fmt.Errorf("seconds must be one of 4, 8, or 12"), "invalid_seconds", http.StatusBadRequest)
	}
	if !secondsSet {
		seconds, secondsSet, err = parseZQBAPIOpenAIPositiveInt(body.Duration, "duration")
		if err != nil {
			return service.TaskErrorWrapperLocal(err, "invalid_duration", http.StatusBadRequest)
		}
		if secondsSet && (seconds < 4 || seconds > 15) {
			return service.TaskErrorWrapperLocal(fmt.Errorf("duration must be between 4 and 15 seconds for this video model"), "invalid_duration", http.StatusBadRequest)
		}
	}

	ctx := zqbapiOpenAIVideoContext{
		ReferenceDeclared: len(req.Images) > 0,
		ReferenceCount:    len(req.Images),
		Seconds:           req.Seconds,
		Size:              req.Size,
	}
	if ctx.ReferenceDeclared {
		ctx.ReferenceSource = "image"
	}

	referenceURL, referenceDeclared, err := parseZQBAPIOpenAIReference(body.InputReference, "input_reference")
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_input_reference", http.StatusBadRequest)
	}
	if referenceDeclared {
		ctx.ReferenceDeclared = true
		ctx.ReferenceSource = "input_reference"
		if referenceURL != "" {
			ctx.FrameReferences = append(ctx.FrameReferences, zqbapiOpenAIFrameReference{URL: referenceURL, Role: "first_frame"})
			ctx.ReferenceCount++
		}
	}
	firstFrameURL, firstFrameDeclared, err := parseZQBAPIOpenAIReference(body.FirstFrame, "first_frame")
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_first_frame", http.StatusBadRequest)
	}
	lastFrameURL, lastFrameDeclared, err := parseZQBAPIOpenAIReference(body.LastFrame, "last_frame")
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_last_frame", http.StatusBadRequest)
	}

	if strings.HasPrefix(strings.ToLower(c.GetHeader("Content-Type")), "multipart/form-data") {
		fileReferences, fileErr := extractZQBAPIOpenAIReferenceFiles(c)
		if fileErr != nil {
			return service.TaskErrorWrapperLocal(fileErr, "invalid_input_reference", http.StatusBadRequest)
		}
		if fileReference := fileReferences["input_reference"]; fileReference != "" {
			if referenceDeclared {
				return service.TaskErrorWrapperLocal(fmt.Errorf("input_reference must be provided only once"), "invalid_input_reference", http.StatusBadRequest)
			}
			referenceDeclared = true
			ctx.ReferenceDeclared = true
			ctx.ReferenceSource = "multipart"
			ctx.FrameReferences = append(ctx.FrameReferences, zqbapiOpenAIFrameReference{URL: fileReference, Role: "first_frame"})
		}
		if fileReference := fileReferences["first_frame"]; fileReference != "" {
			if firstFrameDeclared {
				return service.TaskErrorWrapperLocal(fmt.Errorf("first_frame must be provided only once"), "invalid_first_frame", http.StatusBadRequest)
			}
			firstFrameDeclared = true
			firstFrameURL = fileReference
		}
		if fileReference := fileReferences["last_frame"]; fileReference != "" {
			if lastFrameDeclared {
				return service.TaskErrorWrapperLocal(fmt.Errorf("last_frame must be provided only once"), "invalid_last_frame", http.StatusBadRequest)
			}
			lastFrameDeclared = true
			lastFrameURL = fileReference
		}
	}
	if referenceDeclared && firstFrameDeclared {
		return service.TaskErrorWrapperLocal(fmt.Errorf("input_reference and first_frame cannot be used together"), "invalid_first_frame", http.StatusBadRequest)
	}
	if lastFrameDeclared && !firstFrameDeclared {
		return service.TaskErrorWrapperLocal(fmt.Errorf("last_frame requires first_frame"), "invalid_last_frame", http.StatusBadRequest)
	}
	if (firstFrameDeclared || lastFrameDeclared) && len(req.Images) > 0 {
		return service.TaskErrorWrapperLocal(fmt.Errorf("first_frame/last_frame cannot be mixed with image or images"), "invalid_first_frame", http.StatusBadRequest)
	}
	if firstFrameDeclared {
		ctx.ReferenceDeclared = true
		ctx.ReferenceSource = "first_last_frame"
		ctx.FrameReferences = append(ctx.FrameReferences, zqbapiOpenAIFrameReference{URL: firstFrameURL, Role: "first_frame"})
	}
	if lastFrameDeclared {
		ctx.FrameReferences = append(ctx.FrameReferences, zqbapiOpenAIFrameReference{URL: lastFrameURL, Role: "last_frame"})
	}
	req.Images = deduplicateZQBAPIVideoSources(req.Images)
	ctx.ReferenceCount = len(req.Images) + len(ctx.FrameReferences)

	if strings.TrimSpace(req.Prompt) == "" {
		return service.TaskErrorWrapperLocal(fmt.Errorf("prompt is required"), "invalid_request", http.StatusBadRequest)
	}

	action := constant.TaskActionGenerate
	if info.Action == constant.TaskActionRemix {
		action = constant.TaskActionRemix
		ctx.RemixedFromID = info.OriginTaskID
		if ctx.ReferenceDeclared {
			return service.TaskErrorWrapperLocal(fmt.Errorf("input_reference is not supported for remix"), "invalid_input_reference", http.StatusBadRequest)
		}
		originSeconds, originSize, originErr := zqbapiOpenAIRemixParameters(info.UserId, info.OriginTaskID)
		if originErr != nil {
			return service.TaskErrorWrapperLocal(originErr, "invalid_video", http.StatusBadRequest)
		}
		if !secondsSet {
			seconds, secondsSet = originSeconds, originSeconds > 0
		}
		if req.Size == "" {
			req.Size = originSize
		}
	}
	if !secondsSet {
		seconds = zqbapiOpenAIDefaultSeconds
	}
	req.Seconds = strconv.Itoa(seconds)
	req.Duration = seconds
	if req.Size == "" {
		req.Size = zqbapiOpenAIDefaultSize
	}
	if _, _, ok := zqbapiOpenAISizeToProvider(req.Size); !ok {
		return service.TaskErrorWrapperLocal(fmt.Errorf("unsupported size %q", req.Size), "unsupported_size", http.StatusBadRequest)
	}
	effectiveModel := strings.TrimSpace(info.UpstreamModelName)
	if effectiveModel == "" {
		effectiveModel = strings.TrimSpace(req.Model)
	}
	if effectiveModel == "" {
		effectiveModel = strings.TrimSpace(info.OriginModelName)
	}
	if isZQBAPILimitedResolutionModel(effectiveModel) && isZQBAPIOpenAIHighResolution(req.Size) {
		return service.TaskErrorWrapperLocal(fmt.Errorf("size %q is not supported by video model %q", req.Size, effectiveModel), "unsupported_size", http.StatusBadRequest)
	}
	if lastFrameDeclared && !supportsZQBAPIFirstLastFrames(effectiveModel) {
		return service.TaskErrorWrapperLocal(fmt.Errorf("first_frame/last_frame is not supported by video model %q", effectiveModel), "unsupported_input", http.StatusBadRequest)
	}
	ctx.Seconds = req.Seconds
	ctx.Size = req.Size

	info.Action = action
	c.Set("task_request", req)
	c.Set(zqbapiOpenAIVideoContextKey, ctx)
	return nil
}

func unsupportedZQBAPIOpenAIMediaField(c *gin.Context) (string, error) {
	var fields map[string]json.RawMessage
	if err := common.UnmarshalBodyReusable(c, &fields); err != nil {
		return "", err
	}
	for _, field := range []string{"content", "image_url", "reference_image", "reference_images", "video", "videos", "audio", "audios"} {
		if _, exists := fields[field]; exists {
			return field, nil
		}
	}
	return "", nil
}

func parseZQBAPIOpenAIMetadata(raw json.RawMessage) (map[string]interface{}, error) {
	metadata := make(map[string]interface{})
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return metadata, nil
	}
	var encoded string
	if err := common.Unmarshal(trimmed, &encoded); err == nil {
		if strings.TrimSpace(encoded) == "" {
			return metadata, nil
		}
		if err := common.Unmarshal([]byte(encoded), &metadata); err != nil {
			return nil, fmt.Errorf("metadata must be a JSON object: %w", err)
		}
		return metadata, nil
	}
	if err := common.Unmarshal(trimmed, &metadata); err != nil {
		return nil, fmt.Errorf("metadata must be a JSON object: %w", err)
	}
	return metadata, nil
}

func parseZQBAPIOpenAIPositiveInt(raw json.RawMessage, field string) (int, bool, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return 0, false, nil
	}
	var value int
	if err := common.Unmarshal(trimmed, &value); err != nil {
		var stringValue string
		if stringErr := common.Unmarshal(trimmed, &stringValue); stringErr != nil {
			return 0, true, fmt.Errorf("%s must be a positive integer", field)
		}
		parsed, parseErr := strconv.Atoi(strings.TrimSpace(stringValue))
		if parseErr != nil {
			return 0, true, fmt.Errorf("%s must be a positive integer", field)
		}
		value = parsed
	}
	if value <= 0 {
		return 0, true, fmt.Errorf("%s must be a positive integer", field)
	}
	return value, true, nil
}

func parseZQBAPIOpenAIReference(raw json.RawMessage, field string) (string, bool, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return "", false, nil
	}
	var legacy string
	if err := common.Unmarshal(trimmed, &legacy); err == nil {
		legacy = strings.TrimSpace(legacy)
		if legacy == "" {
			return "", true, fmt.Errorf("%s cannot be empty", field)
		}
		return legacy, true, nil
	}
	var object zqbapiOpenAIReferenceObject
	if err := common.Unmarshal(trimmed, &object); err != nil {
		return "", true, fmt.Errorf("%s must contain image_url or file_id", field)
	}
	imageURL := ""
	fileID := ""
	if object.ImageURL != nil {
		imageURL = strings.TrimSpace(*object.ImageURL)
	}
	if object.FileID != nil {
		fileID = strings.TrimSpace(*object.FileID)
	}
	if imageURL != "" && fileID != "" {
		return "", true, fmt.Errorf("%s must contain exactly one of image_url or file_id", field)
	}
	if fileID != "" {
		return "", true, fmt.Errorf("%s.file_id is not supported by this video endpoint", field)
	}
	if imageURL == "" {
		return "", true, fmt.Errorf("%s.image_url is required", field)
	}
	return imageURL, true, nil
}

func extractZQBAPIOpenAIReferenceFiles(c *gin.Context) (map[string]string, error) {
	form, err := common.ParseMultipartFormReusable(c)
	if err != nil {
		return nil, err
	}
	defer form.RemoveAll()
	result := make(map[string]string)
	for _, field := range []string{"input_reference", "first_frame", "last_frame"} {
		files := form.File[field]
		if len(files) == 0 {
			continue
		}
		if len(files) != 1 {
			return nil, fmt.Errorf("only one %s file is supported", field)
		}
		value, fileErr := encodeZQBAPIOpenAIReferenceFile(files[0], field)
		if fileErr != nil {
			return nil, fileErr
		}
		result[field] = value
	}
	return result, nil
}

func encodeZQBAPIOpenAIReferenceFile(fileHeader *multipart.FileHeader, field string) (string, error) {
	if fileHeader.Size <= 0 || fileHeader.Size > zqbapiImageMaxBytes {
		return "", fmt.Errorf("%s file must be between 1 byte and %d MB", field, zqbapiImageMaxBytes/(1024*1024))
	}
	file, err := fileHeader.Open()
	if err != nil {
		return "", fmt.Errorf("open %s file: %w", field, err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, zqbapiImageMaxBytes+1))
	if err != nil {
		return "", fmt.Errorf("read %s file: %w", field, err)
	}
	if len(data) == 0 || len(data) > zqbapiImageMaxBytes {
		return "", fmt.Errorf("%s file must be between 1 byte and %d MB", field, zqbapiImageMaxBytes/(1024*1024))
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("%s is not a supported image: %w", field, err)
	}
	// Dimension, aspect-ratio and decoded-pixel validation is intentionally
	// centralized in inspectZQBAPIImage. This makes multipart files follow the
	// same resize/normalization path as URL and data-URL references.
	if config.Width <= 0 || config.Height <= 0 {
		return "", fmt.Errorf("%s dimensions are invalid", field)
	}
	mimeType := fileHeader.Header.Get("Content-Type")
	if mimeType == "" || mimeType == "application/octet-stream" {
		mimeType = http.DetectContentType(data)
	}
	switch strings.ToLower(format) {
	case "jpeg":
		mimeType = "image/jpeg"
	case "png", "gif", "webp":
		mimeType = "image/" + strings.ToLower(format)
	default:
		return "", fmt.Errorf("unsupported %s image format %q", field, format)
	}
	return "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}

func deduplicateZQBAPIVideoSources(sources []string) []string {
	seen := make(map[string]struct{}, len(sources))
	result := make([]string, 0, len(sources))
	for _, source := range sources {
		source = strings.TrimSpace(source)
		if source == "" {
			continue
		}
		if _, exists := seen[source]; exists {
			continue
		}
		seen[source] = struct{}{}
		result = append(result, source)
	}
	return result
}

func zqbapiOpenAISizeToProvider(size string) (resolution, ratio string, ok bool) {
	switch strings.ToLower(strings.TrimSpace(size)) {
	case "1280x720":
		return "720p", "16:9", true
	case "720x1280":
		return "720p", "9:16", true
	case "1792x1024":
		return "1080p", "16:9", true
	case "1024x1792":
		return "1080p", "9:16", true
	default:
		return "", "", false
	}
}

func zqbapiProviderSizeToOpenAI(resolution, ratio string) string {
	resolution = strings.ToLower(strings.TrimSpace(resolution))
	ratio = strings.TrimSpace(ratio)
	switch {
	case resolution == "720p" && ratio == "9:16":
		return "720x1280"
	case resolution == "720p":
		return "1280x720"
	case resolution == "1080p" && ratio == "9:16":
		return "1024x1792"
	case resolution == "1080p":
		return "1792x1024"
	default:
		return ""
	}
}

func isZQBAPIOpenAIHighResolution(size string) bool {
	size = strings.ToLower(strings.TrimSpace(size))
	return size == "1792x1024" || size == "1024x1792"
}

func isZQBAPILimitedResolutionModel(modelName string) bool {
	modelName = strings.ToLower(strings.TrimSpace(modelName))
	return strings.Contains(modelName, "seedance-2-0-fast") || strings.Contains(modelName, "seedance-2-0-mini")
}

func supportsZQBAPIFirstLastFrames(modelName string) bool {
	modelName = strings.ToLower(strings.TrimSpace(modelName))
	return strings.Contains(modelName, "seedance-2-0") ||
		strings.Contains(modelName, "seedance-1-5-pro") ||
		strings.Contains(modelName, "seedance-1-0-pro")
}

func zqbapiOpenAIRemixParameters(userID int, taskID string) (int, string, error) {
	originTask, exists, err := model.GetByTaskId(userID, taskID)
	if err != nil {
		return 0, "", fmt.Errorf("get remix origin task: %w", err)
	}
	if !exists || originTask == nil || originTask.Platform != constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeZQBAPI)) || !originTask.Properties.OpenAIVideo {
		return 0, "", fmt.Errorf("remix origin must be an OpenAI Videos video")
	}
	seconds, _ := strconv.Atoi(strings.TrimSpace(originTask.Properties.VideoSeconds))
	size := strings.TrimSpace(originTask.Properties.VideoSize)
	if seconds > 0 && size != "" {
		return seconds, size, nil
	}
	var provider responseTask
	if len(originTask.Data) > 0 {
		_ = common.Unmarshal(originTask.Data, &provider)
	}
	if seconds <= 0 {
		seconds = provider.Duration
	}
	if size == "" {
		size = zqbapiProviderSizeToOpenAI(provider.Resolution, provider.Ratio)
	}
	if seconds <= 0 {
		seconds = zqbapiOpenAIDefaultSeconds
	}
	if size == "" {
		size = zqbapiOpenAIDefaultSize
	}
	return seconds, size, nil
}

func getZQBAPIOpenAIVideoContext(c *gin.Context) (zqbapiOpenAIVideoContext, bool) {
	if c == nil {
		return zqbapiOpenAIVideoContext{}, false
	}
	value, exists := c.Get(zqbapiOpenAIVideoContextKey)
	if !exists {
		return zqbapiOpenAIVideoContext{}, false
	}
	ctx, ok := value.(zqbapiOpenAIVideoContext)
	return ctx, ok
}

func zqbapiPublicVideoFailure(message string) (string, string) {
	lowerMessage := strings.ToLower(message)
	switch {
	case strings.Contains(lowerMessage, "real person"):
		return "The input image may contain a real person and could not be processed.", "reference_image_person_not_supported"
	case strings.Contains(lowerMessage, "3840") || strings.Contains(lowerMessage, "resolution") || strings.Contains(lowerMessage, "volume"):
		return "The reference image exceeds the supported dimensions. Resize it and try again.", "reference_image_dimensions_invalid"
	case strings.Contains(lowerMessage, "resource download failed") || strings.Contains(lowerMessage, "image_url"):
		return "The reference image could not be downloaded. Use a stable public URL or upload the file directly.", "reference_image_download_failed"
	case strings.Contains(lowerMessage, "safety") || strings.Contains(lowerMessage, "sensitive") || strings.Contains(lowerMessage, "policy"):
		return "The video request could not be completed because of a content policy restriction.", "content_policy_violation"
	default:
		return "Video generation failed.", "video_generation_failed"
	}
}

// ApplyZQBAPIOpenAIVideoTaskProperties persists only non-sensitive compatibility
// metadata. It is called only for ChannelTypeZQBAPI submissions.
func ApplyZQBAPIOpenAIVideoTaskProperties(c *gin.Context, task *model.Task) {
	if task == nil {
		return
	}
	ctx, ok := getZQBAPIOpenAIVideoContext(c)
	if !ok {
		return
	}
	task.Properties.OpenAIVideo = true
	if req, err := relaycommon.GetTaskRequest(c); err == nil {
		task.Properties.VideoPrompt = req.Prompt
	}
	task.Properties.VideoSeconds = ctx.Seconds
	task.Properties.VideoSize = ctx.Size
	task.Properties.RemixedFromVideoID = ctx.RemixedFromID
}
