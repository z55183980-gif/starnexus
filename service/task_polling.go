package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"runtime/debug"
	"sort"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/samber/lo"
)

// TaskPollingAdaptor 定义轮询所需的最小适配器接口，避免 service -> relay 的循环依赖
type TaskPollingAdaptor interface {
	Init(info *relaycommon.RelayInfo)
	FetchTask(baseURL string, key string, body map[string]any, proxy string) (*http.Response, error)
	ParseTaskResult(body []byte) (*relaycommon.TaskInfo, error)
	// AdjustBillingOnComplete 在任务到达终态（成功/失败）时由轮询循环调用。
	// 返回正数触发差额结算（补扣/退还），返回 0 保持预扣费金额不变。
	AdjustBillingOnComplete(task *model.Task, taskResult *relaycommon.TaskInfo) int
}

// TaskUsageRetriever is implemented only by providers that expose terminal
// metering separately from their task-status endpoint.
type TaskUsageRetriever interface {
	FetchTaskUsage(baseURL, key, upstreamTaskID, proxy string) (*relaycommon.TaskInfo, bool, error)
}

// TaskFailureRecovery describes a replacement upstream task created while
// preserving the same public task and billing reservation.
type TaskFailureRecovery struct {
	UpstreamTaskID string
	TaskData       []byte
}

// TaskFailureRecoverer is implemented by channels that can repair a provider
// rejection and resubmit once without exposing a new public task.
type TaskFailureRecoverer interface {
	ShouldRecoverFailedTask(task *model.Task, responseBody []byte) bool
	RecoverFailedTask(ctx context.Context, channel *model.Channel, task *model.Task, responseBody []byte) (*TaskFailureRecovery, error)
}

const videoSettlementFallbackAfter = 10 * time.Minute

const zqbapiRecoveryLeaseTimeout = 2 * time.Minute

// GetTaskAdaptorFunc 由 main 包注入，用于获取指定平台的任务适配器。
// 打破 service -> relay -> relay/channel -> service 的循环依赖。
var GetTaskAdaptorFunc func(platform constant.TaskPlatform) TaskPollingAdaptor

// sweepTimedOutTasks 在主轮询之前独立清理超时任务。
// 每次最多处理 100 条，剩余的下个周期继续处理。
// 使用 per-task CAS (UpdateWithStatus) 防止覆盖被正常轮询已推进的任务。
func sweepTimedOutTasks(ctx context.Context) error {
	if constant.TaskTimeoutMinutes <= 0 {
		return nil
	}
	cutoff := time.Now().Unix() - int64(constant.TaskTimeoutMinutes)*60
	tasks, err := model.GetTimedOutUnfinishedTasks(cutoff, 100)
	if err != nil {
		return fmt.Errorf("query timed out tasks: %w", err)
	}
	if len(tasks) == 0 {
		return nil
	}

	const legacyTaskCutoff int64 = 1740182400 // 2026-02-22 00:00:00 UTC
	reason := fmt.Sprintf("任务超时（%d分钟）", constant.TaskTimeoutMinutes)
	legacyReason := "任务超时（旧系统遗留任务，不进行退款，请联系管理员）"
	now := time.Now().Unix()
	timedOutCount := 0

	for _, task := range tasks {
		isLegacy := task.SubmitTime > 0 && task.SubmitTime < legacyTaskCutoff

		oldStatus := task.Status
		task.Status = model.TaskStatusFailure
		task.Progress = "100%"
		task.FinishTime = now
		if isLegacy {
			task.FailReason = legacyReason
		} else {
			task.FailReason = reason
		}

		won, err := task.UpdateWithStatus(oldStatus)
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("sweepTimedOutTasks CAS update error for task %s: %v", task.TaskID, err))
			continue
		}
		if !won {
			logger.LogInfo(ctx, fmt.Sprintf("sweepTimedOutTasks: task %s already transitioned, skip", task.TaskID))
			continue
		}
		timedOutCount++
		if !isLegacy && task.Quota != 0 {
			RefundTaskQuota(ctx, task, reason)
		}
	}

	if timedOutCount > 0 {
		logger.LogInfo(ctx, fmt.Sprintf("sweepTimedOutTasks: timed out %d tasks", timedOutCount))
	}
	return nil
}

// TaskPollingLoop 主轮询循环，每 15 秒检查一次未完成的任务
func TaskPollingLoop() {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		runTaskPollingIterationSafely(context.Background())
	}
}

func runTaskPollingIterationSafely(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			logger.LogError(ctx, fmt.Sprintf("task polling panic: %v\n%s", r, debug.Stack()))
		}
	}()
	if err := runTaskPollingIteration(ctx); err != nil {
		logger.LogError(ctx, fmt.Sprintf("task polling iteration failed: %v", err))
	}
}

