package model

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

func (UserAffiliate) TableName() string       { return "user_affiliates" }
func (UserAffiliateLedger) TableName() string { return "user_affiliate_ledger" }
func (TopUpAffiliateAudit) TableName() string { return "topup_affiliate_audits" }

type UserAffiliate struct {
	UserId               int             `json:"user_id" gorm:"primaryKey"`
	AffCode              string          `json:"aff_code" gorm:"type:varchar(32);uniqueIndex;not null"`
	InviterId            *int            `json:"inviter_id" gorm:"index"`
	AffCount             int             `json:"aff_count" gorm:"not null;default:0"`
	AffQuotaUSD          decimal.Decimal `json:"aff_quota_usd" gorm:"type:decimal(20,8);not null;default:0"`
	AffFrozenQuotaUSD    decimal.Decimal `json:"aff_frozen_quota_usd" gorm:"type:decimal(20,8);not null;default:0"`
	AffHistoryQuotaUSD   decimal.Decimal `json:"aff_history_quota_usd" gorm:"type:decimal(20,8);not null;default:0"`
	AffRebateRatePercent *float64        `json:"aff_rebate_rate_percent" gorm:"type:decimal(5,2)"`
	AffCodeCustom        bool            `json:"aff_code_custom" gorm:"not null;default:false"`
	CreatedAt            time.Time       `json:"created_at"`
	UpdatedAt            time.Time       `json:"updated_at"`
}

type UserAffiliateLedger struct {
	Id                 int64            `json:"id" gorm:"primaryKey;autoIncrement"`
	UserId             int              `json:"user_id" gorm:"index;not null"`
	Action             string           `json:"action" gorm:"type:varchar(32);not null"`
	AmountUSD          decimal.Decimal  `json:"amount_usd" gorm:"type:decimal(20,8);not null"`
	SourceUserId       *int             `json:"source_user_id" gorm:"index"`
	SourceTopUpId      *int             `json:"source_topup_id" gorm:"column:source_topup_id;index"`
	FrozenUntil        *time.Time       `json:"frozen_until"`
	QuotaAfter         *int             `json:"quota_after"`
	AffQuotaUSDAfter   *decimal.Decimal `json:"aff_quota_usd_after" gorm:"column:aff_quota_usd_after;type:decimal(20,8)"`
	AffFrozenUSDAfter  *decimal.Decimal `json:"aff_frozen_quota_usd_after" gorm:"column:aff_frozen_quota_usd_after;type:decimal(20,8)"`
	AffHistoryUSDAfter *decimal.Decimal `json:"aff_history_quota_usd_after" gorm:"column:aff_history_quota_usd_after;type:decimal(20,8)"`
	CreatedAt          time.Time        `json:"created_at"`
	UpdatedAt          time.Time        `json:"updated_at"`
}

type TopUpAffiliateAudit struct {
	Id        int64     `json:"id" gorm:"primaryKey;autoIncrement"`
	TopUpId   int       `json:"topup_id" gorm:"column:topup_id;uniqueIndex:idx_topup_aff_action;not null"`
	Action    string    `json:"action" gorm:"type:varchar(64);uniqueIndex:idx_topup_aff_action;not null"`
	Detail    string    `json:"detail" gorm:"type:text"`
	CreatedAt time.Time `json:"created_at"`
}

var affiliateCodeCharset = []byte("ABCDEFGHJKLMNPQRSTUVWXYZ23456789")

func isValidAffiliateCodeFormat(code string) bool {
	code = strings.ToUpper(strings.TrimSpace(code))
	if len(code) < common.AffiliateCodeMinLength || len(code) > common.AffiliateCodeMaxLength {
		return false
	}
	for i := 0; i < len(code); i++ {
		c := code[i]
		if (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-' {
			continue
		}
		return false
	}
	return true
}

func generateAffiliateCode(length int) (string, error) {
	if length <= 0 {
		length = 12
	}
	buf := make([]byte, length)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	for i := range buf {
		buf[i] = affiliateCodeCharset[int(buf[i])%len(affiliateCodeCharset)]
	}
	return string(buf), nil
}

func clampAffiliateRebateRate(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return common.AffiliateRebateRateDefault
	}
	if v < common.AffiliateRebateRateMin {
		return common.AffiliateRebateRateMin
	}
	if v > common.AffiliateRebateRateMax {
		return common.AffiliateRebateRateMax
	}
	return v
}

