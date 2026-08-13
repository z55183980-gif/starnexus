package model

import (
	"errors"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

const (
	BillingTransactionStateReserved      = "reserved"
	BillingTransactionStateSettlePending = "settle_pending"
	BillingTransactionStateRefundPending = "refund_pending"
	BillingTransactionStateApplying      = "applying"
	BillingTransactionStateSettled       = "settled"
	BillingTransactionStateRefunded      = "refunded"
	BillingTransactionStateActionNeeded  = "action_required"

	BillingOutboxStatePending    = "pending"
	BillingOutboxStateDelivering = "delivering"
	BillingOutboxStateDelivered  = "delivered"

	BillingOutboxEventTaskLog         = "task_billing_log"
	BillingOutboxEventCacheInvalidate = "quota_cache_invalidate"
)

// BillingTransaction is the durable source of truth for a request's reserved
// quota and its eventual settlement/refund. Snapshot remains TEXT so the table
// is portable across SQLite, MySQL and PostgreSQL.
type BillingTransaction struct {
	ID               int64  `json:"id" gorm:"primaryKey"`
	BusinessKey      string `json:"business_key" gorm:"type:varchar(191);uniqueIndex"`
	TaskDBID         int64  `json:"task_db_id" gorm:"index;not null;default:0"`
	UserID           int    `json:"user_id" gorm:"index;not null"`
	TokenID          int    `json:"token_id" gorm:"index;not null;default:0"`
	SkipTokenQuota   bool   `json:"skip_token_quota" gorm:"not null;default:false"`
	SubscriptionID   int    `json:"subscription_id" gorm:"index;not null;default:0"`
	FundingSource    string `json:"funding_source" gorm:"type:varchar(24);not null"`
	ReservedQuota    int    `json:"reserved_quota" gorm:"not null;default:0"`
	FinalQuota       int    `json:"final_quota" gorm:"not null;default:0"`
	PromptTokens     int    `json:"prompt_tokens" gorm:"not null;default:0"`
	CompletionTokens int    `json:"completion_tokens" gorm:"not null;default:0"`
	TargetTaskStatus string `json:"target_task_status" gorm:"type:varchar(24);not null;default:''"`
	State            string `json:"state" gorm:"type:varchar(32);index;not null"`
	Version          int64  `json:"version" gorm:"not null;default:1"`
	LeaseOwner       string `json:"lease_owner" gorm:"type:varchar(96);not null;default:''"`
	LeaseUntil       int64  `json:"lease_until" gorm:"index;not null;default:0"`
	Attempts         int    `json:"attempts" gorm:"not null;default:0"`
	NextRetryAt      int64  `json:"next_retry_at" gorm:"index;not null;default:0"`
	LastError        string `json:"last_error" gorm:"type:text"`
	Reason           string `json:"reason" gorm:"type:text"`
	Snapshot         string `json:"snapshot" gorm:"type:text"`
	CreatedAt        int64  `json:"created_at" gorm:"index"`
	UpdatedAt        int64  `json:"updated_at" gorm:"index"`
}

func (b *BillingTransaction) BeforeCreate(_ *gorm.DB) error {
	now := common.GetTimestamp()
	if b.CreatedAt == 0 {
		b.CreatedAt = now
	}
	b.UpdatedAt = now
	if b.Version == 0 {
		b.Version = 1
	}
	return nil
}

func (b *BillingTransaction) BeforeUpdate(_ *gorm.DB) error {
	b.UpdatedAt = common.GetTimestamp()
	return nil
}

type BillingOutbox struct {
	ID                   int64  `json:"id" gorm:"primaryKey"`
	EventID              string `json:"event_id" gorm:"type:varchar(191);uniqueIndex"`
	BillingTransactionID int64  `json:"billing_transaction_id" gorm:"index;not null"`
	EventType            string `json:"event_type" gorm:"type:varchar(48);index;not null"`
	Payload              string `json:"payload" gorm:"type:text;not null"`
	State                string `json:"state" gorm:"type:varchar(24);index;not null"`
	LeaseOwner           string `json:"lease_owner" gorm:"type:varchar(96);not null;default:''"`
	LeaseUntil           int64  `json:"lease_until" gorm:"index;not null;default:0"`
	Attempts             int    `json:"attempts" gorm:"not null;default:0"`
	NextRetryAt          int64  `json:"next_retry_at" gorm:"index;not null;default:0"`
	LastError            string `json:"last_error" gorm:"type:text"`
	CreatedAt            int64  `json:"created_at" gorm:"index"`
	UpdatedAt            int64  `json:"updated_at" gorm:"index"`
}

func (b *BillingOutbox) BeforeCreate(_ *gorm.DB) error {
	now := common.GetTimestamp()
	if b.CreatedAt == 0 {
		b.CreatedAt = now
	}
	b.UpdatedAt = now
	return nil
}

func (b *BillingOutbox) BeforeUpdate(_ *gorm.DB) error {
	b.UpdatedAt = common.GetTimestamp()
	return nil
}

// BillingLogReceipt makes task log delivery idempotent even when LOG_DB is a
// separate database. Receipt creation and log insertion share one LOG_DB tx.
type BillingLogReceipt struct {
	ID        int64  `json:"id" gorm:"primaryKey"`
	EventID   string `json:"event_id" gorm:"type:varchar(191);uniqueIndex"`
	LogID     int    `json:"log_id" gorm:"not null;default:0"`
	CreatedAt int64  `json:"created_at" gorm:"index"`
}

func (r *BillingLogReceipt) BeforeCreate(_ *gorm.DB) error {
	if r.CreatedAt == 0 {
		r.CreatedAt = common.GetTimestamp()
	}
	return nil
}

func AdjustUserQuotaTx(tx *gorm.DB, userID, delta int) error {
	if tx == nil || userID <= 0 || delta == 0 {
		return nil
	}
	query := tx.Model(&User{}).Where("id = ?", userID)
	if delta < 0 {
		amount := -delta
		query = query.Where("quota >= ?", amount)
	}
	result := query.Update("quota", gorm.Expr("quota + ?", delta))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrUserQuotaInsufficient
	}
	return nil
}

