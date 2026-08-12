package controller

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
)

var codexSettingOptionKeys = map[string]struct{}{
	"codex_setting.client_version":               {},
	"codex_setting.version_auto_sync_enabled":    {},
	"codex_setting.disable_identity_enforcement": {},
	"codex_setting.routing_hint_enabled":         {},
	"codex_setting.fingerprint_default_mode":     {},
}

type codexSettingsUpdateRequest struct {
	Values map[string]any `json:"values"`
}

func UpdateCodexSettings(c *gin.Context) {
	var request codexSettingsUpdateRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil || len(request.Values) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的 Codex 设置参数"})
		return
	}

	values := make(map[string]string, len(request.Values))
	for key, rawValue := range request.Values {
		if _, allowed := codexSettingOptionKeys[key]; !allowed {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "包含不允许更新的 Codex 设置项"})
			return
		}
		value, err := normalizeCodexSettingValue(key, rawValue)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
			return
		}
		values[key] = value
	}
	if err := model.UpdateOptionsBulk(values); err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}

func normalizeCodexSettingValue(key string, rawValue any) (string, error) {
	var value string
	switch typed := rawValue.(type) {
	case bool:
		value = strconv.FormatBool(typed)
	case string:
		value = strings.TrimSpace(typed)
	default:
		value = strings.TrimSpace(fmt.Sprintf("%v", rawValue))
	}

	switch key {
	case "codex_setting.client_version":
		if value != "" && service.NormalizeCodexClientVersion(value) == "" {
			return "", errors.New("Codex 客户端版本格式无效")
		}
	case "codex_setting.version_auto_sync_enabled", "codex_setting.disable_identity_enforcement", "codex_setting.routing_hint_enabled":
		if value != "true" && value != "false" {
			return "", errors.New("Codex 布尔设置值无效")
		}
	case "codex_setting.fingerprint_default_mode":
		switch value {
		case model.UpstreamCodexFingerprintModeOff, model.UpstreamCodexFingerprintModeDevice, model.UpstreamCodexFingerprintModeSession, model.UpstreamCodexFingerprintModeFull:
		default:
			return "", errors.New("Codex 指纹模式无效")
		}
	}
	return value, nil
}

func SyncCodexVersion(c *gin.Context) {
	err := service.SyncCodexVersionNow()
	settings := system_setting.GetCodexSetting()
	data := gin.H{
		"status":         settings.VersionSyncStatus,
		"error":          settings.VersionSyncError,
		"synced_version": settings.SyncedClientVersion,
		"synced_at":      settings.VersionSyncedAt,
		"attempted_at":   settings.VersionSyncAttemptedAt,
	}
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error(), "data": data})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "Codex 官方版本同步成功", "data": data})
}
