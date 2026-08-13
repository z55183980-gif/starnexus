package model

import (
	"bytes"
	"database/sql/driver"
	"encoding/json"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	commonRelay "github.com/QuantumNous/new-api/relay/common"
)

type TaskStatus string

func (t TaskStatus) ToVideoStatus() string {
	var status string
	switch t {
	case TaskStatusQueued, TaskStatusSubmitted:
		status = dto.VideoStatusQueued
	case TaskStatusInProgress, TaskStatusRetrying:
		status = dto.VideoStatusInProgress
	case TaskStatusSuccess:
		status = dto.VideoStatusCompleted
	case TaskStatusPendingSettlement:
		// The media is ready; only the asynchronous billing reconciliation remains.
		status = dto.VideoStatusCompleted
	case TaskStatusFailure:
		status = dto.VideoStatusFailed
	default:
		status = dto.VideoStatusUnknown // Default fallback
	}
	return status
}

const (
	TaskStatusNotStart          = "NOT_START"
	TaskStatusSubmitted         = "SUBMITTED"
	TaskStatusQueued            = "QUEUED"
	TaskStatusInProgress        = "IN_PROGRESS"
	TaskStatusFailure           = "FAILURE"
	TaskStatusSuccess           = "SUCCESS"
	TaskStatusPendingSettlement = "PENDING_SETTLEMENT"
	TaskStatusRetrying          = "RETRYING"
	TaskStatusUnknown           = "UNKNOWN"
)

const TaskProgressPendingSettlement = "settling"

type Task struct {
	ID         int64                 `json:"id" gorm:"primary_key;AUTO_INCREMENT"`
	CreatedAt  int64                 `json:"created_at" gorm:"index"`
	UpdatedAt  int64                 `json:"updated_at"`
	TaskID     string                `json:"task_id" gorm:"type:varchar(191);index"` // 第三方id，不一定有/ song id\ Task id
	Platform   constant.TaskPlatform `json:"platform" gorm:"type:varchar(30);index"` // 平台
	UserId     int                   `json:"user_id" gorm:"index"`
	Group      string                `json:"group" gorm:"type:varchar(50)"` // 修正计费用
	ChannelId  int                   `json:"channel_id" gorm:"index"`
	Quota      int                   `json:"quota"`
	Action     string                `json:"action" gorm:"type:varchar(40);index"` // 任务类型, song, lyrics, description-mode
	Status     TaskStatus            `json:"status" gorm:"type:varchar(20);index"` // 任务状态
	FailReason string                `json:"fail_reason"`
	SubmitTime int64                 `json:"submit_time" gorm:"index"`
	StartTime  int64                 `json:"start_time" gorm:"index"`
	FinishTime int64                 `json:"finish_time" gorm:"index"`
	Progress   string                `json:"progress" gorm:"type:varchar(20);index"`
	Properties Properties            `json:"properties" gorm:"type:json"`
	Username   string                `json:"username,omitempty" gorm:"-"`
	// 禁止返回给用户，内部可能包含key等隐私信息
	PrivateData TaskPrivateData `json:"-" gorm:"column:private_data;type:json"`
	// ResultFile contains only a generated cache file name. The configured
	// cache root remains server-side and must be shared by serving nodes.
	ResultFile string          `json:"-" gorm:"type:varchar(255)"`
	Data       json.RawMessage `json:"data" gorm:"type:json"`
}

func (t *Task) SetData(data any) {
	b, _ := common.Marshal(data)
	t.Data = json.RawMessage(b)
}

func (t *Task) GetData(v any) error {
	return common.Unmarshal(t.Data, &v)
}

type Properties struct {
	Input              string `json:"input"`
	UpstreamModelName  string `json:"upstream_model_name,omitempty"`
	OriginModelName    string `json:"origin_model_name,omitempty"`
	OpenAIVideo        bool   `json:"openai_video,omitempty"`
	VideoPrompt        string `json:"video_prompt,omitempty"`
	VideoSeconds       string `json:"video_seconds,omitempty"`
	VideoSize          string `json:"video_size,omitempty"`
	RemixedFromVideoID string `json:"remixed_from_video_id,omitempty"`
}

