package doubao

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
)

func (a *TaskAdaptor) prepareZQBAPIImages(c *gin.Context, info *relaycommon.RelayInfo, body *requestPayload) error {
	if body == nil || info == nil {
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
			// Detection is an optimization. Preserve the original URL and let the
			// provider be the final authority; its real-person error can retry once.
			logger.LogWarn(c, fmt.Sprintf("ZQBAPI local face detection skipped content[%d]: %v", index, err))
			continue
		}
		if !inspection.HasFace {
			continue
		}
		if materialConfig == nil {
			loaded, err := loadZQBAPIMaterialConfig(info.ChannelSetting.Proxy)
			if err != nil {
				return err
			}
			materialConfig = &loaded
		}
		assetURL, err := prepareZQBAPIAsset(c.Request.Context(), *materialConfig, source, inspection)
		if err != nil {
			return fmt.Errorf("prepare ZQBAPI material for content[%d]: %w", index, err)
		}
		item.ImageURL.URL = assetURL
	}

	// Retain a compact retry request only when every unresolved image is a
	// public URL. Inline base64 images should have been detected locally; they
	// are deliberately not persisted in task private data.
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
	hasUnmaterializedImage := false
	for _, item := range body.Content {
		if item.Type != "image_url" || item.ImageURL == nil {
			continue
		}
		source := strings.TrimSpace(item.ImageURL.URL)
		if source == "" || strings.HasPrefix(source, "asset://") {
			continue
		}
		if !strings.HasPrefix(source, "http://") && !strings.HasPrefix(source, "https://") {
			return false
		}
		hasUnmaterializedImage = true
	}
	return hasUnmaterializedImage
}
