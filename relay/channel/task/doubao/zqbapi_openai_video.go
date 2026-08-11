package doubao

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"io"
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

type zqbapiOpenAIVideoContext struct {
	ReferenceDeclared bool
	ReferenceSource   string
	ReferenceCount    int
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
	var body zqbapiOpenAIVideoRequest
	if err := common.UnmarshalBodyReusable(c, &body); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
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
	if !secondsSet {
		seconds, secondsSet, err = parseZQBAPIOpenAIPositiveInt(body.Duration, "duration")
		if err != nil {
			return service.TaskErrorWrapperLocal(err, "invalid_duration", http.StatusBadRequest)
		}
	}
	if secondsSet {
		req.Seconds = strconv.Itoa(seconds)
		req.Duration = seconds
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

	referenceURL, referenceDeclared, err := parseZQBAPIOpenAIReference(body.InputReference)
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_input_reference", http.StatusBadRequest)
	}
	if referenceDeclared {
		ctx.ReferenceDeclared = true
		ctx.ReferenceSource = "input_reference"
		if referenceURL != "" {
			req.Images = append(req.Images, referenceURL)
			ctx.ReferenceCount++
		}
	}

	if strings.HasPrefix(strings.ToLower(c.GetHeader("Content-Type")), "multipart/form-data") {
		fileReference, fileDeclared, fileErr := extractZQBAPIOpenAIReferenceFile(c)
		if fileErr != nil {
			return service.TaskErrorWrapperLocal(fileErr, "invalid_input_reference", http.StatusBadRequest)
		}
		if fileDeclared {
			if referenceDeclared {
				return service.TaskErrorWrapperLocal(fmt.Errorf("input_reference must be provided only once"), "invalid_input_reference", http.StatusBadRequest)
			}
			ctx.ReferenceDeclared = true
			ctx.ReferenceSource = "multipart"
			ctx.ReferenceCount++
			req.Images = append(req.Images, fileReference)
		}
	}
	req.Images = deduplicateZQBAPIVideoSources(req.Images)
	ctx.ReferenceCount = len(req.Images)

	if strings.TrimSpace(req.Prompt) == "" {
		return service.TaskErrorWrapperLocal(fmt.Errorf("prompt is required"), "invalid_request", http.StatusBadRequest)
	}
	if req.Size != "" {
		if _, _, ok := zqbapiOpenAISizeToProvider(req.Size); !ok {
			return service.TaskErrorWrapperLocal(fmt.Errorf("unsupported size %q for ZQBAPI", req.Size), "unsupported_size", http.StatusBadRequest)
		}
	}

	action := constant.TaskActionGenerate
	if info.Action == constant.TaskActionRemix {
		action = constant.TaskActionRemix
		ctx.RemixedFromID = info.OriginTaskID
		if ctx.ReferenceDeclared {
			return service.TaskErrorWrapperLocal(fmt.Errorf("input_reference is not supported for remix"), "invalid_input_reference", http.StatusBadRequest)
		}
	}

	info.Action = action
	c.Set("task_request", req)
	c.Set(zqbapiOpenAIVideoContextKey, ctx)
	return nil
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

func parseZQBAPIOpenAIReference(raw json.RawMessage) (string, bool, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return "", false, nil
	}
	var legacy string
	if err := common.Unmarshal(trimmed, &legacy); err == nil {
		legacy = strings.TrimSpace(legacy)
		if legacy == "" {
			return "", true, fmt.Errorf("input_reference cannot be empty")
		}
		return legacy, true, nil
	}
	var object zqbapiOpenAIReferenceObject
	if err := common.Unmarshal(trimmed, &object); err != nil {
		return "", true, fmt.Errorf("input_reference must contain image_url or file_id")
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
		return "", true, fmt.Errorf("input_reference must contain exactly one of image_url or file_id")
	}
	if fileID != "" {
		return "", true, fmt.Errorf("input_reference.file_id is not supported by ZQBAPI")
	}
	if imageURL == "" {
		return "", true, fmt.Errorf("input_reference.image_url is required")
	}
	return imageURL, true, nil
}

func extractZQBAPIOpenAIReferenceFile(c *gin.Context) (string, bool, error) {
	form, err := common.ParseMultipartFormReusable(c)
	if err != nil {
		return "", false, err
	}
	defer form.RemoveAll()
	files := form.File["input_reference"]
	if len(files) == 0 {
		return "", false, nil
	}
	if len(files) != 1 {
		return "", true, fmt.Errorf("ZQBAPI supports one input_reference file")
	}
	fileHeader := files[0]
	if fileHeader.Size <= 0 || fileHeader.Size > zqbapiImageMaxBytes {
		return "", true, fmt.Errorf("input_reference file must be between 1 byte and %d MB", zqbapiImageMaxBytes/(1024*1024))
	}
	file, err := fileHeader.Open()
	if err != nil {
		return "", true, fmt.Errorf("open input_reference file: %w", err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, zqbapiImageMaxBytes+1))
	if err != nil {
		return "", true, fmt.Errorf("read input_reference file: %w", err)
	}
	if len(data) == 0 || len(data) > zqbapiImageMaxBytes {
		return "", true, fmt.Errorf("input_reference file must be between 1 byte and %d MB", zqbapiImageMaxBytes/(1024*1024))
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return "", true, fmt.Errorf("input_reference is not a supported image: %w", err)
	}
	if config.Width < zqbapiImageMinDimension || config.Height < zqbapiImageMinDimension ||
		config.Width >= zqbapiImageMaxDimension || config.Height >= zqbapiImageMaxDimension {
		return "", true, fmt.Errorf("input_reference dimensions must be between %d and %d pixels", zqbapiImageMinDimension, zqbapiImageMaxDimension-1)
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
		return "", true, fmt.Errorf("unsupported input_reference image format %q", format)
	}
	return "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(data), true, nil
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
	task.Properties.VideoSeconds = ctx.Seconds
	task.Properties.VideoSize = ctx.Size
	task.Properties.RemixedFromVideoID = ctx.RemixedFromID
}
