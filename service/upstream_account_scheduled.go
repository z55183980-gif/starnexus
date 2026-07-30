package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/bytedance/gopkg/util/gopool"
	"gorm.io/gorm"
)

const (
	upstreamScheduledTestTickInterval = time.Minute
	upstreamScheduledTestBatchSize    = 100
	upstreamScheduledTestResultLimit  = 50
)

var (
	upstreamScheduledTestOnce    sync.Once
	upstreamScheduledTestRunning atomic.Bool
)

type UpstreamAccountStats struct {
	Days          int                          `json:"days"`
	SelectedCount int64                        `json:"selected_count"`
	SuccessCount  int64                        `json:"success_count"`
	ErrorCount    int64                        `json:"error_count"`
	TestCount     int64                        `json:"test_count"`
	SuccessRate   float64                      `json:"success_rate"`
	RecentTests   []UpstreamAccountTestHistory `json:"recent_tests"`
}

type UpstreamAccountTestHistory struct {
	Success              bool   `json:"success"`
	StatusCode           int    `json:"status_code"`
	LatencyMs            int64  `json:"latency_ms"`
	FirstOutputLatencyMs int64  `json:"first_output_latency_ms"`
	Result               string `json:"result"`
	Model                string `json:"model"`
	CreatedAt            int64  `json:"created_at"`
}

func GetUpstreamAccountStats(accountId int, days int) (*UpstreamAccountStats, error) {
	if accountId <= 0 {
		return nil, errors.New("invalid upstream account id")
	}
	if days < 1 {
		days = 30
	}
	if days > 90 {
		days = 90
	}
	if err := ensureUpstreamAccountExists(accountId); err != nil {
		return nil, err
	}
	cutoff := common.GetTimestamp() - int64((time.Duration(days) * 24 * time.Hour).Seconds())
	stats := &UpstreamAccountStats{Days: days, RecentTests: []UpstreamAccountTestHistory{}}
	counts := []struct {
		eventType string
		value     *int64
	}{
		{"request_selected", &stats.SelectedCount},
		{"request_success", &stats.SuccessCount},
		{"request_error", &stats.ErrorCount},
		{"account_test", &stats.TestCount},
	}
	for _, item := range counts {
		if err := model.DB.Model(&model.UpstreamAccountEvent{}).
			Where("account_id = ? AND event_type = ? AND created_at >= ?", accountId, item.eventType, cutoff).
			Count(item.value).Error; err != nil {
			return nil, err
		}
	}
	completed := stats.SuccessCount + stats.ErrorCount
	if completed > 0 {
		stats.SuccessRate = float64(stats.SuccessCount) / float64(completed) * 100
	}
	var testEvents []model.UpstreamAccountEvent
	if err := model.DB.Where("account_id = ? AND event_type = ? AND created_at >= ?", accountId, "account_test", cutoff).
		Order("created_at DESC").Order("id DESC").Limit(20).Find(&testEvents).Error; err != nil {
		return nil, err
	}
	for _, event := range testEvents {
		metadata := struct {
			StatusCode           int    `json:"status_code"`
			LatencyMs            int64  `json:"latency_ms"`
			FirstOutputLatencyMs int64  `json:"first_output_latency_ms"`
			Model                string `json:"model"`
		}{}
		_ = common.UnmarshalJsonStr(event.Metadata, &metadata)
		stats.RecentTests = append(stats.RecentTests, UpstreamAccountTestHistory{
			Success: event.Result == "ok", StatusCode: metadata.StatusCode,
			LatencyMs: metadata.LatencyMs, FirstOutputLatencyMs: metadata.FirstOutputLatencyMs,
			Result: event.Result, Model: metadata.Model, CreatedAt: event.CreatedAt,
		})
	}
	return stats, nil
}

func ListUpstreamAccountScheduledTestPlans(accountId int) ([]model.UpstreamAccountScheduledTestPlan, error) {
	if err := ensureUpstreamAccountExists(accountId); err != nil {
		return nil, err
	}
	plans := []model.UpstreamAccountScheduledTestPlan{}
	err := model.DB.Where("account_id = ?", accountId).Order("id ASC").Find(&plans).Error
	return plans, err
}