func runTaskPollingIteration(ctx context.Context) error {
	common.SysLog("任务进度轮询开始")
	if err := sweepStaleRetryingTasks(ctx); err != nil {
		return err
	}
	if err := sweepTimedOutTasks(ctx); err != nil {
		return err
	}
	allTasks, err := model.GetAllUnFinishSyncTasks(constant.TaskQueryLimit)
	if err != nil {
		return fmt.Errorf("query unfinished tasks: %w", err)
	}
	platformTask := make(map[constant.TaskPlatform][]*model.Task)
	for _, task := range allTasks {
		platformTask[task.Platform] = append(platformTask[task.Platform], task)
	}
	for platform, tasks := range platformTask {
		if len(tasks) == 0 {
			continue
		}
		taskChannelM := make(map[int][]string)
		taskM := make(map[string]*model.Task)
		nullTaskIds := make([]int64, 0)
		for _, task := range tasks {
			upstreamID := task.GetUpstreamTaskID()
			if upstreamID == "" {
				nullTaskIds = append(nullTaskIds, task.ID)
				continue
			}
			taskM[upstreamID] = task
			taskChannelM[task.ChannelId] = append(taskChannelM[task.ChannelId], upstreamID)
		}
		if len(nullTaskIds) > 0 {
			if err := model.TaskBulkUpdateByID(nullTaskIds, map[string]any{
				"status":   model.TaskStatusFailure,
				"progress": taskcommon.ProgressComplete,
			}); err != nil {
				return fmt.Errorf("mark tasks without upstream id failed: %w", err)
			}
			logger.LogInfo(ctx, fmt.Sprintf("Fix null task_id task success: %v", nullTaskIds))
		}
		if len(taskChannelM) > 0 {
			DispatchPlatformUpdate(platform, taskChannelM, taskM)
		}
	}
	common.SysLog("任务进度轮询完成")
	return nil
}

func sweepStaleRetryingTasks(ctx context.Context) error {
	cutoff := time.Now().Add(-zqbapiRecoveryLeaseTimeout).Unix()
	tasks, err := model.GetStaleRetryingTasks(cutoff, 100)
	if err != nil {
		return fmt.Errorf("query stale retrying tasks: %w", err)
	}
	now := time.Now().Unix()
	for _, task := range tasks {
		if task == nil {
			continue
		}
		reason := "ZQBAPI automatic recovery was interrupted; the task was stopped to prevent a duplicate upstream submission"
		task.Status = model.TaskStatusFailure
		task.Progress = taskcommon.ProgressComplete
		task.FinishTime = now
		task.FailReason = reason
		task.PrivateData.ZQBAPIRetryPayload = ""
		task.PrivateData.ZQBAPIRecoveryStartedAt = 0
		task.PrivateData.ZQBAPIRecoveryFromStatus = ""
		won, updateErr := task.UpdateWithStatus(model.TaskStatusRetrying)
		if updateErr != nil {
			logger.LogError(ctx, fmt.Sprintf("fail stale retrying task %s: %v", task.TaskID, updateErr))
			continue
		}
		if won && task.Quota != 0 {
			RefundTaskQuota(ctx, task, reason)
		}
	}
	return nil
}

// DispatchPlatformUpdate 按平台分发轮询更新
func DispatchPlatformUpdate(platform constant.TaskPlatform, taskChannelM map[int][]string, taskM map[string]*model.Task) {
	switch platform {
	case constant.TaskPlatformMidjourney:
		// MJ 轮询由其自身处理，这里预留入口
	case constant.TaskPlatformSuno:
		_ = UpdateSunoTasks(context.Background(), taskChannelM, taskM)
	default:
		if err := UpdateVideoTasks(context.Background(), platform, taskChannelM, taskM); err != nil {
			common.SysLog(fmt.Sprintf("UpdateVideoTasks fail: %s", err))
		}
	}
}

// UpdateSunoTasks 按渠道更新所有 Suno 任务
func UpdateSunoTasks(ctx context.Context, taskChannelM map[int][]string, taskM map[string]*model.Task) error {
	for channelId, taskIds := range taskChannelM {
		err := updateSunoTasks(ctx, channelId, taskIds, taskM)
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("渠道 #%d 更新异步任务失败: %s", channelId, err.Error()))
		}
	}
	return nil
}

