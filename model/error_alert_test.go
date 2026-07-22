package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestErrorAlertLifecycle(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	originalLogDB := LOG_DB
	LOG_DB = db
	t.Cleanup(func() { LOG_DB = originalLogDB })
	require.NoError(t, db.AutoMigrate(&ErrorAlert{}))

	log := &Log{
		Id:        11,
		CreatedAt: 100,
		Type:      LogTypeError,
		Content:   "upstream returned 503 request_id=req-123",
		ChannelId: 7,
		ModelName: "gpt-5.5",
		Other: common.MapToJsonStr(map[string]interface{}{
			"request_path": "/v1/responses",
			"status_code":  503,
			"error_type":   "upstream_error",
			"admin_info":   map[string]interface{}{"node_name": "prod-2"},
		}),
	}
	alert, err := UpsertErrorAlert(log)
	require.NoError(t, err)
	require.Equal(t, ErrorAlertSeverityCritical, alert.Severity)
	require.Equal(t, ErrorAlertStatusUnhandled, alert.Status)
	require.Equal(t, 1, alert.OccurrenceCount)

	log.Id = 12
	log.CreatedAt = 110
	log.Content = "upstream returned 503 request_id=req-456"
	alert, err = UpsertErrorAlert(log)
	require.NoError(t, err)
	require.Equal(t, 2, alert.OccurrenceCount)

	alerts, err := ListErrorAlerts("", 50)
	require.NoError(t, err)
	require.Len(t, alerts, 1)

	alert, err = AcknowledgeErrorAlert(alert.ID, 9, alert.LastLogID)
	require.NoError(t, err)
	require.Equal(t, ErrorAlertStatusAcknowledged, alert.Status)
	require.Equal(t, 12, alert.AcknowledgedLogID)
	require.Equal(t, 9, *alert.AcknowledgedBy)
	alerts, err = ListErrorAlerts("", 50)
	require.NoError(t, err)
	require.Empty(t, alerts)

	log.Id = 13
	log.CreatedAt = 120
	alert, err = UpsertErrorAlert(log)
	require.NoError(t, err)
	require.Equal(t, ErrorAlertStatusUnhandled, alert.Status)
	require.Equal(t, 3, alert.OccurrenceCount)
	require.Equal(t, 12, alert.AcknowledgedLogID)
	alerts, err = ListErrorAlerts("", 50)
	require.NoError(t, err)
	require.Len(t, alerts, 1)

	alert, err = ResolveErrorAlert(alert.ID)
	require.NoError(t, err)
	require.Equal(t, ErrorAlertStatusResolved, alert.Status)
	log.Id = 14
	log.CreatedAt = 130
	alert, err = UpsertErrorAlert(log)
	require.NoError(t, err)
	require.Equal(t, ErrorAlertStatusUnhandled, alert.Status)
	require.Equal(t, 4, alert.OccurrenceCount)
}

func TestErrorAlertFingerprintSeparatesModels(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	originalLogDB := LOG_DB
	LOG_DB = db
	t.Cleanup(func() { LOG_DB = originalLogDB })
	require.NoError(t, db.AutoMigrate(&ErrorAlert{}))

	log := &Log{
		Id:        21,
		CreatedAt: 200,
		Type:      LogTypeError,
		Content:   "status_code=503, Service temporarily unavailable",
		ChannelId: 46,
		ModelName: "gpt-5.5",
		Other: common.MapToJsonStr(map[string]interface{}{
			"request_path": "/v1/responses",
			"status_code":  503,
			"admin_info":   map[string]interface{}{"node_name": "prod-1"},
		}),
	}
	first, err := UpsertErrorAlert(log)
	require.NoError(t, err)

	log.Id = 22
	log.ModelName = "gpt-5.6-sol"
	second, err := UpsertErrorAlert(log)
	require.NoError(t, err)
	require.NotEqual(t, first.ID, second.ID)

	alerts, err := ListErrorAlerts("", 50)
	require.NoError(t, err)
	require.Len(t, alerts, 2)
}

func TestAcknowledgeErrorAlertUsesSeenLogWatermark(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	originalLogDB := LOG_DB
	LOG_DB = db
	t.Cleanup(func() { LOG_DB = originalLogDB })
	require.NoError(t, db.AutoMigrate(&ErrorAlert{}))

	log := &Log{Id: 31, CreatedAt: 300, Type: LogTypeError, Content: "timeout", ChannelId: 9}
	alert, err := UpsertErrorAlert(log)
	require.NoError(t, err)
	seenLogID := alert.LastLogID

	log.Id = 32
	log.CreatedAt = 301
	_, err = UpsertErrorAlert(log)
	require.NoError(t, err)

	alert, err = AcknowledgeErrorAlert(alert.ID, 9, seenLogID)
	require.NoError(t, err)
	require.Equal(t, ErrorAlertStatusUnhandled, alert.Status)
	require.Equal(t, seenLogID, alert.AcknowledgedLogID)

	alerts, err := ListErrorAlerts("", 50)
	require.NoError(t, err)
	require.Len(t, alerts, 1)
}

func TestErrorAlertOutOfOrderUpdatesRemainMonotonic(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	originalLogDB := LOG_DB
	LOG_DB = db
	t.Cleanup(func() { LOG_DB = originalLogDB })
	require.NoError(t, db.AutoMigrate(&ErrorAlert{}))

	log := &Log{Id: 42, CreatedAt: 402, Type: LogTypeError, Content: "timeout", ChannelId: 9}
	alert, err := UpsertErrorAlert(log)
	require.NoError(t, err)

	log.Id = 41
	log.CreatedAt = 401
	alert, err = UpsertErrorAlert(log)
	require.NoError(t, err)
	require.Equal(t, int64(401), alert.FirstSeenAt)
	require.Equal(t, int64(402), alert.LastSeenAt)
	require.Equal(t, 42, alert.LastLogID)

	alert, err = AcknowledgeErrorAlert(alert.ID, 9, 42)
	require.NoError(t, err)
	require.Equal(t, ErrorAlertStatusAcknowledged, alert.Status)

	log.Id = 40
	log.CreatedAt = 400
	alert, err = UpsertErrorAlert(log)
	require.NoError(t, err)
	require.Equal(t, ErrorAlertStatusAcknowledged, alert.Status)
	require.Equal(t, 42, alert.LastLogID)

	alerts, err := ListErrorAlerts("", 50)
	require.NoError(t, err)
	require.Empty(t, alerts)
}