func (m *Properties) Scan(val interface{}) error {
	return scanJSONDatabaseValue(val, m)
}

func (m Properties) Value() (driver.Value, error) {
	if m == (Properties{}) {
		return nil, nil
	}
	return marshalJSONDatabaseValue(m)
}

type TaskPrivateData struct {
	Key               string `json:"key,omitempty"`
	UpstreamTaskID    string `json:"upstream_task_id,omitempty"`    // 上游真实 task ID
	ResultURL         string `json:"result_url,omitempty"`          // 任务成功后的结果 URL（视频地址等）
	UpstreamResultURL string `json:"upstream_result_url,omitempty"` // 内容代理/归档使用的上游 URL
	ConsumeLogID      int    `json:"consume_log_id,omitempty"`      // 提交时的消费日志，用于回写异步总耗时
	// 计费上下文：用于异步退款/差额结算（轮询阶段读取）
	BillingSource  string              `json:"billing_source,omitempty"`  // "wallet" 或 "subscription"
	SubscriptionId int                 `json:"subscription_id,omitempty"` // 订阅 ID，用于订阅退款
	TokenId        int                 `json:"token_id,omitempty"`        // 令牌 ID，用于令牌额度退款
	BillingContext *TaskBillingContext `json:"billing_context,omitempty"` // 计费参数快照（用于轮询阶段重新计算）
	// ZQBAPI retry data contains only the provider request and public image URLs.
	// It is private because prompts and source URLs must not be returned to users.
	ZQBAPIRetryPayload       string `json:"zqbapi_retry_payload,omitempty"`
	ZQBAPIRetryCount         int    `json:"zqbapi_retry_count,omitempty"`
	ZQBAPIRecoveryStartedAt  int64  `json:"zqbapi_recovery_started_at,omitempty"`
	ZQBAPIRecoveryFromStatus string `json:"zqbapi_recovery_from_status,omitempty"`
	// DoubaoVideo2 retry state is independent from ZQBAPI credentials, assets,
	// and recovery counters even though both channels share the polling hook.
	DoubaoVideo2RetryPayload       string `json:"doubao_video2_retry_payload,omitempty"`
	DoubaoVideo2RetryCount         int    `json:"doubao_video2_retry_count,omitempty"`
	DoubaoVideo2RecoveryStartedAt  int64  `json:"doubao_video2_recovery_started_at,omitempty"`
	DoubaoVideo2RecoveryFromStatus string `json:"doubao_video2_recovery_from_status,omitempty"`
}

// TaskBillingContext 记录任务提交时的计费参数，以便轮询阶段可以重新计算额度。
type TaskBillingContext struct {
	ModelPrice           float64            `json:"model_price,omitempty"`       // 模型单价
	GroupRatio           float64            `json:"group_ratio,omitempty"`       // 分组倍率
	ModelRatio           float64            `json:"model_ratio,omitempty"`       // 模型倍率
	OtherRatios          map[string]float64 `json:"other_ratios,omitempty"`      // 附加倍率（时长、分辨率等）
	OriginModelName      string             `json:"origin_model_name,omitempty"` // 模型名称，必须为OriginModelName
	PerCallBilling       bool               `json:"per_call_billing,omitempty"`  // 按次计费：跳过轮询阶段的差额结算
	PreAuthorization     bool               `json:"pre_authorization,omitempty"` // 异步任务预授权：终态前仅冻结额度，不记录消费日志
	PreAuthorizedQuota   int                `json:"pre_authorized_quota,omitempty"`
	RequestPath          string             `json:"request_path,omitempty"`
	QuotaPerUnit         float64            `json:"quota_per_unit,omitempty"`
	VideoResolution      string             `json:"video_resolution,omitempty"`
	VideoTokenBilling    bool               `json:"video_token_billing,omitempty"`
	EstimatedTextTokens  int                `json:"estimated_text_tokens,omitempty"`
	EstimatedVideoTokens int                `json:"estimated_video_tokens,omitempty"`
	SettlementStartedAt  int64              `json:"settlement_started_at,omitempty"`
	SettlementSource     string             `json:"settlement_source,omitempty"`
	// Seedance-only snapshot for settle-time OtherRatio correction. Other channels leave unset.
	VideoHasInput *bool `json:"video_has_input,omitempty"`
}

