package doubao

import (
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
)

func (a *TaskAdaptor) prepareZQBAPIImages(c *gin.Context, info *relaycommon.RelayInfo, body *requestPayload) error {
	if body == nil || info == nil {
		return nil
	}
	materialMode := dto.ZQBAPIMaterialModeFacePreflight
	autoNormalize := true
	if info.ChannelSetting.ZQBAPI != nil {
		info.ChannelSetting.ZQBAPI.Normalize()
		materialMode = info.ChannelSetting.ZQBAPI.MaterialMode
		if info.ChannelSetting.ZQBAPI.AutoNormalize != nil {
			autoNormalize = *info.ChannelSetting.ZQBAPI.AutoNormalize
		}
	}
	if materialMode == dto.ZQBAPIMaterialModeOff {
		return nil
	}
	if materialMode == dto.ZQBAPIMaterialModeRetryOnly {
		if zqbapiRequestCanRetry(body) {
			payload, marshalErr := common.Marshal(body)
			if marshalErr != nil {
				return fmt.Errorf("marshal ZQBAPI retry payload: %w", marshalErr)
			}
			c.Set(string(constant.ContextKeyZQBAPIRetryPayload), payload)
		}
		return nil
	}
	var materialConfig *zqbapiMaterialConfig
	for index := range body.Content {
		item := &body.Content[index]
		if item.Type != "image_url" || item.ImageURL == nil {
			continue
		}
		source := strings.TrimSpace(item.ImageURL.URL)
		if source == "" || strings.HasPrefix(source, "asset://") {
			continue
		}
		inspection, err := inspectZQBAPIImage(c, source)
		if err != nil {
			var buildErr *zqbapiBuildError
			if errors.As(err, &buildErr) && buildErr.Kind == zqbapiErrorInvalidImage {
				return fmt.Errorf("inspect ZQBAPI image content[%d]: %w", index, err)
			}
			// Detection is an optimization. Preserve the original URL and let the
			// provider be the final authority; its real-person error can retry once.
			logger.LogWarn(c, fmt.Sprintf("ZQBAPI local face detection skipped content[%d]: %v", index, err))
			continue
		}
		if inspection.Normalized && !autoNormalize {
			return newZQBAPIBuildError(zqbapiErrorInvalidImage, "normalize", fmt.Errorf("content[%d] requires image normalization but auto normalization is disabled", index))
		}
		if materialMode != dto.ZQBAPIMaterialModeAlways && !inspection.HasFace {
			continue
		}
		if materialConfig == nil {
			loaded, loadErr := loadZQBAPIMaterialConfig(info.ChannelSetting)
			if loadErr != nil {
				return loadErr
			}
			loaded.ChannelID = info.ChannelId
			materialConfig = &loaded
		}
		assetURL, err := prepareZQBAPIAsset(c.Request.Context(), *materialConfig, source, inspection)
		if err != nil {
			return fmt.Errorf("prepare ZQBAPI material for content[%d]: %w", index, err)
		}
		item.ImageURL.URL = assetURL
	}
	// Video references use the same approved material group and API credentials
	// as images. We do not attempt to decode video frames in the gateway; when
	// local preflight is desired, material_mode=always uploads the video before
	// submission. In face_preflight/retry_only modes the provider's real-person
	// response is handled by the one-time recovery path.
	if materialMode == dto.ZQBAPIMaterialModeAlways {
		for index := range body.Content {
			item := &body.Content[index]
			if item.Type != "video_url" || item.VideoURL == nil {
				continue
			}
			source := strings.TrimSpace(item.VideoURL.URL)
			if source == "" || strings.HasPrefix(source, "asset://") {
				continue
			}
			inspection, err := inspectZQBAPIVideo(c, source)
			if err != nil {
				return fmt.Errorf("inspect ZQBAPI video content[%d]: %w", index, err)
			}
			if materialConfig == nil {
				loaded, loadErr := loadZQBAPIMaterialConfig(info.ChannelSetting)
				if loadErr != nil {
					return loadErr
				}
				loaded.ChannelID = info.ChannelId
				materialConfig = &loaded
			}
			assetURL, err := prepareZQBAPIVideoAsset(c.Request.Context(), *materialConfig, source, inspection)
			if err != nil {
				return fmt.Errorf("prepare ZQBAPI video material for content[%d]: %w", index, err)
			}
			item.VideoURL.URL = assetURL
		}
	}

	// Retain a compact retry request only when every unresolved image/video is a
	// public URL. Inline media should have been externalized or detected
	// locally; it is deliberately not persisted in task private data.
	if zqbapiRequestCanRetry(body) {
		payload, err := common.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal ZQBAPI retry payload: %w", err)
		}
		c.Set(string(constant.ContextKeyZQBAPIRetryPayload), payload)
	}
	return nil
}

func zqbapiRequestCanRetry(body *requestPayload) bool {
	hasUnmaterializedMedia := false
	for _, item := range body.Content {
		var source string
		switch item.Type {
		case "image_url":
			if item.ImageURL == nil {
				continue
			}
			source = strings.TrimSpace(item.ImageURL.URL)
		case "video_url":
			if item.VideoURL == nil {
				continue
			}
			source = strings.TrimSpace(item.VideoURL.URL)
		default:
			continue
		}
		if source == "" || strings.HasPrefix(source, "asset://") {
			continue
		}
		if !strings.HasPrefix(source, "http://") && !strings.HasPrefix(source, "https://") {
			return false
		}
		hasUnmaterializedMedia = true
	}
	return hasUnmaterializedMedia
}
