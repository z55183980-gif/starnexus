package controller

import (
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

type doubaoVideo2PresignUploadRequest struct {
	ContentType    string `json:"content_type"`
	ContentLength  int64  `json:"content_length"`
	ChecksumSHA256 string `json:"checksum_sha256"`
}

type doubaoVideo2CompleteUploadRequest struct {
	ObjectID      string `json:"object_id"`
	CompleteToken string `json:"complete_token"`
}

func PresignDoubaoVideo2Upload(c *gin.Context) {
	var request doubaoVideo2PresignUploadRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{
			"message": "Invalid upload request.", "type": "invalid_request_error", "code": "invalid_upload_request",
		}})
		return
	}
	upload, err := service.CreateDoubaoVideo2DirectUpload(c.Request.Context(), c.GetInt("id"), request.ContentType, request.ContentLength, request.ChecksumSHA256)
	if err != nil {
		status := http.StatusBadRequest
		code := "invalid_upload_request"
		if !service.DoubaoVideo2R2Configured() {
			status = http.StatusServiceUnavailable
			code = "temporary_media_storage_unavailable"
		}
		c.JSON(status, gin.H{"error": gin.H{
			"message": sanitizePublicErrorMessage(err.Error()), "type": openAIVideoErrorType(status), "code": code,
		}})
		return
	}
	c.JSON(http.StatusOK, upload)
}

func CompleteDoubaoVideo2Upload(c *gin.Context) {
	var request doubaoVideo2CompleteUploadRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{
			"message": "Invalid upload completion request.", "type": "invalid_request_error", "code": "invalid_upload_request",
		}})
		return
	}
	completed, err := service.CompleteDoubaoVideo2DirectUpload(c.Request.Context(), c.GetInt("id"), request.ObjectID, request.CompleteToken)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{
			"message": sanitizePublicErrorMessage(err.Error()), "type": "invalid_request_error", "code": "invalid_upload_request",
		}})
		return
	}
	c.JSON(http.StatusOK, completed)
}

// UploadDoubaoVideo2Input accepts a small authenticated multipart upload and
// returns the temporary HTTPS URL needed by native Seedance video_url/audio_url
// content items. The URL is intentionally short-lived and private.
func UploadDoubaoVideo2Input(c *gin.Context) {
	fileHeader, err := c.FormFile("file")
	if err != nil || fileHeader == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{
			"message": "A multipart file field named file is required.", "type": "invalid_request_error", "code": "invalid_upload_request",
		}})
		return
	}
	if fileHeader.Size <= 0 || fileHeader.Size > service.DoubaoVideo2MaximumMediaSize {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": gin.H{
			"message": "The uploaded media is empty or exceeds the temporary storage limit.", "type": "invalid_request_error", "code": "invalid_upload_request",
		}})
		return
	}
	file, err := fileHeader.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{
			"message": "The uploaded media could not be opened.", "type": "invalid_request_error", "code": "invalid_upload_request",
		}})
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, service.DoubaoVideo2MaximumMediaSize+1))
	if err != nil || int64(len(data)) > service.DoubaoVideo2MaximumMediaSize {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": gin.H{
			"message": "The uploaded media exceeds the temporary storage limit.", "type": "invalid_request_error", "code": "invalid_upload_request",
		}})
		return
	}
	contentType := strings.TrimSpace(fileHeader.Header.Get("Content-Type"))
	if contentType == "" {
		contentType = mime.TypeByExtension(filepath.Ext(fileHeader.Filename))
	}
	completed, err := service.StoreDoubaoVideo2Media(c.Request.Context(), data, contentType)
	if err != nil {
		status := http.StatusBadRequest
		code := "invalid_upload_request"
		if !service.DoubaoVideo2R2Configured() {
			status = http.StatusServiceUnavailable
			code = "temporary_media_storage_unavailable"
		}
		c.JSON(status, gin.H{"error": gin.H{
			"message": sanitizePublicErrorMessage(err.Error()), "type": openAIVideoErrorType(status), "code": code,
		}})
		return
	}
	c.JSON(http.StatusOK, completed)
}
