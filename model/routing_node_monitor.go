package model

import (
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

type RoutingNodeMonitorStatus struct {
	NodeId                          int                               `json:"node_id" gorm:"primaryKey;autoIncrement:false"`
	NodeName                        string                            `json:"node_name" gorm:"type:varchar(128);not null"`
	CPUUsage                        float64                           `json:"cpu_usage" gorm:"not null"`
	CPUCores                        int                               `json:"cpu_cores" gorm:"not null"`
	LoadOne                         float64                           `json:"load_one" gorm:"not null"`
	LoadPercent                     float64                           `json:"load_percent" gorm:"not null"`
	MemoryUsed                      uint64                            `json:"memory_used" gorm:"not null"`
	MemoryTotal                     uint64                            `json:"memory_total" gorm:"not null"`
	MemoryPercent                   float64                           `json:"memory_percent" gorm:"not null"`
	DiskUsed                        uint64                            `json:"disk_used" gorm:"not null"`
	DiskTotal                       uint64                            `json:"disk_total" gorm:"not null"`
	DiskPercent                     float64                           `json:"disk_percent" gorm:"not null"`
	NetworkBytesSent                uint64                            `json:"network_bytes_sent" gorm:"default:0;not null"`
	NetworkBytesReceived            uint64                            `json:"network_bytes_received" gorm:"default:0;not null"`
	NetworkUploadBps                float64                           `json:"network_upload_bps" gorm:"default:0;not null"`
	NetworkDownloadBps              float64                           `json:"network_download_bps" gorm:"default:0;not null"`
	DatabaseMetricsEnabled          bool                              `json:"database_metrics_enabled" gorm:"default:false;not null"`
	PostgreSQLStatus                string                            `json:"postgresql_status" gorm:"type:varchar(24);default:'not_configured';not null"`
	PostgreSQLConnections           int                               `json:"postgresql_connections" gorm:"default:0;not null"`
	PostgreSQLMaxConnections        int                               `json:"postgresql_max_connections" gorm:"default:0;not null"`
	PostgreSQLDatabaseSize          uint64                            `json:"postgresql_database_size" gorm:"default:0;not null"`
	PostgreSQLCacheHitPercent       float64                           `json:"postgresql_cache_hit_percent" gorm:"default:0;not null"`
	PostgreSQLReplicationStatus     string                            `json:"postgresql_replication_status" gorm:"type:varchar(24);default:'not_configured';not null"`
	PostgreSQLReplicationLagSeconds float64                           `json:"postgresql_replication_lag_seconds" gorm:"default:0;not null"`
	RedisStatus                     string                            `json:"redis_status" gorm:"type:varchar(24);default:'not_configured';not null"`
	RedisMemoryUsed                 uint64                            `json:"redis_memory_used" gorm:"default:0;not null"`
	RedisMemoryMax                  uint64                            `json:"redis_memory_max" gorm:"default:0;not null"`
	PgBouncerStatus                 string                            `json:"pgbouncer_status" gorm:"type:varchar(24);default:'not_configured';not null"`
	BackupLastAt                    int64                             `json:"backup_last_at" gorm:"bigint;default:0;not null"`
	BackupSize                      uint64                            `json:"backup_size" gorm:"default:0;not null"`
	UptimeSeconds                   uint64                            `json:"uptime_seconds" gorm:"not null"`
	AppVersion                      string                            `json:"app_version" gorm:"type:varchar(64);not null"`
	ReportedAt                      int64                             `json:"reported_at" gorm:"bigint;index;not null"`
	UpdatedAt                       int64                             `json:"updated_at" gorm:"bigint;not null"`
	NetworkSamples                  []RoutingNodeMonitorNetworkSample `json:"network_samples,omitempty" gorm:"-"`
}

type RoutingNodeMonitorNetworkSample struct {
	Id          int64   `json:"-"`
	NodeId      int     `json:"-" gorm:"index:idx_routing_node_monitor_network_samples_node_reported;not null"`
	UploadBps   float64 `json:"upload_bps" gorm:"not null"`
	DownloadBps float64 `json:"download_bps" gorm:"not null"`
	ReportedAt  int64   `json:"reported_at" gorm:"index:idx_routing_node_monitor_network_samples_node_reported;not null"`
}

type RoutingNodeMonitorEnrollment struct {
	Id        int    `json:"id" gorm:"primaryKey;autoIncrement:false"`
	Token     string `json:"-" gorm:"type:varchar(128);default:'';not null"`
	TokenHash string `json:"-" gorm:"type:varchar(64);not null"`
	UpdatedAt int64  `json:"updated_at" gorm:"bigint;not null"`
}

func attachRoutingNodeMonitorStatuses(nodes []RoutingNodeWithCount) error {
	if len(nodes) == 0 {
		return nil
	}
	ids := make([]int, 0, len(nodes))
	for _, node := range nodes {
		ids = append(ids, node.Id)
	}
	var statuses []RoutingNodeMonitorStatus
	if err := DB.Where("node_id IN ?", ids).Find(&statuses).Error; err != nil {
		return err
	}
	byNodeId := make(map[int]*RoutingNodeMonitorStatus, len(statuses))
	for i := range statuses {
		byNodeId[statuses[i].NodeId] = &statuses[i]
	}
	for i := range nodes {
		nodes[i].MonitorStatus = byNodeId[nodes[i].Id]
	}
	var samples []RoutingNodeMonitorNetworkSample
	if err := DB.Where("node_id IN ?", ids).
		Order("node_id ASC").
		Order("reported_at ASC").
		Order("id ASC").
		Find(&samples).Error; err != nil {
		return err
	}
	for i := range samples {
		if status := byNodeId[samples[i].NodeId]; status != nil {
			status.NetworkSamples = append(status.NetworkSamples, samples[i])
		}
	}
	return nil
}

func GetRoutingNodeByMonitorTokenHash(tokenHash string) (*RoutingNode, error) {
	tokenHash = strings.TrimSpace(tokenHash)
	if tokenHash == "" {
		return nil, errors.New("monitor token hash is required")
	}
	var node RoutingNode
	if err := DB.Where("monitor_enabled = ? AND monitor_token_hash = ?", true, tokenHash).First(&node).Error; err != nil {
		return nil, err
	}
	return &node, nil
}

func SaveRoutingNodeMonitorToken(nodeId int, tokenHash string) error {
	if nodeId <= 0 || strings.TrimSpace(tokenHash) == "" {
		return errors.New("invalid routing node monitor token")
	}
	now := common.GetTimestamp()
	result := DB.Model(&RoutingNode{}).Where("id = ?", nodeId).Updates(map[string]any{
		"monitor_enabled":          true,
		"monitor_token_hash":       strings.TrimSpace(tokenHash),
		"monitor_token_updated_at": now,
		"updated_at":               now,
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New("routing node not found")
	}
	return nil
}

func SaveRoutingNodeMonitorStatus(status *RoutingNodeMonitorStatus) error {
	if status == nil || status.NodeId <= 0 {
		return errors.New("invalid routing node monitor status")
	}
	status.UpdatedAt = common.GetTimestamp()
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(status).Error; err != nil {
			return err
		}
		if err := tx.Create(&RoutingNodeMonitorNetworkSample{
			NodeId:      status.NodeId,
			UploadBps:   status.NetworkUploadBps,
			DownloadBps: status.NetworkDownloadBps,
			ReportedAt:  status.ReportedAt,
		}).Error; err != nil {
			return err
		}
		var staleIds []int64
		if err := tx.Model(&RoutingNodeMonitorNetworkSample{}).
			Where("node_id = ?", status.NodeId).
			Order("reported_at DESC").
			Order("id DESC").
			Offset(60).
			Pluck("id", &staleIds).Error; err != nil {
			return err
		}
		if len(staleIds) == 0 {
			return nil
		}
		return tx.Where("id IN ?", staleIds).Delete(&RoutingNodeMonitorNetworkSample{}).Error
	})
}

func GetRoutingNodeMonitorEnrollment() (*RoutingNodeMonitorEnrollment, error) {
	var enrollment RoutingNodeMonitorEnrollment
	if err := DB.First(&enrollment, 1).Error; err != nil {
		return nil, err
	}
	return &enrollment, nil
}

func SaveRoutingNodeMonitorEnrollmentToken(token string, tokenHash string) error {
	token = strings.TrimSpace(token)
	tokenHash = strings.TrimSpace(tokenHash)
	if token == "" || tokenHash == "" {
		return errors.New("routing node monitor enrollment token is required")
	}
	return DB.Save(&RoutingNodeMonitorEnrollment{
		Id:        1,
		Token:     token,
		TokenHash: tokenHash,
		UpdatedAt: common.GetTimestamp(),
	}).Error
}
