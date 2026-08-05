package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
)

// LogTaskConsumption 记录任务消费日志和统计信息（仅记录，不涉及实际扣费）。
// 实际扣费已由 BillingSession（PreConsumeBilling + SettleBilling）完成。
func LogTaskConsumption(c *gin.Context, info *relaycommon.RelayInfo) int {
	tokenName := c.GetString("token_name")
	logContent := fmt.Sprintf("操作 %s", info.Action)
	// 支持任务仅按次计费
	if common.StringsContains(constant.TaskPricePatches, info.OriginModelName) {
		logContent = fmt.Sprintf("%s，按次计费", logContent)
	} else {
		if len(info.PriceData.OtherRatios) > 0 {
			var contents []string
			for key, ra := range info.PriceData.OtherRatios {
				if 1.0 != ra {
					contents = append(contents, fmt.Sprintf("%s: %.2f", key, ra))
				}
			}
			if len(contents) > 0 {
				logContent = fmt.Sprintf("%s, 计算参数：%s", logContent, strings.Join(contents, ", "))
			}
		}
	}
	other := make(map[string]interface{})
	other["is_task"] = true
	other["request_path"] = c.Request.URL.Path
	other["task_id"] = info.PublicTaskID
	other["model_price"] = info.PriceData.ModelPrice
	if info.PriceData.ModelRatio > 0 {
		other["model_ratio"] = info.PriceData.ModelRatio
	}
	other["group_ratio"] = info.PriceData.GroupRatioInfo.GroupRatio
	if info.PriceData.GroupRatioInfo.HasSpecialRatio {
		other["user_group_ratio"] = info.PriceData.GroupRatioInfo.GroupSpecialRatio
	}
	if info.IsModelMapped {
		other["is_model_mapped"] = true
		other["upstream_model_name"] = info.UpstreamModelName
	}
	attachQuotaSaturation(c, info, other)
	logID := model.RecordConsumeLogWithID(c, info.UserId, model.RecordConsumeLogParams{
		ChannelId: info.ChannelId,
		ModelName: info.OriginModelName,
		TokenName: tokenName,
		Quota:     info.PriceData.Quota,
		Content:   logContent,
		TokenId:   info.TokenId,
		Group:     info.UsingGroup,
		Other:     other,
	})
	model.UpdateUserUsedQuotaAndRequestCount(info.UserId, info.PriceData.Quota)
	model.UpdateChannelUsedQuota(info.ChannelId, info.PriceData.Quota)
	return logID
}

// ---------------------------------------------------------------------------
// 异步任务计费辅助函数
// ---------------------------------------------------------------------------

// resolveTokenKey 通过 TokenId 运行时获取令牌 Key（用于 Redis 缓存操作）。
// 如果令牌已被删除或查询失败，返回空字符串。
func resolveTokenKey(ctx context.Context, tokenId int, taskID string) string {
	token, err := model.GetTokenById(tokenId)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("获取令牌 key 失败 (tokenId=%d, task=%s): %s", tokenId, taskID, err.Error()))
		return ""
	}
	return token.Key
}

// taskIsSubscription 判断任务是否通过订阅计费。
func taskIsSubscription(task *model.Task) bool {
	return task.PrivateData.BillingSource == BillingSourceSubscription && task.PrivateData.SubscriptionId > 0
}

func taskUsesPreAuthorization(task *model.Task) bool {
	return task != nil && task.PrivateData.BillingContext != nil && task.PrivateData.BillingContext.PreAuthorization
}

// taskAdjustFunding 调整任务的资金来源（钱包或订阅），delta > 0 表示扣费，delta < 0 表示退还。
func taskAdjustFunding(task *model.Task, delta int) error {
	if taskIsSubscription(task) {
		return model.PostConsumeUserSubscriptionDelta(task.PrivateData.SubscriptionId, int64(delta))
	}
	if delta > 0 {
		return model.DecreaseUserQuota(task.UserId, delta, false)
	}
	return model.IncreaseUserQuota(task.UserId, -delta, false)
}