func updateSunoTasks(ctx context.Context, channelId int, taskIds []string, taskM map[string]*model.Task) error {
	logger.LogInfo(ctx, fmt.Sprintf("渠道 #%d 未完成的任务有: %d", channelId, len(taskIds)))
	if len(taskIds) == 0 {
		return nil
	}
	ch, err := model.CacheGetChannel(channelId)
	if err != nil {
		common.SysLog(fmt.Sprintf("CacheGetChannel: %v", err))
		// Collect DB primary key IDs for bulk update (taskIds are upstream IDs, not task_id column values)
		var failedIDs []int64
		for _, upstreamID := range taskIds {
			if t, ok := taskM[upstreamID]; ok {
				failedIDs = append(failedIDs, t.ID)
			}
		}
		err = model.TaskBulkUpdateByID(failedIDs, map[string]any{
			"fail_reason": fmt.Sprintf("获取渠道信息失败，请联系管理员，渠道ID：%d", channelId),
			"status":      "FAILURE",
			"progress":    "100%",
		})
		if err != nil {
			common.SysLog(fmt.Sprintf("UpdateSunoTask error: %v", err))
		}
		return err
	}
	adaptor := GetTaskAdaptorFunc(constant.TaskPlatformSuno)
	if adaptor == nil {
		return errors.New("adaptor not found")
	}
	proxy := ch.GetSetting().Proxy
	resp, err := adaptor.FetchTask(*ch.BaseURL, ch.Key, map[string]any{
		"ids": taskIds,
	}, proxy)
	if err != nil {
		common.SysLog(fmt.Sprintf("Get Task Do req error: %v", err))
		return err
	}
	if resp.StatusCode != http.StatusOK {
		logger.LogError(ctx, fmt.Sprintf("Get Task status code: %d", resp.StatusCode))
		return fmt.Errorf("Get Task status code: %d", resp.StatusCode)
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		common.SysLog(fmt.Sprintf("Get Suno Task parse body error: %v", err))
		return err
	}
	var responseItems dto.TaskResponse[[]dto.SunoDataResponse]
	err = common.Unmarshal(responseBody, &responseItems)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("Get Suno Task parse body error2: %v, body: %s", err, string(responseBody)))
		return err
	}
	if !responseItems.IsSuccess() {
		common.SysLog(fmt.Sprintf("渠道 #%d 未完成的任务有: %d, 成功获取到任务数: %s", channelId, len(taskIds), string(responseBody)))
		return err
	}

	for _, responseItem := range responseItems.Data {
		task := taskM[responseItem.TaskID]
		if !taskNeedsUpdate(task, responseItem) {
			continue
		}

		task.Status = lo.If(model.TaskStatus(responseItem.Status) != "", model.TaskStatus(responseItem.Status)).Else(task.Status)
		task.FailReason = lo.If(responseItem.FailReason != "", responseItem.FailReason).Else(task.FailReason)
		task.SubmitTime = lo.If(responseItem.SubmitTime != 0, responseItem.SubmitTime).Else(task.SubmitTime)
		task.StartTime = lo.If(responseItem.StartTime != 0, responseItem.StartTime).Else(task.StartTime)
		task.FinishTime = lo.If(responseItem.FinishTime != 0, responseItem.FinishTime).Else(task.FinishTime)
		if responseItem.FailReason != "" || task.Status == model.TaskStatusFailure {
			logger.LogInfo(ctx, task.TaskID+" 构建失败，"+task.FailReason)
			task.Progress = "100%"
			RefundTaskQuota(ctx, task, task.FailReason)
		}
		if responseItem.Status == model.TaskStatusSuccess {
			task.Progress = "100%"
		}
		task.Data = responseItem.Data

		err = task.Update()
		if err != nil {
			common.SysLog("UpdateSunoTask task error: " + err.Error())
		}
	}
	return nil
}

// taskNeedsUpdate 检查 Suno 任务是否需要更新
func taskNeedsUpdate(oldTask *model.Task, newTask dto.SunoDataResponse) bool {
	if oldTask.SubmitTime != newTask.SubmitTime {
		return true
	}
	if oldTask.StartTime != newTask.StartTime {
		return true
	}
	if oldTask.FinishTime != newTask.FinishTime {
		return true
	}
	if string(oldTask.Status) != newTask.Status {
		return true
	}
	if oldTask.FailReason != newTask.FailReason {
		return true
	}

	if (oldTask.Status == model.TaskStatusFailure || oldTask.Status == model.TaskStatusSuccess) && oldTask.Progress != "100%" {
		return true
	}

	oldData, _ := common.Marshal(oldTask.Data)
	newData, _ := common.Marshal(newTask.Data)

	sort.Slice(oldData, func(i, j int) bool {
		return oldData[i] < oldData[j]
	})
	sort.Slice(newData, func(i, j int) bool {
		return newData[i] < newData[j]
	})

	if string(oldData) != string(newData) {
		return true
	}
	return false
}

// UpdateVideoTasks 按渠道更新所有视频任务
func UpdateVideoTasks(ctx context.Context, platform constant.TaskPlatform, taskChannelM map[int][]string, taskM map[string]*model.Task) error {
	for channelId, taskIds := range taskChannelM {
		if err := updateVideoTasks(ctx, platform, channelId, taskIds, taskM); err != nil {
			logger.LogError(ctx, fmt.Sprintf("Channel #%d failed to update video async tasks: %s", channelId, err.Error()))
		}
	}
	return nil
}

