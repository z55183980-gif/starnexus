package model

import (
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

const (
	UserNodeAuto = "auto"
	UserNodeS1   = "s1"
	UserNodeS2   = "s2"
	UserNodeS3   = "s3"
)

type UserNodeBinding struct {
	UserId    int    `json:"user_id" gorm:"primaryKey;column:user_id"`
	Node      string `json:"node" gorm:"type:varchar(16);not null"`
	UpdatedAt int64  `json:"updated_at" gorm:"bigint;not null"`
}

func NormalizeUserNode(node string) (string, error) {
	node = strings.ToLower(strings.TrimSpace(node))
	if node == "" {
		return UserNodeAuto, nil
	}
	switch node {
	case UserNodeAuto, UserNodeS1, UserNodeS2, UserNodeS3:
		return node, nil
	default:
		return "", errors.New("invalid user routing node")
	}
}

func GetUserNodeBinding(userId int) (string, error) {
	if userId <= 0 {
		return "", errors.New("invalid user id")
	}
	var binding UserNodeBinding
	err := DB.Where("user_id = ?", userId).First(&binding).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return UserNodeAuto, nil
		}
		return "", err
	}
	return NormalizeUserNode(binding.Node)
}

func SaveUserNodeBinding(userId int, node string) error {
	normalized, err := NormalizeUserNode(node)
	if err != nil {
		return err
	}
	if normalized == UserNodeAuto {
		return DB.Where("user_id = ?", userId).Delete(&UserNodeBinding{}).Error
	}
	binding := UserNodeBinding{
		UserId:    userId,
		Node:      normalized,
		UpdatedAt: common.GetTimestamp(),
	}
	return DB.Save(&binding).Error
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
