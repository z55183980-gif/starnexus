package controller

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// GetUnreadLoginAnnouncements returns login-target announcements the user has not read.
// GET /api/user/login-announcements/unread
func GetUnreadLoginAnnouncements(c *gin.Context) {
	userID := c.GetInt("id")
	if userID <= 0 {
		common.ApiError(c, errors.New("unauthorized"))
		return
	}

	items, err := model.GetUnreadLoginAnnouncements(userID)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    items,
	})
}

type MarkLoginAnnouncementsReadRequest struct {
	IDs []int `json:"ids"`
}

// MarkLoginAnnouncementsRead marks login announcements as read for the current user.
// POST /api/user/login-announcements/read
func MarkLoginAnnouncementsRead(c *gin.Context) {
	userID := c.GetInt("id")
	if userID <= 0 {
		common.ApiError(c, errors.New("unauthorized"))
		return
	}

	var req MarkLoginAnnouncementsReadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}

	if err := model.MarkLoginAnnouncementsRead(userID, req.IDs); err != nil {
		common.ApiError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
}
