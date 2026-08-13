package doubao

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
	_ "golang.org/x/image/webp"
)

const (
	doubaoVideo2OpenAIContextKey     = "doubao_video2_openai_video_context"
	doubaoVideo2OpenAIDefaultSeconds = 4
	doubaoVideo2OpenAIDefaultSize    = "720x1280"
	doubaoVideo2InputFileMaxBytes    = 20 << 20
)

type doubaoVideo2OpenAIVideoContext struct {
	Seconds string
	Size    string
}

func isDoubaoVideo2OpenAIVideoRequest(c *gin.Context) bool {
	if c == nil || c.Request == nil {
		return false
	}
	return strings.TrimSuffix(c.Request.URL.Path, "/") == "/v1/videos"
}

func setDoubaoVideo2OpenAIVideoContext(c *gin.Context, req *relaycommon.TaskSubmitReq) error {
	if c == nil || req == nil || !isDoubaoVideo2OpenAIVideoRequest(c) {
		return nil
	}
	if strings.TrimSpace(req.Seconds) == "" {
		req.Seconds = strconv.Itoa(doubaoVideo2OpenAIDefaultSeconds)
		req.Duration = doubaoVideo2OpenAIDefaultSeconds
		req.Metadata["duration"] = doubaoVideo2OpenAIDefaultSeconds
	}
	if strings.TrimSpace(req.Size) == "" {
		resolution, _ := req.Metadata["resolution"].(string)
		ratio, _ := req.Metadata["ratio"].(string)
		if strings.TrimSpace(resolution) == "" && strings.TrimSpace(ratio) == "" {
			req.Size = doubaoVideo2OpenAIDefaultSize
			if err := applyDoubaoVideo2Size(req.Size, req.Metadata); err != nil {
				return err
			}
		} else {
			req.Size = doubaoVideo2ProviderSizeToOpenAI(resolution, ratio)
		}
	}
	c.Set(doubaoVideo2OpenAIContextKey, doubaoVideo2OpenAIVideoContext{
		Seconds: req.Seconds,
		Size:    req.Size,
	})
	c.Set(string(constant.ContextKeyOpenAIVideoRequest), true)
	return nil
}

func getDoubaoVideo2OpenAIVideoContext(c *gin.Context) (doubaoVideo2OpenAIVideoContext, bool) {
	if c == nil {
		return doubaoVideo2OpenAIVideoContext{}, false
	}
	value, exists := c.Get(doubaoVideo2OpenAIContextKey)
	if !exists {
		return doubaoVideo2OpenAIVideoContext{}, false
	}
	ctx, ok := value.(doubaoVideo2OpenAIVideoContext)
	return ctx, ok
}

// extractDoubaoVideo2InputReferenceFile converts a multipart image directly to
// a data URL for the provider request. It intentionally does not create or
// upload any material-library asset.
func extractDoubaoVideo2InputReferenceFile(c *gin.Context) (string, bool, error) {
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
		return "", true, fmt.Errorf("only one input_reference file is supported")
	}
	dataURL, err := encodeDoubaoVideo2InputReferenceFile(files[0])
	return dataURL, true, err
}

func encodeDoubaoVideo2InputReferenceFile(fileHeader *multipart.FileHeader) (string, error) {
	if fileHeader == nil || fileHeader.Size <= 0 || fileHeader.Size > doubaoVideo2InputFileMaxBytes {
		return "", fmt.Errorf("input_reference file must be between 1 byte and %d MB", doubaoVideo2InputFileMaxBytes/(1<<20))
	}
	file, err := fileHeader.Open()
	if err != nil {
		return "", fmt.Errorf("open input_reference file: %w", err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, doubaoVideo2InputFileMaxBytes+1))
	if err != nil {
		return "", fmt.Errorf("read input_reference file: %w", err)
	}
	if len(data) == 0 || len(data) > doubaoVideo2InputFileMaxBytes {
		return "", fmt.Errorf("input_reference file must be between 1 byte and %d MB", doubaoVideo2InputFileMaxBytes/(1<<20))
	}
	if _, _, err := image.DecodeConfig(bytes.NewReader(data)); err != nil {
		return "", fmt.Errorf("input_reference is not a supported image: %w", err)
	}
	mimeType := http.DetectContentType(data)
	switch mimeType {
	case "image/jpeg", "image/png", "image/gif", "image/webp":
	default:
		return "", fmt.Errorf("unsupported input_reference content type %q", mimeType)
	}
	return "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}

func doubaoVideo2ProviderSizeToOpenAI(resolution, ratio string) string {
	resolution = strings.ToLower(strings.TrimSpace(resolution))
	ratio = strings.TrimSpace(ratio)
	switch resolution + "/" + ratio {
	case "720p/9:16":
		return "720x1280"
	case "720p/16:9":
		return "1280x720"
	case "1080p/9:16":
		return "1024x1792"
	case "1080p/16:9":
		return "1792x1024"
	default:
		return ""
	}
}

// ApplyDoubaoVideo2OpenAIVideoTaskProperties persists only the compatibility
// fields needed to reconstruct a stable OpenAI video response.
func ApplyDoubaoVideo2OpenAIVideoTaskProperties(c *gin.Context, task *model.Task) {
	if task == nil {
		return
	}
	ctx, ok := getDoubaoVideo2OpenAIVideoContext(c)
	if !ok {
		return
	}
	task.Properties.OpenAIVideo = true
	if req, err := relaycommon.GetTaskRequest(c); err == nil {
		task.Properties.VideoPrompt = req.Prompt
	}
	task.Properties.VideoSeconds = ctx.Seconds
	task.Properties.VideoSize = ctx.Size
}

func doubaoVideo2PublicVideoFailure(message string) (string, string) {
	lower := strings.ToLower(message)
	switch {
	case strings.Contains(lower, "download") || strings.Contains(lower, "image_url"):
		return "The reference image could not be downloaded. Use a stable public URL or upload the file directly.", "reference_image_download_failed"
	case strings.Contains(lower, "safety") || strings.Contains(lower, "sensitive") || strings.Contains(lower, "policy"):
		return "The video request could not be completed because of a content policy restriction.", "content_policy_violation"
	default:
		return "Video generation failed.", "video_generation_failed"
	}
}
