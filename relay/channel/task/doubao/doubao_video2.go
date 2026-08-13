package doubao

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

type doubaoVideo2ModelLimits struct {
	resolutions map[string]bool
	maxDuration int
	maxImages   int
	maxVideos   int
	maxAudios   int
}

// doubaoVideo2SubmitRequest accepts both the gateway's task format and the
// OpenAI Videos compatibility fields used by downstream clients. The request
// is normalized into relaycommon.TaskSubmitReq before the ordinary adaptor
// builds the provider-native content array.
type doubaoVideo2SubmitRequest struct {
	Prompt         *string         `json:"prompt,omitempty"`
	Model          *string         `json:"model,omitempty"`
	Mode           *string         `json:"mode,omitempty"`
	Image          *string         `json:"image,omitempty"`
	Images         []string        `json:"images,omitempty"`
	Size           *string         `json:"size,omitempty"`
	Seconds        json.RawMessage `json:"seconds,omitempty"`
	Duration       json.RawMessage `json:"duration,omitempty"`
	InputReference json.RawMessage `json:"input_reference,omitempty"`
	Content        json.RawMessage `json:"content,omitempty"`
	Metadata       json.RawMessage `json:"metadata,omitempty"`
}

type doubaoVideo2ReferenceObject struct {
	ImageURL json.RawMessage `json:"image_url,omitempty"`
	URL      *string         `json:"url,omitempty"`
	FileID   *string         `json:"file_id,omitempty"`
}

func (a *TaskAdaptor) validateAndSetDoubaoVideo2Request(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	if info == nil {
		return doubaoVideo2RequestError(fmt.Errorf("relay info is required"), "invalid_request")
	}
	if c == nil || c.Request == nil || c.Request.Method != http.MethodPost {
		return doubaoVideo2RequestError(fmt.Errorf("DoubaoVideo2.0 generation requires POST"), "invalid_request")
	}
	path := strings.TrimSuffix(c.Request.URL.Path, "/")
	if path != "/v1/videos" && path != "/v1/video/generations" {
		return doubaoVideo2RequestError(fmt.Errorf("unsupported DoubaoVideo2.0 task path %q", c.Request.URL.Path), "invalid_request")
	}
	if info.TaskRelayInfo == nil {
		info.TaskRelayInfo = &relaycommon.TaskRelayInfo{}
	}
	req, err := parseDoubaoVideo2SubmitRequest(c)
	if err != nil {
		return doubaoVideo2RequestError(err, "invalid_request")
	}
	if strings.TrimSpace(req.Prompt) == "" {
		return doubaoVideo2RequestError(fmt.Errorf("prompt or content text is required"), "invalid_request")
	}
	if err := setDoubaoVideo2OpenAIVideoContext(c, &req); err != nil {
		return doubaoVideo2RequestError(err, "invalid_request")
	}
	info.Action = constant.TaskActionGenerate
	c.Set("task_request", req)
	return a.validateDoubaoVideo2Request(c, info)
}