// GetUpstreamTaskID 获取上游真实 task ID（用于与 provider 通信）
// 旧数据没有 UpstreamTaskID 时，TaskID 本身就是上游 ID
func (t *Task) GetUpstreamTaskID() string {
	if t.PrivateData.UpstreamTaskID != "" {
		return t.PrivateData.UpstreamTaskID
	}
	return t.TaskID
}

// GetResultURL 获取任务结果 URL（视频地址等）
// 新数据存在 PrivateData.ResultURL 中；旧数据回退到 FailReason（历史兼容）
func (t *Task) GetResultURL() string {
	if t.PrivateData.ResultURL != "" {
		return t.PrivateData.ResultURL
	}
	return t.FailReason
}

// GetUpstreamResultURL returns the actual provider URL used by the
// authenticated content proxy. Older tasks stored that URL in ResultURL.
func (t *Task) GetUpstreamResultURL() string {
	if t.PrivateData.UpstreamResultURL != "" {
		return t.PrivateData.UpstreamResultURL
	}
	return t.GetResultURL()
}

// GenerateTaskID 生成对外暴露的 task_xxxx 格式 ID
func GenerateTaskID() string {
	key, _ := common.GenerateRandomCharsKey(32)
	return "task_" + key
}

func (p *TaskPrivateData) Scan(val interface{}) error {
	return scanJSONDatabaseValue(val, p)
}

func (p TaskPrivateData) Value() (driver.Value, error) {
	if (p == TaskPrivateData{}) {
		return nil, nil
	}
	return marshalJSONDatabaseValue(p)
}

// SyncTaskQueryParams 用于包含所有搜索条件的结构体，可以根据需求添加更多字段
type SyncTaskQueryParams struct {
	Platform       constant.TaskPlatform
	ChannelID      string
	TaskID         string
	UserID         string
	Action         string
	Status         string
	StartTimestamp int64
	EndTimestamp   int64
	UserIDs        []int
}

func InitTask(platform constant.TaskPlatform, relayInfo *commonRelay.RelayInfo) *Task {
	properties := Properties{}
	privateData := TaskPrivateData{}
	if relayInfo != nil && relayInfo.ChannelMeta != nil {
		if relayInfo.ChannelMeta.ChannelType == constant.ChannelTypeGemini ||
			relayInfo.ChannelMeta.ChannelType == constant.ChannelTypeVertexAi {
			privateData.Key = relayInfo.ChannelMeta.ApiKey
		}
		if relayInfo.UpstreamModelName != "" {
			properties.UpstreamModelName = relayInfo.UpstreamModelName
		}
		if relayInfo.OriginModelName != "" {
			properties.OriginModelName = relayInfo.OriginModelName
		}
	}

	// 使用预生成的公开 ID（如果有），否则新生成
	taskID := ""
	if relayInfo.TaskRelayInfo != nil && relayInfo.TaskRelayInfo.PublicTaskID != "" {
		taskID = relayInfo.TaskRelayInfo.PublicTaskID
	} else {
		taskID = GenerateTaskID()
	}

	t := &Task{
		TaskID:      taskID,
		UserId:      relayInfo.UserId,
		Group:       relayInfo.UsingGroup,
		SubmitTime:  time.Now().Unix(),
		Status:      TaskStatusNotStart,
		Progress:    "0%",
		ChannelId:   relayInfo.ChannelId,
		Platform:    platform,
		Properties:  properties,
		PrivateData: privateData,
	}
	return t
}