func resolveGlobalAffiliateRebateRatePercent() float64 {
	return clampAffiliateRebateRate(common.AffiliateRebateRate)
}

func resolveAffiliateRebateRatePercent(profile *UserAffiliate) float64 {
	if profile != nil && profile.AffRebateRatePercent != nil {
		return clampAffiliateRebateRate(*profile.AffRebateRatePercent)
	}
	if profile != nil {
		var user User
		if err := DB.Select("role").Where("id = ?", profile.UserId).First(&user).Error; err == nil && user.Role == common.RoleAgentUser {
			return clampAffiliateRebateRate(common.AgentAffiliateRebateRate)
		}
	}
	return resolveGlobalAffiliateRebateRatePercent()
}

func BackfillUserAffiliatesFromUsers() error {
	var users []User
	if err := DB.Find(&users).Error; err != nil {
		return err
	}
	for _, u := range users {
		record, err := ensureUserAffiliateRecord(u.Id, u.AffCode)
		if err != nil {
			return err
		}
		if u.InviterId > 0 && (record.InviterId == nil || *record.InviterId == 0) {
			if _, bindErr := BindAffiliateInviter(u.Id, u.InviterId); bindErr != nil {
				common.SysLog(fmt.Sprintf("affiliate backfill bind inviter failed user_id=%d err=%v", u.Id, bindErr))
			}
			record, _ = GetUserAffiliateByUserID(u.Id)
		}
		var inviteeCount int64
		if err := DB.Model(&UserAffiliate{}).Where("inviter_id = ?", u.Id).Count(&inviteeCount).Error; err == nil {
			count := int(inviteeCount)
			if record != nil && record.AffCount != count {
				_ = DB.Model(record).Update("aff_count", count).Error
				_ = DB.Model(&User{}).Where("id = ?", u.Id).Update("aff_count", count).Error
			}
		}
	}
	return nil
}

func ensureUserAffiliateRecord(userID int, affCode string) (*UserAffiliate, error) {
	if userID <= 0 {
		return nil, errors.New("invalid user id")
	}
	var existing UserAffiliate
	err := DB.Where("user_id = ?", userID).First(&existing).Error
	if err == nil {
		syncExistingUserAffiliateRecord(&existing, affCode)
		return &existing, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	code := strings.ToUpper(strings.TrimSpace(affCode))
	if code == "" {
		var genErr error
		code, genErr = generateAffiliateCode(12)
		if genErr != nil {
			return nil, genErr
		}
	}
	record := &UserAffiliate{
		UserId:  userID,
		AffCode: code,
	}
	for i := 0; i < 12; i++ {
		if err := DB.Create(record).Error; err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "unique") {
				code, genErr := generateAffiliateCode(12)
				if genErr != nil {
					return nil, genErr
				}
				record.AffCode = code
				continue
			}
			return nil, err
		}
		return record, nil
	}
	return nil, errors.New("failed to allocate affiliate code")
}

func loadUserAffCode(userID int) string {
	var user User
	if err := DB.Select("aff_code").Where("id = ?", userID).First(&user).Error; err != nil {
		return ""
	}
	return user.AffCode
}

func syncExistingUserAffiliateRecord(existing *UserAffiliate, affCode string) {
	if existing == nil {
		return
	}
	normalized := strings.ToUpper(strings.TrimSpace(affCode))
	if normalized == "" || existing.AffCodeCustom || existing.AffCode == normalized {
		return
	}
	if err := DB.Model(existing).Update("aff_code", normalized).Error; err == nil {
		existing.AffCode = normalized
	}
}

func bindAffiliateInviterAfterRegistration(userID, inviterID int) {
	if userID <= 0 || inviterID <= 0 {
		return
	}
	if _, err := BindAffiliateInviter(userID, inviterID); err != nil {
		common.SysLog(fmt.Sprintf("bind affiliate inviter failed user_id=%d inviter_id=%d err=%v", userID, inviterID, err))
	}
}

func GetUserAffiliateByUserID(userID int) (*UserAffiliate, error) {
	return ensureUserAffiliateRecord(userID, loadUserAffCode(userID))
}

func GetUserAffiliateByCode(code string) (*UserAffiliate, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == "" {
		return nil, gorm.ErrRecordNotFound
	}
	var record UserAffiliate
	if err := DB.Where("aff_code = ?", code).First(&record).Error; err != nil {
		return nil, err
	}
	return &record, nil
}

