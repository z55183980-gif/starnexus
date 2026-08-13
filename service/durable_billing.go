package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func durableBillingBusinessKey(info *relaycommon.RelayInfo) string {
	key := strings.TrimSpace(info.RequestId)
	if key != "" {
		if len(key) > 183 {
			key = common.GenerateHMAC(key)
		}
		return "request:" + key
	}
	random, _ := common.GenerateRandomCharsKey(32)
	return "request:fallback:" + random
}

func durableSubscriptionRequestID(info *relaycommon.RelayInfo) string {
	requestID := strings.TrimSpace(info.RequestId)
	if requestID == "" {
		requestID, _ = common.GenerateRandomCharsKey(32)
	}
	if len(requestID) > 64 {
		return common.GenerateHMAC(requestID)
	}
	return requestID
}

func newDurableBillingSession(c *gin.Context, info *relaycommon.RelayInfo, quota int, preference string) (*BillingSession, *types.NewAPIError) {
	businessKey := durableBillingBusinessKey(info)
	tryWallet := func() (*BillingSession, error) {
		row := &model.BillingTransaction{
			BusinessKey: businessKey, UserID: info.UserId, TokenID: info.TokenId,
			SkipTokenQuota: info.IsPlayground,
			FundingSource:  BillingSourceWallet, ReservedQuota: quota, FinalQuota: quota,
			State: model.BillingTransactionStateReserved,
		}
		err := model.DB.Transaction(func(tx *gorm.DB) error {
			if err := model.ReserveWalletAndTokenTx(tx, info.UserId, info.TokenId, quota, info.IsPlayground); err != nil {
				return err
			}
			return model.CreateReservedBillingTransactionTx(tx, row)
		})
		if err != nil {
			return nil, err
		}
		info.UserQuota -= quota
		return durableSessionFromRow(info, row, quota, &WalletFunding{userId: info.UserId, consumed: quota}), nil
	}
	trySubscription := func() (*BillingSession, error) {
		subscriptionRequestID := durableSubscriptionRequestID(info)
		row := &model.BillingTransaction{
			BusinessKey: businessKey, UserID: info.UserId, TokenID: info.TokenId,
			SkipTokenQuota: info.IsPlayground,
			FundingSource:  BillingSourceSubscription, ReservedQuota: quota, FinalQuota: quota,
			State: model.BillingTransactionStateReserved,
		}
		var subResult *model.SubscriptionPreConsumeResult
		err := model.DB.Transaction(func(tx *gorm.DB) error {
			var err error
			subResult, err = model.ReserveSubscriptionAndTokenTx(tx, subscriptionRequestID, info.UserId, info.TokenId, info.OriginModelName, quota, info.IsPlayground)
			if err != nil {
				return err
			}
			row.SubscriptionID = subResult.UserSubscriptionId
			return model.CreateReservedBillingTransactionTx(tx, row)
		})
		if err != nil {
			return nil, err
		}
		funding := &SubscriptionFunding{
			requestId: subscriptionRequestID, userId: info.UserId, modelName: info.OriginModelName,
			amount: int64(quota), subscriptionId: subResult.UserSubscriptionId,
			preConsumed: subResult.PreConsumed, AmountTotal: subResult.AmountTotal,
			AmountUsedAfter: subResult.AmountUsedAfter,
		}
		return durableSessionFromRow(info, row, quota, funding), nil
	}

	choose := func(subscriptionFirst bool, only bool) (*BillingSession, error) {
		if subscriptionFirst {
			s, err := trySubscription()
			if err == nil || only {
				return s, err
			}
			return tryWallet()
		}
		s, err := tryWallet()
		if err == nil || only {
			return s, err
		}
		return trySubscription()
	}

	var session *BillingSession
	var err error
	switch preference {
	case "wallet_only":
		session, err = choose(false, true)
	case "subscription_only":
		session, err = choose(true, true)
	case "wallet_first":
		session, err = choose(false, false)
	default:
		hasSub, checkErr := model.HasActiveUserSubscription(info.UserId)
		if checkErr != nil {
			return nil, types.NewError(checkErr, types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry())
		}
		session, err = choose(hasSub, false)
	}
	if err != nil {
		return nil, durableBillingAPIError(err)
	}
	if common.RedisEnabled {
		_ = model.InvalidateUserCache(info.UserId)
		_ = model.InvalidateTokenCacheByID(info.TokenId)
	}
	return session, nil
}

func durableSessionFromRow(info *relaycommon.RelayInfo, row *model.BillingTransaction, quota int, funding FundingSource) *BillingSession {
	session := &BillingSession{
		relayInfo: info, funding: funding, preConsumedQuota: quota, tokenConsumed: quota,
		durableTxID: row.ID, businessKey: row.BusinessKey,
	}
	session.syncRelayInfo()
	return session
}