func TaskGetAllUserTask(userId int, startIdx int, num int, queryParams SyncTaskQueryParams) ([]*Task, error) {
	var tasks []*Task
	var err error

	// 初始化查询构建器
	query := DB.Where("user_id = ?", userId)

	if queryParams.TaskID != "" {
		query = query.Where("task_id = ?", queryParams.TaskID)
	}
	if queryParams.Action != "" {
		query = query.Where("action = ?", queryParams.Action)
	}
	if queryParams.Status != "" {
		query = query.Where("status = ?", queryParams.Status)
	}
	if queryParams.Platform != "" {
		query = query.Where("platform = ?", queryParams.Platform)
	}
	if queryParams.StartTimestamp != 0 {
		// 假设您已将前端传来的时间戳转换为数据库所需的时间格式，并处理了时间戳的验证和解析
		query = query.Where("submit_time >= ?", queryParams.StartTimestamp)
	}
	if queryParams.EndTimestamp != 0 {
		query = query.Where("submit_time <= ?", queryParams.EndTimestamp)
	}

	// 获取数据
	err = query.Omit("channel_id").Order("id desc").Limit(num).Offset(startIdx).Find(&tasks).Error
	if err != nil {
		return nil, err
	}

	return tasks, nil
}

func TaskGetAllTasks(startIdx int, num int, queryParams SyncTaskQueryParams) ([]*Task, error) {
	var tasks []*Task
	var err error

	// 初始化查询构建器
	query := DB

	// 添加过滤条件
	if queryParams.ChannelID != "" {
		query = query.Where("channel_id = ?", queryParams.ChannelID)
	}
	if queryParams.Platform != "" {
		query = query.Where("platform = ?", queryParams.Platform)
	}
	if queryParams.UserID != "" {
		query = query.Where("user_id = ?", queryParams.UserID)
	}
	if len(queryParams.UserIDs) != 0 {
		query = query.Where("user_id in (?)", queryParams.UserIDs)
	}
	if queryParams.TaskID != "" {
		query = query.Where("task_id = ?", queryParams.TaskID)
	}
	if queryParams.Action != "" {
		query = query.Where("action = ?", queryParams.Action)
	}
	if queryParams.Status != "" {
		query = query.Where("status = ?", queryParams.Status)
	}
	if queryParams.StartTimestamp != 0 {
		query = query.Where("submit_time >= ?", queryParams.StartTimestamp)
	}
	if queryParams.EndTimestamp != 0 {
		query = query.Where("submit_time <= ?", queryParams.EndTimestamp)
	}

	// 获取数据
	err = query.Order("id desc").Limit(num).Offset(startIdx).Find(&tasks).Error
	if err != nil {
		return nil, err
	}

	return tasks, nil
}

func GetTimedOutUnfinishedTasks(cutoffUnix int64, limit int) ([]*Task, error) {
	var tasks []*Task
	err := DB.Where("progress != ?", "100%").
		Where("status NOT IN ?", []string{TaskStatusFailure, TaskStatusSuccess, TaskStatusPendingSettlement}).
		Where("submit_time < ?", cutoffUnix).
		Order("submit_time").
		Limit(limit).
		Find(&tasks).Error
	if err != nil {
		return nil, err
	}
	return tasks, nil
}

func GetAllUnFinishSyncTasks(limit int) ([]*Task, error) {
	var tasks []*Task
	var err error
	// get all tasks progress is not 100%
	err = DB.Where("progress != ?", "100%").
		Where("status NOT IN ?", []string{TaskStatusFailure, TaskStatusSuccess, TaskStatusRetrying}).
		Limit(limit).Order("id").Find(&tasks).Error
	if err != nil {
		return nil, err
	}
	return tasks, nil
}