func parseDoubaoVideo2SubmitRequest(c *gin.Context) (relaycommon.TaskSubmitReq, error) {
	var body doubaoVideo2SubmitRequest
	if err := common.UnmarshalBodyReusable(c, &body); err != nil {
		return relaycommon.TaskSubmitReq{}, err
	}

	metadata, err := parseDoubaoVideo2Metadata(body.Metadata)
	if err != nil {
		return relaycommon.TaskSubmitReq{}, err
	}
	if err := mergeDoubaoVideo2TopLevelOptions(c, metadata); err != nil {
		return relaycommon.TaskSubmitReq{}, err
	}

	req := relaycommon.TaskSubmitReq{
		Prompt:   doubaoVideo2OptionalString(body.Prompt),
		Model:    doubaoVideo2OptionalString(body.Model),
		Mode:     doubaoVideo2OptionalString(body.Mode),
		Image:    doubaoVideo2OptionalString(body.Image),
		Size:     doubaoVideo2OptionalString(body.Size),
		Metadata: metadata,
	}
	for _, source := range body.Images {
		if source = strings.TrimSpace(source); source != "" {
			req.Images = append(req.Images, source)
		}
	}
	if req.Image != "" {
		req.Images = append(req.Images, req.Image)
	}
	req.Images = deduplicateDoubaoVideo2Sources(req.Images)

	seconds, secondsSet, err := parseDoubaoVideo2Integer(body.Seconds, "seconds")
	if err != nil {
		return relaycommon.TaskSubmitReq{}, err
	}
	duration, durationSet, err := parseDoubaoVideo2Integer(body.Duration, "duration")
	if err != nil {
		return relaycommon.TaskSubmitReq{}, err
	}
	if secondsSet && durationSet && seconds != duration {
		return relaycommon.TaskSubmitReq{}, fmt.Errorf("seconds and duration must match when both are provided")
	}
	if secondsSet {
		req.Seconds = strconv.Itoa(seconds)
		req.Duration = seconds
		metadata["duration"] = seconds
	} else if durationSet {
		req.Seconds = strconv.Itoa(duration)
		req.Duration = duration
		metadata["duration"] = duration
	}

	content, contentDeclared, contentText, err := parseDoubaoVideo2Content(body.Content)
	if err != nil {
		return relaycommon.TaskSubmitReq{}, err
	}
	if req.Prompt != "" && contentText != "" {
		return relaycommon.TaskSubmitReq{}, fmt.Errorf("prompt and content text cannot be used together")
	}
	if req.Prompt == "" {
		req.Prompt = contentText
	}

	referenceURL, referenceDeclared, err := parseDoubaoVideo2InputReference(body.InputReference)
	if err != nil {
		return relaycommon.TaskSubmitReq{}, err
	}
	if strings.HasPrefix(strings.ToLower(c.GetHeader("Content-Type")), "multipart/form-data") {
		fileReference, fileDeclared, fileErr := extractDoubaoVideo2InputReferenceFile(c)
		if fileErr != nil {
			return relaycommon.TaskSubmitReq{}, fileErr
		}
		if fileDeclared {
			if referenceDeclared {
				return relaycommon.TaskSubmitReq{}, fmt.Errorf("input_reference must be provided only once")
			}
			referenceURL = fileReference
			referenceDeclared = true
		}
	}
	_, metadataContentDeclared := metadata["content"]
	if contentDeclared && metadataContentDeclared {
		return relaycommon.TaskSubmitReq{}, fmt.Errorf("top-level content and metadata.content cannot be used together")
	}
	if (contentDeclared || metadataContentDeclared) && referenceDeclared {
		return relaycommon.TaskSubmitReq{}, fmt.Errorf("content and input_reference cannot be used together")
	}
	if (contentDeclared || metadataContentDeclared) && len(req.Images) > 0 {
		return relaycommon.TaskSubmitReq{}, fmt.Errorf("content cannot be mixed with image or images")
	}
	if referenceDeclared && len(req.Images) > 0 {
		return relaycommon.TaskSubmitReq{}, fmt.Errorf("input_reference cannot be mixed with image or images")
	}
	if referenceDeclared {
		content = []ContentItem{{
			Type:     "image_url",
			ImageURL: &MediaURL{URL: referenceURL},
			Role:     "first_frame",
		}}
	}
	if len(content) > 0 {
		metadata["content"] = doubaoVideo2ContentAsAny(content)
	}
	if err := applyDoubaoVideo2Size(req.Size, metadata); err != nil {
		return relaycommon.TaskSubmitReq{}, err
	}
	return req, nil
}

func mergeDoubaoVideo2TopLevelOptions(c *gin.Context, metadata map[string]interface{}) error {
	var fields map[string]json.RawMessage
	if err := common.UnmarshalBodyReusable(c, &fields); err != nil {
		return err
	}
	for _, field := range []string{
		"callback_url", "return_last_frame", "service_tier", "execution_expires_after",
		"generate_audio", "draft", "tools", "resolution", "ratio", "frames", "seed",
		"camera_fixed", "watermark", "moderation_options", "safety_identifier",
		"bitrate_mode", "output_format",
	} {
		raw, exists := fields[field]
		if !exists {
			continue
		}
		var value interface{}
		if err := common.Unmarshal(raw, &value); err != nil {
			return fmt.Errorf("%s is invalid: %w", field, err)
		}
		metadata[field] = value
	}
	return nil
}

