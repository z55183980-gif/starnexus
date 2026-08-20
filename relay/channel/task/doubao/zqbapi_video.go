package doubao

import (
	"encoding/base64"
	"fmt"
	"mime"
	"path/filepath"
	"strings"

	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

const (
	// Keep this aligned with the gateway's temporary media limit. The material
	// API accepts the same public video inputs, and rejecting oversized files
	// before UploadAssetFile gives callers a deterministic error.
	zqbapiVideoMaxBytes        = 64 << 20
	zqbapiVideoNormalizationV1 = "video-v1"
)

type zqbapiVideoInspection struct {
	Data                 []byte
	MIMEType             string
	Size                 int
	NormalizationVersion string
}

// inspectZQBAPIVideo loads a public URL or inline data URL once so the same
// bytes can be sent to UploadAssetFile. Unlike image preflight, video face
// detection is intentionally not attempted here: the provider's moderation
// result remains authoritative, while material_mode=always can opt into
// pre-uploading every reference video.
func inspectZQBAPIVideo(c *gin.Context, input string) (*zqbapiVideoInspection, error) {
	source := types.NewFileSourceFromData(input, "video/mp4")
	fileData, err := service.LoadFileSource(c, source, "ZQBAPI video material upload")
	if err != nil {
		return nil, fmt.Errorf("load video for material upload: %w", err)
	}
	if c == nil {
		defer fileData.Close()
	}
	encoded, err := fileData.GetBase64Data()
	if err != nil {
		return nil, fmt.Errorf("read video for material upload: %w", err)
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("decode video for material upload: %w", err)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("video data is empty")
	}
	if len(data) > zqbapiVideoMaxBytes {
		return nil, fmt.Errorf("video size %d bytes exceeds the %d-byte material limit", len(data), zqbapiVideoMaxBytes)
	}
	mimeType := strings.ToLower(strings.TrimSpace(strings.SplitN(fileData.MimeType, ";", 2)[0]))
	if mimeType == "" || mimeType == "application/octet-stream" {
		if extensionType := mime.TypeByExtension(strings.ToLower(filepath.Ext(input))); strings.HasPrefix(extensionType, "video/") {
			mimeType = extensionType
		}
	}
	if !strings.HasPrefix(mimeType, "video/") {
		return nil, fmt.Errorf("unsupported video MIME type %q", mimeType)
	}
	return &zqbapiVideoInspection{
		Data:                 data,
		MIMEType:             mimeType,
		Size:                 len(data),
		NormalizationVersion: zqbapiVideoNormalizationV1,
	}, nil
}
