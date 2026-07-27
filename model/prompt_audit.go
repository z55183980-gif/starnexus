package model

import (
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

const (
	PromptAuditActionRecorded = "recorded"
	PromptAuditActionHit      = "hit"
	PromptAuditActionDelayed  = "delayed"
	PromptAuditActionBlocked  = "blocked"

	PromptAuditDefaultDelaySeconds = 3
)

type PromptAuditPolicy struct {
	Id             int   `json:"id"`
	UserId         int   `json:"user_id" gorm:"uniqueIndex;not null"`
	MonitorEnabled bool  `json:"monitor_enabled" gorm:"not null;default:true"`
	DelayOnHit     bool  `json:"delay_on_hit" gorm:"not null;default:false"`
	DelaySeconds   int   `json:"delay_seconds" gorm:"not null;default:3"`
	BlockOnHit     bool  `json:"block_on_hit" gorm:"not null;default:false"`
	CreatedBy      int   `json:"created_by" gorm:"not null;default:0"`
	CreatedAt      int64 `json:"created_at" gorm:"bigint;not null"`
	UpdatedAt      int64 `json:"updated_at" gorm:"bigint;not null"`
}

func IsPromptAuditDelaySecondsAllowed(seconds int) bool {
	switch seconds {
	case 3, 5, 9, 15, 20, 30, 45:
		return true
	default:
		return false
	}
}

type PromptAuditPolicyView struct {
	PromptAuditPolicy
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
}

type PromptAuditLog struct {
	Id           int     `json:"id" gorm:"index:idx_prompt_audit_created_id,priority:2"`
	UserId       int     `json:"user_id" gorm:"index:idx_prompt_audit_user_created,priority:1;not null"`
	Username     string  `json:"username" gorm:"type:varchar(64);not null;default:''"`
	TokenId      int     `json:"token_id" gorm:"index;not null;default:0"`
	TokenName    string  `json:"token_name" gorm:"type:varchar(64);not null;default:''"`
	RequestId    string  `json:"request_id" gorm:"type:varchar(64);index;not null;default:''"`
	ModelName    string  `json:"model_name" gorm:"type:varchar(255);index;not null;default:''"`
	Protocol     string  `json:"protocol" gorm:"type:varchar(32);not null;default:''"`
	Endpoint     string  `json:"endpoint" gorm:"type:varchar(128);not null;default:''"`
	Prompt       string  `json:"prompt" gorm:"type:text;not null"`
	PromptHash   string  `json:"prompt_hash" gorm:"type:varchar(64);index;not null"`
	Hit          bool    `json:"hit" gorm:"index;not null;default:false"`
	MatchedWords string  `json:"matched_words" gorm:"type:text;not null;default:'[]'"`
	Action       string  `json:"action" gorm:"type:varchar(24);index;not null;default:'recorded'"`
	DelayMs      int     `json:"delay_ms" gorm:"not null;default:0"`
	Truncated    bool    `json:"truncated" gorm:"not null;default:false"`
	CreatedAt    int64   `json:"created_at" gorm:"bigint;index:idx_prompt_audit_created_id,priority:1;index:idx_prompt_audit_user_created,priority:2;not null"`
	Score        float64 `json:"score" gorm:"not null;default:0"`
}

func ListPromptAuditPolicies() ([]PromptAuditPolicyView, error) {
	var policies []PromptAuditPolicyView
	err := DB.Table("prompt_audit_policies AS p").
		Select("p.*, COALESCE(u.username, '') AS username, COALESCE(u.display_name, '') AS display_name, COALESCE(u.email, '') AS email").
		Joins("LEFT JOIN users AS u ON u.id = p.user_id").
		Order("p.id DESC").
		Scan(&policies).Error
	return policies, err
}

func ListPromptAuditPolicyModels() ([]PromptAuditPolicy, error) {
	var policies []PromptAuditPolicy
	err := DB.Order("id ASC").Find(&policies).Error
	return policies, err
}

func GetPromptAuditPolicyById(id int) (*PromptAuditPolicy, error) {
	if id <= 0 {
		return nil, errors.New("invalid prompt audit policy id")
	}
	var policy PromptAuditPolicy
	if err := DB.First(&policy, id).Error; err != nil {
		return nil, err
	}
	return &policy, nil
}

func GetPromptAuditPolicyByUserId(userId int) (*PromptAuditPolicy, error) {
	if userId <= 0 {
		return nil, errors.New("invalid user id")
	}
	var policy PromptAuditPolicy
	if err := DB.Where("user_id = ?", userId).First(&policy).Error; err != nil {
		return nil, err
	}
	return &policy, nil
}

func CreatePromptAuditPolicy(userId int, createdBy int) (*PromptAuditPolicy, error) {
	if userId <= 0 {
		return nil, errors.New("invalid user id")
	}
	if _, err := GetUserById(userId, false); err != nil {
		return nil, errors.New("user not found")
	}
	if _, err := GetPromptAuditPolicyByUserId(userId); err == nil {
		return nil, errors.New("user already has a security audit record")
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	now := common.GetTimestamp()
	policy := &PromptAuditPolicy{
		UserId:         userId,
		MonitorEnabled: true,
		DelaySeconds:   PromptAuditDefaultDelaySeconds,
		CreatedBy:      createdBy,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := DB.Create(policy).Error; err != nil {
		return nil, err
	}
	return policy, nil
}

func UpdatePromptAuditPolicy(policy *PromptAuditPolicy) error {
	if policy == nil || policy.Id <= 0 {
		return errors.New("invalid prompt audit policy")
	}
	if policy.DelayOnHit && policy.BlockOnHit {
		return errors.New("delay and block actions cannot be enabled together")
	}
	if policy.DelaySeconds == 0 {
		policy.DelaySeconds = PromptAuditDefaultDelaySeconds
	}
	if !IsPromptAuditDelaySecondsAllowed(policy.DelaySeconds) {
		return errors.New("invalid prompt audit delay seconds")
	}
	return DB.Model(&PromptAuditPolicy{}).
		Where("id = ?", policy.Id).
		Updates(map[string]any{
			"monitor_enabled": policy.MonitorEnabled,
			"delay_on_hit":    policy.DelayOnHit,
			"delay_seconds":   policy.DelaySeconds,
			"block_on_hit":    policy.BlockOnHit,
			"updated_at":      common.GetTimestamp(),
		}).Error
}

func DeletePromptAuditPolicyAndLogs(id int) error {
	if id <= 0 {
		return errors.New("invalid prompt audit policy id")
	}
	policy, err := GetPromptAuditPolicyById(id)
	if err != nil {
		return err
	}

	deletePolicy := func(db *gorm.DB) error {
		result := db.Delete(&PromptAuditPolicy{}, id)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return nil
	}

	if DB == LOG_DB {
		return DB.Transaction(func(tx *gorm.DB) error {
			if err := tx.Where("user_id = ?", policy.UserId).Delete(&PromptAuditLog{}).Error; err != nil {
				return err
			}
			return deletePolicy(tx)
		})
	}

	if err := LOG_DB.Where("user_id = ?", policy.UserId).Delete(&PromptAuditLog{}).Error; err != nil {
		return err
	}
	return deletePolicy(DB)
}

func CreatePromptAuditLog(log *PromptAuditLog) error {
	if log == nil || log.UserId <= 0 || strings.TrimSpace(log.Prompt) == "" {
		return errors.New("invalid prompt audit log")
	}
	return LOG_DB.Create(log).Error
}

func DeletePromptAuditLogsByUserId(userId int) (int64, error) {
	if userId <= 0 {
		return 0, errors.New("invalid user id")
	}
	result := LOG_DB.Where("user_id = ?", userId).Delete(&PromptAuditLog{})
	return result.RowsAffected, result.Error
}

func ListPromptAuditLogsByUserId(userId int, pageInfo *common.PageInfo) ([]PromptAuditLog, int64, error) {
	if userId <= 0 {
		return nil, 0, errors.New("invalid user id")
	}
	if pageInfo == nil {
		pageInfo = &common.PageInfo{Page: 1, PageSize: common.ItemsPerPage}
	}
	query := LOG_DB.Model(&PromptAuditLog{}).Where("user_id = ?", userId)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var logs []PromptAuditLog
	err := query.Order("created_at DESC").Order("id DESC").
		Limit(pageInfo.GetPageSize()).
		Offset(pageInfo.GetStartIdx()).
		Find(&logs).Error
	return logs, total, err
}

func ListPromptAuditLogsBeforeId(userId int, beforeId int, limit int) ([]PromptAuditLog, bool, error) {
	if userId <= 0 {
		return nil, false, errors.New("invalid user id")
	}
	if beforeId < 0 {
		return nil, false, errors.New("invalid prompt audit log cursor")
	}
	limit = normalizePromptAuditLogLimit(limit)

	query := LOG_DB.Where("user_id = ?", userId)
	if beforeId > 0 {
		query = query.Where("id < ?", beforeId)
	}
	logs := make([]PromptAuditLog, 0, limit+1)
	if err := query.Order("id DESC").Limit(limit + 1).Find(&logs).Error; err != nil {
		return nil, false, err
	}
	hasMore := len(logs) > limit
	if hasMore {
		logs = logs[:limit]
	}
	return logs, hasMore, nil
}

func ListPromptAuditLogsAfterId(userId int, afterId int, limit int) ([]PromptAuditLog, bool, error) {
	if userId <= 0 {
		return nil, false, errors.New("invalid user id")
	}
	if afterId < 0 {
		return nil, false, errors.New("invalid prompt audit log cursor")
	}
	limit = normalizePromptAuditLogLimit(limit)

	logs := make([]PromptAuditLog, 0, limit+1)
	err := LOG_DB.Where("user_id = ? AND id > ?", userId, afterId).
		Order("id ASC").
		Limit(limit + 1).
		Find(&logs).Error
	if err != nil {
		return nil, false, err
	}
	hasMore := len(logs) > limit
	if hasMore {
		logs = logs[:limit]
	}
	return logs, hasMore, nil
}

func normalizePromptAuditLogLimit(limit int) int {
	if limit <= 0 {
		return common.ItemsPerPage
	}
	if limit > 100 {
		return 100
	}
	return limit
}