func durableBillingAPIError(err error) *types.NewAPIError {
	message := err.Error()
	if errors.Is(err, model.ErrUserQuotaInsufficient) || strings.Contains(message, "insufficient") || strings.Contains(message, "额度不足") {
		return types.NewErrorWithStatusCode(err, types.ErrorCodeInsufficientUserQuota, http.StatusForbidden, types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
	}
	return types.NewError(err, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
}

func settleDurableRequestBilling(s *BillingSession, actualQuota int) error {
	delta := actualQuota - s.preConsumedQuota
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		var row model.BillingTransaction
		if err := tx.Where("id = ?", s.durableTxID).First(&row).Error; err != nil {
			return err
		}
		if row.State != model.BillingTransactionStateReserved {
			if row.State == model.BillingTransactionStateSettled {
				return nil
			}
			return fmt.Errorf("billing transaction state is %s", row.State)
		}
		if err := adjustDurableFundingTx(tx, &row, delta); err != nil {
			return err
		}
		if !row.SkipTokenQuota {
			if err := model.AdjustTokenQuotaTx(tx, row.TokenID, -delta); err != nil {
				return err
			}
		}
		return tx.Model(&model.BillingTransaction{}).Where("id = ? AND state = ?", row.ID, model.BillingTransactionStateReserved).
			Updates(map[string]any{"state": model.BillingTransactionStateSettled, "final_quota": actualQuota}).Error
	})
	if err == nil && common.RedisEnabled {
		_ = model.InvalidateUserCache(s.relayInfo.UserId)
		_ = model.InvalidateTokenCacheByID(s.relayInfo.TokenId)
	}
	return err
}

func refundDurableRequestBilling(s *BillingSession) error {
	now := common.GetTimestamp()
	result := model.DB.Model(&model.BillingTransaction{}).
		Where("id = ? AND task_db_id = ? AND state = ?", s.durableTxID, 0, model.BillingTransactionStateReserved).
		Updates(map[string]any{
			"state": model.BillingTransactionStateRefundPending, "target_task_status": string(model.TaskStatusFailure),
			"final_quota": 0, "reason": "request failed before task binding", "next_retry_at": now,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		var row model.BillingTransaction
		if err := model.DB.Select("state").First(&row, s.durableTxID).Error; err != nil {
			return err
		}
		if row.State != model.BillingTransactionStateRefundPending && row.State != model.BillingTransactionStateApplying && row.State != model.BillingTransactionStateRefunded {
			return fmt.Errorf("billing transaction state is %s", row.State)
		}
	}
	// The durable intent is already committed. Best-effort immediate processing
	// reduces user-visible refund latency; the coordinator guarantees retries.
	_ = ProcessDurableBillingOnce(context.Background(), 100)
	return nil
}

func reserveDurableRequestBilling(s *BillingSession, targetQuota int) error {
	return adjustDurableReservation(s, targetQuota)
}

func adjustDurableReservation(s *BillingSession, targetQuota int) error {
	delta := targetQuota - s.preConsumedQuota
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		var row model.BillingTransaction
		if err := tx.Where("id = ? AND state = ?", s.durableTxID, model.BillingTransactionStateReserved).First(&row).Error; err != nil {
			return err
		}
		if err := adjustDurableFundingTx(tx, &row, delta); err != nil {
			return err
		}
		if !row.SkipTokenQuota {
			if err := model.AdjustTokenQuotaTx(tx, row.TokenID, -delta); err != nil {
				return err
			}
		}
		return tx.Model(&model.BillingTransaction{}).Where("id = ?", row.ID).
			Updates(map[string]any{"reserved_quota": targetQuota, "final_quota": targetQuota}).Error
	})
	if err == nil && common.RedisEnabled {
		_ = model.InvalidateUserCache(s.relayInfo.UserId)
		_ = model.InvalidateTokenCacheByID(s.relayInfo.TokenId)
	}
	return err
}

func adjustDurableFundingTx(tx *gorm.DB, row *model.BillingTransaction, consumeDelta int) error {
	if consumeDelta == 0 {
		return nil
	}
	if row.FundingSource == BillingSourceSubscription {
		return model.PostConsumeUserSubscriptionDeltaTx(tx, row.SubscriptionID, int64(consumeDelta))
	}
	return model.AdjustUserQuotaTx(tx, row.UserID, -consumeDelta)
}

// InsertTaskWithBilling atomically persists the local async task and binds its
// durable reservation. A crash cannot leave an accepted task without a billing
// owner, or a reservation without a task reference.
func InsertTaskWithBilling(task *model.Task, info *relaycommon.RelayInfo) error {
	if task == nil {
		return errors.New("task is nil")
	}
	session, durable := info.Billing.(*BillingSession)
	if !durable || session.durableTxID <= 0 {
		return task.Insert()
	}
	return model.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(task).Error; err != nil {
			return err
		}
		result := tx.Model(&model.BillingTransaction{}).
			Where("id = ? AND task_db_id = ? AND state IN ?", session.durableTxID, 0, []string{
				model.BillingTransactionStateReserved, model.BillingTransactionStateSettled,
			}).
			Update("task_db_id", task.ID)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("durable billing reservation could not be bound to task")
		}
		return nil
	})
}

// RefundUnboundDurableBilling synchronously refunds a reservation when the
// upstream accepted a request but the local task row could not be persisted.
// It returns the error so callers can surface/alert instead of silently losing
// the durable refund requirement.
func RefundUnboundDurableBilling(info *relaycommon.RelayInfo) error {
	if info == nil || info.Billing == nil {
		return nil
	}
	if session, ok := info.Billing.(*BillingSession); ok && session.durableTxID > 0 {
		return refundDurableRequestBilling(session)
	}
	return nil
}

func HasDurableBilling(info *relaycommon.RelayInfo) bool {
	if info == nil {
		return false
	}
	session, ok := info.Billing.(*BillingSession)
	return ok && session.durableTxID > 0
}
