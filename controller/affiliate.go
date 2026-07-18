package controller

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

// GetAffiliateDetail returns the current user's affiliate profile (USD rebate + invitees).
// GET /api/user/affiliate
func GetAffiliateDetail(c *gin.Context) {
	userID := c.GetInt("id")
	if userID <= 0 {
		common.ApiError(c, errors.New("unauthorized"))
		return
	}

	profile, err := model.GetUserAffiliateByUserID(userID)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	_, _ = model.ThawAffiliateFrozenQuota(userID)
	profile, _ = model.GetUserAffiliateByUserID(userID)

	pageInfo := common.GetPageQuery(c)
	invitees, total, err := model.ListAffiliateInvitees(userID, pageInfo)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"aff_code":                      profile.AffCode,
			"inviter_id":                    profile.InviterId,
			"aff_count":                     profile.AffCount,
			"aff_quota_usd":                 profile.AffQuotaUSD,
			"aff_frozen_quota_usd":          profile.AffFrozenQuotaUSD,
			"aff_history_quota_usd":         profile.AffHistoryQuotaUSD,
			"effective_rebate_rate_percent": model.ResolveEffectiveRebateRatePercent(profile),
			"affiliate_enabled":             common.AffiliateEnabled,
			"invitees":                      invitees,
			"invitees_total":                total,
			"invitees_page":                 pageInfo.GetPage(),
			"invitees_page_size":            pageInfo.GetPageSize(),
		},
	})
}

// TransferAffiliateUSD transfers available USD affiliate rebate into user quota.
// POST /api/user/affiliate/transfer
func TransferAffiliateUSD(c *gin.Context) {
	userID := c.GetInt("id")
	if userID <= 0 {
		common.ApiError(c, errors.New("unauthorized"))
		return
	}
	quotaAdded, transferred, err := model.TransferAffiliateUSDToQuota(userID)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"quota_added":     quotaAdded,
			"transferred_usd": transferred,
		},
	})
}

type affiliateLedgerItem struct {
	Id                int64           `json:"id"`
	UserId            int             `json:"user_id"`
	Username          string          `json:"username"`
	Action            string          `json:"action"`
	AmountUSD         decimal.Decimal `json:"amount_usd"`
	SourceUserId      *int            `json:"source_user_id"`
	SourceUsername    *string         `json:"source_username"`
	SourceTopUpId     *int            `json:"source_topup_id" gorm:"column:source_topup_id"`
	SourceTopUpAmount *int64          `json:"source_topup_amount" gorm:"column:source_topup_amount"`
	FrozenUntil       *time.Time      `json:"frozen_until,omitempty"`
	QuotaAfter        *int            `json:"quota_after,omitempty"`
	CreatedAt         time.Time       `json:"created_at"`
}

func listAffiliateLedgers(c *gin.Context, scopedUserID *int) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize

	var total int64
	var rows []affiliateLedgerItem
	q := model.DB.Table("user_affiliate_ledger AS l").
		Joins("LEFT JOIN users AS u ON u.id = l.user_id").
		Joins("LEFT JOIN users AS su ON su.id = l.source_user_id").
		Joins("LEFT JOIN top_ups AS tu ON tu.id = l.source_topup_id")
	if scopedUserID != nil {
		q = q.Where("l.user_id = ?", *scopedUserID)
	} else if userID := c.Query("user_id"); userID != "" {
		parsedUserID, err := strconv.Atoi(userID)
		if err != nil || parsedUserID <= 0 {
			common.ApiError(c, errors.New("invalid user_id"))
			return
		}
		q = q.Where("l.user_id = ?", parsedUserID)
	}
	if action := c.Query("action"); action != "" {
		q = q.Where("l.action = ?", action)
	}
	if start := c.Query("start_time"); start != "" {
		startTime, err := time.Parse(time.RFC3339, start)
		if err != nil {
			common.ApiError(c, errors.New("invalid start_time"))
			return
		}
		q = q.Where("l.created_at >= ?", startTime)
	}
	if end := c.Query("end_time"); end != "" {
		endTime, err := time.Parse(time.RFC3339, end)
		if err != nil {
			common.ApiError(c, errors.New("invalid end_time"))
			return
		}
		q = q.Where("l.created_at <= ?", endTime)
	}
	if keyword := strings.TrimSpace(c.Query("keyword")); keyword != "" {
		pattern := "%" + escapeAffiliateLedgerLikePattern(keyword) + "%"
		if keywordID, err := strconv.Atoi(keyword); err == nil {
			q = q.Where(
				"(l.id = ? OR l.user_id = ? OR l.source_user_id = ? OR l.source_topup_id = ? OR l.action LIKE ? ESCAPE '!' OR u.username LIKE ? ESCAPE '!' OR su.username LIKE ? ESCAPE '!')",
				keywordID,
				keywordID,
				keywordID,
				keywordID,
				pattern,
				pattern,
				pattern,
			)
		} else {
			q = q.Where("(l.action LIKE ? ESCAPE '!' OR u.username LIKE ? ESCAPE '!' OR su.username LIKE ? ESCAPE '!')", pattern, pattern, pattern)
		}
	}
	if err := q.Count(&total).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	selectColumns := strings.Join([]string{
		"l.id",
		"l.user_id",
		"COALESCE(u.username, '') AS username",
		"l.action",
		"l.amount_usd",
		"l.source_user_id",
		"su.username AS source_username",
		"l.source_topup_id",
		"tu.amount AS source_topup_amount",
		"l.frozen_until",
		"l.quota_after",
		"l.created_at",
	}, ", ")
	if err := q.Select(selectColumns).Order("l.id desc").Offset(offset).Limit(pageSize).Scan(&rows).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"items":     rows,
			"total":     total,
			"page":      page,
			"page_size": pageSize,
		},
	})
}

// AgentListAffiliateLedgers lists the current agent's affiliate ledger entries.
// GET /api/affiliate/agent/ledger
func AgentListAffiliateLedgers(c *gin.Context) {
	userID := c.GetInt("id")
	listAffiliateLedgers(c, &userID)
}

// AdminListAffiliateLedgers lists affiliate ledger entries for admin.
// GET /api/affiliate/admin/ledger
func AdminListAffiliateLedgers(c *gin.Context) {
	listAffiliateLedgers(c, nil)
}
func escapeAffiliateLedgerLikePattern(input string) string {
	input = strings.ReplaceAll(input, "!", "!!")
	input = strings.ReplaceAll(input, "%", "!%")
	input = strings.ReplaceAll(input, "_", "!_")
	return input
}
