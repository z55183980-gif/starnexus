package controller

import (
	"net/http"

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