func GetUpstreamAccountScheduledTestPlan(accountId int, planId int) (*model.UpstreamAccountScheduledTestPlan, error) {
	if accountId <= 0 || planId <= 0 {
		return nil, errors.New("invalid scheduled test plan id")
	}
	var plan model.UpstreamAccountScheduledTestPlan
	if err := model.DB.Where("id = ? AND account_id = ?", planId, accountId).First(&plan).Error; err != nil {
		return nil, err
	}
	return &plan, nil
}

func CreateUpstreamAccountScheduledTestPlan(plan *model.UpstreamAccountScheduledTestPlan) error {
	if err := validateUpstreamScheduledTestPlan(plan); err != nil {
		return err
	}
	if err := ensureUpstreamAccountExists(plan.AccountId); err != nil {
		return err
	}
	now := common.GetTimestamp()
	plan.CreatedAt = now
	plan.UpdatedAt = now
	if plan.Enabled {
		nextRun := scheduledTestNextRun(now, plan.IntervalMinutes)
		plan.NextRunAt = &nextRun
	} else {
		plan.NextRunAt = nil
	}
	return model.DB.Create(plan).Error
}

func UpdateUpstreamAccountScheduledTestPlan(plan *model.UpstreamAccountScheduledTestPlan) error {
	if plan == nil || plan.Id <= 0 {
		return errors.New("invalid scheduled test plan")
	}
	if err := validateUpstreamScheduledTestPlan(plan); err != nil {
		return err
	}
	now := common.GetTimestamp()
	var nextRunAt *int64
	if plan.Enabled {
		nextRun := scheduledTestNextRun(now, plan.IntervalMinutes)
		nextRunAt = &nextRun
	}
	result := model.DB.Model(&model.UpstreamAccountScheduledTestPlan{}).
		Where("id = ? AND account_id = ?", plan.Id, plan.AccountId).
		Updates(map[string]any{
			"name": plan.Name, "model": plan.Model, "interval_minutes": plan.IntervalMinutes,
			"enabled": plan.Enabled, "auto_recover": plan.AutoRecover,
			"next_run_at": nextRunAt, "updated_at": now,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return gorm.ErrRecordNotFound
	}
	plan.NextRunAt = nextRunAt
	plan.UpdatedAt = now
	return nil
}

func DeleteUpstreamAccountScheduledTestPlan(accountId int, planId int) error {
	if accountId <= 0 || planId <= 0 {
		return errors.New("invalid scheduled test plan id")
	}
	return model.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("plan_id = ?", planId).Delete(&model.UpstreamAccountScheduledTestResult{}).Error; err != nil {
			return err
		}
		result := tx.Where("id = ? AND account_id = ?", planId, accountId).Delete(&model.UpstreamAccountScheduledTestPlan{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
}

func ListUpstreamAccountScheduledTestResults(accountId int, planId int) ([]model.UpstreamAccountScheduledTestResult, error) {
	if _, err := GetUpstreamAccountScheduledTestPlan(accountId, planId); err != nil {
		return nil, err
	}
	results := []model.UpstreamAccountScheduledTestResult{}
	err := model.DB.Where("plan_id = ? AND account_id = ?", planId, accountId).
		Order("created_at DESC").Order("id DESC").Limit(upstreamScheduledTestResultLimit).Find(&results).Error
	return results, err
}

func StartUpstreamAccountScheduledTestTask() {
	upstreamScheduledTestOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}
		gopool.Go(func() {
			logger.LogInfo(context.Background(), fmt.Sprintf("upstream account scheduled test task started: tick=%s", upstreamScheduledTestTickInterval))
			ticker := time.NewTicker(upstreamScheduledTestTickInterval)
			defer ticker.Stop()
			runUpstreamAccountScheduledTestsOnce()
			for range ticker.C {
				runUpstreamAccountScheduledTestsOnce()
			}
		})
	})
}

