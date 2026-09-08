package model

import (
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	UserAPIAccessActionSuspend = "suspend"
	UserAPIAccessActionRestore = "restore"
	UserAPIAccessSourceCyber   = "upstream_cyber_policy"
)

type UserAPIAccessEvent struct {
	Id                int64  `json:"id"`
	UserId            int    `json:"user_id" gorm:"index;not null"`
	Action            string `json:"action" gorm:"type:varchar(24);index;not null"`
	Source            string `json:"source" gorm:"type:varchar(64);index;not null"`
	Reason            string `json:"reason" gorm:"type:varchar(255);not null;default:''"`
	RequestId         string `json:"request_id" gorm:"type:varchar(64);index;not null;default:''"`
	TokenId           int    `json:"token_id" gorm:"index;not null;default:0"`
	ModelName         string `json:"model_name" gorm:"type:varchar(255);index;not null;default:''"`
	ChannelId         int    `json:"channel_id" gorm:"index;not null;default:0"`
	UpstreamAccountId int    `json:"upstream_account_id" gorm:"index;not null;default:0"`
	NodeName          string `json:"node_name" gorm:"type:varchar(128);not null;default:''"`
	ActorId           int    `json:"actor_id" gorm:"index;not null;default:0"`
	Evidence          string `json:"evidence" gorm:"type:text;not null"`
	CreatedAt         int64  `json:"created_at" gorm:"bigint;index;not null"`
}

type SuspendUserAPIInput struct {
	UserId            int
	Reason            string
	RequestId         string
	TokenId           int
	ModelName         string
	ChannelId         int
	UpstreamAccountId int
	NodeName          string
	Evidence          string
}

type SuspendedUserView struct {
	Id                    int    `json:"id"`
	Username              string `json:"username"`
	DisplayName           string `json:"display_name"`
	Email                 string `json:"email"`
	APIStatus             int    `json:"api_status"`
	APISuspendedAt        int64  `json:"api_suspended_at"`
	APISuspendedReason    string `json:"api_suspended_reason"`
	RequestId             string `json:"request_id"`
	TokenId               int    `json:"token_id"`
	ModelName             string `json:"model_name"`
	ChannelId             int    `json:"channel_id"`
	UpstreamAccountId     int    `json:"upstream_account_id"`
	NodeName              string `json:"node_name"`
	PromptMonitoringAdded bool   `json:"prompt_monitoring_added"`
}

func SuspendUserAPIForCyberPolicy(input SuspendUserAPIInput) (bool, error) {
	if input.UserId <= 0 {
		return false, errors.New("invalid user id")
	}
	now := common.GetTimestamp()
	changed := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&User{}).
			Where("id = ? AND api_status <> ?", input.UserId, common.UserAPIStatusSuspended).
			Updates(map[string]any{
				"api_status":           common.UserAPIStatusSuspended,
				"api_suspended_at":     now,
				"api_suspended_reason": UserAPIAccessSourceCyber,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected > 0 {
			changed = true
			if err := tx.Create(&UserAPIAccessEvent{
				UserId: input.UserId, Action: UserAPIAccessActionSuspend, Source: UserAPIAccessSourceCyber,
				Reason: strings.TrimSpace(input.Reason), RequestId: strings.TrimSpace(input.RequestId), TokenId: input.TokenId,
				ModelName: strings.TrimSpace(input.ModelName), ChannelId: input.ChannelId,
				UpstreamAccountId: input.UpstreamAccountId, NodeName: strings.TrimSpace(input.NodeName),
				Evidence: strings.TrimSpace(input.Evidence), CreatedAt: now,
			}).Error; err != nil {
				return err
			}
		}
		policy := &PromptAuditPolicy{
			UserId: input.UserId, MonitorEnabled: true, DelaySeconds: PromptAuditDefaultDelaySeconds,
			CreatedBy: 0, CreatedAt: now, UpdatedAt: now,
		}
		return tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "user_id"}},
			DoUpdates: clause.Assignments(map[string]any{
				"monitor_enabled": true,
				"updated_at":      now,
			}),
		}).Create(policy).Error
	})
	return changed, err
}

func RestoreUserAPIAccess(userId int, actorId int, reason string) (bool, error) {
	if userId <= 0 || actorId <= 0 {
		return false, errors.New("invalid restore request")
	}
	now := common.GetTimestamp()
	changed := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&User{}).
			Where("id = ? AND api_status = ?", userId, common.UserAPIStatusSuspended).
			Updates(map[string]any{
				"api_status": common.UserAPIStatusEnabled, "api_suspended_at": 0, "api_suspended_reason": "",
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return nil
		}
		changed = true
		return tx.Create(&UserAPIAccessEvent{
			UserId: userId, Action: UserAPIAccessActionRestore, Source: "admin", Reason: strings.TrimSpace(reason),
			ActorId: actorId, CreatedAt: now,
		}).Error
	})
	return changed, err
}

func ListSuspendedUsers(keyword string, startIdx int, limit int) ([]SuspendedUserView, int64, error) {
	query := DB.Model(&User{}).Where("api_status = ?", common.UserAPIStatusSuspended)
	if keyword = strings.TrimSpace(keyword); keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("username LIKE ? OR display_name LIKE ? OR email LIKE ?", like, like, like)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	users := make([]User, 0)
	if err := query.Select("id", "username", "display_name", "email", "api_status", "api_suspended_at", "api_suspended_reason").
		Order("api_suspended_at DESC").Order("id DESC").Offset(startIdx).Limit(limit).Find(&users).Error; err != nil {
		return nil, 0, err
	}
	views := make([]SuspendedUserView, len(users))
	if len(users) == 0 {
		return views, total, nil
	}
	ids := make([]int, len(users))
	for i, user := range users {
		ids[i] = user.Id
		views[i] = SuspendedUserView{Id: user.Id, Username: user.Username, DisplayName: user.DisplayName, Email: user.Email,
			APIStatus: user.APIStatus, APISuspendedAt: user.APISuspendedAt, APISuspendedReason: user.APISuspendedReason}
	}
	events := make([]UserAPIAccessEvent, 0)
	if err := DB.Where("user_id IN ? AND action = ?", ids, UserAPIAccessActionSuspend).
		Order("created_at DESC").Order("id DESC").Find(&events).Error; err != nil {
		return nil, 0, err
	}
	latest := make(map[int]UserAPIAccessEvent, len(ids))
	for _, event := range events {
		if _, exists := latest[event.UserId]; !exists {
			latest[event.UserId] = event
		}
	}
	var monitoredIds []int
	if err := DB.Model(&PromptAuditPolicy{}).
		Where("user_id IN ? AND monitor_enabled = ?", ids, true).
		Pluck("user_id", &monitoredIds).Error; err != nil {
		return nil, 0, err
	}
	monitored := make(map[int]struct{}, len(monitoredIds))
	for _, id := range monitoredIds {
		monitored[id] = struct{}{}
	}
	for i := range views {
		event := latest[views[i].Id]
		views[i].RequestId, views[i].TokenId, views[i].ModelName = event.RequestId, event.TokenId, event.ModelName
		views[i].ChannelId, views[i].UpstreamAccountId, views[i].NodeName = event.ChannelId, event.UpstreamAccountId, event.NodeName
		_, views[i].PromptMonitoringAdded = monitored[views[i].Id]
	}
	return views, total, nil
}