func updateVideoTasks(ctx context.Context, platform constant.TaskPlatform, channelId int, taskIds []string, taskM map[string]*model.Task) error {
	logger.LogInfo(ctx, fmt.Sprintf("Channel #%d pending video tasks: %d", channelId, len(taskIds)))
	if len(taskIds) == 0 {
		return nil
	}
	cacheGetChannel, err := model.CacheGetChannel(channelId)
	if err != nil {
		// Collect DB primary key IDs for bulk update (taskIds are upstream IDs, not task_id column values)
		var failedIDs []int64
		for _, upstreamID := range taskIds {
			if t, ok := taskM[upstreamID]; ok {
				failedIDs = append(failedIDs, t.ID)
			}
		}
		errUpdate := model.TaskBulkUpdateByID(failedIDs, map[string]any{
			"fail_reason": fmt.Sprintf("Failed to get channel info, channel ID: %d", channelId),
			"status":      "FAILURE",
			"progress":    "100%",
		})
		if errUpdate != nil {
			common.SysLog(fmt.Sprintf("UpdateVideoTask error: %v", errUpdate))
		}
		return fmt.Errorf("CacheGetChannel failed: %w", err)
	}
	adaptor := GetTaskAdaptorFunc(platform)
	if adaptor == nil {
		return fmt.Errorf("video adaptor not found")
	}
	info := &relaycommon.RelayInfo{}
	info.ChannelMeta = &relaycommon.ChannelMeta{
		ChannelType:    cacheGetChannel.Type,
		ChannelBaseUrl: cacheGetChannel.GetBaseURL(),
		ChannelSetting: cacheGetChannel.GetSetting(),
	}
	info.ApiKey = cacheGetChannel.Key
	adaptor.Init(info)
	for _, taskId := range taskIds {
		if err := updateVideoSingleTask(ctx, adaptor, cacheGetChannel, taskId, taskM); err != nil {
			logger.LogError(ctx, fmt.Sprintf("Failed to update video task %s: %s", taskId, err.Error()))
		}
		// sleep 1 second between each task to avoid hitting rate limits of upstream platforms
		time.Sleep(1 * time.Second)
	}
	return nil
}

func updateVideoSingleTask(ctx context.Context, adaptor TaskPollingAdaptor, ch *model.Channel, taskId string, taskM map[string]*model.Task) error {
	baseURL := constant.ChannelBaseURLs[ch.Type]
	if ch.GetBaseURL() != "" {
		baseURL = ch.GetBaseURL()
	}
	proxy := ch.GetSetting().Proxy

	task := taskM[taskId]
	if task == nil {
		logger.LogError(ctx, fmt.Sprintf("Task %s not found in taskM", taskId))
		return fmt.Errorf("task %s not found", taskId)
	}
	key := ch.Key

	privateData := task.PrivateData
	if privateData.Key != "" {
		key = privateData.Key
	}
	resp, err := adaptor.FetchTask(baseURL, key, map[string]any{
		"task_id": task.GetUpstreamTaskID(),
		"action":  task.Action,
		"context": ctx,
	}, proxy)
	if err != nil {
		return fmt.Errorf("fetchTask failed for task %s: %w", taskId, err)
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, (2<<20)+1))
	if err != nil {
		return fmt.Errorf("readAll failed for task %s: %w", taskId, err)
	}
	if len(responseBody) > 2<<20 {
		return fmt.Errorf("task response exceeds 2MB for task %s", taskId)
	}
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return fmt.Errorf("upstream task query returned transient HTTP %d for task %s", resp.StatusCode, taskId)
	}

	logger.LogDebug(ctx, "updateVideoSingleTask response: %s", responseBody)

	taskResult := &relaycommon.TaskInfo{}
	// try parse as New API response format
	var responseItems dto.TaskResponse[model.Task]
	if err = common.Unmarshal(responseBody, &responseItems); err == nil && responseItems.IsSuccess() {
		logger.LogDebug(ctx, "updateVideoSingleTask parsed as new api response format: %+v", responseItems)
		t := responseItems.Data
		taskResult.TaskID = t.TaskID
		taskResult.Status = string(t.Status)
		taskResult.Url = t.GetResultURL()
		taskResult.Progress = t.Progress
		taskResult.Reason = t.FailReason
	} else if taskResult, err = adaptor.ParseTaskResult(responseBody); err != nil {
		return fmt.Errorf("parseTaskResult failed for task %s: %w", taskId, err)
	}
	if recovered, recoveryErr := TryRecoverFailedVideoTask(ctx, adaptor, ch, task, taskResult, responseBody); recoveryErr != nil {
		logger.LogWarn(ctx, fmt.Sprintf("Task %s automatic recovery failed: %s", task.TaskID, recoveryErr.Error()))
	} else if recovered {
		return nil
	}
	if taskResult.Status == "" {
		errorResult := &dto.GeneralErrorResponse{}
		if err = common.Unmarshal(responseBody, errorResult); err == nil {
			openaiError := errorResult.TryToOpenAIError()
			if openaiError != nil && openaiError.Code == "429" {
				return nil
			}
			if openaiError != nil {
				taskResult = relaycommon.FailTaskInfo("upstream returned error")
			} else if isVideoTokenPreAuthorizationTask(task) {
				logger.LogWarn(ctx, fmt.Sprintf("Task %s returned a transient empty status; keep it pending", taskId))
				return nil
			} else {
				logger.LogError(ctx, fmt.Sprintf("Task %s returned empty status with unrecognized error format", taskId))
				taskResult = relaycommon.FailTaskInfo("upstream returned unrecognized message")
			}
		}
	}

	logger.LogDebug(ctx, "updateVideoSingleTask taskResult: %+v", taskResult)

	_, err = ApplyTaskResult(ctx, adaptor, task, taskResult, responseBody)
	return err
}

