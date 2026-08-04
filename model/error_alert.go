package model

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	ErrorAlertStatusUnhandled    = "unhandled"
	ErrorAlertStatusAcknowledged = "acknowledged"
	ErrorAlertStatusResolved     = "resolved"
	ErrorAlertSeverityCritical   = "critical"
	ErrorAlertSeverityWarning    = "warning"
	ErrorAlertSeverityInfo       = "info"
)

type ErrorAlert struct {
	ID                uint   `json:"id" gorm:"primaryKey"`
	Fingerprint       string `json:"fingerprint" gorm:"uniqueIndex;size:64"`
	Title             string `json:"title" gorm:"size:512"`
	Severity          string `json:"severity" gorm:"size:16;index"`
	Status            string `json:"status" gorm:"size:16;index"`
	ChannelID         int    `json:"channel_id" gorm:"index"`
	ChannelName       string `json:"channel_name" gorm:"size:256"`
	NodeName          string `json:"node_name" gorm:"size:256"`
	ModelName         string `json:"model_name" gorm:"size:256;index"`
	OccurrenceCount   int    `json:"occurrence_count"`
	FirstSeenAt       int64  `json:"first_seen_at" gorm:"index"`
	LastSeenAt        int64  `json:"last_seen_at" gorm:"index"`
	LastLogID         int    `json:"last_log_id"`
	AcknowledgedLogID int    `json:"acknowledged_log_id" gorm:"default:0"`
	AcknowledgedBy    *int   `json:"acknowledged_by,omitempty"`
	AcknowledgedAt    *int64 `json:"acknowledged_at,omitempty"`
	ResolvedAt        *int64 `json:"resolved_at,omitempty"`
	CreatedAt         int64  `json:"created_at"`
	UpdatedAt         int64  `json:"updated_at"`
}

var errorAlertVariablePattern = regexp.MustCompile(`(?i)(request[_ -]?id|trace[_ -]?id|channel[_ -]?id)\s*[:=]\s*[^\s,;]+`)

func errorAlertNodeName(log *Log) string {
	other, err := common.StrToMap(log.Other)
	if err != nil {
		return ""
	}
	adminInfo, ok := other["admin_info"].(map[string]interface{})
	if !ok {
		return ""
	}
	nodeName, _ := adminInfo["node_name"].(string)
	return nodeName
}

func errorAlertChannelName(log *Log) string {
	if log.ChannelName != "" || log.ChannelId == 0 {
		return log.ChannelName
	}
	channel, err := CacheGetChannel(log.ChannelId)
	if err != nil || channel == nil {
		return ""
	}
	return channel.Name
}

func normalizeErrorAlertTitle(content string) string {
	title := strings.TrimSpace(content)
	if title == "" {
		return "Unknown upstream error"
	}
	title = errorAlertVariablePattern.ReplaceAllString(title, "$1")
	return strings.Join(strings.Fields(title), " ")
}

func errorAlertSeverity(content string) string {
	content = strings.ToLower(content)
	if strings.Contains(content, "502") || strings.Contains(content, "503") ||
		strings.Contains(content, "unavailable") || strings.Contains(content, "authentication") ||
		strings.Contains(content, "credential") {
		return ErrorAlertSeverityCritical
	}
	if strings.Contains(content, "timeout") || strings.Contains(content, "rate limit") ||
		strings.Contains(content, "connection reset") || strings.Contains(content, "429") {
		return ErrorAlertSeverityWarning
	}
	return ErrorAlertSeverityInfo
}

func errorAlertOtherValue(other map[string]interface{}, key string) string {
	value, ok := other[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func errorAlertFingerprint(log *Log, title, nodeName string) string {
	other, _ := common.StrToMap(log.Other)
	parts := []string{
		fmt.Sprint(log.ChannelId),
		nodeName,
		log.ModelName,
		errorAlertOtherValue(other, "request_path"),
		fmt.Sprint(log.IsStream),
		errorAlertOtherValue(other, "status_code"),
		errorAlertOtherValue(other, "error_type"),
		errorAlertOtherValue(other, "error_code"),
		title,
	}
	for i := range parts {
		parts[i] = strings.ToLower(strings.TrimSpace(parts[i]))
	}
	value := strings.Join(parts, "\x1f")
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func UpsertErrorAlert(log *Log) (*ErrorAlert, error) {
	nodeName := errorAlertNodeName(log)
	channelName := errorAlertChannelName(log)
	title := normalizeErrorAlertTitle(log.Content)
	fingerprint := errorAlertFingerprint(log, title, nodeName)
	now := time.Now().Unix()
	alert := ErrorAlert{
		Fingerprint:     fingerprint,
		Title:           title,
		Severity:        errorAlertSeverity(log.Content),
		Status:          ErrorAlertStatusUnhandled,
		ChannelID:       log.ChannelId,
		ChannelName:     channelName,
		NodeName:        nodeName,
		ModelName:       log.ModelName,
		OccurrenceCount: 1,
		FirstSeenAt:     log.CreatedAt,
		LastSeenAt:      log.CreatedAt,
		LastLogID:       log.Id,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	updates := map[string]interface{}{
		"title":    title,
		"severity": errorAlertSeverity(log.Content),
		"status": gorm.Expr(
			"CASE WHEN error_alerts.acknowledged_log_id < ? THEN ? ELSE error_alerts.status END",
			log.Id,
			ErrorAlertStatusUnhandled,
		),
		"channel_name":     channelName,
		"node_name":        nodeName,
		"model_name":       log.ModelName,
		"occurrence_count": gorm.Expr("error_alerts.occurrence_count + ?", 1),
		"first_seen_at": gorm.Expr(
			"CASE WHEN error_alerts.first_seen_at > ? THEN ? ELSE error_alerts.first_seen_at END",
			log.CreatedAt,
			log.CreatedAt,
		),
		"last_seen_at": gorm.Expr(
			"CASE WHEN error_alerts.last_seen_at < ? THEN ? ELSE error_alerts.last_seen_at END",
			log.CreatedAt,
			log.CreatedAt,
		),
		"last_log_id": gorm.Expr(
			"CASE WHEN error_alerts.last_log_id < ? THEN ? ELSE error_alerts.last_log_id END",
			log.Id,
			log.Id,
		),
		"resolved_at": gorm.Expr(
			"CASE WHEN error_alerts.acknowledged_log_id < ? THEN NULL ELSE error_alerts.resolved_at END",
			log.Id,
		),
		"updated_at": now,
	}
	if err := LOG_DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "fingerprint"}},
		DoUpdates: clause.Assignments(updates),
	}).Create(&alert).Error; err != nil {
		return nil, err
	}
	if err := LOG_DB.Where("fingerprint = ?", fingerprint).First(&alert).Error; err != nil {
		return nil, err
	}
	return &alert, nil
}

