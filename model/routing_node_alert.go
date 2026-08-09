package model

import (
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

const (
	RoutingNodeAlertStatusNormal       = "normal"
	RoutingNodeAlertStatusFiring       = "firing"
	RoutingNodeAlertStatusAcknowledged = "acknowledged"
	RoutingNodeAlertStatusResolved     = "resolved"

	RoutingNodeAlertSeverityWarning  = "warning"
	RoutingNodeAlertSeverityCritical = "critical"

	RoutingNodeAlertEventTriggered    = "triggered"
	RoutingNodeAlertEventEscalated    = "escalated"
	RoutingNodeAlertEventAcknowledged = "acknowledged"
	RoutingNodeAlertEventSilenced     = "silenced"
	RoutingNodeAlertEventUnsilenced   = "unsilenced"
	RoutingNodeAlertEventRecovered    = "recovered"
	RoutingNodeAlertEventResolved     = "resolved"
)

type RoutingNodeAlertRule struct {
	Id                int     `json:"id"`
	Key               string  `json:"key" gorm:"type:varchar(64);uniqueIndex;not null"`
	Name              string  `json:"name" gorm:"type:varchar(128);not null"`
	Metric            string  `json:"metric" gorm:"type:varchar(64);not null"`
	WarningThreshold  float64 `json:"warning_threshold" gorm:"not null"`
	CriticalThreshold float64 `json:"critical_threshold" gorm:"not null"`
	RecoveryThreshold float64 `json:"recovery_threshold" gorm:"not null"`
	TriggerCount      int     `json:"trigger_count" gorm:"default:1;not null"`
	RecoveryCount     int     `json:"recovery_count" gorm:"default:1;not null"`
	Enabled           bool    `json:"enabled" gorm:"default:true;not null"`
	Sort              int     `json:"sort" gorm:"default:0;not null"`
	CreatedAt         int64   `json:"created_at" gorm:"bigint;not null"`
	UpdatedAt         int64   `json:"updated_at" gorm:"bigint;not null"`
}

type RoutingNodeAlertState struct {
	Id                    int64   `json:"id"`
	NodeId                int     `json:"node_id" gorm:"uniqueIndex:idx_routing_node_alert_state_node_rule;not null"`
	NodeKey               string  `json:"node_key" gorm:"type:varchar(32);index;not null"`
	NodeName              string  `json:"node_name" gorm:"type:varchar(64);not null"`
	RuleKey               string  `json:"rule_key" gorm:"type:varchar(64);uniqueIndex:idx_routing_node_alert_state_node_rule;not null"`
	Metric                string  `json:"metric" gorm:"type:varchar(64);not null"`
	Status                string  `json:"status" gorm:"type:varchar(24);index;not null"`
	Severity              string  `json:"severity" gorm:"type:varchar(16);index;not null"`
	CurrentValue          float64 `json:"current_value" gorm:"not null"`
	PeakValue             float64 `json:"peak_value" gorm:"not null"`
	WarningThreshold      float64 `json:"warning_threshold" gorm:"not null"`
	CriticalThreshold     float64 `json:"critical_threshold" gorm:"not null"`
	ConsecutiveBreaches   int     `json:"consecutive_breaches" gorm:"default:0;not null"`
	ConsecutiveRecoveries int     `json:"consecutive_recoveries" gorm:"default:0;not null"`
	FirstSeenAt           int64   `json:"first_seen_at" gorm:"bigint;default:0;not null"`
	LastSeenAt            int64   `json:"last_seen_at" gorm:"bigint;default:0;not null"`
	TriggeredAt           int64   `json:"triggered_at" gorm:"bigint;default:0;not null"`
	ResolvedAt            int64   `json:"resolved_at" gorm:"bigint;default:0;not null"`
	AcknowledgedBy        int     `json:"acknowledged_by" gorm:"default:0;not null"`
	AcknowledgedAt        int64   `json:"acknowledged_at" gorm:"bigint;default:0;not null"`
	SilencedUntil         int64   `json:"silenced_until" gorm:"bigint;default:0;not null"`
	OccurrenceCount       int     `json:"occurrence_count" gorm:"default:0;not null"`
	CreatedAt             int64   `json:"created_at" gorm:"bigint;not null"`
	UpdatedAt             int64   `json:"updated_at" gorm:"bigint;not null"`
}

type RoutingNodeAlertEvent struct {
	Id        int64   `json:"id"`
	StateId   int64   `json:"state_id" gorm:"index;not null"`
	NodeId    int     `json:"node_id" gorm:"index;not null"`
	RuleKey   string  `json:"rule_key" gorm:"type:varchar(64);index;not null"`
	EventType string  `json:"event_type" gorm:"type:varchar(24);not null"`
	Severity  string  `json:"severity" gorm:"type:varchar(16);not null"`
	Value     float64 `json:"value" gorm:"not null"`
	Threshold float64 `json:"threshold" gorm:"not null"`
	ActorId   int     `json:"actor_id" gorm:"default:0;not null"`
	CreatedAt int64   `json:"created_at" gorm:"bigint;index;not null"`
}

type RoutingNodeAlertSummary struct {
	HealthState     string `json:"health_state"`
	ActiveCount     int    `json:"active_count"`
	WarningCount    int    `json:"warning_count"`
	CriticalCount   int    `json:"critical_count"`
	SilencedCount   int    `json:"silenced_count"`
	HighestSeverity string `json:"highest_severity"`
}

type RoutingNodeAlertUnreadSummary struct {
	UnreadCount     int64 `json:"unread_count"`
	LatestEventId   int64 `json:"latest_event_id"`
	LastReadEventId int64 `json:"last_read_event_id"`
}

func EnsureDefaultRoutingNodeAlertRules() error {
	now := common.GetTimestamp()
	defaults := []RoutingNodeAlertRule{
		{Key: "heartbeat_age", Name: "Monitor heartbeat overdue", Metric: "heartbeat_age_seconds", WarningThreshold: 45, CriticalThreshold: 90, RecoveryThreshold: 30, TriggerCount: 1, RecoveryCount: 1, Enabled: true, Sort: 1},
		{Key: "cpu_usage", Name: "CPU usage high", Metric: "cpu_usage_percent", WarningThreshold: 85, CriticalThreshold: 95, RecoveryThreshold: 75, TriggerCount: 2, RecoveryCount: 4, Enabled: true, Sort: 2},
		{Key: "load_percent", Name: "System load high", Metric: "load_percent", WarningThreshold: 150, CriticalThreshold: 300, RecoveryThreshold: 100, TriggerCount: 2, RecoveryCount: 4, Enabled: true, Sort: 3},
		{Key: "memory_percent", Name: "Memory usage high", Metric: "memory_usage_percent", WarningThreshold: 85, CriticalThreshold: 95, RecoveryThreshold: 75, TriggerCount: 4, RecoveryCount: 4, Enabled: true, Sort: 4},
		{Key: "disk_percent", Name: "Disk usage high", Metric: "disk_usage_percent", WarningThreshold: 85, CriticalThreshold: 95, RecoveryThreshold: 80, TriggerCount: 4, RecoveryCount: 4, Enabled: true, Sort: 5},
	}
	for i := range defaults {
		defaults[i].CreatedAt = now
		defaults[i].UpdatedAt = now
		if err := DB.Where("key = ?", defaults[i].Key).FirstOrCreate(&defaults[i]).Error; err != nil {
			return err
		}
	}
	return nil
}

func ListRoutingNodeAlertRules(enabledOnly bool) ([]RoutingNodeAlertRule, error) {
	var rules []RoutingNodeAlertRule
	query := DB.Order("sort ASC").Order("id ASC")
	if enabledOnly {
		query = query.Where("enabled = ?", true)
	}
	return rules, query.Find(&rules).Error
}

func ListRoutingNodeAlerts(status, severity string, nodeId, limit int) ([]RoutingNodeAlertState, error) {
	var alerts []RoutingNodeAlertState
	query := DB.Order("updated_at DESC").Order("id DESC")
	if normalized := strings.TrimSpace(status); normalized != "" {
		query = query.Where("status = ?", normalized)
	} else {
		query = query.Where("status IN ?", []string{RoutingNodeAlertStatusFiring, RoutingNodeAlertStatusAcknowledged})
	}
	if normalized := strings.TrimSpace(severity); normalized != "" {
		query = query.Where("severity = ?", normalized)
	}
	if nodeId > 0 {
		query = query.Where("node_id = ?", nodeId)
	}
	if limit < 1 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	return alerts, query.Limit(limit).Find(&alerts).Error
}

func GetRoutingNodeAlertState(id int64) (*RoutingNodeAlertState, error) {
	var state RoutingNodeAlertState
	if err := DB.First(&state, id).Error; err != nil {
		return nil, err
	}
	return &state, nil
}

func ListRoutingNodeAlertEvents(stateId int64, limit int) ([]RoutingNodeAlertEvent, error) {
	var events []RoutingNodeAlertEvent
	if limit < 1 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	return events, DB.Where("state_id = ?", stateId).Order("created_at DESC").Order("id DESC").Limit(limit).Find(&events).Error
}

func latestUnreadRoutingNodeAlertEventId() (int64, error) {
	var event RoutingNodeAlertEvent
	err := DB.Where("event_type IN ?", []string{RoutingNodeAlertEventTriggered, RoutingNodeAlertEventEscalated}).
		Order("id DESC").First(&event).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return event.Id, nil
}

func GetRoutingNodeAlertUnreadSummary(userId int) (*RoutingNodeAlertUnreadSummary, error) {
	if userId <= 0 {
		return nil, errors.New("invalid user id")
	}
	setting, err := GetUserSetting(userId, false)
	if err != nil {
		return nil, err
	}
	latestEventId, err := latestUnreadRoutingNodeAlertEventId()
	if err != nil {
		return nil, err
	}
	summary := &RoutingNodeAlertUnreadSummary{
		LatestEventId: latestEventId, LastReadEventId: setting.ReadRoutingNodeAlertEventId,
	}
	if latestEventId <= setting.ReadRoutingNodeAlertEventId {
		return summary, nil
	}
	var stateIds []int64
	if err := DB.Model(&RoutingNodeAlertEvent{}).
		Where("id > ? AND event_type IN ?", setting.ReadRoutingNodeAlertEventId, []string{RoutingNodeAlertEventTriggered, RoutingNodeAlertEventEscalated}).
		Distinct("state_id").Pluck("state_id", &stateIds).Error; err != nil {
		return nil, err
	}
	if len(stateIds) == 0 {
		return summary, nil
	}
	if err := DB.Model(&RoutingNodeAlertState{}).
		Where("id IN ? AND status IN ?", stateIds, []string{RoutingNodeAlertStatusFiring, RoutingNodeAlertStatusAcknowledged}).
		Count(&summary.UnreadCount).Error; err != nil {
		return nil, err
	}
	return summary, nil
}

func MarkRoutingNodeAlertsRead(userId int) (*RoutingNodeAlertUnreadSummary, error) {
	if userId <= 0 {
		return nil, errors.New("invalid user id")
	}
	latestEventId, err := latestUnreadRoutingNodeAlertEventId()
	if err != nil {
		return nil, err
	}
	user, err := GetUserById(userId, false)
	if err != nil {
		return nil, err
	}
	setting := user.GetSetting()
	if latestEventId > setting.ReadRoutingNodeAlertEventId {
		setting.ReadRoutingNodeAlertEventId = latestEventId
		user.SetSetting(setting)
		if err := user.Update(false); err != nil {
			return nil, err
		}
	}
	return &RoutingNodeAlertUnreadSummary{
		UnreadCount: 0, LatestEventId: latestEventId, LastReadEventId: setting.ReadRoutingNodeAlertEventId,
	}, nil
}

func MutateRoutingNodeAlert(id int64, actorId int, eventType string, silencedUntil int64) (*RoutingNodeAlertState, error) {
	var state RoutingNodeAlertState
	now := common.GetTimestamp()
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&state, id).Error; err != nil {
			return err
		}
		switch eventType {
		case RoutingNodeAlertEventAcknowledged:
			if state.Status != RoutingNodeAlertStatusFiring && state.Status != RoutingNodeAlertStatusAcknowledged {
				return errors.New("only active alerts can be acknowledged")
			}
			state.Status = RoutingNodeAlertStatusAcknowledged
			state.AcknowledgedBy = actorId
			state.AcknowledgedAt = now
		case RoutingNodeAlertEventSilenced, RoutingNodeAlertEventUnsilenced:
			state.SilencedUntil = silencedUntil
		case RoutingNodeAlertEventResolved:
			if state.Status != RoutingNodeAlertStatusFiring && state.Status != RoutingNodeAlertStatusAcknowledged {
				return errors.New("only active alerts can be resolved")
			}
			state.Status = RoutingNodeAlertStatusResolved
			state.ResolvedAt = now
			state.ConsecutiveBreaches = 0
			state.ConsecutiveRecoveries = 0
		default:
			return errors.New("invalid routing node alert mutation")
		}
		state.UpdatedAt = now
		if err := tx.Save(&state).Error; err != nil {
			return err
		}
		threshold := state.WarningThreshold
		if state.Severity == RoutingNodeAlertSeverityCritical {
			threshold = state.CriticalThreshold
		}
		return tx.Create(&RoutingNodeAlertEvent{
			StateId: state.Id, NodeId: state.NodeId, RuleKey: state.RuleKey,
			EventType: eventType, Severity: state.Severity, Value: state.CurrentValue,
			Threshold: threshold, ActorId: actorId, CreatedAt: now,
		}).Error
	})
	if err != nil {
		return nil, err
	}
	return &state, nil
}