func runUpstreamAccountScheduledTestsOnce() {
	if !upstreamScheduledTestRunning.CompareAndSwap(false, true) {
		return
	}
	defer upstreamScheduledTestRunning.Store(false)
	now := common.GetTimestamp()
	var plans []model.UpstreamAccountScheduledTestPlan
	if err := model.DB.Where("enabled = ? AND next_run_at IS NOT NULL AND next_run_at <= ?", true, now).
		Order("next_run_at ASC").Limit(upstreamScheduledTestBatchSize).Find(&plans).Error; err != nil {
		logger.LogWarn(context.Background(), fmt.Sprintf("upstream scheduled tests query failed: %v", err))
		return
	}
	for i := range plans {
		plan := plans[i]
		gopool.Go(func() {
			runUpstreamAccountScheduledTest(&plan, now)
		})
	}
}

func runUpstreamAccountScheduledTest(plan *model.UpstreamAccountScheduledTestPlan, now int64) {
	if plan == nil || plan.NextRunAt == nil {
		return
	}
	nextRun := scheduledTestNextRun(now, plan.IntervalMinutes)
	claim := model.DB.Model(&model.UpstreamAccountScheduledTestPlan{}).
		Where("id = ? AND enabled = ? AND next_run_at = ?", plan.Id, true, *plan.NextRunAt).
		Updates(map[string]any{"next_run_at": nextRun, "updated_at": now})
	if claim.Error != nil || claim.RowsAffected != 1 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	result, err := TestUpstreamAccount(ctx, plan.AccountId, UpstreamAccountTestOptions{Model: plan.Model})
	cancel()
	testResult := model.UpstreamAccountScheduledTestResult{
		PlanId: plan.Id, AccountId: plan.AccountId, Result: "test_failed", Model: plan.Model, CreatedAt: common.GetTimestamp(),
	}
	if err == nil && result != nil {
		testResult.Success = result.Success
		testResult.StatusCode = result.StatusCode
		testResult.LatencyMs = result.LatencyMs
		testResult.FirstOutputLatencyMs = result.FirstOutputLatencyMs
		testResult.Result = result.Result
		testResult.Model = result.Model
	}
	if err := model.DB.Create(&testResult).Error; err != nil {
		logger.LogWarn(context.Background(), fmt.Sprintf("upstream scheduled test result save failed: plan_id=%d", plan.Id))
		return
	}
	lastRun := testResult.CreatedAt
	_ = model.DB.Model(&model.UpstreamAccountScheduledTestPlan{}).Where("id = ?", plan.Id).
		Updates(map[string]any{"last_run_at": lastRun, "updated_at": lastRun}).Error
	pruneUpstreamScheduledTestResults(plan.Id)
	if plan.AutoRecover && testResult.Success {
		_ = RecoverUpstreamAccountRuntimeState(plan.AccountId, UpstreamAccountRecoveryAll)
	}
}

func pruneUpstreamScheduledTestResults(planId int) {
	var keepIds []int
	if err := model.DB.Model(&model.UpstreamAccountScheduledTestResult{}).Where("plan_id = ?", planId).
		Order("created_at DESC").Order("id DESC").Limit(upstreamScheduledTestResultLimit).Pluck("id", &keepIds).Error; err != nil {
		return
	}
	if len(keepIds) < upstreamScheduledTestResultLimit {
		return
	}
	_ = model.DB.Where("plan_id = ? AND id NOT IN ?", planId, keepIds).Delete(&model.UpstreamAccountScheduledTestResult{}).Error
}

func validateUpstreamScheduledTestPlan(plan *model.UpstreamAccountScheduledTestPlan) error {
	if plan == nil || plan.AccountId <= 0 {
		return errors.New("scheduled test account is required")
	}
	plan.Name = strings.TrimSpace(plan.Name)
	plan.Model = strings.TrimSpace(plan.Model)
	if plan.Name == "" {
		return errors.New("scheduled test name is required")
	}
	if plan.IntervalMinutes < 5 || plan.IntervalMinutes > 43_200 {
		return errors.New("scheduled test interval must be between 5 and 43200 minutes")
	}
	return nil
}

func ensureUpstreamAccountExists(accountId int) error {
	if accountId <= 0 {
		return errors.New("invalid upstream account id")
	}
	var count int64
	if err := model.DB.Model(&model.UpstreamAccount{}).Where("id = ?", accountId).Count(&count).Error; err != nil {
		return err
	}
	if count != 1 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func scheduledTestNextRun(now int64, intervalMinutes int) int64 {
	return now + int64((time.Duration(intervalMinutes) * time.Minute).Seconds())
}