func parseDoubaoVideo2Metadata(raw json.RawMessage) (map[string]interface{}, error) {
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
		if err := common.UnmarshalJsonStr(encoded, &metadata); err != nil {
			return nil, fmt.Errorf("metadata must be a JSON object or encoded JSON object: %w", err)
		}
		return metadata, nil
	}

	if err := common.Unmarshal(trimmed, &metadata); err != nil {
		return nil, fmt.Errorf("metadata must be a JSON object or encoded JSON object: %w", err)
	}
	return metadata, nil
}

func deduplicateDoubaoVideo2Sources(sources []string) []string {
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

func doubaoVideo2ContentAsAny(content []ContentItem) []interface{} {
	result := make([]interface{}, 0, len(content))
	for _, item := range content {
		itemMap := map[string]interface{}{"type": item.Type}
		if item.Role != "" {
			itemMap["role"] = item.Role
		}
		if item.ImageURL != nil {
			itemMap["image_url"] = map[string]interface{}{"url": item.ImageURL.URL}
		}
		if item.VideoURL != nil {
			itemMap["video_url"] = map[string]interface{}{"url": item.VideoURL.URL}
		}
		if item.AudioURL != nil {
			itemMap["audio_url"] = map[string]interface{}{"url": item.AudioURL.URL}
		}
		result = append(result, itemMap)
	}
	return result
}

func parseDoubaoVideo2Integer(raw json.RawMessage, field string) (int, bool, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return 0, false, nil
	}
	var value int
	if err := common.Unmarshal(trimmed, &value); err != nil {
		var encoded string
		if stringErr := common.Unmarshal(trimmed, &encoded); stringErr != nil {
			return 0, true, fmt.Errorf("%s must be an integer", field)
		}
		parsed, parseErr := strconv.Atoi(strings.TrimSpace(encoded))
		if parseErr != nil {
			return 0, true, fmt.Errorf("%s must be an integer", field)
		}
		value = parsed
	}
	return value, true, nil
}

func parseDoubaoVideo2Content(raw json.RawMessage) ([]ContentItem, bool, string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, false, "", nil
	}
	var items []ContentItem
	if err := common.Unmarshal(trimmed, &items); err != nil {
		return nil, true, "", fmt.Errorf("content must be an array of provider content items: %w", err)
	}
	media := make([]ContentItem, 0, len(items))
	texts := make([]string, 0, 1)
	for index, item := range items {
		item.Type = strings.TrimSpace(item.Type)
		item.Role = strings.TrimSpace(item.Role)
		switch item.Type {
		case "text":
			text := strings.TrimSpace(item.Text)
			if text == "" {
				return nil, true, "", fmt.Errorf("content[%d].text cannot be empty", index)
			}
			if len(texts) > 0 {
				return nil, true, "", fmt.Errorf("content must contain at most one text item")
			}
			if item.Role != "" {
				return nil, true, "", fmt.Errorf("content[%d].role is not supported for text", index)
			}
			texts = append(texts, text)
		case "image_url":
			if item.ImageURL == nil || strings.TrimSpace(item.ImageURL.URL) == "" {
				return nil, true, "", fmt.Errorf("content[%d].image_url.url is required", index)
			}
			item.ImageURL.URL = strings.TrimSpace(item.ImageURL.URL)
			if !isDoubaoVideo2ImageRole(item.Role) {
				return nil, true, "", fmt.Errorf("content[%d].role %q is not supported for image_url", index, item.Role)
			}
			media = append(media, item)
		case "video_url":
			if item.VideoURL == nil || strings.TrimSpace(item.VideoURL.URL) == "" {
				return nil, true, "", fmt.Errorf("content[%d].video_url.url is required", index)
			}
			item.VideoURL.URL = strings.TrimSpace(item.VideoURL.URL)
			if item.Role != "" && item.Role != "reference_video" {
				return nil, true, "", fmt.Errorf("content[%d].role %q is not supported for video_url", index, item.Role)
			}
			media = append(media, item)
		case "audio_url":
			if item.AudioURL == nil || strings.TrimSpace(item.AudioURL.URL) == "" {
				return nil, true, "", fmt.Errorf("content[%d].audio_url.url is required", index)
			}
			item.AudioURL.URL = strings.TrimSpace(item.AudioURL.URL)
			if item.Role != "" && item.Role != "reference_audio" {
				return nil, true, "", fmt.Errorf("content[%d].role %q is not supported for audio_url", index, item.Role)
			}
			media = append(media, item)
		default:
			return nil, true, "", fmt.Errorf("content[%d].type %q is not supported", index, item.Type)
		}
	}
	return media, true, strings.Join(texts, "\n"), nil
}