func AdjustTokenQuotaTx(tx *gorm.DB, tokenID, delta int) error {
	if tx == nil || tokenID <= 0 || delta == 0 {
		return nil
	}
	updates := map[string]any{
		"remain_quota":  gorm.Expr("remain_quota + ?", delta),
		"used_quota":    gorm.Expr("used_quota - ?", delta),
		"accessed_time": common.GetTimestamp(),
	}
	query := tx.Model(&Token{}).Where("id = ?", tokenID)
	if delta < 0 {
		amount := -delta
		query = query.Where("unlimited_quota = ? OR remain_quota >= ?", true, amount)
	}
	result := query.Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("token quota is insufficient or token does not exist")
	}
	return nil
}

// ReserveWalletAndTokenTx atomically reserves wallet and token quota. Token
// unlimited semantics are preserved: the database counters still track usage,
// while only limited tokens are subject to the remaining-quota guard.
func ReserveWalletAndTokenTx(tx *gorm.DB, userID, tokenID, quota int, playground bool) error {
	if quota <= 0 {
		return nil
	}
	if err := AdjustUserQuotaTx(tx, userID, -quota); err != nil {
		return err
	}
	if !playground {
		if err := AdjustTokenQuotaTx(tx, tokenID, -quota); err != nil {
			return err
		}
	}
	return nil
}

