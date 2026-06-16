package controller

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
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

	invitees, err := model.ListAffiliateInvitees(userID, 100)
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

// AdminListAffiliateLedgers lists affiliate ledger entries for admin.
// GET /api/affiliate/admin/ledger
func AdminListAffiliateLedgers(c *gin.Context) {
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
	var rows []model.UserAffiliateLedger
	q := model.DB.Model(&model.UserAffiliateLedger{})
	if userID := c.Query("user_id"); userID != "" {
		q = q.Where("user_id = ?", userID)
	}
	if action := c.Query("action"); action != "" {
		q = q.Where("action = ?", action)
	}
	if keyword := strings.TrimSpace(c.Query("keyword")); keyword != "" {
		pattern := "%" + escapeAffiliateLedgerLikePattern(keyword) + "%"
		if keywordID, err := strconv.Atoi(keyword); err == nil {
			q = q.Where(
				"(id = ? OR user_id = ? OR source_user_id = ? OR source_topup_id = ? OR action LIKE ? ESCAPE '!')",
				keywordID,
				keywordID,
				keywordID,
				keywordID,
				pattern,
			)
		} else {
			q = q.Where("action LIKE ? ESCAPE '!'", pattern)
		}
	}
	if err := q.Count(&total).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	if err := q.Order("id desc").Offset(offset).Limit(pageSize).Find(&rows).Error; err != nil {
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

func escapeAffiliateLedgerLikePattern(input string) string {
	input = strings.ReplaceAll(input, "!", "!!")
	input = strings.ReplaceAll(input, "%", "!%")
	input = strings.ReplaceAll(input, "_", "!_")
	return input
}
