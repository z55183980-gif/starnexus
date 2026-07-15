package model

import (
	"errors"
	"regexp"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

var routingNodeKeyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

type RoutingNode struct {
	Id        int    `json:"id"`
	Key       string `json:"key" gorm:"type:varchar(32);uniqueIndex;not null"`
	Name      string `json:"name" gorm:"type:varchar(64);not null"`
	Origin    string `json:"origin" gorm:"type:varchar(255);not null"`
	Enabled   bool   `json:"enabled" gorm:"not null"`
	Sort      int    `json:"sort" gorm:"default:0;not null"`
	CreatedAt int64  `json:"created_at" gorm:"bigint;not null"`
	UpdatedAt int64  `json:"updated_at" gorm:"bigint;not null"`
}

type RoutingNodeWithCount struct {
	RoutingNode
	BindingCount int64 `json:"binding_count"`
}

type RoutingNodeBoundUser struct {
	UserId      int    `json:"user_id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
}

func NormalizeRoutingNodeKey(key string) (string, error) {
	key = strings.ToLower(strings.TrimSpace(key))
	if !routingNodeKeyPattern.MatchString(key) || key == UserNodeAuto {
		return "", errors.New("invalid routing node key")
	}
	return key, nil
}

func ListRoutingNodes(includeDisabled bool) ([]RoutingNodeWithCount, error) {
	var nodes []RoutingNode
	query := DB.Order("sort ASC").Order("id ASC")
	if !includeDisabled {
		query = query.Where("enabled = ?", true)
	}
	if err := query.Find(&nodes).Error; err != nil {
		return nil, err
	}

	type countRow struct {
		Node  string
		Count int64
	}
	var rows []countRow
	if err := DB.Model(&UserNodeBinding{}).
		Select("node, COUNT(*) AS count").
		Group("node").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	counts := make(map[string]int64, len(rows))
	for _, row := range rows {
		counts[row.Node] = row.Count
	}

	result := make([]RoutingNodeWithCount, 0, len(nodes))
	for _, node := range nodes {
		result = append(result, RoutingNodeWithCount{
			RoutingNode:  node,
			BindingCount: counts[node.Key],
		})
	}
	return result, nil
}

func GetRoutingNodeById(id int) (*RoutingNode, error) {
	var node RoutingNode
	if err := DB.First(&node, id).Error; err != nil {
		return nil, err
	}
	return &node, nil
}

func GetRoutingNodeByKey(key string, enabledOnly bool) (*RoutingNode, error) {
	var node RoutingNode
	query := DB.Where("key = ?", strings.ToLower(strings.TrimSpace(key)))
	if enabledOnly {
		query = query.Where("enabled = ?", true)
	}
	if err := query.First(&node).Error; err != nil {
		return nil, err
	}
	return &node, nil
}

func CountUserNodeBindingsByNode(node string) (int64, error) {
	var count int64
	err := DB.Model(&UserNodeBinding{}).
		Where("node = ?", strings.ToLower(strings.TrimSpace(node))).
		Count(&count).Error
	return count, err
}

func ListRoutingNodeBoundUsers(node string, pageInfo *common.PageInfo) ([]RoutingNodeBoundUser, int64, error) {
	node = strings.ToLower(strings.TrimSpace(node))
	if pageInfo == nil {
		pageInfo = &common.PageInfo{Page: 1, PageSize: common.ItemsPerPage}
	}
	if pageInfo.Page < 1 {
		pageInfo.Page = 1
	}
	if pageInfo.PageSize < 1 {
		pageInfo.PageSize = common.ItemsPerPage
	}
	if pageInfo.PageSize > 100 {
		pageInfo.PageSize = 100
	}

	query := DB.Table("user_node_bindings").
		Joins("JOIN users ON users.id = user_node_bindings.user_id").
		Where("user_node_bindings.node = ?", node)

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var users []RoutingNodeBoundUser
	err := query.
		Select("users.id AS user_id, users.username, users.display_name").
		Order("users.id ASC").
		Limit(pageInfo.GetPageSize()).
		Offset(pageInfo.GetStartIdx()).
		Scan(&users).Error
	return users, total, err
}

func CreateRoutingNode(node *RoutingNode) error {
	if node == nil {
		return errors.New("routing node is required")
	}
	key, err := NormalizeRoutingNodeKey(node.Key)
	if err != nil {
		return err
	}
	now := common.GetTimestamp()
	node.Key = key
	node.Name = strings.TrimSpace(node.Name)
	node.Origin = strings.TrimSpace(node.Origin)
	node.CreatedAt = now
	node.UpdatedAt = now
	return DB.Create(node).Error
}

func UpdateRoutingNode(node *RoutingNode) error {
	if node == nil || node.Id <= 0 {
		return errors.New("invalid routing node")
	}
	return DB.Model(&RoutingNode{}).Where("id = ?", node.Id).Updates(map[string]any{
		"name":       strings.TrimSpace(node.Name),
		"origin":     strings.TrimSpace(node.Origin),
		"enabled":    node.Enabled,
		"sort":       node.Sort,
		"updated_at": common.GetTimestamp(),
	}).Error
}

func DeleteRoutingNode(id int) error {
	if id <= 0 {
		return errors.New("invalid routing node id")
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var node RoutingNode
		if err := tx.First(&node, id).Error; err != nil {
			return err
		}
		var count int64
		if err := tx.Model(&UserNodeBinding{}).Where("node = ?", node.Key).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return errors.New("routing node still has bound users")
		}
		return tx.Delete(&node).Error
	})
}

func EnsureDefaultRoutingNodes() error {
	defaults := []RoutingNode{
		{Key: "s1", Name: "S1", Origin: "origin-s1.dkby.com", Enabled: true, Sort: 1},
		{Key: "s2", Name: "S2", Origin: "origin-s2.dkby.com", Enabled: true, Sort: 2},
		{Key: "s3", Name: "S3", Origin: "origin-s3.dkby.com", Enabled: true, Sort: 3},
		{Key: "s4", Name: "S4", Origin: "origin-s4.dkby.com", Enabled: true, Sort: 4},
	}
	for i := range defaults {
		now := common.GetTimestamp()
		defaults[i].CreatedAt = now
		defaults[i].UpdatedAt = now
		if err := DB.Where("key = ?", defaults[i].Key).FirstOrCreate(&defaults[i]).Error; err != nil {
			return err
		}
	}
	return nil
}