// ReserveSubscriptionAndTokenTx atomically reserves subscription and token
// quota, including the subscription's idempotency record.
func ReserveSubscriptionAndTokenTx(tx *gorm.DB, requestID string, userID, tokenID int, modelName string, quota int, playground bool) (*SubscriptionPreConsumeResult, error) {
	result, err := PreConsumeUserSubscriptionTx(tx, requestID, userID, modelName, 0, int64(quota))
	if err != nil {
		return nil, err
	}
	if !playground {
		if err := AdjustTokenQuotaTx(tx, tokenID, -quota); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func CreateReservedBillingTransactionTx(tx *gorm.DB, row *BillingTransaction) error {
	if tx == nil || row == nil || row.BusinessKey == "" {
		return errors.New("invalid billing transaction")
	}
	return tx.Create(row).Error
}

func BindBillingTransactionTask(txID, taskDBID int64) error {
	if txID <= 0 || taskDBID <= 0 {
		return errors.New("invalid billing transaction task binding")
	}
	return DB.Model(&BillingTransaction{}).
		Where("id = ? AND task_db_id = ?", txID, 0).
		Update("task_db_id", taskDBID).Error
}

func AdjustUserUsageTx(tx *gorm.DB, userID, quotaDelta, requestDelta int) error {
	if tx == nil || userID <= 0 || (quotaDelta == 0 && requestDelta == 0) {
		return nil
	}
	return tx.Model(&User{}).Where("id = ?", userID).Updates(map[string]any{
		"used_quota":    gorm.Expr("used_quota + ?", quotaDelta),
		"request_count": gorm.Expr("request_count + ?", requestDelta),
	}).Error
}

func AdjustChannelUsageTx(tx *gorm.DB, channelID, quotaDelta int) error {
	if tx == nil || channelID <= 0 || quotaDelta == 0 {
		return nil
	}
	return tx.Model(&Channel{}).Where("id = ?", channelID).
		Update("used_quota", gorm.Expr("used_quota + ?", quotaDelta)).Error
}

func GetDueBillingTransactions(limit int) ([]*BillingTransaction, error) {
	if limit <= 0 {
		limit = 100
	}
	now := common.GetTimestamp()
	var rows []*BillingTransaction
	err := DB.Where(
		"(state IN ? AND next_retry_at <= ?) OR (state = ? AND lease_until < ?)",
		[]string{BillingTransactionStateSettlePending, BillingTransactionStateRefundPending}, now,
		BillingTransactionStateApplying, now,
	).Order("id").Limit(limit).Find(&rows).Error
	return rows, err
}

// MarkStaleUnboundBillingRefundPending recovers reservations whose request
// process disappeared before it could bind an async task or queue a refund.
// The one-hour grace period is deliberately longer than normal relay timeouts.
func MarkStaleUnboundBillingRefundPending(before int64) error {
	if before <= 0 {
		return nil
	}
	now := common.GetTimestamp()
	return DB.Model(&BillingTransaction{}).
		Where("task_db_id = ? AND state = ? AND updated_at < ?", 0, BillingTransactionStateReserved, before).
		Updates(map[string]any{
			"state": BillingTransactionStateRefundPending, "target_task_status": string(TaskStatusFailure),
			"final_quota": 0, "reason": "stale unbound reservation recovery", "next_retry_at": now,
		}).Error
}

func ClaimBillingTransaction(id, version int64, owner string, lease time.Duration) (bool, error) {
	now := common.GetTimestamp()
	result := DB.Model(&BillingTransaction{}).
		Where("id = ? AND version = ?", id, version).
		Where("(state IN ? AND next_retry_at <= ?) OR (state = ? AND lease_until < ?)",
			[]string{BillingTransactionStateSettlePending, BillingTransactionStateRefundPending}, now,
			BillingTransactionStateApplying, now).
		Updates(map[string]any{
			"state":       BillingTransactionStateApplying,
			"lease_owner": owner,
			"lease_until": now + int64(lease/time.Second),
			"version":     gorm.Expr("version + 1"),
			"updated_at":  now,
		})
	return result.RowsAffected == 1, result.Error
}

func ReleaseBillingTransaction(id int64, pendingState, errorMessage string, attempts int) error {
	if pendingState != BillingTransactionStateSettlePending && pendingState != BillingTransactionStateRefundPending {
		return errors.New("invalid pending billing state")
	}
	now := common.GetTimestamp()
	delay := int64(1 << min(attempts, 8))
	state := pendingState
	if attempts >= 20 {
		state = BillingTransactionStateActionNeeded
	}
	return DB.Model(&BillingTransaction{}).Where("id = ? AND state = ?", id, BillingTransactionStateApplying).
		Updates(map[string]any{
			"state": state, "lease_owner": "", "lease_until": 0,
			"attempts": attempts, "next_retry_at": now + delay, "last_error": errorMessage,
		}).Error
}
