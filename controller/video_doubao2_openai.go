package controller

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

type doubaoVideo2OpenAIVideoList struct {
	Object  string             `json:"object"`
	Data    []*dto.OpenAIVideo `json:"data"`
	FirstID string             `json:"first_id,omitempty"`
	LastID  string             `json:"last_id,omitempty"`
	HasMore bool               `json:"has_more"`
}

func doubaoVideo2Platform() constant.TaskPlatform {
	return constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeDoubaoVideo2))
}

func doubaoVideo2OpenAIVideoError(c *gin.Context, status int, code, param, message string) {
	writeOpenAIVideoError(c, status, code, param, message)
}

// ListDoubaoVideo2OpenAIVideos exposes only compatibility tasks created via
// /v1/videos. Native DoubaoVideo2.0 tasks remain isolated from this collection.
func ListDoubaoVideo2OpenAIVideos(c *gin.Context) {
	limit := 20
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			doubaoVideo2OpenAIVideoError(c, http.StatusBadRequest, "invalid_limit", "limit", "limit must be between 1 and 100")
			return
		}
		limit = parsed
	}

	order := strings.ToLower(strings.TrimSpace(c.DefaultQuery("order", "desc")))
	if order != "asc" && order != "desc" {
		doubaoVideo2OpenAIVideoError(c, http.StatusBadRequest, "invalid_order", "order", "order must be asc or desc")
		return
	}

	userID := c.GetInt("id")
	afterID := int64(0)
	if after := strings.TrimSpace(c.Query("after")); after != "" {
		cursor, exists, err := model.GetByTaskId(userID, after)
		if err != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to resolve DoubaoVideo2.0 video cursor: %v", err))
			doubaoVideo2OpenAIVideoError(c, http.StatusInternalServerError, "server_error", "after", "Failed to list videos")
			return
		}
		if !exists || cursor == nil || cursor.Platform != doubaoVideo2Platform() || !cursor.Properties.OpenAIVideo {
			doubaoVideo2OpenAIVideoError(c, http.StatusBadRequest, "invalid_after", "after", "after must identify one of your videos")
			return
		}
		afterID = cursor.ID
	}

	tasks, hasMore, err := model.ListUserOpenAIVideoTasksByPlatformCursor(userID, doubaoVideo2Platform(), afterID, limit, order == "asc")
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to list DoubaoVideo2.0 videos: %v", err))
		doubaoVideo2OpenAIVideoError(c, http.StatusInternalServerError, "server_error", "", "Failed to list videos")
		return
	}

	data := make([]*dto.OpenAIVideo, 0, len(tasks))
	for _, task := range tasks {
		video, convertErr := convertDoubaoVideo2TaskToOpenAIVideo(task)
		if convertErr != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to convert DoubaoVideo2.0 video %s: %v", task.TaskID, convertErr))
			doubaoVideo2OpenAIVideoError(c, http.StatusInternalServerError, "server_error", "", "Failed to list videos")
			return
		}
		data = append(data, video)
	}

	response := doubaoVideo2OpenAIVideoList{Object: "list", Data: data, HasMore: hasMore}
	if len(data) > 0 {
		response.FirstID = data[0].ID
		response.LastID = data[len(data)-1].ID
	}
	c.JSON(http.StatusOK, response)
}

func convertDoubaoVideo2TaskToOpenAIVideo(task *model.Task) (*dto.OpenAIVideo, error) {
	if task == nil || task.Platform != doubaoVideo2Platform() || !task.Properties.OpenAIVideo {
		return nil, fmt.Errorf("task is not a DoubaoVideo2.0 OpenAI video")
	}
	adaptor := relay.GetTaskAdaptor(task.Platform)
	converter, ok := adaptor.(channel.OpenAIVideoConverter)
	if !ok {
		return nil, fmt.Errorf("DoubaoVideo2.0 video converter is unavailable")
	}
	data, err := converter.ConvertToOpenAIVideo(task)
	if err != nil {
		return nil, err
	}
	var video dto.OpenAIVideo
	if err := common.Unmarshal(data, &video); err != nil {
		return nil, err
	}
	video.StandardFields = true
	return &video, nil
}

// DeleteDoubaoVideo2OpenAIVideo deletes the gateway video resource and its
// optional local result cache. Material assets are reusable channel resources,
// so deletion deliberately performs no material or asset API call.
func DeleteDoubaoVideo2OpenAIVideo(c *gin.Context) {
	taskID := strings.TrimSpace(c.Param("task_id"))
	userID := c.GetInt("id")
	task, exists, err := model.GetByTaskId(userID, taskID)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to query DoubaoVideo2.0 video %s: %v", taskID, err))
		doubaoVideo2OpenAIVideoError(c, http.StatusInternalServerError, "server_error", "video_id", "Failed to delete video")
		return
	}
	if !exists || task == nil || task.Platform != doubaoVideo2Platform() || !task.Properties.OpenAIVideo {
		doubaoVideo2OpenAIVideoError(c, http.StatusNotFound, "video_not_found", "video_id", "Video not found")
		return
	}
	// PENDING_SETTLEMENT is intentionally not deletable: removing the task row
	// before asynchronous billing reconciliation would make settlement unsafe.
	if task.Status != model.TaskStatusSuccess && task.Status != model.TaskStatusFailure {
		doubaoVideo2OpenAIVideoError(c, http.StatusConflict, "video_not_terminal", "video_id", "Only completed or failed videos can be deleted")
		return
	}
	if task.BillingState != "" && task.BillingState != model.BillingTransactionStateSettled && task.BillingState != model.BillingTransactionStateRefunded {
		doubaoVideo2OpenAIVideoError(c, http.StatusConflict, "video_billing_pending", "video_id", "Video billing reconciliation is still pending")
		return
	}

	if err := model.DeleteUserTaskByID(userID, task.ID, doubaoVideo2Platform()); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to delete DoubaoVideo2.0 video %s: %v", taskID, err))
		doubaoVideo2OpenAIVideoError(c, http.StatusInternalServerError, "server_error", "video_id", "Failed to delete video")
		return
	}
	if err := service.DeleteVideoResultCache(task.ResultFile); err != nil {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("Failed to remove cached result for deleted DoubaoVideo2.0 video %s: %v", taskID, err))
	}
	c.JSON(http.StatusOK, gin.H{"id": taskID, "deleted": true, "object": "video.deleted"})
}