// TryRecoverFailedVideoTask gives a channel one chance to replace a rejected
// upstream task. A status-changing CAS makes the retry safe across pollers.
func TryRecoverFailedVideoTask(ctx context.Context, adaptor TaskPollingAdaptor, ch *model.Channel, task *model.Task, taskResult *relaycommon.TaskInfo, responseBody []byte) (bool, error) {
	if taskResult == nil || taskResult.Status != string(model.TaskStatusFailure) || task == nil || ch == nil {
		return false, nil
	}
	recoverer, ok := adaptor.(TaskFailureRecoverer)
	if !ok || !recoverer.ShouldRecoverFailedTask(task, responseBody) {
		return false, nil
	}
	snapshot := task.Snapshot()
	if task.PrivateData.ZQBAPIRetryCount > 0 {
		return false, nil
	}
	previousPrivateData := task.PrivateData
	task.Status = model.TaskStatusRetrying
	task.PrivateData.ZQBAPIRetryCount++
	task.PrivateData.ZQBAPIRecoveryStartedAt = time.Now().Unix()
	task.PrivateData.ZQBAPIRecoveryFromStatus = string(snapshot.Status)
	task.Progress = taskcommon.ProgressInProgress
	won, err := task.UpdateWithStatus(snapshot.Status)
	if err != nil {
		return false, fmt.Errorf("claim automatic recovery for task %s: %w", task.TaskID, err)
	}
	if !won {
		if err := model.DB.First(task, task.ID).Error; err != nil {
			return false, fmt.Errorf("reload task %s after recovery claim CAS loss: %w", task.TaskID, err)
		}
		return task.Status == model.TaskStatusRetrying || task.PrivateData.ZQBAPIRetryCount > 0, nil
	}

	recovery, recoveryErr := recoverer.RecoverFailedTask(ctx, ch, task, responseBody)
	if recoveryErr != nil || recovery == nil || recovery.UpstreamTaskID == "" {
		task.Status = snapshot.Status
		task.Progress = snapshot.Progress
		task.StartTime = snapshot.StartTime
		task.FinishTime = snapshot.FinishTime
		task.FailReason = snapshot.FailReason
		task.PrivateData = previousPrivateData
		temporary := false
		if recoveryErr != nil {
			var temporaryError interface{ Temporary() bool }
			temporary = errors.As(recoveryErr, &temporaryError) && temporaryError.Temporary()
		}
		if temporary {
			// The original provider task is already terminal, but materialization
			// can safely be retried because no replacement generation was posted.
			task.PrivateData.ZQBAPIRetryCount = 0
		} else {
			task.PrivateData.ZQBAPIRetryCount = 1
		}
		_, restoreErr := task.UpdateWithStatus(model.TaskStatusRetrying)
		if restoreErr != nil {
			return false, fmt.Errorf("restore task %s after recovery failure: %w", task.TaskID, restoreErr)
		}
		if recoveryErr != nil {
			if temporary {
				logger.LogWarn(ctx, fmt.Sprintf("Task %s ZQBAPI material recovery is temporarily unavailable; retry on next poll: %s", task.TaskID, recoveryErr.Error()))
				return true, nil
			}
			return false, recoveryErr
		}
		return false, errors.New("automatic recovery returned an empty upstream task ID")
	}

	task.PrivateData.UpstreamTaskID = recovery.UpstreamTaskID
	task.PrivateData.ZQBAPIRetryPayload = ""
	task.PrivateData.ZQBAPIRecoveryStartedAt = 0
	task.PrivateData.ZQBAPIRecoveryFromStatus = ""
	task.Data = append(task.Data[:0], recovery.TaskData...)
	task.FailReason = ""
	task.Progress = taskcommon.ProgressQueued
	task.StartTime = 0
	task.Status = model.TaskStatusSubmitted
	won, err = task.UpdateWithStatus(model.TaskStatusRetrying)
	if err != nil {
		return false, fmt.Errorf("persist automatic recovery for task %s: %w", task.TaskID, err)
	}
	if !won {
		if err := model.DB.First(task, task.ID).Error; err != nil {
			return false, fmt.Errorf("reload task %s after recovery CAS loss: %w", task.TaskID, err)
		}
		return task.PrivateData.ZQBAPIRetryCount > 0, nil
	}
	logger.LogInfo(ctx, fmt.Sprintf("Task %s automatically resubmitted after ZQBAPI real-person materialization", task.TaskID))
	return true, nil
}