func ResolveRoutingNodeAlertsForNode(nodeId int, actorId int) ([]RoutingNodeAlertState, error) {
	var active []RoutingNodeAlertState
	if err := DB.Where("node_id = ? AND status IN ?", nodeId, []string{RoutingNodeAlertStatusFiring, RoutingNodeAlertStatusAcknowledged}).Find(&active).Error; err != nil {
		return nil, err
	}
	resolved := make([]RoutingNodeAlertState, 0, len(active))
	for i := range active {
		state, err := MutateRoutingNodeAlert(active[i].Id, actorId, RoutingNodeAlertEventResolved, 0)
		if err != nil {
			return nil, err
		}
		resolved = append(resolved, *state)
	}
	return resolved, nil
}

func attachRoutingNodeAlertSummaries(nodes []RoutingNodeWithCount) error {
	if len(nodes) == 0 {
		return nil
	}
	// Keep rolling upgrades and narrowly scoped unit-test schemas readable until
	// the new alert tables have been migrated.
	if !DB.Migrator().HasTable(&RoutingNodeAlertState{}) {
		return nil
	}
	ids := make([]int, 0, len(nodes))
	for i := range nodes {
		ids = append(ids, nodes[i].Id)
		nodes[i].AlertSummary = &RoutingNodeAlertSummary{HealthState: "healthy"}
	}
	var states []RoutingNodeAlertState
	if err := DB.Where("node_id IN ? AND status IN ?", ids, []string{RoutingNodeAlertStatusFiring, RoutingNodeAlertStatusAcknowledged}).Find(&states).Error; err != nil {
		return err
	}
	byNode := make(map[int]*RoutingNodeAlertSummary, len(nodes))
	for i := range nodes {
		byNode[nodes[i].Id] = nodes[i].AlertSummary
	}
	now := common.GetTimestamp()
	for i := range states {
		summary := byNode[states[i].NodeId]
		if summary == nil {
			continue
		}
		summary.ActiveCount++
		if states[i].SilencedUntil > now {
			summary.SilencedCount++
		}
		if states[i].Severity == RoutingNodeAlertSeverityCritical {
			summary.CriticalCount++
			summary.HighestSeverity = RoutingNodeAlertSeverityCritical
			summary.HealthState = RoutingNodeAlertSeverityCritical
		} else {
			summary.WarningCount++
			if summary.HighestSeverity == "" {
				summary.HighestSeverity = RoutingNodeAlertSeverityWarning
				summary.HealthState = RoutingNodeAlertSeverityWarning
			}
		}
	}
	return nil
}
