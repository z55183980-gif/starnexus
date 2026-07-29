package service

import (
	"context"
	"database/sql"
	"io/fs"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	redis "github.com/go-redis/redis/v8"
	_ "github.com/jackc/pgx/v5/stdlib"
)

const (
	databaseServiceStatusUp            = "up"
	databaseServiceStatusDown          = "down"
	databaseServiceStatusNotConfigured = "not_configured"
)

func validDatabaseServiceStatus(status string) bool {
	switch strings.TrimSpace(status) {
	case "", databaseServiceStatusUp, databaseServiceStatusDown, databaseServiceStatusNotConfigured:
		return true
	default:
		return false
	}
}

func validDatabaseReplicationStatus(status string) bool {
	switch strings.TrimSpace(status) {
	case "", "primary", "streaming", "stopped", databaseServiceStatusNotConfigured:
		return true
	default:
		return false
	}
}

func collectRoutingNodeDatabaseMetrics(report *RoutingNodeMonitorReport) {
	if report == nil || !common.GetEnvOrDefaultBool("NODE_MONITOR_DATABASE_METRICS_ENABLED", false) {
		return
	}

	report.DatabaseMetricsEnabled = true
	report.PostgreSQLStatus = databaseServiceStatusNotConfigured
	report.PostgreSQLReplicationStatus = databaseServiceStatusNotConfigured
	report.RedisStatus = databaseServiceStatusNotConfigured
	report.PgBouncerStatus = databaseServiceStatusNotConfigured

	collectPostgreSQLMonitorMetrics(report, strings.TrimSpace(os.Getenv("NODE_MONITOR_POSTGRES_DSN")))
	collectRedisMonitorMetrics(report, strings.TrimSpace(os.Getenv("NODE_MONITOR_REDIS_URL")))
	collectPgBouncerMonitorMetrics(report, strings.TrimSpace(os.Getenv("NODE_MONITOR_PGBOUNCER_DSN")))
	collectBackupMonitorMetrics(report, strings.TrimSpace(os.Getenv("NODE_MONITOR_BACKUP_PATH")))
}

func collectPostgreSQLMonitorMetrics(report *RoutingNodeMonitorReport, dsn string) {
	if dsn == "" {
		return
	}
	report.PostgreSQLStatus = databaseServiceStatusDown
	report.PostgreSQLReplicationStatus = databaseServiceStatusNotConfigured

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return
	}
	defer db.Close()
	if err = db.PingContext(ctx); err != nil {
		return
	}
	report.PostgreSQLStatus = databaseServiceStatusUp

	_ = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM pg_stat_activity").Scan(&report.PostgreSQLConnections)
	_ = db.QueryRowContext(ctx, "SELECT current_setting('max_connections')::int").Scan(&report.PostgreSQLMaxConnections)
	_ = db.QueryRowContext(ctx, "SELECT pg_database_size(current_database())").Scan(&report.PostgreSQLDatabaseSize)

	var cacheHit sql.NullFloat64
	if err = db.QueryRowContext(ctx, `
		SELECT CASE
			WHEN SUM(blks_hit) + SUM(blks_read) = 0 THEN 100
			ELSE SUM(blks_hit) * 100.0 / (SUM(blks_hit) + SUM(blks_read))
		END
		FROM pg_stat_database
		WHERE datname = current_database()
	`).Scan(&cacheHit); err == nil && cacheHit.Valid {
		report.PostgreSQLCacheHitPercent = math.Max(0, math.Min(100, cacheHit.Float64))
	}

	var inRecovery bool
	var receiverStatus sql.NullString
	var receiverLag sql.NullFloat64
	if err = db.QueryRowContext(ctx, `
		SELECT
			pg_is_in_recovery(),
			(SELECT status FROM pg_stat_wal_receiver LIMIT 1),
			(SELECT GREATEST(EXTRACT(EPOCH FROM (clock_timestamp() - last_msg_receipt_time)), 0)
			 FROM pg_stat_wal_receiver LIMIT 1)
	`).Scan(&inRecovery, &receiverStatus, &receiverLag); err != nil {
		return
	}
	if !inRecovery {
		report.PostgreSQLReplicationStatus = "primary"
		return
	}
	if receiverStatus.String == "streaming" {
		report.PostgreSQLReplicationStatus = "streaming"
		if receiverLag.Valid {
			report.PostgreSQLReplicationLagSeconds = math.Max(0, receiverLag.Float64)
		}
	} else if receiverStatus.Valid && receiverStatus.String != "" {
		report.PostgreSQLReplicationStatus = "stopped"
	}
}

func collectRedisMonitorMetrics(report *RoutingNodeMonitorReport, rawURL string) {
	if rawURL == "" {
		return
	}
	report.RedisStatus = databaseServiceStatusDown
	options, err := redis.ParseURL(rawURL)
	if err != nil {
		return
	}
	client := redis.NewClient(options)
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err = client.Ping(ctx).Err(); err != nil {
		return
	}
	report.RedisStatus = databaseServiceStatusUp
	info, err := client.Info(ctx, "memory").Result()
	if err != nil {
		return
	}
	values := parseRedisInfo(info)
	report.RedisMemoryUsed, _ = strconv.ParseUint(values["used_memory"], 10, 64)
	report.RedisMemoryMax, _ = strconv.ParseUint(values["maxmemory"], 10, 64)
}

func parseRedisInfo(info string) map[string]string {
	values := make(map[string]string)
	for _, line := range strings.Split(info, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if ok {
			values[key] = strings.TrimSpace(value)
		}
	}
	return values
}

func collectPgBouncerMonitorMetrics(report *RoutingNodeMonitorReport, dsn string) {
	if dsn == "" {
		return
	}
	report.PgBouncerStatus = databaseServiceStatusDown
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return
	}
	defer db.Close()
	if err = db.PingContext(ctx); err == nil {
		report.PgBouncerStatus = databaseServiceStatusUp
	}
}

func collectBackupMonitorMetrics(report *RoutingNodeMonitorReport, path string) {
	if path == "" {
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	if !info.IsDir() {
		report.BackupLastAt = info.ModTime().Unix()
		report.BackupSize = uint64(max(info.Size(), 0))
		return
	}

	_ = fs.WalkDir(os.DirFS(path), ".", func(relativePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return nil
		}
		entryInfo, infoErr := entry.Info()
		if infoErr != nil || !entryInfo.Mode().IsRegular() {
			return nil
		}
		if entryInfo.ModTime().Unix() > report.BackupLastAt {
			report.BackupLastAt = entryInfo.ModTime().Unix()
			report.BackupSize = uint64(max(entryInfo.Size(), 0))
		}
		return nil
	})
}