// ApplyTaskResult is the single terminal-state transition path for background
// polling and user-triggered realtime fetches. Billing runs only after winning
// the task status CAS, preventing both skipped and duplicated adjustments.
func ApplyTaskResult(ctx context.Context, adaptor TaskPollingAdaptor, task *model.Task, taskResult *relaycommon.TaskInfo, responseBody []byte) (bool, error) {
	if adaptor == nil || task == nil || taskResult == nil {
		return false, errors.New("invalid task result transition")
	}

	snap := task.Snapshot()
	if responseBody != nil {
		task.Data = redactVideoResponseBody(responseBody)
	}
	now := time.Now().Unix()
	if taskResult.Status == "" {
		return false, fmt.Errorf("upstream returned empty status for task %s", task.TaskID)
	}

	shouldRefund := false
	shouldSettle := false
	quota := task.Quota
	settlementPending := false
	if taskResult.Status == string(model.TaskStatusSuccess) && isVideoTokenPreAuthorizationTask(task) {
		settlementPending = prepareVideoTaskSettlement(ctx, adaptor, task, taskResult, now)
	}

	task.Status = model.TaskStatus(taskResult.Status)
	switch taskResult.Status {
	case model.TaskStatusSubmitted:
		task.Progress = taskcommon.ProgressSubmitted
	case model.TaskStatusQueued:
		task.Progress = taskcommon.ProgressQueued
	case model.TaskStatusInProgress:
		task.Progress = taskcommon.ProgressInProgress
		if task.StartTime == 0 {
			task.StartTime = taskResult.CreatedAt
			if task.StartTime == 0 {
				task.StartTime = now
			}
		}
	case model.TaskStatusSuccess:
		task.PrivateData.ZQBAPIRetryPayload = ""
		task.PrivateData.ZQBAPIRecoveryStartedAt = 0
		task.PrivateData.ZQBAPIRecoveryFromStatus = ""
		if settlementPending {
			task.Status = model.TaskStatusPendingSettlement
			task.Progress = model.TaskProgressPendingSettlement
		} else {
			task.Progress = taskcommon.ProgressComplete
		}
		if task.FinishTime == 0 {
			task.FinishTime = taskResult.CompletedAt
			if task.FinishTime == 0 {
				task.FinishTime = now
			}
		}
		if task.StartTime == 0 {
			task.StartTime = taskResult.CreatedAt
			if task.StartTime == 0 {
				task.StartTime = task.SubmitTime
			}
		}
		if strings.HasPrefix(taskResult.Url, "data:") {
			// data: URI (e.g. Vertex base64 encoded video) — keep in Data, not in ResultURL
			task.PrivateData.ResultURL = taskcommon.BuildProxyURL(task.TaskID)
		} else if taskResult.Url != "" {
			// ZQBAPI signed result URLs are kept private behind the authenticated
			// content proxy. This also gives the optional result archive one stable
			// public URL independent of provider URL expiry.
			if channel, channelErr := model.CacheGetChannel(task.ChannelId); channelErr == nil && channel.Type == constant.ChannelTypeZQBAPI {
				task.PrivateData.UpstreamResultURL = taskResult.Url
				task.PrivateData.ResultURL = taskcommon.BuildProxyURL(task.TaskID)
			} else {
				// Direct upstream URL (e.g. Kling, Ali, Doubao, etc.)
				task.PrivateData.ResultURL = taskResult.Url
			}
		} else {
			// No URL from adaptor — construct proxy URL using public task ID
			task.PrivateData.ResultURL = taskcommon.BuildProxyURL(task.TaskID)
		}
		task.FailReason = ""
		shouldSettle = !settlementPending
	case model.TaskStatusFailure:
		task.PrivateData.ZQBAPIRetryPayload = ""
		task.PrivateData.ZQBAPIRecoveryStartedAt = 0
		task.PrivateData.ZQBAPIRecoveryFromStatus = ""
		logger.LogJson(ctx, fmt.Sprintf("Task %s failed", task.GetUpstreamTaskID()), task)
		task.Status = model.TaskStatusFailure
		task.Progress = taskcommon.ProgressComplete
		if task.FinishTime == 0 {
			task.FinishTime = taskResult.CompletedAt
			if task.FinishTime == 0 {
				task.FinishTime = now
			}
		}
		if task.StartTime == 0 {
			task.StartTime = taskResult.CreatedAt
			if task.StartTime == 0 {
				task.StartTime = task.SubmitTime
			}
		}
		task.FailReason = taskResult.Reason
		logger.LogInfo(ctx, fmt.Sprintf("Task %s failed: %s", task.TaskID, task.FailReason))
		taskResult.Progress = taskcommon.ProgressComplete
		if quota != 0 {
			shouldRefund = true
		}
	default:
		return false, fmt.Errorf("unknown task status %s for task %s", taskResult.Status, task.TaskID)
	}
	if taskResult.Progress != "" && !settlementPending {
		task.Progress = taskResult.Progress
	}

	isDone := task.Status == model.TaskStatusSuccess || task.Status == model.TaskStatusFailure
	if isDone && snap.Status != task.Status {
		won, err := task.UpdateWithStatus(snap.Status)
		if err != nil {
			return false, fmt.Errorf("update terminal task %s: %w", task.TaskID, err)
		}
		if !won {
			logger.LogWarn(ctx, fmt.Sprintf("Task %s already transitioned by another process, skip billing", task.TaskID))
			if err := model.DB.First(task, task.ID).Error; err != nil {
				return false, fmt.Errorf("reload task %s after CAS loss: %w", task.TaskID, err)
			}
			return false, nil
		}
	} else if !snap.Equal(task.Snapshot()) {
		won, err := task.UpdateWithStatus(snap.Status)
		if err != nil {
			return false, fmt.Errorf("update task %s: %w", task.TaskID, err)
		}
		if !won {
			if err := model.DB.First(task, task.ID).Error; err != nil {
				return false, fmt.Errorf("reload task %s after CAS loss: %w", task.TaskID, err)
			}
			return false, nil
		}
	} else {
		// No changes, skip update
		logger.LogDebug(ctx, "No update needed for task %s", task.TaskID)
		return false, nil
	}

	if shouldSettle {
		if taskResult.Resolution != "" && task.PrivateData.BillingContext != nil && task.PrivateData.BillingContext.VideoTokenBilling {
			task.PrivateData.BillingContext.VideoResolution = taskResult.Resolution
		}
		settleTaskBillingOnComplete(ctx, adaptor, task, taskResult)
		FinalizeTaskPreAuthorization(ctx, task, taskResult)
	}
	if shouldRefund {
		RefundTaskQuota(ctx, task, task.FailReason)
	}
	if isDone {
		updateTaskConsumeLogTiming(ctx, task)
		if task.Status == model.TaskStatusSuccess {
			archiveZQBAPIVideoResultAsync(task)
		}
	}

	return true, nil
}