func GetStaleRetryingTasks(cutoffUnix int64, limit int) ([]*Task, error) {
	if limit <= 0 {
		return nil, nil
	}
	var tasks []*Task
	err := DB.Where("status = ?", TaskStatusRetrying).
		Where("updated_at < ?", cutoffUnix).
		Order("updated_at").Limit(limit).Find(&tasks).Error
	return tasks, err
}

func GetByOnlyTaskId(taskId string) (*Task, bool, error) {
	if taskId == "" {
		return nil, false, nil
	}
	var task *Task
	var err error
	err = DB.Where("task_id = ?", taskId).First(&task).Error
	exist, err := RecordExist(err)
	if err != nil {
		return nil, false, err
	}
	return task, exist, err
}

func GetByTaskId(userId int, taskId string) (*Task, bool, error) {
	if taskId == "" {
		return nil, false, nil
	}
	var task *Task
	var err error
	err = DB.Where("user_id = ? and task_id = ?", userId, taskId).
		First(&task).Error
	exist, err := RecordExist(err)
	if err != nil {
		return nil, false, err
	}
	return task, exist, err
}

func GetByTaskIds(userId int, taskIds []any) ([]*Task, error) {
	if len(taskIds) == 0 {
		return nil, nil
	}
	var task []*Task
	var err error
	err = DB.Where("user_id = ? and task_id in (?)", userId, taskIds).
		Find(&task).Error
	if err != nil {
		return nil, err
	}
	return task, nil
}

// ListUserTasksByPlatformCursor implements stable cursor pagination for a
// user's tasks on one provider platform. It intentionally uses the internal
// numeric ID only as the ordering cursor; callers continue to expose TaskID.
func ListUserTasksByPlatformCursor(userID int, platform constant.TaskPlatform, afterID int64, limit int, ascending bool) ([]*Task, bool, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	query := DB.Where("user_id = ? AND platform = ?", userID, platform)
	order := "id desc"
	if ascending {
		order = "id asc"
		if afterID > 0 {
			query = query.Where("id > ?", afterID)
		}
	} else if afterID > 0 {
		query = query.Where("id < ?", afterID)
	}

	var tasks []*Task
	if err := query.Order(order).Limit(limit + 1).Find(&tasks).Error; err != nil {
		return nil, false, err
	}
	hasMore := len(tasks) > limit
	if hasMore {
		tasks = tasks[:limit]
	}
	return tasks, hasMore, nil
}

// ListUserOpenAIVideoTasksByPlatformCursor keeps the OpenAI compatibility
// collection isolated without relying on database-specific JSON operators.
// Properties is decoded by GORM and filtered in Go so SQLite, MySQL and
// PostgreSQL retain identical behavior.
func ListUserOpenAIVideoTasksByPlatformCursor(userID int, platform constant.TaskPlatform, afterID int64, limit int, ascending bool) ([]*Task, bool, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	const batchSize = 100
	cursor := afterID
	matched := make([]*Task, 0, limit+1)
	for len(matched) <= limit {
		query := DB.Where("user_id = ? AND platform = ?", userID, platform)
		order := "id desc"
		if ascending {
			order = "id asc"
			if cursor > 0 {
				query = query.Where("id > ?", cursor)
			}
		} else if cursor > 0 {
			query = query.Where("id < ?", cursor)
		}

		var batch []*Task
		if err := query.Order(order).Limit(batchSize).Find(&batch).Error; err != nil {
			return nil, false, err
		}
		if len(batch) == 0 {
			break
		}
		for _, task := range batch {
			cursor = task.ID
			if task.Properties.OpenAIVideo {
				matched = append(matched, task)
				if len(matched) > limit {
					break
				}
			}
		}
		if len(matched) > limit || len(batch) < batchSize {
			break
		}
	}

	hasMore := len(matched) > limit
	if hasMore {
		matched = matched[:limit]
	}
	return matched, hasMore, nil
}

