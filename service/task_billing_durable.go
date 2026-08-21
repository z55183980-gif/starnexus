package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/bytedance/gopkg/util/gopool"
	"gorm.io/gorm"
)

const durableBillingLease = 30 * time.Second

var durableBillingWorkerOnce sync.Once

type taskBillingLogPayload struct {
	Params model.RecordTaskBillingLogParams `json:"params"`
	TaskID int64                            `json:"task_db_id"`
}

type quotaCachePayload struct {
	UserID  int `json:"user_id"`
	TokenID int `json:"token_id"`
}

func StartDurableBillingCoordinator() {
	durableBillingWorkerOnce.Do(func() {
		gopool.Go(func() {
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for range ticker.C {
				ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
				if err := ProcessDurableBillingOnce(ctx, 100); err != nil {
					common.SysLog("durable billing coordinator error: " + err.Error())
				}
				if err := ProcessBillingOutboxOnce(ctx, 100); err != nil {
					common.SysLog("billing outbox error: " + err.Error())
				}
				cancel()
			}
		})
	})
}

func durableBillingOwner() string {
	return common.NodeName + ":" + strconv.Itoa(os.Getpid())
}

func CreateTaskBillingIntent(task *model.Task, oldStatus model.TaskStatus, finalQuota int, taskResult *relaycommon.TaskInfo, reason string) (bool, error) {
	if task == nil || task.ID <= 0 {
		return false, errors.New("persisted task is required for billing intent")
	}
	if finalQuota < 0 {
		return false, errors.New("final quota cannot be negative")
	}
	state := model.BillingTransactionStateSettlePending
	billingState := state
	if task.Status == model.TaskStatusFailure {
		state = model.BillingTransactionStateRefundPending
		billingState = state
		finalQuota = 0
	}
	now := common.GetTimestamp()
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&model.Task{}).Where("id = ? AND status = ?", task.ID, oldStatus).Select("*").Updates(task)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		var billing model.BillingTransaction
		if err := tx.Where("task_db_id = ?", task.ID).First(&billing).Error; err != nil {
			return err
		}
		if billing.State == model.BillingTransactionStateSettled && state == model.BillingTransactionStateSettlePending {
			return tx.Model(&model.Task{}).Where("id = ?", task.ID).Update("billing_state", model.BillingTransactionStateSettled).Error
		}
		if billing.State != model.BillingTransactionStateReserved && billing.State != model.BillingTransactionStateSettled && billing.State != state {
			return fmt.Errorf("billing transaction is not reservable: %s", billing.State)
		}
		promptTokens, completionTokens := 0, 0
		if taskResult != nil {
			completionTokens = taskResult.CompletionTokens
			promptTokens = max(0, taskResult.TotalTokens-completionTokens)
		}
		if err := tx.Model(&model.BillingTransaction{}).Where("id = ?", billing.ID).Updates(map[string]any{
			"state": state, "final_quota": finalQuota, "target_task_status": string(task.Status),
			"prompt_tokens": promptTokens, "completion_tokens": completionTokens,
			"reason": reason, "next_retry_at": now, "last_error": "",
		}).Error; err != nil {
			return err
		}
		return tx.Model(&model.Task{}).Where("id = ?", task.ID).Update("billing_state", billingState).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	task.BillingState = billingState
	return true, nil
}

func taskHasDurableBilling(task *model.Task) bool {
	if task == nil || task.ID <= 0 {
		return false
	}
	var count int64
	return model.DB.Model(&model.BillingTransaction{}).Where("task_db_id = ?", task.ID).Count(&count).Error == nil && count > 0
}

func failTaskWithDurableBilling(ctx context.Context, task *model.Task, oldStatus model.TaskStatus, reason string) (bool, error) {
	if task == nil {
		return false, errors.New("task is nil")
	}
	task.Status = model.TaskStatusFailure
	task.Progress = taskcommon.ProgressComplete
	if task.FinishTime == 0 {
		task.FinishTime = common.GetTimestamp()
	}
	task.FailReason = reason
	if taskHasDurableBilling(task) {
		won, err := CreateTaskBillingIntent(task, oldStatus, 0, nil, reason)
		if won {
			_ = ProcessDurableBillingOnce(ctx, 1)
		}
		return won, err
	}
	return task.UpdateWithStatus(oldStatus)
}