func isVideoTokenPreAuthorizationTask(task *model.Task) bool {
	if task == nil || task.PrivateData.BillingContext == nil {
		return false
	}
	bc := task.PrivateData.BillingContext
	return bc.PreAuthorization && bc.VideoTokenBilling
}

// prepareVideoTaskSettlement enriches a completed Seedance task with the
// provider's separate metering log. Until that record arrives, the estimated
// reservation remains held and the task stays eligible for compensation polls.
func prepareVideoTaskSettlement(ctx context.Context, adaptor TaskPollingAdaptor, task *model.Task, taskResult *relaycommon.TaskInfo, now int64) bool {
	bc := task.PrivateData.BillingContext
	if taskResult.TotalTokens > 0 {
		if taskResult.UsageSource == "" {
			taskResult.UsageSource = "upstream_status"
		}
		bc.SettlementSource = taskResult.UsageSource
		return false
	}

	if retriever, ok := adaptor.(TaskUsageRetriever); ok {
		channel, err := model.CacheGetChannel(task.ChannelId)
		if err != nil {
			logger.LogWarn(ctx, fmt.Sprintf("load channel for task usage settlement failed task %s: %s", task.TaskID, err.Error()))
		} else {
			baseURL := constant.ChannelBaseURLs[channel.Type]
			if channel.GetBaseURL() != "" {
				baseURL = channel.GetBaseURL()
			}
			key := channel.Key
			if task.PrivateData.Key != "" {
				key = task.PrivateData.Key
			}
			usage, found, fetchErr := retriever.FetchTaskUsage(baseURL, key, task.GetUpstreamTaskID(), channel.GetSetting().Proxy)
			if fetchErr != nil {
				logger.LogWarn(ctx, fmt.Sprintf("fetch upstream task usage failed task %s: %s", task.TaskID, fetchErr.Error()))
			} else if found && usage != nil && usage.TotalTokens > 0 {
				taskResult.TotalTokens = usage.TotalTokens
				taskResult.CompletionTokens = usage.CompletionTokens
				taskResult.Resolution = usage.Resolution
				taskResult.UsageSource = usage.UsageSource
				bc.SettlementSource = usage.UsageSource
				return false
			}
		}
	}

	if bc.SettlementStartedAt == 0 {
		bc.SettlementStartedAt = now
	}
	if now-bc.SettlementStartedAt < int64(videoSettlementFallbackAfter/time.Second) {
		return true
	}

	estimatedTextTokens := bc.EstimatedTextTokens
	if estimatedTextTokens <= 0 {
		estimatedTextTokens = 500
	}
	estimatedVideoTokens := bc.EstimatedVideoTokens
	if estimatedVideoTokens <= 0 {
		estimatedVideoTokens = 250_000
	}
	taskResult.CompletionTokens = estimatedVideoTokens
	taskResult.TotalTokens = estimatedTextTokens + estimatedVideoTokens
	taskResult.Resolution = bc.VideoResolution
	taskResult.UsageSource = "estimated_inference"
	bc.SettlementSource = taskResult.UsageSource
	logger.LogWarn(ctx, fmt.Sprintf("finalize task %s with inferred estimated usage after settlement timeout", task.TaskID))
	return false
}

