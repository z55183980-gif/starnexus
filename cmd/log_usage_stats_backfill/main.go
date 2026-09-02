// Command log_usage_stats_backfill normalizes the usage-stat columns on
// historical log rows without asking the database to parse the large `other`
// JSON payload. Rows are read with keyset pagination and updated in bounded
// batches so the command can be stopped and resumed safely.
//
// Examples:
//
//	go run ./cmd/log_usage_stats_backfill -mode plan
//	go run ./cmd/log_usage_stats_backfill -mode backfill -batch-size 1000 -pause 200ms
//	go run ./cmd/log_usage_stats_backfill -mode verify
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

type options struct {
	Mode      string
	BatchSize int
	Pause     time.Duration
	StartID   int
	EndID     int
	MaxRows   int
}

type backfillRow struct {
	ID    int    `gorm:"column:id"`
	Other string `gorm:"column:other"`
}

type normalizedRow struct {
	ID                     int
	CacheReadTokens        int64
	BillingCacheReadTokens int64
	CacheCreationTokens    int64
	IsAnthropic            int8
	GroupRatio             float64
	BillingMode            string
	BillingSource          string
}

func main() {
	var opts options
	flag.StringVar(&opts.Mode, "mode", "plan", "plan, backfill, or verify")
	flag.IntVar(&opts.BatchSize, "batch-size", 1000, "rows per database batch")
	flag.DurationVar(&opts.Pause, "pause", 200*time.Millisecond, "pause between write batches")
	flag.IntVar(&opts.StartID, "start-id", 0, "only process rows with id greater than this value")
	flag.IntVar(&opts.EndID, "end-id", 0, "optional inclusive upper id bound")
	flag.IntVar(&opts.MaxRows, "max-rows", 0, "optional maximum rows to update in this run")
	flag.Parse()

	common.InitEnv()
	if err := model.InitDB(); err != nil {
		fatal("init database", err)
	}
	if err := model.InitLogDB(); err != nil {
		fatal("init log database", err)
	}
	defer func() { _ = model.CloseDB() }()

	if err := run(opts); err != nil {
		fatal("log usage stats backfill", err)
	}
}

func run(opts options) error {
	opts.Mode = strings.ToLower(strings.TrimSpace(opts.Mode))
	// Each row contributes 15 bind parameters to the portable CASE update.
	// Keeping the cap at 2,000 stays below SQLite's 32,766 parameter limit and
	// PostgreSQL's 65,535 parameter limit.
	if opts.BatchSize <= 0 || opts.BatchSize > 2000 {
		return errors.New("batch-size must be between 1 and 2000")
	}
	if opts.StartID < 0 || opts.EndID < 0 || opts.MaxRows < 0 {
		return errors.New("start-id, end-id, and max-rows must not be negative")
	}

	var pendingCount int64
	if err := pendingQuery(opts).Count(&pendingCount).Error; err != nil {
		return fmt.Errorf("count pending rows: %w", err)
	}
	fmt.Printf("mode=%s pending=%d start_id=%d end_id=%d batch_size=%d max_rows=%d\n", opts.Mode, pendingCount, opts.StartID, opts.EndID, opts.BatchSize, opts.MaxRows)

	switch opts.Mode {
	case "plan":
		return nil
	case "verify":
		if pendingCount != 0 {
			return fmt.Errorf("%d log rows still need usage-stat normalization", pendingCount)
		}
		return nil
	case "backfill":
		return backfill(opts)
	default:
		return errors.New("mode must be plan, backfill, or verify")
	}
}

func pendingQuery(opts options) *gorm.DB {
	query := model.LOG_DB.Model(&model.Log{}).Where("usage_stats_ready = ?", 0)
	if opts.StartID > 0 {
		query = query.Where("id > ?", opts.StartID)
	}
	if opts.EndID > 0 {
		query = query.Where("id <= ?", opts.EndID)
	}
	return query
}

