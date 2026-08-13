package service

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

func seedDurableWalletTask(t *testing.T, taskStatus model.TaskStatus, reserved int) (*model.Task, *model.BillingTransaction) {
	t.Helper()
	userID, tokenID, channelID := 711, 712, 713
	seedUser(t, userID, 10_000-reserved)
	seedToken(t, tokenID, userID, "durable-token", 10_000-reserved)
	seedChannel(t, channelID)
	task := &model.Task{
		TaskID: "task_durable", UserId: userID, ChannelId: channelID, Quota: reserved,
		Status: taskStatus, Progress: "30%", BillingState: "",
		PrivateData: model.TaskPrivateData{TokenId: tokenID, BillingSource: BillingSourceWallet,
			BillingContext: &model.TaskBillingContext{PreAuthorization: true, VideoTokenBilling: true, ModelRatio: 1, GroupRatio: 1}},
	}
	require.NoError(t, model.DB.Create(task).Error)
	row := &model.BillingTransaction{
		BusinessKey: "request:durable-test", TaskDBID: task.ID, UserID: userID, TokenID: tokenID,
		FundingSource: BillingSourceWallet, ReservedQuota: reserved, FinalQuota: reserved,
		State: model.BillingTransactionStateReserved,
	}
	require.NoError(t, model.DB.Create(row).Error)
	return task, row
}

func TestCreateTaskBillingIntentFailureRefundsOnce(t *testing.T) {
	truncate(t)
	task, row := seedDurableWalletTask(t, model.TaskStatusInProgress, 1_000)
	task.Status = model.TaskStatusFailure
	task.Progress = "100%"
	task.FailReason = "provider failed"

	won, err := CreateTaskBillingIntent(task, model.TaskStatusInProgress, 0, nil, task.FailReason)
	require.NoError(t, err)
	require.True(t, won)
	require.NoError(t, ProcessDurableBillingOnce(context.Background(), 10))
	require.NoError(t, ProcessDurableBillingOnce(context.Background(), 10))

	var user model.User
	require.NoError(t, model.DB.First(&user, task.UserId).Error)
	require.Equal(t, 10_000, user.Quota)
	var token model.Token
	require.NoError(t, model.DB.First(&token, task.PrivateData.TokenId).Error)
	require.Equal(t, 10_000, token.RemainQuota)
	var reloaded model.BillingTransaction
	require.NoError(t, model.DB.First(&reloaded, row.ID).Error)
	require.Equal(t, model.BillingTransactionStateRefunded, reloaded.State)
	var refreshed model.Task
	require.NoError(t, model.DB.First(&refreshed, task.ID).Error)
	require.Zero(t, refreshed.Quota)
	require.Equal(t, model.BillingTransactionStateRefunded, refreshed.BillingState)
}

func TestExpiredApplyingBillingLeaseIsRecovered(t *testing.T) {
	truncate(t)
	task, row := seedDurableWalletTask(t, model.TaskStatusFailure, 750)
	task.BillingState = model.BillingTransactionStateRefundPending
	require.NoError(t, model.DB.Model(task).Update("billing_state", task.BillingState).Error)
	require.NoError(t, model.DB.Model(row).Updates(map[string]any{
		"state": model.BillingTransactionStateApplying, "target_task_status": string(model.TaskStatusFailure),
		"final_quota": 0, "lease_until": common.GetTimestamp() - 1,
	}).Error)

	require.NoError(t, ProcessDurableBillingOnce(context.Background(), 10))
	var reloaded model.BillingTransaction
	require.NoError(t, model.DB.First(&reloaded, row.ID).Error)
	require.Equal(t, model.BillingTransactionStateRefunded, reloaded.State)
}

func TestDurableSuccessCreatesIdempotentLogOutbox(t *testing.T) {
	truncate(t)
	task, row := seedDurableWalletTask(t, model.TaskStatusInProgress, 1_000)
	task.Status = model.TaskStatusSuccess
	task.Progress = "100%"
	result := &relaycommon.TaskInfo{Status: string(model.TaskStatusSuccess), TotalTokens: 800, CompletionTokens: 800}
	won, err := CreateTaskBillingIntent(task, model.TaskStatusInProgress, 800, result, "")
	require.NoError(t, err)
	require.True(t, won)
	require.NoError(t, ProcessDurableBillingOnce(context.Background(), 10))
	require.NoError(t, ProcessBillingOutboxOnce(context.Background(), 10))
	require.NoError(t, ProcessBillingOutboxOnce(context.Background(), 10))

	var logs int64
	require.NoError(t, model.LOG_DB.Model(&model.Log{}).Where("type = ?", model.LogTypeConsume).Count(&logs).Error)
	require.EqualValues(t, 1, logs)
	var billing model.BillingTransaction
	require.NoError(t, model.DB.First(&billing, row.ID).Error)
	require.Equal(t, model.BillingTransactionStateSettled, billing.State)
}

func TestUnboundDurableRefundRecoversWithoutTask(t *testing.T) {
	truncate(t)
	userID, tokenID := 721, 722
	seedUser(t, userID, 9_500)
	seedToken(t, tokenID, userID, "unbound-token", 9_500)
	row := &model.BillingTransaction{
		BusinessKey: "request:unbound-test", UserID: userID, TokenID: tokenID,
		FundingSource: BillingSourceWallet, ReservedQuota: 500, FinalQuota: 0,
		TargetTaskStatus: string(model.TaskStatusFailure), State: model.BillingTransactionStateRefundPending,
	}
	require.NoError(t, model.DB.Create(row).Error)
	require.NoError(t, ProcessDurableBillingOnce(context.Background(), 10))

	var user model.User
	require.NoError(t, model.DB.First(&user, userID).Error)
	require.Equal(t, 10_000, user.Quota)
	var token model.Token
	require.NoError(t, model.DB.First(&token, tokenID).Error)
	require.Equal(t, 10_000, token.RemainQuota)
	require.NoError(t, model.DB.First(row, row.ID).Error)
	require.Equal(t, model.BillingTransactionStateRefunded, row.State)
}

func TestPlaygroundDurableRefundDoesNotCreditToken(t *testing.T) {
	truncate(t)
	userID, tokenID := 731, 732
	seedUser(t, userID, 9_500)
	seedToken(t, tokenID, userID, "playground-token", 10_000)
	row := &model.BillingTransaction{
		BusinessKey: "request:playground-test", UserID: userID, TokenID: tokenID, SkipTokenQuota: true,
		FundingSource: BillingSourceWallet, ReservedQuota: 500, FinalQuota: 0,
		TargetTaskStatus: string(model.TaskStatusFailure), State: model.BillingTransactionStateRefundPending,
	}
	require.NoError(t, model.DB.Create(row).Error)
	require.NoError(t, ProcessDurableBillingOnce(context.Background(), 10))

	var token model.Token
	require.NoError(t, model.DB.First(&token, tokenID).Error)
	require.Equal(t, 10_000, token.RemainQuota)
	require.Zero(t, token.UsedQuota)
}