func ProcessDurableBillingOnce(ctx context.Context, limit int) error {
	if err := model.MarkStaleUnboundBillingRefundPending(common.GetTimestamp() - int64(time.Hour/time.Second)); err != nil {
		return err
	}
	if err := repairPendingTaskBillingIntents(limit); err != nil {
		return err
	}
	rows, err := model.GetDueBillingTransactions(limit)
	if err != nil {
		return err
	}
	for _, candidate := range rows {
		pendingState := candidate.State
		if pendingState == model.BillingTransactionStateApplying {
			pendingState = model.BillingTransactionStateSettlePending
			if candidate.TargetTaskStatus == string(model.TaskStatusFailure) {
				pendingState = model.BillingTransactionStateRefundPending
			}
		}
		won, claimErr := model.ClaimBillingTransaction(candidate.ID, candidate.Version, durableBillingOwner(), durableBillingLease)
		if claimErr != nil || !won {
			continue
		}
		if err := applyDurableTaskBilling(candidate.ID); err != nil {
			attempts := candidate.Attempts + 1
			_ = model.ReleaseBillingTransaction(candidate.ID, pendingState, err.Error(), attempts)
			logger.LogError(ctx, fmt.Sprintf("durable task billing failed transaction %d: %s", candidate.ID, err.Error()))
		}
	}
	return nil
}

func repairPendingTaskBillingIntents(limit int) error {
	tasks, err := model.GetTasksWithPendingBilling(limit)
	if err != nil {
		return err
	}
	now := common.GetTimestamp()
	for _, task := range tasks {
		state := task.BillingState
		target := string(task.Status)
		finalQuota := task.Quota
		if state == model.BillingTransactionStateRefundPending {
			finalQuota = 0
		}
		if err := model.DB.Model(&model.BillingTransaction{}).
			Where("task_db_id = ? AND state = ?", task.ID, model.BillingTransactionStateReserved).
			Updates(map[string]any{
				"state": state, "target_task_status": target, "final_quota": finalQuota, "next_retry_at": now,
			}).Error; err != nil {
			return err
		}
	}
	return nil
}

func applyDurableTaskBilling(transactionID int64) error {
	var invalidateUser int
	var invalidateToken int
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		var billing model.BillingTransaction
		if err := tx.Where("id = ? AND state = ?", transactionID, model.BillingTransactionStateApplying).First(&billing).Error; err != nil {
			return err
		}
		finalQuota := billing.FinalQuota
		if billing.TargetTaskStatus == string(model.TaskStatusFailure) {
			finalQuota = 0
		}
		delta := finalQuota - billing.ReservedQuota
		if err := adjustDurableFundingTx(tx, &billing, delta); err != nil {
			return err
		}
		if !billing.SkipTokenQuota {
			if err := model.AdjustTokenQuotaTx(tx, billing.TokenID, -delta); err != nil {
				return err
			}
		}
		invalidateUser = billing.UserID
		invalidateToken = billing.TokenID
		if billing.TaskDBID == 0 {
			if billing.TargetTaskStatus != string(model.TaskStatusFailure) {
				return errors.New("unbound billing transaction can only be refunded")
			}
			if err := tx.Model(&model.BillingTransaction{}).Where("id = ? AND state = ?", billing.ID, model.BillingTransactionStateApplying).
				Updates(map[string]any{"state": model.BillingTransactionStateRefunded, "final_quota": 0, "lease_owner": "", "lease_until": 0, "last_error": ""}).Error; err != nil {
				return err
			}
			return enqueueQuotaCacheOutbox(tx, &billing)
		}
		var task model.Task
		if err := tx.First(&task, billing.TaskDBID).Error; err != nil {
			return err
		}
		if billing.TargetTaskStatus == string(model.TaskStatusSuccess) {
			if err := model.AdjustUserUsageTx(tx, billing.UserID, finalQuota, 1); err != nil {
				return err
			}
			if err := model.AdjustChannelUsageTx(tx, task.ChannelId, finalQuota); err != nil {
				return err
			}
		}
		task.Quota = finalQuota
		task.BillingState = model.BillingTransactionStateSettled
		finalState := model.BillingTransactionStateSettled
		if billing.TargetTaskStatus == string(model.TaskStatusFailure) {
			task.BillingState = model.BillingTransactionStateRefunded
			finalState = model.BillingTransactionStateRefunded
		}
		if err := tx.Model(&model.Task{}).Where("id = ?", task.ID).Updates(map[string]any{
			"quota": task.Quota, "billing_state": task.BillingState, "private_data": task.PrivateData,
		}).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.BillingTransaction{}).Where("id = ? AND state = ?", billing.ID, model.BillingTransactionStateApplying).
			Updates(map[string]any{"state": finalState, "final_quota": finalQuota, "lease_owner": "", "lease_until": 0, "last_error": ""}).Error; err != nil {
			return err
		}
		if taskUsesPreAuthorization(&task) && billing.TargetTaskStatus == string(model.TaskStatusFailure) {
			return enqueueQuotaCacheOutbox(tx, &billing)
		}
		writeBillingLog := billing.TargetTaskStatus == string(model.TaskStatusSuccess) || !taskUsesPreAuthorization(&task)
		if writeBillingLog {
			params := durableTaskLogParams(&task, &billing)
			payload, err := common.Marshal(taskBillingLogPayload{Params: params, TaskID: task.ID})
			if err != nil {
				return err
			}
			if err := tx.Create(&model.BillingOutbox{
				EventID: fmt.Sprintf("task-billing:%d", billing.ID), BillingTransactionID: billing.ID,
				EventType: model.BillingOutboxEventTaskLog, Payload: string(payload), State: model.BillingOutboxStatePending,
				NextRetryAt: common.GetTimestamp(),
			}).Error; err != nil {
				return err
			}
		}
		if err := enqueueQuotaCacheOutbox(tx, &billing); err != nil {
			return err
		}
		return nil
	})
	if err == nil && invalidateUser > 0 && common.RedisEnabled {
		_ = model.InvalidateUserCache(invalidateUser)
		_ = model.InvalidateTokenCacheByID(invalidateToken)
	}
	return err
}

