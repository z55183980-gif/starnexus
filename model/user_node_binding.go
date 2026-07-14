package model

import (
	"errors"
	"regexp"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

const (
	UserNodeAuto = "auto"
	UserNodeS1   = "s1"
	UserNodeS2   = "s2"
	UserNodeS3   = "s3"
	UserNodeS4   = "s4"
)

var userNodeKeyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

type UserNodeBinding struct {
	UserId       int    `json:"user_id" gorm:"primaryKey;column:user_id"`
	Node         string `json:"node" gorm:"type:varchar(32);not null"`
	Revision     int64  `json:"revision" gorm:"bigint;default:0;not null"`
	TokensSynced bool   `json:"tokens_synced" gorm:"default:false;not null"`
	UpdatedAt    int64  `json:"updated_at" gorm:"bigint;not null"`
}

type UserNodeRoutingLock struct {
	UserId    int    `gorm:"primaryKey;column:user_id"`
	Owner     string `gorm:"type:varchar(64);not null"`
	ExpiresAt int64  `gorm:"bigint;not null;index"`
}

func NormalizeUserNode(node string) (string, error) {
	node = strings.ToLower(strings.TrimSpace(node))
	if node == "" {
		return UserNodeAuto, nil
	}
	if node == UserNodeAuto || userNodeKeyPattern.MatchString(node) {
		return node, nil
	}
	return "", errors.New("invalid user routing node")
}

func GetUserNodeBindingRecord(userId int) (*UserNodeBinding, error) {
	if userId <= 0 {
		return nil, errors.New("invalid user id")
	}
	var binding UserNodeBinding
	err := DB.Where("user_id = ?", userId).First(&binding).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &UserNodeBinding{UserId: userId, Node: UserNodeAuto}, nil
		}
		return nil, err
	}
	node, err := NormalizeUserNode(binding.Node)
	if err != nil {
		return nil, err
	}
	binding.Node = node
	return &binding, nil
}

func GetUserNodeBinding(userId int) (string, error) {
	binding, err := GetUserNodeBindingRecord(userId)
	if err != nil {
		return "", err
	}
	return binding.Node, nil
}

func SaveUserNodeBinding(userId int, node string) error {
	return SaveUserNodeBindingWithRevision(userId, node, 0)
}

func SaveUserNodeBindingWithRevision(userId int, node string, revision int64) error {
	return SaveUserNodeBindingState(userId, node, revision, false)
}

func SaveUserNodeBindingState(userId int, node string, revision int64, tokensSynced bool) error {
	normalized, err := NormalizeUserNode(node)
	if err != nil {
		return err
	}
	binding := UserNodeBinding{
		UserId:       userId,
		Node:         normalized,
		Revision:     revision,
		TokensSynced: tokensSynced,
		UpdatedAt:    common.GetTimestamp(),
	}
	return DB.Save(&binding).Error
}

func UpdateUserNodeTokensSynced(userId int, synced bool) error {
	return DB.Model(&UserNodeBinding{}).
		Where("user_id = ?", userId).
		Update("tokens_synced", synced).Error
}

func DeleteUserNodeBinding(userId int) error {
	return DB.Where("user_id = ?", userId).Delete(&UserNodeBinding{}).Error
}

func ListUserNodeBindingsBatch(afterUserId int, limit int) ([]UserNodeBinding, error) {
	if limit <= 0 {
		limit = 100
	}
	var bindings []UserNodeBinding
	err := DB.Where("user_id > ? AND node <> ?", afterUserId, UserNodeAuto).
		Order("user_id ASC").
		Limit(limit).
		Find(&bindings).Error
	return bindings, err
}

func TryAcquireUserNodeRoutingLock(userId int, owner string, expiresAt int64) (bool, error) {
	now := common.GetTimestamp()
	if err := DB.Where("user_id = ? AND expires_at <= ?", userId, now).
		Delete(&UserNodeRoutingLock{}).Error; err != nil {
		return false, err
	}
	lock := UserNodeRoutingLock{UserId: userId, Owner: owner, ExpiresAt: expiresAt}
	createErr := DB.Create(&lock).Error
	if createErr == nil {
		return true, nil
	}
	var count int64
	if err := DB.Model(&UserNodeRoutingLock{}).Where("user_id = ?", userId).Count(&count).Error; err != nil {
		return false, err
	}
	if count == 0 {
		return false, createErr
	}
	return false, nil
}

func ReleaseUserNodeRoutingLock(userId int, owner string) error {
	return DB.Where("user_id = ? AND owner = ?", userId, owner).
		Delete(&UserNodeRoutingLock{}).Error
}

func ListUserNodeBindingsByNode(node string) ([]UserNodeBinding, error) {
	var bindings []UserNodeBinding
	err := DB.Where("node = ?", strings.ToLower(strings.TrimSpace(node))).Find(&bindings).Error
	return bindings, err
}

func GetUserTokenRoutingKeys(userId int) ([]string, error) {
	var tokens []Token
	if err := DB.Where("user_id = ?", userId).
		Find(&tokens).Error; err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(tokens))
	for _, token := range tokens {
		if token.Key != "" {
			keys = append(keys, token.Key)
		}
	}
	return keys, nil
}
