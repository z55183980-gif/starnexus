package doubao

import (
	"fmt"
	"net/http"
	"strings"

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

	resolution := strings.ToLower(strings.TrimSpace(payload.Resolution))
	if resolution != "" && !limits.resolutions[resolution] {
		return doubaoVideo2RequestError(fmt.Errorf("resolution %q is not supported by model %s", payload.Resolution, modelName), "unsupported_resolution")
	}
	if payload.Duration != nil {
		duration := int(*payload.Duration)
		if duration != -1 && (duration < 4 || duration > limits.maxDuration) {
			return doubaoVideo2RequestError(fmt.Errorf("duration must be 4-%d or -1 for model %s, got %d", limits.maxDuration, modelName, duration), "unsupported_duration")
		}
	}
	if payload.Ratio != "" && !isDoubaoVideo2Ratio(payload.Ratio) {
		return doubaoVideo2RequestError(fmt.Errorf("unsupported ratio %q", payload.Ratio), "unsupported_ratio")
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

	images, videos, audios, hasFirstOrLastFrame := countDoubaoVideo2Content(payload.Content)
	if images > limits.maxImages || videos > limits.maxVideos || audios > limits.maxAudios {
		return doubaoVideo2RequestError(fmt.Errorf("reference limits exceeded for model %s: images=%d/%d videos=%d/%d audios=%d/%d", modelName, images, limits.maxImages, videos, limits.maxVideos, audios, limits.maxAudios), "too_many_references")
	}
	if audios > 0 && modelName != "doubao-seedance-2-5-260628" && images == 0 && videos == 0 {
		return doubaoVideo2RequestError(fmt.Errorf("audio references require an image or video for model %s", modelName), "unsupported_audio_reference")
	}
	if modelName == "doubao-seedance-2-5-260628" && hasFirstOrLastFrame && payload.Ratio != "" && payload.Ratio != "adaptive" {
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

func preferredDoubaoVideoURL(task responseTask) string {
	if task.Content.KZVideoURL != "" {
		return task.Content.KZVideoURL
	}
	return task.Content.VideoURL
}