func enqueueQuotaCacheOutbox(tx *gorm.DB, billing *model.BillingTransaction) error {
	cachePayload, err := common.Marshal(quotaCachePayload{UserID: billing.UserID, TokenID: billing.TokenID})
	if err != nil {
		return err
	}
	return tx.Create(&model.BillingOutbox{
		EventID: fmt.Sprintf("quota-cache:%d", billing.ID), BillingTransactionID: billing.ID,
		EventType: model.BillingOutboxEventCacheInvalidate, Payload: string(cachePayload), State: model.BillingOutboxStatePending,
		NextRetryAt: common.GetTimestamp(),
	}).Error
}

func durableTaskLogParams(task *model.Task, billing *model.BillingTransaction) model.RecordTaskBillingLogParams {
	logType := model.LogTypeConsume
	quota := billing.FinalQuota
	content := fmt.Sprintf("操作 %s", task.Action)
	if billing.TargetTaskStatus == string(model.TaskStatusFailure) {
		logType = model.LogTypeRefund
		quota = billing.ReservedQuota
		content = billing.Reason
	}
	other := taskBillingOther(task)
	other["is_task"] = true
	other["task_id"] = task.TaskID
	other["billing_transaction_id"] = billing.ID
	other["pre_consumed_quota"] = billing.ReservedQuota
	other["actual_quota"] = billing.FinalQuota
	start := task.StartTime
	if start <= 0 {
		start = task.SubmitTime
	}
	useSeconds := 0
	if task.FinishTime >= start && start > 0 {
		useSeconds = int(task.FinishTime - start)
	}
	return model.RecordTaskBillingLogParams{
		UserId: task.UserId, LogType: logType, Content: content, ChannelId: task.ChannelId,
		ModelName: taskModelName(task), Quota: quota, TokenId: billing.TokenID, Group: task.Group,
		PromptTokens: billing.PromptTokens, CompletionTokens: billing.CompletionTokens,
		UseTimeSeconds: useSeconds, Other: other,
	}
}