func isDoubaoVideo2ImageRole(role string) bool {
	switch role {
	case "", "first_frame", "last_frame", "reference_image":
		return true
	default:
		return false
	}
}

func parseDoubaoVideo2InputReference(raw json.RawMessage) (string, bool, error) {
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
	var object doubaoVideo2ReferenceObject
	if err := common.Unmarshal(trimmed, &object); err != nil {
		return "", true, fmt.Errorf("input_reference must be a URL string or image reference object")
	}
	if doubaoVideo2OptionalString(object.FileID) != "" {
		return "", true, fmt.Errorf("input_reference.file_id is not supported by DoubaoVideo2.0")
	}
	url := doubaoVideo2OptionalString(object.URL)
	if len(bytes.TrimSpace(object.ImageURL)) > 0 && !bytes.Equal(bytes.TrimSpace(object.ImageURL), []byte("null")) {
		if url != "" {
			return "", true, fmt.Errorf("input_reference must contain only one URL")
		}
		if err := common.Unmarshal(object.ImageURL, &url); err != nil {
			var media MediaURL
			if mediaErr := common.Unmarshal(object.ImageURL, &media); mediaErr != nil {
				return "", true, fmt.Errorf("input_reference.image_url must be a URL string or object")
			}
			url = media.URL
		}
	}
	url = strings.TrimSpace(url)
	if url == "" {
		return "", true, fmt.Errorf("input_reference.image_url is required")
	}
	return url, true, nil
}

func doubaoVideo2OptionalString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func applyDoubaoVideo2Size(size string, metadata map[string]interface{}) error {
	if strings.TrimSpace(size) == "" {
		return nil
	}
	resolution, ratio, ok := doubaoVideo2SizeToProvider(size)
	if !ok {
		return fmt.Errorf("unsupported size %q", size)
	}
	if existing, exists := metadata["resolution"]; exists {
		if value, ok := existing.(string); !ok || !strings.EqualFold(strings.TrimSpace(value), resolution) {
			return fmt.Errorf("size %q conflicts with resolution", size)
		}
	} else {
		metadata["resolution"] = resolution
	}
	if existing, exists := metadata["ratio"]; exists {
		if value, ok := existing.(string); !ok || strings.TrimSpace(value) != ratio {
			return fmt.Errorf("size %q conflicts with ratio", size)
		}
	} else {
		metadata["ratio"] = ratio
	}
	return nil
}

func doubaoVideo2SizeToProvider(size string) (string, string, bool) {
	switch strings.ToLower(strings.TrimSpace(size)) {
	case "854x480":
		return "480p", "16:9", true
	case "480x854":
		return "480p", "9:16", true
	case "1280x720":
		return "720p", "16:9", true
	case "720x1280":
		return "720p", "9:16", true
	case "1792x1024", "1920x1080":
		return "1080p", "16:9", true
	case "1024x1792", "1080x1920":
		return "1080p", "9:16", true
	case "3840x2160":
		return "4k", "16:9", true
	case "2160x3840":
		return "4k", "9:16", true
	default:
		return "", "", false
	}
}