func ListErrorAlerts(status string, limit int) ([]ErrorAlert, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	tx := LOG_DB.Model(&ErrorAlert{}).Order("last_seen_at desc").Limit(limit)
	if status != "" {
		tx = tx.Where("status = ?", status)
	} else {
		tx = tx.Where("last_log_id > acknowledged_log_id")
	}
	var alerts []ErrorAlert
	err := tx.Find(&alerts).Error
	return alerts, err
}

func GetErrorAlertLatestLog(id uint) (*Log, error) {
	var alert ErrorAlert
	if err := LOG_DB.Select("last_log_id").First(&alert, id).Error; err != nil {
		return nil, err
	}

	var log Log
	if err := LOG_DB.Where("id = ? AND type = ?", alert.LastLogID, LogTypeError).
		First(&log).Error; err != nil {
		return nil, err
	}
	if log.ChannelId > 0 {
		if channel, err := CacheGetChannel(log.ChannelId); err == nil && channel != nil {
			log.ChannelName = channel.Name
		}
	}
	_ = populateLogUpstreamAccountName(&log)
	return &log, nil
}

func AcknowledgeErrorAlert(id uint, userID int, throughLogID int) (*ErrorAlert, error) {
	var alert ErrorAlert
	if err := LOG_DB.First(&alert, id).Error; err != nil {
		return nil, err
	}
	if throughLogID <= 0 || throughLogID > alert.LastLogID {
		throughLogID = alert.LastLogID
	}
	now := time.Now().Unix()
	if err := LOG_DB.Model(&alert).Updates(map[string]interface{}{
		"status": gorm.Expr(
			"CASE WHEN last_log_id <= ? THEN ? ELSE ? END",
			throughLogID,
			ErrorAlertStatusAcknowledged,
			ErrorAlertStatusUnhandled,
		),
		"acknowledged_log_id": gorm.Expr(
			"CASE WHEN acknowledged_log_id < ? THEN ? ELSE acknowledged_log_id END",
			throughLogID,
			throughLogID,
		),
		"acknowledged_by": userID,
		"acknowledged_at": now,
		"updated_at":      now,
	}).Error; err != nil {
		return nil, err
	}
	if err := LOG_DB.First(&alert, id).Error; err != nil {
		return nil, err
	}
	return &alert, nil
}

func AcknowledgeAllErrorAlerts(userID int) ([]ErrorAlert, error) {
	now := time.Now().Unix()
	var alerts []ErrorAlert
	err := LOG_DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("last_log_id > acknowledged_log_id").
			Order("last_seen_at desc").Find(&alerts).Error; err != nil {
			return err
		}
		if len(alerts) == 0 {
			return nil
		}
		if err := tx.Model(&ErrorAlert{}).
			Where("last_log_id > acknowledged_log_id").
			Updates(map[string]interface{}{
				"status":              ErrorAlertStatusAcknowledged,
				"acknowledged_log_id": gorm.Expr("last_log_id"),
				"acknowledged_by":     userID,
				"acknowledged_at":     now,
				"updated_at":          now,
			}).Error; err != nil {
			return err
		}

		acknowledgedBy := userID
		for i := range alerts {
			alerts[i].Status = ErrorAlertStatusAcknowledged
			alerts[i].AcknowledgedLogID = alerts[i].LastLogID
			alerts[i].AcknowledgedBy = &acknowledgedBy
			alerts[i].AcknowledgedAt = &now
			alerts[i].UpdatedAt = now
		}
		return nil
	})
	return alerts, err
}

func ResolveErrorAlert(id uint) (*ErrorAlert, error) {
	var alert ErrorAlert
	if err := LOG_DB.First(&alert, id).Error; err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	if err := LOG_DB.Model(&alert).Updates(map[string]interface{}{
		"status":              ErrorAlertStatusResolved,
		"acknowledged_log_id": alert.LastLogID,
		"resolved_at":         now,
		"updated_at":          now,
	}).Error; err != nil {
		return nil, err
	}
	alert.Status = ErrorAlertStatusResolved
	alert.AcknowledgedLogID = alert.LastLogID
	alert.ResolvedAt = &now
	return &alert, nil
}
