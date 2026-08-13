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

func (a *TaskAdaptor) prepareDoubaoVideo2Images(c *gin.Context, info *relaycommon.RelayInfo, body *requestPayload) error {
	if body == nil || info == nil || info.ChannelSetting.DoubaoVideo2 == nil {
		return nil
	}
	settings := info.ChannelSetting.DoubaoVideo2
	settings.Normalize()
	if err := settings.Validate(); err != nil {
		return &doubaoVideo2BuildError{Kind: doubaoVideo2ErrorMaterialConfig, Stage: "config", Err: err}
	}
	if settings.MaterialMode == dto.DoubaoVideo2MaterialModeOff {
		return nil
	}
	if settings.MaterialMode == dto.DoubaoVideo2MaterialModeRetryOnly {
		return storeDoubaoVideo2RetryPayload(c, body)
	}

	config, err := loadDoubaoVideo2MaterialConfig(info.ChannelId, a.apiKey, a.baseURL, info.ChannelSetting)
	if err != nil {
		return err
	}
	for index := range body.Content {
		item := &body.Content[index]
		if item.Type != "image_url" || item.ImageURL == nil {
			continue
		}
		source := strings.TrimSpace(item.ImageURL.URL)
		if source == "" || strings.HasPrefix(source, "asset://") {
			continue
		}
		publicURL := isPublicDoubaoVideo2MaterialURL(source)
		if settings.MaterialMode == dto.DoubaoVideo2MaterialModeAlways && !publicURL {
			return &doubaoVideo2BuildError{Kind: doubaoVideo2ErrorInvalidImage, Stage: "preflight", Err: fmt.Errorf("content[%d] must be a public HTTP(S) URL because the DoubaoVideo2.0 material API does not accept inline bytes", index)}
		}
		inspection, inspectErr := inspectDoubaoVideo2Image(c, source)
		if inspectErr != nil {
			var invalid *zqbapiBuildError
			if errors.As(inspectErr, &invalid) && invalid.Kind == zqbapiErrorInvalidImage {
				return &doubaoVideo2BuildError{Kind: doubaoVideo2ErrorInvalidImage, Stage: "inspect", Err: invalid.Err}
			}
			if settings.MaterialMode == dto.DoubaoVideo2MaterialModeAlways {
				return &doubaoVideo2BuildError{Kind: doubaoVideo2ErrorMaterialTransient, Stage: "inspect", Err: fmt.Errorf("content[%d] could not be inspected before mandatory material creation: %w", index, inspectErr)}
			}
			// Face detection is an optimization. Preserve a public URL so an
			// explicit upstream real-person rejection can still recover once.
			logger.LogWarn(c, fmt.Sprintf("DoubaoVideo2.0 local face detection skipped content[%d]: %v", index, inspectErr))
			continue
		}
		if settings.MaterialMode != dto.DoubaoVideo2MaterialModeAlways && !inspection.HasFace {
			continue
		}
		if !publicURL {
			return &doubaoVideo2BuildError{Kind: doubaoVideo2ErrorInvalidImage, Stage: "preflight", Err: fmt.Errorf("content[%d] contains a face but is not a public HTTP(S) URL; the DoubaoVideo2.0 material API cannot upload inline bytes", index)}
		}
		assetURL, prepareErr := prepareDoubaoVideo2Asset(c.Request.Context(), config, source, inspection)
		if prepareErr != nil {
			return fmt.Errorf("prepare DoubaoVideo2.0 material for content[%d]: %w", index, prepareErr)
		}
		item.ImageURL.URL = assetURL
	}
	return storeDoubaoVideo2RetryPayload(c, body)
}

func storeDoubaoVideo2RetryPayload(c *gin.Context, body *requestPayload) error {
	if c == nil || !doubaoVideo2RequestCanRetry(body) {
		return nil
	}
	payload, err := common.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal DoubaoVideo2.0 retry payload: %w", err)
	}
	c.Set(string(constant.ContextKeyDoubaoVideo2RetryPayload), payload)
	return nil
}

func doubaoVideo2RequestCanRetry(body *requestPayload) bool {
	if body == nil {
		return false
	}
	hasPublicImage := false
	for _, item := range body.Content {
		if item.Type != "image_url" || item.ImageURL == nil {
			continue
		}
		source := strings.TrimSpace(item.ImageURL.URL)
		if source == "" || strings.HasPrefix(source, "asset://") {
			continue
		}
		if !isPublicDoubaoVideo2MaterialURL(source) {
			return false
		}
		hasPublicImage = true
	}
	return hasPublicImage
}

func isPublicDoubaoVideo2MaterialURL(source string) bool {
	lower := strings.ToLower(strings.TrimSpace(source))
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
}