func BindAffiliateInviter(userID, inviterID int) (bool, error) {
	if userID <= 0 || inviterID <= 0 || userID == inviterID {
		return false, errors.New("invalid inviter binding")
	}
	if !common.AffiliateEnabled {
		return false, nil
	}
	profile, err := ensureUserAffiliateRecord(userID, loadUserAffCode(userID))
	if err != nil {
		return false, err
	}
	if profile.InviterId != nil && *profile.InviterId > 0 {
		return false, nil
	}
	tx := DB.Begin()
	if tx.Error != nil {
		return false, tx.Error
	}
	defer tx.Rollback()

	if err := tx.Model(&UserAffiliate{}).Where("user_id = ? AND (inviter_id IS NULL OR inviter_id = 0)", userID).
		Update("inviter_id", inviterID).Error; err != nil {
		return false, err
	}
	if err := tx.Model(&User{}).Where("id = ? AND (inviter_id IS NULL OR inviter_id = 0)", userID).
		Update("inviter_id", inviterID).Error; err != nil {
		return false, err
	}
	if err := tx.Model(&UserAffiliate{}).Where("user_id = ?", inviterID).
		UpdateColumn("aff_count", gorm.Expr("aff_count + 1")).Error; err != nil {
		return false, err
	}
	if err := tx.Model(&User{}).Where("id = ?", inviterID).
		UpdateColumn("aff_count", gorm.Expr("aff_count + 1")).Error; err != nil {
		return false, err
	}
	return true, tx.Commit().Error
}