func updateTaskConsumeLogTiming(ctx context.Context, task *model.Task) {
	if task == nil || task.PrivateData.ConsumeLogID <= 0 || task.FinishTime <= 0 {
		return
	}
	startTime := task.StartTime
	if startTime <= 0 {
		startTime = task.SubmitTime
	}
	if startTime <= 0 || task.FinishTime < startTime {
		return
	}
	useTimeMilliseconds := (task.FinishTime - startTime) * 1000
	if err := model.UpdateLogUseTime(task.PrivateData.ConsumeLogID, useTimeMilliseconds); err != nil {
		logger.LogError(ctx, fmt.Sprintf("update task consume log timing failed task %s: %s", task.TaskID, err.Error()))
	}
}

func redactVideoResponseBody(body []byte) []byte {
	var m map[string]any
	if err := common.Unmarshal(body, &m); err != nil {
		return body
	}
	resp, _ := m["response"].(map[string]any)
	if resp != nil {
		delete(resp, "bytesBase64Encoded")
		if v, ok := resp["video"].(string); ok {
			resp["video"] = truncateBase64(v)
		}
		if vs, ok := resp["videos"].([]any); ok {
			for i := range vs {
				if vm, ok := vs[i].(map[string]any); ok {
					delete(vm, "bytesBase64Encoded")
				}
			}
		}
	}
	b, err := common.Marshal(m)
	if err != nil {
		return body
	}
	return b
}

func truncateBase64(s string) string {
	const maxKeep = 256
	if len(s) <= maxKeep {
		return s
	}
	return s[:maxKeep] + "..."
}

// settleTaskBillingOnComplete 任务完成时的统一计费调整。
// 优先级：1. adaptor.AdjustBillingOnComplete 返回正数 → 使用 adaptor 计算的额度
//
//  2. taskResult.TotalTokens > 0 → 按 token 重算
//  3. 都不满足 → 保持预扣额度不变
func settleTaskBillingOnComplete(ctx context.Context, adaptor TaskPollingAdaptor, task *model.Task, taskResult *relaycommon.TaskInfo) {
	// 0. 按次计费的任务不做差额结算
	bc := task.PrivateData.BillingContext
	if bc != nil && bc.VideoTokenBilling && taskResult.TotalTokens > 0 {
		adaptor.AdjustBillingOnComplete(task, taskResult)
		RecalculateTaskQuotaByTokenDetails(ctx, task, taskResult.TotalTokens, taskResult.CompletionTokens)
		return
	}
	if bc != nil && bc.PerCallBilling {
		logger.LogInfo(ctx, fmt.Sprintf("任务 %s 按次计费，跳过差额结算", task.TaskID))
		return
	}
	// 1. 优先让 adaptor 决定最终额度
	if actualQuota := adaptor.AdjustBillingOnComplete(task, taskResult); actualQuota > 0 {
		RecalculateTaskQuota(ctx, task, actualQuota, "adaptor计费调整")
		return
	}
	// 2. 回退到 token 重算
	if taskResult.TotalTokens > 0 {
		RecalculateTaskQuotaByTokenDetails(ctx, task, taskResult.TotalTokens, taskResult.CompletionTokens)
		return
	}
	// 3. 无调整，保持预扣额度
}