// ListRecentTaskResultsForArchive returns a bounded, stable batch for the
// per-node video result repair worker. JSON-backed compatibility properties
// are deliberately filtered by the caller to keep this query portable across
// SQLite, MySQL and PostgreSQL.
func ListRecentTaskResultsForArchive(platform constant.TaskPlatform, beforeID, updatedAfter int64, limit int) ([]*Task, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	query := DB.Where(
		"platform = ? AND updated_at >= ? AND status IN ?",
		platform,
		updatedAfter,
		[]TaskStatus{TaskStatusSuccess, TaskStatusPendingSettlement},
	)
	if beforeID > 0 {
		query = query.Where("id < ?", beforeID)
	}
	var tasks []*Task
	if err := query.Order("id desc").Limit(limit).Find(&tasks).Error; err != nil {
		return nil, err
	}
	return tasks, nil
}

func DeleteUserTaskByID(userID int, id int64, platform constant.TaskPlatform) error {
	return DB.Where("id = ? AND user_id = ? AND platform = ?", id, userID, platform).Delete(&Task{}).Error
}

func (Task *Task) Insert() error {
	var err error
	err = DB.Create(Task).Error
	return err
}

type taskSnapshot struct {
	Status     TaskStatus
	Progress   string
	StartTime  int64
	FinishTime int64
	FailReason string
	ResultURL  string
	Data       json.RawMessage
}

func (s taskSnapshot) Equal(other taskSnapshot) bool {
	return s.Status == other.Status &&
		s.Progress == other.Progress &&
		s.StartTime == other.StartTime &&
		s.FinishTime == other.FinishTime &&
		s.FailReason == other.FailReason &&
		s.ResultURL == other.ResultURL &&
		bytes.Equal(s.Data, other.Data)
}

func (t *Task) Snapshot() taskSnapshot {
	return taskSnapshot{
		Status:     t.Status,
		Progress:   t.Progress,
		StartTime:  t.StartTime,
		FinishTime: t.FinishTime,
		FailReason: t.FailReason,
		ResultURL:  t.PrivateData.ResultURL,
		Data:       t.Data,
	}
}

func (Task *Task) Update() error {
	var err error
	err = DB.Save(Task).Error
	return err
}

// UpdateTaskResultFile persists the optional shared-cache reference without
// overwriting task status, billing context, or other concurrent updates.
func UpdateTaskResultFile(taskID int64, resultFile string) error {
	if DB == nil || taskID == 0 {
		return nil
	}
	return DB.Model(&Task{}).Where("id = ?", taskID).Update("result_file", resultFile).Error
}

// UpdateBillingSnapshot persists only fields changed by asynchronous settlement.
// The terminal status transition is already protected by UpdateWithStatus; a
// narrow update here avoids overwriting unrelated concurrent task fields.
func (t *Task) UpdateBillingSnapshot() error {
	if t.ID == 0 {
		return nil
	}
	return DB.Model(&Task{}).Where("id = ?", t.ID).Updates(map[string]any{
		"quota":        t.Quota,
		"private_data": t.PrivateData,
	}).Error
}