var doubaoVideo2Limits = map[string]doubaoVideo2ModelLimits{
	"doubao-seedance-2-0-260128": {
		resolutions: map[string]bool{"480p": true, "720p": true, "1080p": true, "4k": true},
		maxDuration: 15, maxImages: 9, maxVideos: 3, maxAudios: 3,
	},
	"doubao-seedance-2-0-fast-260128": {
		resolutions: map[string]bool{"480p": true, "720p": true, "1080p": true},
		maxDuration: 15, maxImages: 9, maxVideos: 3, maxAudios: 3,
	},
	"doubao-seedance-2-0-mini-260615": {
		resolutions: map[string]bool{"480p": true, "720p": true, "1080p": true},
		maxDuration: 15, maxImages: 9, maxVideos: 3, maxAudios: 3,
	},
	"doubao-seedance-2-5-260628": {
		resolutions: map[string]bool{"480p": true, "720p": true},
		maxDuration: 30, maxImages: 30, maxVideos: 10, maxAudios: 10,
	},
}

func (a *TaskAdaptor) validateDoubaoVideo2Request(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return doubaoVideo2RequestError(err, "invalid_request")
	}
	payload, err := a.convertToRequestPayload(&req)
	if err != nil {
		return doubaoVideo2RequestError(err, "invalid_request")
	}

	modelName := strings.TrimSpace(info.OriginModelName)
	if modelName == "" {
		modelName = strings.TrimSpace(req.Model)
	}
	limits, ok := doubaoVideo2Limits[modelName]
	if !ok {
		return doubaoVideo2RequestError(fmt.Errorf("unsupported DoubaoVideo2.0 model %q", modelName), "unsupported_model")
	}

	resolution := strings.ToLower(strings.TrimSpace(doubaoVideo2OptionalString(payload.Resolution)))
	if resolution != "" && !limits.resolutions[resolution] {
		return doubaoVideo2RequestError(fmt.Errorf("resolution %q is not supported by model %s", doubaoVideo2OptionalString(payload.Resolution), modelName), "unsupported_resolution")
	}
	if payload.Duration != nil {
		duration := int(*payload.Duration)
		if duration != -1 && (duration < 4 || duration > limits.maxDuration) {
			return doubaoVideo2RequestError(fmt.Errorf("duration must be 4-%d or -1 for model %s, got %d", limits.maxDuration, modelName, duration), "unsupported_duration")
		}
	}
	ratio := doubaoVideo2OptionalString(payload.Ratio)
	if ratio != "" && !isDoubaoVideo2Ratio(ratio) {
		return doubaoVideo2RequestError(fmt.Errorf("unsupported ratio %q", ratio), "unsupported_ratio")
	}
	if payload.BitrateMode != nil && *payload.BitrateMode != "" && *payload.BitrateMode != "standard" {
		return doubaoVideo2RequestError(fmt.Errorf("unsupported bitrate_mode %q", *payload.BitrateMode), "unsupported_bitrate_mode")
	}
	if payload.OutputFormat != nil && *payload.OutputFormat != "" && *payload.OutputFormat != "mp4" {
		return doubaoVideo2RequestError(fmt.Errorf("unsupported output_format %q", *payload.OutputFormat), "unsupported_output_format")
	}
	if payload.SafetyIdentifier != nil && len([]rune(*payload.SafetyIdentifier)) > 64 {
		return doubaoVideo2RequestError(fmt.Errorf("safety_identifier must not exceed 64 characters"), "invalid_safety_identifier")
	}
	if payload.ModerationOptions != nil && len(payload.ModerationOptions.IPs) > 5 {
		return doubaoVideo2RequestError(fmt.Errorf("moderation_options.ips must contain at most 5 items"), "invalid_moderation_options")
	}

	if err := validateDoubaoVideo2ContentItems(payload.Content); err != nil {
		return doubaoVideo2RequestError(err, "invalid_content")
	}
	images, videos, audios, hasFirstOrLastFrame := countDoubaoVideo2Content(payload.Content)
	if err := validateDoubaoVideo2FrameRoles(payload.Content); err != nil {
		return doubaoVideo2RequestError(err, "invalid_reference_role")
	}
	if images > limits.maxImages || videos > limits.maxVideos || audios > limits.maxAudios {
		return doubaoVideo2RequestError(fmt.Errorf("reference limits exceeded for model %s: images=%d/%d videos=%d/%d audios=%d/%d", modelName, images, limits.maxImages, videos, limits.maxVideos, audios, limits.maxAudios), "too_many_references")
	}
	if audios > 0 && modelName != "doubao-seedance-2-5-260628" && images == 0 && videos == 0 {
		return doubaoVideo2RequestError(fmt.Errorf("audio references require an image or video for model %s", modelName), "unsupported_audio_reference")
	}
	if modelName == "doubao-seedance-2-5-260628" && hasFirstOrLastFrame && ratio != "" && ratio != "adaptive" {
		return doubaoVideo2RequestError(fmt.Errorf("Seedance 2.5 first/last-frame tasks require ratio=adaptive"), "unsupported_ratio")
	}
	return nil
}

