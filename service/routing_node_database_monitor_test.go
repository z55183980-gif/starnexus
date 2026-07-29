package service

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParseRedisInfo(t *testing.T) {
	values := parseRedisInfo("# Memory\r\nused_memory:2048\r\nmaxmemory:4096\r\n")
	require.Equal(t, "2048", values["used_memory"])
	require.Equal(t, "4096", values["maxmemory"])
}

func TestCollectBackupMonitorMetricsFromFile(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "backup.dump")
	require.NoError(t, os.WriteFile(path, []byte("backup"), 0o600))
	modifiedAt := time.Unix(1_700_000_000, 0)
	require.NoError(t, os.Chtimes(path, modifiedAt, modifiedAt))

	report := &RoutingNodeMonitorReport{}
	collectBackupMonitorMetrics(report, path)

	require.Equal(t, modifiedAt.Unix(), report.BackupLastAt)
	require.Equal(t, uint64(6), report.BackupSize)
}

func TestDatabaseMonitorStatusValidation(t *testing.T) {
	require.True(t, validDatabaseServiceStatus(""))
	require.True(t, validDatabaseServiceStatus(databaseServiceStatusUp))
	require.False(t, validDatabaseServiceStatus("unknown"))
	require.True(t, validDatabaseReplicationStatus("streaming"))
	require.False(t, validDatabaseReplicationStatus("catching_up"))
}
