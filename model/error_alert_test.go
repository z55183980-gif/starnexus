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
		Other: common.MapToJsonStr(map[string]interface{}{
			"admin_info": map[string]interface{}{"node_name": "prod-2"},
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

	alert, err = AcknowledgeErrorAlert(alert.ID, 9)
	require.NoError(t, err)
	require.Equal(t, ErrorAlertStatusAcknowledged, alert.Status)
	require.Equal(t, 9, *alert.AcknowledgedBy)
	log.Id = 13
	log.CreatedAt = 120
	alert, err = UpsertErrorAlert(log)
	require.NoError(t, err)
	require.Equal(t, ErrorAlertStatusAcknowledged, alert.Status)
	require.Equal(t, 3, alert.OccurrenceCount)

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