func doubaoVideo2RequestError(err error, code string) *dto.TaskError {
	return service.TaskErrorWrapperLocal(err, code, http.StatusBadRequest)
}

func isDoubaoVideo2Ratio(ratio string) bool {
	switch strings.TrimSpace(ratio) {
	case "16:9", "4:3", "1:1", "3:4", "9:16", "21:9", "adaptive":
		return true
	default:
		return false
	}
}

func countDoubaoVideo2Content(content []ContentItem) (images, videos, audios int, hasFirstOrLastFrame bool) {
	for _, item := range content {
		if item.ImageURL != nil && strings.TrimSpace(item.ImageURL.URL) != "" {
			images++
		}
		if item.VideoURL != nil && strings.TrimSpace(item.VideoURL.URL) != "" {
			videos++
		}
		if item.AudioURL != nil && strings.TrimSpace(item.AudioURL.URL) != "" {
			audios++
		}
		if item.Role == "first_frame" || item.Role == "last_frame" {
			hasFirstOrLastFrame = true
		}
	}
	return
}

func validateDoubaoVideo2ContentItems(content []ContentItem) error {
	for index, item := range content {
		switch strings.TrimSpace(item.Type) {
		case "text":
			if strings.TrimSpace(item.Text) == "" {
				return fmt.Errorf("content[%d].text cannot be empty", index)
			}
			if strings.TrimSpace(item.Role) != "" {
				return fmt.Errorf("content[%d].role is not supported for text", index)
			}
		case "image_url":
			if item.ImageURL == nil || strings.TrimSpace(item.ImageURL.URL) == "" {
				return fmt.Errorf("content[%d].image_url.url is required", index)
			}
			if !isDoubaoVideo2ImageRole(strings.TrimSpace(item.Role)) {
				return fmt.Errorf("content[%d].role %q is not supported for image_url", index, item.Role)
			}
		case "video_url":
			if item.VideoURL == nil || strings.TrimSpace(item.VideoURL.URL) == "" {
				return fmt.Errorf("content[%d].video_url.url is required", index)
			}
			if role := strings.TrimSpace(item.Role); role != "" && role != "reference_video" {
				return fmt.Errorf("content[%d].role %q is not supported for video_url", index, item.Role)
			}
		case "audio_url":
			if item.AudioURL == nil || strings.TrimSpace(item.AudioURL.URL) == "" {
				return fmt.Errorf("content[%d].audio_url.url is required", index)
			}
			if role := strings.TrimSpace(item.Role); role != "" && role != "reference_audio" {
				return fmt.Errorf("content[%d].role %q is not supported for audio_url", index, item.Role)
			}
		default:
			return fmt.Errorf("content[%d].type %q is not supported", index, item.Type)
		}
	}
	return nil
}

func validateDoubaoVideo2FrameRoles(content []ContentItem) error {
	firstFrames := 0
	lastFrames := 0
	for _, item := range content {
		switch strings.TrimSpace(item.Role) {
		case "first_frame":
			firstFrames++
		case "last_frame":
			lastFrames++
		}
	}
	if firstFrames > 1 {
		return fmt.Errorf("content must contain at most one first_frame")
	}
	if lastFrames > 1 {
		return fmt.Errorf("content must contain at most one last_frame")
	}
	if lastFrames == 1 && firstFrames == 0 {
		return fmt.Errorf("last_frame requires a first_frame")
	}
	return nil
}

func preferredDoubaoVideoURL(task responseTask) string {
	if task.Content.KZVideoURL != "" {
		return task.Content.KZVideoURL
	}
	return task.Content.VideoURL
}