// taskAdjustTokenQuota 调整任务的令牌额度，delta > 0 表示扣费，delta < 0 表示退还。
// 需要通过 resolveTokenKey 运行时获取 key（不从 PrivateData 中读取）。
func taskAdjustTokenQuota(ctx context.Context, task *model.Task, delta int) {
	if task.PrivateData.TokenId <= 0 || delta == 0 {
		return
	}
	tokenKey := resolveTokenKey(ctx, task.PrivateData.TokenId, task.TaskID)
	if tokenKey == "" {
		return
	}
	var err error
	if delta > 0 {
		err = model.DecreaseTokenQuota(task.PrivateData.TokenId, tokenKey, delta)
	} else {
		err = model.IncreaseTokenQuota(task.PrivateData.TokenId, tokenKey, -delta)
	}
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("调整令牌额度失败 (delta=%d, task=%s): %s", delta, task.TaskID, err.Error()))
	}
}

// taskBillingOther 从 task 的 BillingContext 构建日志 Other 字段。
func taskBillingOther(task *model.Task) map[string]interface{} {
	other := make(map[string]interface{})
	if bc := task.PrivateData.BillingContext; bc != nil {
		other["model_price"] = bc.ModelPrice
		if bc.ModelRatio > 0 {
			other["model_ratio"] = bc.ModelRatio
		}
		other["group_ratio"] = bc.GroupRatio
		if bc.RequestPath != "" {
			other["request_path"] = bc.RequestPath
		}
		if bc.PreAuthorizedQuota > 0 {
			other["pre_authorized_quota"] = bc.PreAuthorizedQuota
		}
		if bc.QuotaPerUnit > 0 {
			other["quota_per_unit"] = bc.QuotaPerUnit
		}
		if bc.VideoResolution != "" {
			other["video_resolution"] = ratio_setting.NormalizeVideoResolution(bc.VideoResolution)
		}
		if bc.SettlementSource != "" {
			other["video_token_source"] = bc.SettlementSource
		}
		if len(bc.OtherRatios) > 0 {
			for k, v := range bc.OtherRatios {
				other[k] = v
			}
		}
	}
	props := task.Properties
	if props.UpstreamModelName != "" && props.UpstreamModelName != props.OriginModelName {
		other["is_model_mapped"] = true
		other["upstream_model_name"] = props.UpstreamModelName
	}
	return other
}

// taskModelName 从 BillingContext 或 Properties 中获取模型名称。
func taskModelName(task *model.Task) string {
	if bc := task.PrivateData.BillingContext; bc != nil && bc.OriginModelName != "" {
		return bc.OriginModelName
	}
	if task.Properties.OriginModelName != "" {
		return task.Properties.OriginModelName
	}
	var taskData struct {
		Model string `json:"model"`
	}
	if len(task.Data) > 0 && common.Unmarshal(task.Data, &taskData) == nil {
		return taskData.Model
	}
	return ""
}

func tokenPricingContextFromTask(task *model.Task) billing_setting.TokenPricingContext {
	if task == nil {
		return billing_setting.TokenPricingContext{}
	}
	return billing_setting.TokenPricingContext{
		Model:  taskModelName(task),
		Group:  task.Group,
		UserId: task.UserId,
	}
}

// RefundTaskQuota 统一的任务失败退款逻辑。
// 当异步任务失败时，将预扣的 quota 退还给用户（支持钱包和订阅），并退还令牌额度。
func RefundTaskQuota(ctx context.Context, task *model.Task, reason string) {
	quota := task.Quota
	if quota == 0 {
		return
	}

	// 1. 退还资金来源（钱包或订阅）
	if err := taskAdjustFunding(task, -quota); err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("退还资金来源失败 task %s: %s", task.TaskID, err.Error()))
		return
	}

	// 2. 退还令牌额度
	taskAdjustTokenQuota(ctx, task, -quota)
	if taskUsesPreAuthorization(task) {
		task.Quota = 0
		if err := task.UpdateBillingSnapshot(); err != nil {
			logger.LogError(ctx, fmt.Sprintf("persist released task pre-authorization failed task %s: %s", task.TaskID, err.Error()))
		}
		return
	}

	// 3. 记录日志
	other := taskBillingOther(task)
	other["task_id"] = task.TaskID
	other["reason"] = reason
	model.RecordTaskBillingLog(model.RecordTaskBillingLogParams{
		UserId:    task.UserId,
		LogType:   model.LogTypeRefund,
		Content:   "",
		ChannelId: task.ChannelId,
		ModelName: taskModelName(task),
		Quota:     quota,
		TokenId:   task.PrivateData.TokenId,
		Group:     task.Group,
		Other:     other,
	})
}

