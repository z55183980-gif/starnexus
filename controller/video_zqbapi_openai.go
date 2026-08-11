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

type zqbapiOpenAIVideoList struct {
	Object  string             `json:"object"`
	Data    []*dto.OpenAIVideo `json:"data"`
	FirstID string             `json:"first_id,omitempty"`
	LastID  string             `json:"last_id,omitempty"`
	HasMore bool               `json:"has_more"`
}

func zqbapiVideoPlatform() constant.TaskPlatform {
	return constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeZQBAPI))
}

func zqbapiOpenAIVideoError(c *gin.Context, status int, code, param, message string) {
	c.JSON(status, gin.H{"error": gin.H{
		"message": message,
		"type":    "invalid_request_error",
		"param":   param,
		"code":    code,
	}})
}

// ListZQBAPIOpenAIVideos exposes only ZQBAPI-backed tasks. Other provider
// tasks are intentionally absent so this compatibility route cannot alter
// their behavior.
func ListZQBAPIOpenAIVideos(c *gin.Context) {
	limit := 20
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			zqbapiOpenAIVideoError(c, http.StatusBadRequest, "invalid_limit", "limit", "limit must be between 1 and 100")
			return
		}
		limit = parsed
	}

	order := strings.ToLower(strings.TrimSpace(c.DefaultQuery("order", "desc")))
	if order != "asc" && order != "desc" {
		zqbapiOpenAIVideoError(c, http.StatusBadRequest, "invalid_order", "order", "order must be asc or desc")
		return
	}

	userID := c.GetInt("id")
	afterID := int64(0)
	if after := strings.TrimSpace(c.Query("after")); after != "" {
		cursor, exists, err := model.GetByTaskId(userID, after)
		if err != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to resolve ZQBAPI video cursor: %v", err))
			zqbapiOpenAIVideoError(c, http.StatusInternalServerError, "server_error", "after", "Failed to list videos")
			return
		}
		if !exists || cursor == nil || cursor.Platform != zqbapiVideoPlatform() {
			zqbapiOpenAIVideoError(c, http.StatusBadRequest, "invalid_after", "after", "after must identify one of your ZQBAPI videos")
			return
		}
		afterID = cursor.ID
	}

	tasks, hasMore, err := model.ListUserTasksByPlatformCursor(userID, zqbapiVideoPlatform(), afterID, limit, order == "asc")
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to list ZQBAPI videos: %v", err))
		zqbapiOpenAIVideoError(c, http.StatusInternalServerError, "server_error", "", "Failed to list videos")
		return
	}

	data := make([]*dto.OpenAIVideo, 0, len(tasks))
	for _, task := range tasks {
		video, convertErr := convertZQBAPITaskToOpenAIVideo(task)
		if convertErr != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to convert ZQBAPI video %s: %v", task.TaskID, convertErr))
			zqbapiOpenAIVideoError(c, http.StatusInternalServerError, "server_error", "", "Failed to list videos")
			return
		}
		data = append(data, video)
	}

	response := zqbapiOpenAIVideoList{Object: "list", Data: data, HasMore: hasMore}
	if len(data) > 0 {
		response.FirstID = data[0].ID
		response.LastID = data[len(data)-1].ID
	}
	c.JSON(http.StatusOK, response)
}

func convertZQBAPITaskToOpenAIVideo(task *model.Task) (*dto.OpenAIVideo, error) {
	if task == nil || task.Platform != zqbapiVideoPlatform() {
		return nil, fmt.Errorf("task is not a ZQBAPI video")
	}
	adaptor := relay.GetTaskAdaptor(task.Platform)
	converter, ok := adaptor.(channel.OpenAIVideoConverter)
	if !ok {
		return nil, fmt.Errorf("ZQBAPI video converter is unavailable")
	}
	data, err := converter.ConvertToOpenAIVideo(task)
	if err != nil {
		return nil, err
	}
	var video dto.OpenAIVideo
	if err := common.Unmarshal(data, &video); err != nil {
		return nil, err
	}
	return &video, nil
}

func DeleteZQBAPIOpenAIVideo(c *gin.Context) {
	taskID := strings.TrimSpace(c.Param("task_id"))
	userID := c.GetInt("id")
	task, exists, err := model.GetByTaskId(userID, taskID)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to query ZQBAPI video %s: %v", taskID, err))
		zqbapiOpenAIVideoError(c, http.StatusInternalServerError, "server_error", "video_id", "Failed to delete video")
		return
	}
	if !exists || task == nil || task.Platform != zqbapiVideoPlatform() {
		zqbapiOpenAIVideoError(c, http.StatusNotFound, "video_not_found", "video_id", "Video not found")
		return
	}
	if task.Status != model.TaskStatusSuccess && task.Status != model.TaskStatusFailure {
		zqbapiOpenAIVideoError(c, http.StatusConflict, "video_not_terminal", "video_id", "Only completed or failed videos can be deleted")
		return
	}

	if err := model.DeleteUserTaskByID(userID, task.ID, zqbapiVideoPlatform()); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to delete ZQBAPI video %s: %v", taskID, err))
		zqbapiOpenAIVideoError(c, http.StatusInternalServerError, "server_error", "video_id", "Failed to delete video")
		return
	}
	if err := service.DeleteVideoResultCache(task.ResultFile); err != nil {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("Failed to remove cached result for deleted ZQBAPI video %s: %v", taskID, err))
	}
	c.JSON(http.StatusOK, gin.H{"id": taskID, "deleted": true, "object": "video.deleted"})
}