func UpdateUserInviter(userID int, inviterID int) error {
	if userID <= 0 || inviterID < 0 || userID == inviterID {
		return errors.New("invalid inviter binding")
	}
	if inviterID > 0 {
		if _, err := ensureUserAffiliateRecord(inviterID, loadUserAffCode(inviterID)); err != nil {
			return err
		}
	}
	if _, err := ensureUserAffiliateRecord(userID, loadUserAffCode(userID)); err != nil {
		return err
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var oldProfile UserAffiliate
		if err := tx.Where("user_id = ?", userID).First(&oldProfile).Error; err != nil {
			return err
		}
		oldInviterID := 0
		if oldProfile.InviterId != nil {
			oldInviterID = *oldProfile.InviterId
		}
		if oldInviterID == inviterID {
			return nil
		}

		var inviterPtr *int
		if inviterID > 0 {
			v := inviterID
			inviterPtr = &v
		}
		if err := tx.Model(&UserAffiliate{}).Where("user_id = ?", userID).Update("inviter_id", inviterPtr).Error; err != nil {
			return err
		}
		if err := tx.Model(&User{}).Where("id = ?", userID).Update("inviter_id", inviterID).Error; err != nil {
			return err
		}
		for _, affectedInviterID := range []int{oldInviterID, inviterID} {
			if affectedInviterID <= 0 {
				continue
			}
			var inviteeCount int64
			if err := tx.Model(&UserAffiliate{}).Where("inviter_id = ?", affectedInviterID).Count(&inviteeCount).Error; err != nil {
				return err
			}
			if err := tx.Model(&UserAffiliate{}).Where("user_id = ?", affectedInviterID).Update("aff_count", int(inviteeCount)).Error; err != nil {
				return err
			}
			if err := tx.Model(&User{}).Where("id = ?", affectedInviterID).Update("aff_count", int(inviteeCount)).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func BindAffiliateInviterByCode(userID int, rawCode string) error {
	code := strings.ToUpper(strings.TrimSpace(rawCode))
	if code == "" {
		return nil
	}
	if !common.AffiliateEnabled {
		return nil
	}
	if !isValidAffiliateCodeFormat(code) {
		return errors.New("invalid affiliate code")
	}
	inviter, err := GetUserAffiliateByCode(code)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("invalid affiliate code")
		}
		return err
	}
	if inviter.UserId <= 0 || inviter.UserId == userID {
		return errors.New("invalid affiliate code")
	}
	_, err = BindAffiliateInviter(userID, inviter.UserId)
	return err
}

func ThawAffiliateFrozenQuota(userID int) (decimal.Decimal, error) {
	var thawed decimal.Decimal
	err := DB.Transaction(func(tx *gorm.DB) error {
		var profile UserAffiliate
		if err := tx.Set("gorm:query_option", "FOR UPDATE").Where("user_id = ?", userID).First(&profile).Error; err != nil {
			return err
		}
		now := time.Now()
		var entries []UserAffiliateLedger
		if err := tx.Where("user_id = ? AND action = ? AND frozen_until IS NOT NULL AND frozen_until <= ?",
			userID, common.AffiliateLedgerActionAccrue, now).Find(&entries).Error; err != nil {
			return err
		}
		for _, e := range entries {
			thawed = thawed.Add(e.AmountUSD)
			if err := tx.Model(&UserAffiliateLedger{}).Where("id = ?", e.Id).Update("frozen_until", nil).Error; err != nil {
				return err
			}
		}
		if thawed.IsZero() {
			return nil
		}
		profile.AffQuotaUSD = profile.AffQuotaUSD.Add(thawed)
		profile.AffFrozenQuotaUSD = profile.AffFrozenQuotaUSD.Sub(thawed)
		if profile.AffFrozenQuotaUSD.IsNegative() {
			profile.AffFrozenQuotaUSD = decimal.Zero
		}
		return tx.Save(&profile).Error
	})
	return thawed, err
}

func AccrueAffiliateRebateForTopUp(topUp *TopUp, rechargeUSD decimal.Decimal) (decimal.Decimal, error) {
	if topUp == nil || topUp.Id <= 0 || !common.AffiliateEnabled {
		return decimal.Zero, nil
	}
	if rechargeUSD.LessThanOrEqual(decimal.Zero) {
		return decimal.Zero, nil
	}

	var applied decimal.Decimal
	err := DB.Transaction(func(tx *gorm.DB) error {
		var audit TopUpAffiliateAudit
		err := tx.Where("topup_id = ?", topUp.Id).First(&audit).Error
		if err == nil {
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		invitee, err := ensureUserAffiliateRecord(topUp.UserId, loadUserAffCode(topUp.UserId))
		if err != nil {
			return err
		}
		if invitee.InviterId == nil || *invitee.InviterId <= 0 {
			return tx.Create(&TopUpAffiliateAudit{TopUpId: topUp.Id, Action: common.AffiliateTopUpAuditSkipped, Detail: "no inviter"}).Error
		}
		inviterID := *invitee.InviterId

		if common.AffiliateRebateDurationDays > 0 {
			if time.Since(invitee.CreatedAt) > time.Duration(common.AffiliateRebateDurationDays)*24*time.Hour {
				return tx.Create(&TopUpAffiliateAudit{TopUpId: topUp.Id, Action: common.AffiliateTopUpAuditSkipped, Detail: "rebate duration expired"}).Error
			}
		}

		var inviter UserAffiliate
		if err := tx.Set("gorm:query_option", "FOR UPDATE").Where("user_id = ?", inviterID).First(&inviter).Error; err != nil {
			return err
		}

		rate := resolveAffiliateRebateRatePercent(&inviter)
		rebate := rechargeUSD.Mul(decimal.NewFromFloat(rate / 100)).Round(8)
		if rebate.LessThanOrEqual(decimal.Zero) {
			return tx.Create(&TopUpAffiliateAudit{TopUpId: topUp.Id, Action: common.AffiliateTopUpAuditSkipped, Detail: "zero rebate"}).Error
		}

		if common.AffiliateRebatePerInviteeCapUSD > 0 {
			capUSD := decimal.NewFromFloat(common.AffiliateRebatePerInviteeCapUSD)
			var existing decimal.Decimal
			if err := tx.Model(&UserAffiliateLedger{}).
				Where("user_id = ? AND action = ? AND source_user_id = ?", inviterID, common.AffiliateLedgerActionAccrue, topUp.UserId).
				Select("COALESCE(SUM(amount_usd), 0)").Scan(&existing).Error; err != nil {
				return err
			}
			if existing.GreaterThanOrEqual(capUSD) {
				return tx.Create(&TopUpAffiliateAudit{TopUpId: topUp.Id, Action: common.AffiliateTopUpAuditSkipped, Detail: "per-invitee cap reached"}).Error
			}
			if remaining := capUSD.Sub(existing); rebate.GreaterThan(remaining) {
				rebate = remaining
			}
		}

		var frozenUntil *time.Time
		if common.AffiliateRebateFreezeHours > 0 {
			t := time.Now().Add(time.Duration(common.AffiliateRebateFreezeHours) * time.Hour)
			frozenUntil = &t
			inviter.AffFrozenQuotaUSD = inviter.AffFrozenQuotaUSD.Add(rebate)
		} else {
			inviter.AffQuotaUSD = inviter.AffQuotaUSD.Add(rebate)
		}
		inviter.AffHistoryQuotaUSD = inviter.AffHistoryQuotaUSD.Add(rebate)
		if err := tx.Save(&inviter).Error; err != nil {
			return err
		}

		topUpID := topUp.Id
		sourceUserID := topUp.UserId
		if err := tx.Create(&UserAffiliateLedger{
			UserId:        inviterID,
			Action:        common.AffiliateLedgerActionAccrue,
			AmountUSD:     rebate,
			SourceUserId:  &sourceUserID,
			SourceTopUpId: &topUpID,
			FrozenUntil:   frozenUntil,
		}).Error; err != nil {
			return err
		}

		if err := tx.Create(&TopUpAffiliateAudit{
			TopUpId: topUp.Id,
			Action:  common.AffiliateTopUpAuditApplied,
			Detail:  fmt.Sprintf(`{"rebate_usd":%s}`, rebate.StringFixed(8)),
		}).Error; err != nil {
			return err
		}
		applied = rebate
		return nil
	})
	return applied, err
}

func TransferAffiliateUSDToQuota(userID int) (int, decimal.Decimal, error) {
	if userID <= 0 {
		return 0, decimal.Zero, errors.New("invalid user")
	}
	var quotaAdded int
	var transferred decimal.Decimal
	err := DB.Transaction(func(tx *gorm.DB) error {
		var user User
		if err := tx.Set("gorm:query_option", "FOR UPDATE").Where("id = ?", userID).First(&user).Error; err != nil {
			return err
		}
		var profile UserAffiliate
		if err := tx.Set("gorm:query_option", "FOR UPDATE").Where("user_id = ?", userID).First(&profile).Error; err != nil {
			return err
		}

		// 先解冻
		now := time.Now()
		var entries []UserAffiliateLedger
		if err := tx.Where("user_id = ? AND action = ? AND frozen_until IS NOT NULL AND frozen_until <= ?",
			userID, common.AffiliateLedgerActionAccrue, now).Find(&entries).Error; err != nil {
			return err
		}
		for _, e := range entries {
			profile.AffQuotaUSD = profile.AffQuotaUSD.Add(e.AmountUSD)
			profile.AffFrozenQuotaUSD = profile.AffFrozenQuotaUSD.Sub(e.AmountUSD)
			if err := tx.Model(&UserAffiliateLedger{}).Where("id = ?", e.Id).Update("frozen_until", nil).Error; err != nil {
				return err
			}
		}
		if profile.AffFrozenQuotaUSD.IsNegative() {
			profile.AffFrozenQuotaUSD = decimal.Zero
		}

		amount := profile.AffQuotaUSD
		if amount.LessThanOrEqual(decimal.Zero) {
			return errors.New("no affiliate rebate available")
		}

		quotaPerUnit := decimal.NewFromFloat(common.QuotaPerUnit)
		if quotaPerUnit.LessThanOrEqual(decimal.Zero) {
			return errors.New("invalid quota per unit")
		}
		quotaAdded = int(amount.Mul(quotaPerUnit).IntPart())
		if quotaAdded <= 0 {
			return fmt.Errorf("transfer amount too small, minimum %s", loggerMinQuotaHint())
		}

		user.Quota += quotaAdded
		profile.AffQuotaUSD = decimal.Zero
		transferred = amount

		quotaAfter := user.Quota
		affAfter := profile.AffQuotaUSD
		frozenAfter := profile.AffFrozenQuotaUSD
		historyAfter := profile.AffHistoryQuotaUSD
		if err := tx.Save(&user).Error; err != nil {
			return err
		}
		if err := tx.Save(&profile).Error; err != nil {
			return err
		}
		return tx.Create(&UserAffiliateLedger{
			UserId:             userID,
			Action:             common.AffiliateLedgerActionTransfer,
			AmountUSD:          transferred,
			QuotaAfter:         &quotaAfter,
			AffQuotaUSDAfter:   &affAfter,
			AffFrozenUSDAfter:  &frozenAfter,
			AffHistoryUSDAfter: &historyAfter,
		}).Error
	})
	return quotaAdded, transferred, err
}

func loggerMinQuotaHint() string {
	return fmt.Sprintf("$%.4f", 1.0/common.QuotaPerUnit)
}

func ResolveEffectiveRebateRatePercent(profile *UserAffiliate) float64 {
	return resolveAffiliateRebateRatePercent(profile)
}

func TopUpRechargeUSD(topUp *TopUp) decimal.Decimal {
	if topUp == nil {
		return decimal.Zero
	}

	switch topUp.PaymentProvider {
	case PaymentProviderStripe, PaymentProviderCreem:
		if topUp.Money > 0 {
			return decimal.NewFromFloat(topUp.Money)
		}
		return decimal.Zero
	case PaymentProviderEpusdt, PaymentProviderEpay, PaymentProviderWaffo, PaymentProviderWaffoPancake:
		return topUpUserRechargeUSD(topUp)
	default:
		return topUpUserRechargeUSD(topUp)
	}
}

// topUpUserRechargeUSD converts the user-facing recharge amount stored on TopUp.Amount
// into USD. TopUp.Money is the gateway payment amount (USDT/CNY/etc.) and must not be
// used for affiliate rebates.
func topUpUserRechargeUSD(topUp *TopUp) decimal.Decimal {
	if topUp == nil || topUp.Amount <= 0 {
		if topUp != nil && topUp.Money > 0 {
			return decimal.NewFromFloat(topUp.Money)
		}
		return decimal.Zero
	}

	base := decimal.NewFromInt(topUp.Amount)
	switch operation_setting.GetQuotaDisplayType() {
	case operation_setting.QuotaDisplayTypeCNY:
		rate := operation_setting.USDExchangeRate
		if rate <= 0 {
			rate = 1
		}
		return base.Div(decimal.NewFromFloat(rate)).Round(8)
	case operation_setting.QuotaDisplayTypeCustom:
		rate := operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate
		if rate <= 0 {
			rate = 1
		}
		return base.Div(decimal.NewFromFloat(rate)).Round(8)
	default:
		// USD and TOKENS: Amount is already normalized to USD at order creation for
		// epusdt/epay/waffo flows.
		return base
	}
}

func ProcessAffiliateRebateAfterTopUp(topUp *TopUp) {
	if topUp == nil || topUp.Id <= 0 {
		return
	}
	rechargeUSD := TopUpRechargeUSD(topUp)
	if _, err := AccrueAffiliateRebateForTopUp(topUp, rechargeUSD); err != nil {
		common.SysLog(fmt.Sprintf("affiliate rebate failed topup_id=%d err=%v", topUp.Id, err))
	}
}

type AffiliateInviteeView struct {
	UserId      int             `json:"user_id"`
	Username    string          `json:"username"`
	Email       string          `json:"email"`
	CreatedAt   time.Time       `json:"created_at"`
	TotalRebate decimal.Decimal `json:"total_rebate_usd"`
}

func ListAffiliateInvitees(inviterID int, limit int) ([]AffiliateInviteeView, error) {
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	var rows []AffiliateInviteeView
	err := DB.Raw(`
SELECT ua.user_id,
       COALESCE(u.username, '') AS username,
       COALESCE(u.email, '') AS email,
       ua.created_at,
       COALESCE(SUM(l.amount_usd), 0) AS total_rebate
FROM user_affiliates ua
JOIN users u ON u.id = ua.user_id
LEFT JOIN user_affiliate_ledger l
       ON l.user_id = ? AND l.action = ? AND l.source_user_id = ua.user_id
WHERE ua.inviter_id = ?
GROUP BY ua.user_id, u.username, u.email, ua.created_at
ORDER BY ua.created_at DESC
LIMIT ?
`, inviterID, common.AffiliateLedgerActionAccrue, inviterID, limit).Scan(&rows).Error
	return rows, err
}

// RepairAffiliateColumnNames fixes legacy GORM column names (top_up_id, source_top_up_id)
// on MySQL/PostgreSQL after explicit column tags were added to affiliate models.
func RepairAffiliateColumnNames() error {
	if common.UsingSQLite {
		return nil
	}
	migrator := DB.Migrator()
	if !migrator.HasTable(&TopUpAffiliateAudit{}) {
		return nil
	}
	if err := reconcileAffiliateAuditTopUpColumn(migrator); err != nil {
		return err
	}
	if migrator.HasTable(&UserAffiliateLedger{}) {
		if err := reconcileAffiliateLedgerSourceTopUpColumn(migrator); err != nil {
			return err
		}
		if err := dropAffiliateLedgerLegacyColumns(migrator); err != nil {
			return err
		}
	}
	return nil
}

func reconcileAffiliateAuditTopUpColumn(migrator gorm.Migrator) error {
	hasLegacy := migrator.HasColumn(&TopUpAffiliateAudit{}, "top_up_id")
	hasCanonical := migrator.HasColumn(&TopUpAffiliateAudit{}, "topup_id")
	switch {
	case hasLegacy && !hasCanonical:
		return renameAffiliateColumn("topup_affiliate_audits", "top_up_id", "topup_id")
	case hasLegacy && hasCanonical:
		if err := DB.Exec(`UPDATE topup_affiliate_audits SET topup_id = top_up_id WHERE topup_id IS NULL AND top_up_id IS NOT NULL`).Error; err != nil {
			return err
		}
		return dropAffiliateColumn("topup_affiliate_audits", "top_up_id")
	default:
		return nil
	}
}

func reconcileAffiliateLedgerSourceTopUpColumn(migrator gorm.Migrator) error {
	hasLegacy := migrator.HasColumn(&UserAffiliateLedger{}, "source_top_up_id")
	hasCanonical := migrator.HasColumn(&UserAffiliateLedger{}, "source_topup_id")
	switch {
	case hasLegacy && !hasCanonical:
		return renameAffiliateColumn("user_affiliate_ledger", "source_top_up_id", "source_topup_id")
	case hasLegacy && hasCanonical:
		if err := DB.Exec(`UPDATE user_affiliate_ledger SET source_topup_id = source_top_up_id WHERE source_topup_id IS NULL AND source_top_up_id IS NOT NULL`).Error; err != nil {
			return err
		}
		return dropAffiliateColumn("user_affiliate_ledger", "source_top_up_id")
	default:
		return nil
	}
}

func dropAffiliateLedgerLegacyColumns(migrator gorm.Migrator) error {
	for _, col := range []string{"aff_frozen_usd_after", "aff_history_usd_after"} {
		if migrator.HasColumn(&UserAffiliateLedger{}, col) {
			if err := dropAffiliateColumn("user_affiliate_ledger", col); err != nil {
				return err
			}
		}
	}
	return nil
}

func renameAffiliateColumn(tableName, fromCol, toCol string) error {
	if common.UsingPostgreSQL {
		return DB.Exec(fmt.Sprintf(`ALTER TABLE %s RENAME COLUMN %s TO %s`, tableName, fromCol, toCol)).Error
	}
	if common.UsingMySQL {
		if tableName == "topup_affiliate_audits" && fromCol == "top_up_id" {
			return DB.Exec(fmt.Sprintf("ALTER TABLE `%s` CHANGE `%s` `%s` bigint NOT NULL", tableName, fromCol, toCol)).Error
		}
		return DB.Exec(fmt.Sprintf("ALTER TABLE `%s` CHANGE `%s` `%s` bigint NULL", tableName, fromCol, toCol)).Error
	}
	return nil
}

func dropAffiliateColumn(tableName, columnName string) error {
	if common.UsingPostgreSQL {
		return DB.Exec(fmt.Sprintf(`ALTER TABLE %s DROP COLUMN IF EXISTS %s`, tableName, columnName)).Error
	}
	if common.UsingMySQL {
		return DB.Exec(fmt.Sprintf("ALTER TABLE `%s` DROP COLUMN `%s`", tableName, columnName)).Error
	}
	return nil
}

// BackfillMissedAffiliateRebates re-processes successful top-ups that never accrued rebate
// (e.g. after fixing schema). Accrual is idempotent per top-up.
func BackfillMissedAffiliateRebates() {
	if !common.AffiliateEnabled {
		return
	}
	var topUps []TopUp
	if err := DB.Where("status = ?", common.TopUpStatusSuccess).Find(&topUps).Error; err != nil {
		common.SysLog("affiliate rebate backfill list topups failed: " + err.Error())
		return
	}
	for i := range topUps {
		ProcessAffiliateRebateAfterTopUp(&topUps[i])
	}
}