// RecalculateTaskQuota 通用的异步差额结算。
// actualQuota 是任务完成后的实际应扣额度，与预扣额度 (task.Quota) 做差额结算。
// reason 用于日志记录（例如 "token重算" 或 "adaptor调整"）。
func RecalculateTaskQuota(ctx context.Context, task *model.Task, actualQuota int, reason string, clamps ...*common.QuotaClamp) {
	RecalculateTaskQuotaWithOther(ctx, task, actualQuota, reason, nil, clamps...)
}

func RecalculateTaskQuotaWithOther(ctx context.Context, task *model.Task, actualQuota int, reason string, extraOther map[string]interface{}, clamps ...*common.QuotaClamp) {
	if actualQuota <= 0 {
		return
	}
	preConsumedQuota := task.Quota
	quotaDelta := actualQuota - preConsumedQuota

	if quotaDelta == 0 {
		logger.LogInfo(ctx, fmt.Sprintf("任务 %s 预扣费准确（%s，%s）",
			task.TaskID, logger.LogQuota(actualQuota), reason))
		task.Quota = actualQuota
		return
	}

	logger.LogInfo(ctx, fmt.Sprintf("任务 %s 差额结算：delta=%s（实际：%s，预扣：%s，%s）",
		task.TaskID,
		logger.LogQuota(quotaDelta),
		logger.LogQuota(actualQuota),
		logger.LogQuota(preConsumedQuota),
		reason,
	))

	// 调整资金来源
	if err := taskAdjustFunding(task, quotaDelta); err != nil {
		logger.LogError(ctx, fmt.Sprintf("差额结算资金调整失败 task %s: %s", task.TaskID, err.Error()))
		return
	}

	// 调整令牌额度
	taskAdjustTokenQuota(ctx, task, quotaDelta)

	task.Quota = actualQuota
	if err := task.UpdateBillingSnapshot(); err != nil {
		logger.LogError(ctx, fmt.Sprintf("persist task billing snapshot failed task %s: %s", task.TaskID, err.Error()))
	}

	if taskUsesPreAuthorization(task) {
		return
	}

	var logType int
	var logQuota int
	if quotaDelta > 0 {
		logType = model.LogTypeConsume
		logQuota = quotaDelta
		model.UpdateUserUsedQuota(task.UserId, quotaDelta)
		model.UpdateChannelUsedQuota(task.ChannelId, quotaDelta)
	} else {
		logType = model.LogTypeRefund
		logQuota = -quotaDelta
	}
	other := taskBillingOther(task)
	other["task_id"] = task.TaskID
	other["pre_consumed_quota"] = preConsumedQuota
	other["actual_quota"] = actualQuota
	for key, value := range extraOther {
		other[key] = value
	}
	for _, clamp := range clamps {
		attachQuotaSaturationToOther(other, clamp)
	}
	model.RecordTaskBillingLog(model.RecordTaskBillingLogParams{
		UserId:    task.UserId,
		LogType:   logType,
		Content:   reason,
		ChannelId: task.ChannelId,
		ModelName: taskModelName(task),
		Quota:     logQuota,
		TokenId:   task.PrivateData.TokenId,
		Group:     task.Group,
		Other:     other,
	})
}

// RecalculateTaskQuotaByTokens 根据实际 token 消耗重新计费（异步差额结算）。
// 当任务成功且返回了 totalTokens 时，根据模型倍率和分组倍率重新计算实际扣费额度，
// 与预扣费的差额进行补扣或退还。支持钱包和订阅计费来源。
func RecalculateTaskQuotaByTokens(ctx context.Context, task *model.Task, totalTokens int) {
	RecalculateTaskQuotaByTokenDetails(ctx, task, totalTokens, 0)
}