// UpdateWithStatus performs a conditional UPDATE guarded by fromStatus (CAS).
// Returns (true, nil) if this caller won the update, (false, nil) if
// another process already moved the task out of fromStatus.
//
// Uses Model().Select("*").Updates() instead of Save() because GORM's Save
// falls back to INSERT ON CONFLICT when the WHERE-guarded UPDATE matches
// zero rows, which silently bypasses the CAS guard.
func (t *Task) UpdateWithStatus(fromStatus TaskStatus) (bool, error) {
	result := DB.Model(t).Where("status = ?", fromStatus).Select("*").Updates(t)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

// TaskBulkUpdate performs an unconditional bulk UPDATE by upstream task_id strings.
// Same caveats as TaskBulkUpdateByID — no CAS guard.
func TaskBulkUpdate(taskIds []string, params map[string]any) error {
	if len(taskIds) == 0 {
		return nil
	}
	return DB.Model(&Task{}).
		Where("task_id in (?)", taskIds).
		Updates(params).Error
}

// TaskBulkUpdateByID performs an unconditional bulk UPDATE by primary key IDs.
// WARNING: This function has NO CAS (Compare-And-Swap) guard — it will overwrite
// any concurrent status changes. DO NOT use in billing/quota lifecycle flows
// (e.g., timeout, success, failure transitions that trigger refunds or settlements).
// For status transitions that involve billing, use Task.UpdateWithStatus() instead.
func TaskBulkUpdateByID(ids []int64, params map[string]any) error {
	if len(ids) == 0 {
		return nil
	}
	return DB.Model(&Task{}).
		Where("id in (?)", ids).
		Updates(params).Error
}

type TaskQuotaUsage struct {
	Mode  string  `json:"mode"`
	Count float64 `json:"count"`
}

// TaskCountAllTasks returns total tasks that match the given query params (admin usage)
func TaskCountAllTasks(queryParams SyncTaskQueryParams) (int64, error) {
	var total int64
	query := DB.Model(&Task{})
	if queryParams.ChannelID != "" {
		query = query.Where("channel_id = ?", queryParams.ChannelID)
	}
	if queryParams.Platform != "" {
		query = query.Where("platform = ?", queryParams.Platform)
	}
	if queryParams.UserID != "" {
		query = query.Where("user_id = ?", queryParams.UserID)
	}
	if len(queryParams.UserIDs) != 0 {
		query = query.Where("user_id in (?)", queryParams.UserIDs)
	}
	if queryParams.TaskID != "" {
		query = query.Where("task_id = ?", queryParams.TaskID)
	}
	if queryParams.Action != "" {
		query = query.Where("action = ?", queryParams.Action)
	}
	if queryParams.Status != "" {
		query = query.Where("status = ?", queryParams.Status)
	}
	if queryParams.StartTimestamp != 0 {
		query = query.Where("submit_time >= ?", queryParams.StartTimestamp)
	}
	if queryParams.EndTimestamp != 0 {
		query = query.Where("submit_time <= ?", queryParams.EndTimestamp)
	}
	if err := query.Count(&total).Error; err != nil {
		return 0, err
	}
	return total, nil
}

// TaskCountAllUserTask returns total tasks for given user
func TaskCountAllUserTask(userId int, queryParams SyncTaskQueryParams) (int64, error) {
	var total int64
	query := DB.Model(&Task{}).Where("user_id = ?", userId)
	if queryParams.TaskID != "" {
		query = query.Where("task_id = ?", queryParams.TaskID)
	}
	if queryParams.Action != "" {
		query = query.Where("action = ?", queryParams.Action)
	}
	if queryParams.Status != "" {
		query = query.Where("status = ?", queryParams.Status)
	}
	if queryParams.Platform != "" {
		query = query.Where("platform = ?", queryParams.Platform)
	}
	if queryParams.StartTimestamp != 0 {
		query = query.Where("submit_time >= ?", queryParams.StartTimestamp)
	}
	if queryParams.EndTimestamp != 0 {
		query = query.Where("submit_time <= ?", queryParams.EndTimestamp)
	}
	if err := query.Count(&total).Error; err != nil {
		return 0, err
	}
	return total, nil
}
func (t *Task) ToOpenAIVideo() *dto.OpenAIVideo {
	openAIVideo := dto.NewOpenAIVideo()
	openAIVideo.ID = t.TaskID
	openAIVideo.Status = t.Status.ToVideoStatus()
	openAIVideo.Model = t.Properties.OriginModelName
	openAIVideo.SetProgressStr(t.Progress)
	openAIVideo.CreatedAt = t.CreatedAt
	openAIVideo.CompletedAt = t.UpdatedAt
	openAIVideo.SetMetadata("url", t.GetResultURL())
	return openAIVideo
}