func backfill(opts options) error {
	lastID := opts.StartID
	updated := 0
	malformed := 0
	for {
		limit := opts.BatchSize
		if opts.MaxRows > 0 && opts.MaxRows-updated < limit {
			limit = opts.MaxRows - updated
		}
		if limit <= 0 {
			break
		}

		query := model.LOG_DB.Table("logs").
			Select("id, other").
			Where("usage_stats_ready = ? AND id > ?", 0, lastID).
			Order("id ASC").Limit(limit)
		if opts.EndID > 0 {
			query = query.Where("id <= ?", opts.EndID)
		}
		var rows []backfillRow
		if err := query.Scan(&rows).Error; err != nil {
			return fmt.Errorf("load rows after id %d: %w", lastID, err)
		}
		if len(rows) == 0 {
			break
		}

		normalized := make([]normalizedRow, 0, len(rows))
		for _, row := range rows {
			log := model.Log{Id: row.ID, Other: row.Other}
			if !model.NormalizeLogUsageStats(&log) {
				malformed++
			}
			normalized = append(normalized, normalizedRow{
				ID:                     row.ID,
				CacheReadTokens:        log.UsageCacheReadTokens,
				BillingCacheReadTokens: log.UsageBillingCacheRead,
				CacheCreationTokens:    log.UsageCacheCreationTokens,
				IsAnthropic:            log.UsageIsAnthropic,
				GroupRatio:             log.UsageGroupRatio,
				BillingMode:            log.UsageBillingMode,
				BillingSource:          log.UsageBillingSource,
			})
		}
		if err := updateBatch(model.LOG_DB, normalized); err != nil {
			return fmt.Errorf("update batch ending at id %d: %w", rows[len(rows)-1].ID, err)
		}

		lastID = rows[len(rows)-1].ID
		updated += len(rows)
		fmt.Printf("updated=%d malformed=%d last_id=%d\n", updated, malformed, lastID)
		if opts.Pause > 0 {
			time.Sleep(opts.Pause)
		}
	}
	fmt.Printf("complete updated=%d malformed=%d last_id=%d\n", updated, malformed, lastID)
	return nil
}

func updateBatch(db *gorm.DB, rows []normalizedRow) error {
	if len(rows) == 0 {
		return nil
	}
	ids := make([]int, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}

	updates := map[string]any{
		"usage_stats_ready":               1,
		"usage_cache_read_tokens":         buildCaseExpression("usage_cache_read_tokens", rows, func(row normalizedRow) any { return row.CacheReadTokens }),
		"usage_billing_cache_read_tokens": buildCaseExpression("usage_billing_cache_read_tokens", rows, func(row normalizedRow) any { return row.BillingCacheReadTokens }),
		"usage_cache_creation_tokens":     buildCaseExpression("usage_cache_creation_tokens", rows, func(row normalizedRow) any { return row.CacheCreationTokens }),
		"usage_is_anthropic":              buildCaseExpression("usage_is_anthropic", rows, func(row normalizedRow) any { return row.IsAnthropic }),
		"usage_group_ratio":               buildCaseExpression("usage_group_ratio", rows, func(row normalizedRow) any { return row.GroupRatio }),
		"usage_billing_mode":              buildCaseExpression("usage_billing_mode", rows, func(row normalizedRow) any { return row.BillingMode }),
		"usage_billing_source":            buildCaseExpression("usage_billing_source", rows, func(row normalizedRow) any { return row.BillingSource }),
	}
	return db.Model(&model.Log{}).
		Where("id IN ? AND usage_stats_ready = ?", ids, 0).
		Updates(updates).Error
}

func buildCaseExpression(column string, rows []normalizedRow, value func(normalizedRow) any) any {
	var sql strings.Builder
	sql.WriteString("CASE id")
	args := make([]any, 0, len(rows)*2)
	for _, row := range rows {
		sql.WriteString(" WHEN ? THEN ?")
		args = append(args, row.ID, value(row))
	}
	sql.WriteString(" ELSE ")
	sql.WriteString(column)
	sql.WriteString(" END")
	return gorm.Expr(sql.String(), args...)
}

func fatal(action string, err error) {
	fmt.Fprintf(os.Stderr, "%s failed: %v\n", action, err)
	os.Exit(1)
}