// RecalculateTaskQuotaByTokenDetails 根据实际 token 明细重新计费。completionTokens
// 缺失时将 totalTokens 视为输入 token，避免凭空猜测输出占比。
func RecalculateTaskQuotaByTokenDetails(ctx context.Context, task *model.Task, totalTokens int, completionTokens int) {
	if totalTokens <= 0 {
		return
	}
	if completionTokens < 0 {
		completionTokens = 0
	}
	if completionTokens > totalTokens {
		completionTokens = totalTokens
	}
	promptTokens := totalTokens - completionTokens
	tokenPricingCtx := tokenPricingContextFromTask(task)
	billingPromptTokens := billing_setting.ApplyInputTokenPricingForContext(promptTokens, tokenPricingCtx)
	billingCompletionTokens := billing_setting.ApplyOutputTokenPricingForContext(completionTokens, tokenPricingCtx)
	billingTotalTokens := billingPromptTokens + billingCompletionTokens

	modelName := taskModelName(task)

	// 获取模型价格和倍率
	modelRatio, hasRatioSetting, _ := ratio_setting.GetModelRatio(modelName)
	if bc := task.PrivateData.BillingContext; bc != nil && bc.VideoTokenBilling && bc.ModelRatio > 0 {
		modelRatio = bc.ModelRatio
		hasRatioSetting = true
	}
	// 只有配置了倍率(非固定价格)时才按 token 重新计费
	if !hasRatioSetting || modelRatio <= 0 {
		return
	}

	// 获取用户和组的倍率信息
	group := task.Group
	if group == "" {
		user, err := model.GetUserById(task.UserId, false)
		if err == nil {
			group = user.Group
		}
	}
	if group == "" {
		return
	}

	groupRatio := ratio_setting.GetGroupRatio(group)
	userGroupRatio, hasUserGroupRatio := ratio_setting.GetGroupGroupRatio(group, group)

	var finalGroupRatio float64
	if hasUserGroupRatio {
		finalGroupRatio = userGroupRatio
	} else {
		finalGroupRatio = groupRatio
	}
	if bc := task.PrivateData.BillingContext; bc != nil && bc.VideoTokenBilling {
		if bc.ModelRatio > 0 {
			modelRatio = bc.ModelRatio
		}
		if bc.GroupRatio > 0 {
			finalGroupRatio = bc.GroupRatio
		}
	}

	// 计算 OtherRatios 乘积（视频折扣、时长等）
	otherMultiplier := 1.0
	if bc := task.PrivateData.BillingContext; bc != nil {
		for _, r := range bc.OtherRatios {
			if r != 1.0 && r > 0 {
				otherMultiplier *= r
			}
		}
	}

	extraOther := map[string]interface{}{}
	// 视频 Token 渠道的上游 completion_tokens 是实际视频 Token。清晰度/视频输入
	// 只影响视频 Token 单价，文本 Token 始终使用模型基础单价。
	actualQuota, clamp := common.QuotaFromFloatChecked(float64(billingTotalTokens) * modelRatio * finalGroupRatio * otherMultiplier)
	if bc := task.PrivateData.BillingContext; bc != nil && bc.VideoTokenBilling && completionTokens > 0 {
		videoMultiplier := 1.0
		if ratio := bc.OtherRatios["video_input"]; ratio > 0 {
			videoMultiplier = ratio
		} else if ratio := bc.OtherRatios["size"]; ratio > 0 {
			videoMultiplier = ratio
		}
		textQuota, textClamp := common.QuotaFromFloatChecked(float64(billingPromptTokens) * modelRatio * finalGroupRatio)
		videoQuota, videoClamp := common.QuotaFromFloatChecked(float64(billingCompletionTokens) * modelRatio * finalGroupRatio * videoMultiplier)
		actualQuota = textQuota + videoQuota
		clamp = textClamp
		if clamp == nil {
			clamp = videoClamp
		}
		extraOther["video_enabled"] = true
		extraOther["text_tokens"] = promptTokens
		extraOther["text_quota"] = textQuota
		extraOther["video_tokens"] = completionTokens
		extraOther["video_quota"] = videoQuota
		extraOther["video_unit_price"] = modelRatio * 2 * videoMultiplier
		condition := "base"
		if bc.VideoHasInput != nil && *bc.VideoHasInput {
			condition = "with_video"
		}
		resolution := ratio_setting.NormalizeVideoResolution(bc.VideoResolution)
		if resolution == "" {
			resolution = "default"
		}
		extraOther["video_price_tier"] = resolution + "_" + condition
	}

	reason := fmt.Sprintf("token重算：tokens=%d, modelRatio=%.2f, groupRatio=%.2f, otherMultiplier=%.4f", billingTotalTokens, modelRatio, finalGroupRatio, otherMultiplier)
	if tokenPricing := billing_setting.GetEffectiveTokenPricing(tokenPricingCtx); tokenPricing.Enabled {
		reason = fmt.Sprintf("%s, token定价已应用", reason)
		extraOther["token_pricing_enabled"] = true
		extraOther["token_pricing_input_ratio"] = tokenPricing.InputRatio
		extraOther["token_pricing_output_ratio"] = tokenPricing.OutputRatio
		if len(tokenPricing.RuleNames) > 0 {
			extraOther["token_pricing_rules"] = tokenPricing.RuleNames
		}
		extraOther["raw_prompt_tokens"] = promptTokens
		extraOther["raw_completion_tokens"] = completionTokens
		extraOther["raw_total_tokens"] = totalTokens
		extraOther["billing_prompt_tokens"] = billingPromptTokens
		extraOther["billing_completion_tokens"] = billingCompletionTokens
		extraOther["billing_total_tokens"] = billingTotalTokens
	}
	RecalculateTaskQuotaWithOther(ctx, task, actualQuota, reason, extraOther, clamp)
}