func calculateDurableTaskFinalQuota(task *model.Task, result *relaycommon.TaskInfo) int {
	if task == nil || result == nil || result.TotalTokens <= 0 {
		return task.Quota
	}
	completion := min(max(result.CompletionTokens, 0), result.TotalTokens)
	prompt := result.TotalTokens - completion
	ctx := tokenPricingContextFromTask(task)
	prompt = billing_setting.ApplyInputTokenPricingForContext(prompt, ctx)
	completion = billing_setting.ApplyOutputTokenPricingForContext(completion, ctx)
	bc := task.PrivateData.BillingContext
	if bc == nil || !bc.VideoTokenBilling || bc.ModelRatio <= 0 || bc.GroupRatio <= 0 {
		return task.Quota
	}
	videoRatio := 1.0
	if ratio := bc.OtherRatios["video_input"]; ratio > 0 {
		videoRatio = ratio
	} else if ratio := bc.OtherRatios["size"]; ratio > 0 {
		videoRatio = ratio
	}
	textQuota, _ := common.QuotaFromFloatChecked(float64(prompt) * bc.ModelRatio * bc.GroupRatio)
	videoQuota, _ := common.QuotaFromFloatChecked(float64(completion) * bc.ModelRatio * bc.GroupRatio * videoRatio)
	return applyMinimumTokenConsumeQuota(textQuota+videoQuota, prompt+completion, bc.ModelRatio > 0 && bc.GroupRatio > 0)
}

func ProcessBillingOutboxOnce(ctx context.Context, limit int) error {
	if limit <= 0 {
		limit = 100
	}
	now := common.GetTimestamp()
	var rows []*model.BillingOutbox
	if err := model.DB.Where(
		"(state = ? AND next_retry_at <= ?) OR (state = ? AND lease_until < ?)",
		model.BillingOutboxStatePending, now, model.BillingOutboxStateDelivering, now,
	).Order("id").Limit(limit).Find(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		result := model.DB.Model(&model.BillingOutbox{}).Where("id = ?", row.ID).
			Where("(state = ? AND next_retry_at <= ?) OR (state = ? AND lease_until < ?)",
				model.BillingOutboxStatePending, now, model.BillingOutboxStateDelivering, now).
			Updates(map[string]any{"state": model.BillingOutboxStateDelivering, "lease_owner": durableBillingOwner(), "lease_until": now + 30})
		if result.Error != nil || result.RowsAffected != 1 {
			continue
		}
		err := deliverBillingOutbox(row)
		if err == nil {
			_ = model.DB.Model(&model.BillingOutbox{}).Where("id = ?", row.ID).Updates(map[string]any{
				"state": model.BillingOutboxStateDelivered, "lease_owner": "", "lease_until": 0, "last_error": "",
			}).Error
			continue
		}
		attempts := row.Attempts + 1
		_ = model.DB.Model(&model.BillingOutbox{}).Where("id = ?", row.ID).Updates(map[string]any{
			"state": model.BillingOutboxStatePending, "lease_owner": "", "lease_until": 0,
			"attempts": attempts, "next_retry_at": now + int64(1<<min(attempts, 8)), "last_error": err.Error(),
		}).Error
		logger.LogError(ctx, fmt.Sprintf("billing outbox event %s failed: %s", row.EventID, err.Error()))
	}
	return nil
}

func deliverBillingOutbox(row *model.BillingOutbox) error {
	switch row.EventType {
	case model.BillingOutboxEventCacheInvalidate:
		var payload quotaCachePayload
		if err := common.UnmarshalJsonStr(row.Payload, &payload); err != nil {
			return err
		}
		if err := model.InvalidateUserCache(payload.UserID); err != nil {
			return err
		}
		return model.InvalidateTokenCacheByID(payload.TokenID)
	case model.BillingOutboxEventTaskLog:
		var payload taskBillingLogPayload
		if err := common.UnmarshalJsonStr(row.Payload, &payload); err != nil {
			return err
		}
		logID, err := model.RecordTaskBillingLogIdempotent(row.EventID, payload.Params)
		if err != nil {
			return err
		}
		if logID > 0 && payload.TaskID > 0 {
			var task model.Task
			if err := model.DB.First(&task, payload.TaskID).Error; err == nil {
				task.PrivateData.ConsumeLogID = logID
				return model.DB.Model(&model.Task{}).Where("id = ?", payload.TaskID).Update("private_data", task.PrivateData).Error
			}
		}
		return nil
	default:
		return fmt.Errorf("unsupported billing outbox event %s", row.EventType)
	}
}

// recalculateDurableVideoBilling captures the actual quota without mutating
// balances. The coordinator applies it atomically with the terminal task.
func recalculateDurableVideoBilling(adaptor TaskPollingAdaptor, task *model.Task, result *relaycommon.TaskInfo) int {
	if adaptor != nil {
		adaptor.AdjustBillingOnComplete(task, result)
	}
	return calculateDurableTaskFinalQuota(task, result)
}