// FinalizeTaskPreAuthorization writes the sole user-visible consumption record
// after an async task succeeds. Pricing fields are calculated locally; the
// upstream contributes only output resolution and actual token usage.
func FinalizeTaskPreAuthorization(ctx context.Context, task *model.Task, taskResult *relaycommon.TaskInfo) {
	if !taskUsesPreAuthorization(task) || taskResult == nil {
		return
	}
	bc := task.PrivateData.BillingContext
	other := taskBillingOther(task)
	other["is_task"] = true
	other["task_id"] = task.TaskID
	other["actual_quota"] = task.Quota

	promptTokens := taskResult.TotalTokens - taskResult.CompletionTokens
	if promptTokens < 0 {
		promptTokens = 0
	}
	completionTokens := taskResult.CompletionTokens
	if bc.VideoTokenBilling && completionTokens > 0 {
		videoMultiplier := 1.0
		if ratio := bc.OtherRatios["video_input"]; ratio > 0 {
			videoMultiplier = ratio
		} else if ratio := bc.OtherRatios["size"]; ratio > 0 {
			videoMultiplier = ratio
		}
		textQuota, _ := common.QuotaFromFloatChecked(float64(promptTokens) * bc.ModelRatio * bc.GroupRatio)
		videoQuota, _ := common.QuotaFromFloatChecked(float64(completionTokens) * bc.ModelRatio * bc.GroupRatio * videoMultiplier)
		other["video_enabled"] = true
		other["text_tokens"] = promptTokens
		other["text_quota"] = textQuota
		other["video_tokens"] = completionTokens
		other["video_quota"] = videoQuota
		other["video_unit_price"] = bc.ModelRatio * 2 * videoMultiplier
		condition := "base"
		if bc.VideoHasInput != nil && *bc.VideoHasInput {
			condition = "with_video"
		}
		resolution := ratio_setting.NormalizeVideoResolution(bc.VideoResolution)
		if resolution == "" {
			resolution = "default"
		}
		other["video_price_tier"] = resolution + "_" + condition
	}

	startTime := task.StartTime
	if startTime <= 0 {
		startTime = task.SubmitTime
	}
	useTimeSeconds := 0
	var useTimeMilliseconds *int64
	if startTime > 0 && task.FinishTime >= startTime {
		useTimeSeconds = int(task.FinishTime - startTime)
		ms := int64(useTimeSeconds) * 1000
		useTimeMilliseconds = &ms
	}
	logID := model.RecordTaskBillingLog(model.RecordTaskBillingLogParams{
		UserId: task.UserId, LogType: model.LogTypeConsume,
		Content: fmt.Sprintf("操作 %s", task.Action), ChannelId: task.ChannelId,
		ModelName: taskModelName(task), Quota: task.Quota, TokenId: task.PrivateData.TokenId,
		Group: task.Group, PromptTokens: promptTokens, CompletionTokens: completionTokens,
		UseTimeSeconds: useTimeSeconds, UseTimeMilliseconds: useTimeMilliseconds, Other: other,
	})
	if logID <= 0 {
		return
	}
	task.PrivateData.ConsumeLogID = logID
	if err := task.UpdateBillingSnapshot(); err != nil {
		logger.LogError(ctx, fmt.Sprintf("persist final task billing log id failed task %s: %s", task.TaskID, err.Error()))
	}
	model.UpdateUserUsedQuotaAndRequestCount(task.UserId, task.Quota)
	model.UpdateChannelUsedQuota(task.ChannelId, task.Quota)
}
