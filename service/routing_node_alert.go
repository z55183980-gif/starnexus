package service

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/bytedance/gopkg/util/gopool"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var routingNodeAlertEvaluatorOnce sync.Once

func severityRank(severity string) int {
	switch severity {
	case model.RoutingNodeAlertSeverityCritical:
		return 2
	case model.RoutingNodeAlertSeverityWarning:
		return 1
	default:
		return 0
	}
}

func routingNodeAlertTargetSeverity(rule *model.RoutingNodeAlertRule, value float64) string {
	if value >= rule.CriticalThreshold {
		return model.RoutingNodeAlertSeverityCritical
	}
	if value >= rule.WarningThreshold {
		return model.RoutingNodeAlertSeverityWarning
	}
	return ""
}

func routingNodeAlertThreshold(state *model.RoutingNodeAlertState) float64 {
	if state.Severity == model.RoutingNodeAlertSeverityCritical {
		return state.CriticalThreshold
	}
	return state.WarningThreshold
}

func evaluateRoutingNodeAlertValue(node *model.RoutingNode, rule *model.RoutingNodeAlertRule, value float64, now int64) (*model.RoutingNodeAlertState, *model.RoutingNodeAlertEvent, error) {
	if node == nil || rule == nil || !rule.Enabled || math.IsNaN(value) || math.IsInf(value, 0) {
		return nil, nil, errors.New("invalid routing node alert evaluation")
	}
	if now <= 0 {
		now = common.GetTimestamp()
	}
	var state model.RoutingNodeAlertState
	var event *model.RoutingNodeAlertEvent
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		query := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("node_id = ? AND rule_key = ?", node.Id, rule.Key).
			Limit(1).
			Find(&state)
		err := query.Error
		if err == nil && query.RowsAffected == 0 {
			state = model.RoutingNodeAlertState{
				NodeId: node.Id, NodeKey: node.Key, NodeName: node.Name,
				RuleKey: rule.Key, Metric: rule.Metric,
				Status:            model.RoutingNodeAlertStatusNormal,
				WarningThreshold:  rule.WarningThreshold,
				CriticalThreshold: rule.CriticalThreshold,
				CreatedAt:         now, UpdatedAt: now,
			}
			if err = tx.Create(&state).Error; err != nil {
				return err
			}
		} else if err != nil {
			return err
		}

		state.NodeKey = node.Key
		state.NodeName = node.Name
		state.Metric = rule.Metric
		state.WarningThreshold = rule.WarningThreshold
		state.CriticalThreshold = rule.CriticalThreshold
		state.CurrentValue = value
		state.UpdatedAt = now

		targetSeverity := routingNodeAlertTargetSeverity(rule, value)
		active := state.Status == model.RoutingNodeAlertStatusFiring || state.Status == model.RoutingNodeAlertStatusAcknowledged
		if targetSeverity != "" {
			if state.ConsecutiveBreaches == 0 && !active {
				state.FirstSeenAt = now
				state.PeakValue = value
			}
			state.ConsecutiveBreaches++
			state.ConsecutiveRecoveries = 0
			state.LastSeenAt = now
			if value > state.PeakValue {
				state.PeakValue = value
			}
			triggerCount := rule.TriggerCount
			if triggerCount < 1 {
				triggerCount = 1
			}
			if !active && state.ConsecutiveBreaches >= triggerCount {
				state.Status = model.RoutingNodeAlertStatusFiring
				state.Severity = targetSeverity
				state.TriggeredAt = now
				state.ResolvedAt = 0
				state.AcknowledgedBy = 0
				state.AcknowledgedAt = 0
				state.OccurrenceCount++
				event = &model.RoutingNodeAlertEvent{EventType: model.RoutingNodeAlertEventTriggered}
			} else if active && severityRank(targetSeverity) > severityRank(state.Severity) {
				state.Status = model.RoutingNodeAlertStatusFiring
				state.Severity = targetSeverity
				state.AcknowledgedBy = 0
				state.AcknowledgedAt = 0
				event = &model.RoutingNodeAlertEvent{EventType: model.RoutingNodeAlertEventEscalated}
			}
		} else if active {
			state.ConsecutiveBreaches = 0
			if value <= rule.RecoveryThreshold {
				state.ConsecutiveRecoveries++
			} else {
				state.ConsecutiveRecoveries = 0
			}
			recoveryCount := rule.RecoveryCount
			if recoveryCount < 1 {
				recoveryCount = 1
			}
			if state.ConsecutiveRecoveries >= recoveryCount {
				state.Status = model.RoutingNodeAlertStatusResolved
				state.ResolvedAt = now
				state.ConsecutiveRecoveries = 0
				event = &model.RoutingNodeAlertEvent{EventType: model.RoutingNodeAlertEventRecovered}
			}
		} else {
			state.ConsecutiveBreaches = 0
			state.ConsecutiveRecoveries = 0
		}

		if err := tx.Save(&state).Error; err != nil {
			return err
		}
		if event == nil {
			return nil
		}
		event.StateId = state.Id
		event.NodeId = state.NodeId
		event.RuleKey = state.RuleKey
		event.Severity = state.Severity
		event.Value = value
		event.Threshold = routingNodeAlertThreshold(&state)
		event.CreatedAt = now
		return tx.Create(event).Error
	})
	if err != nil {
		return nil, nil, err
	}
	return &state, event, nil
}

func evaluateRoutingNodeMonitorAlerts(node *model.RoutingNode, status *model.RoutingNodeMonitorStatus) error {
	if node == nil || status == nil {
		return errors.New("routing node monitor status is required")
	}
	rules, err := model.ListRoutingNodeAlertRules(true)
	if err != nil {
		return err
	}
	values := map[string]float64{
		"cpu_usage":      status.CPUUsage,
		"load_percent":   status.LoadPercent,
		"memory_percent": status.MemoryPercent,
		"disk_percent":   status.DiskPercent,
	}
	for i := range rules {
		value, ok := values[rules[i].Key]
		if !ok {
			continue
		}
		state, event, evaluateErr := evaluateRoutingNodeAlertValue(node, &rules[i], value, status.ReportedAt)
		if evaluateErr != nil {
			return evaluateErr
		}
		dispatchRoutingNodeAlertTransition(state, event)
	}
	return nil
}

func EvaluateRoutingNodeHeartbeatAlerts() error {
	var rule model.RoutingNodeAlertRule
	if err := model.DB.Where("key = ? AND enabled = ?", "heartbeat_age", true).First(&rule).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	var nodes []model.RoutingNode
	if err := model.DB.Where("monitor_enabled = ? AND monitor_token_hash <> ?", true, "").Find(&nodes).Error; err != nil {
		return err
	}
	now := common.GetTimestamp()
	for i := range nodes {
		var status model.RoutingNodeMonitorStatus
		baseline := nodes[i].MonitorTokenUpdatedAt
		err := model.DB.Where("node_id = ?", nodes[i].Id).First(&status).Error
		if err == nil {
			baseline = status.ReportedAt
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if baseline <= 0 {
			baseline = nodes[i].UpdatedAt
		}
		age := float64(now - baseline)
		if age < 0 {
			age = 0
		}
		state, event, evaluateErr := evaluateRoutingNodeAlertValue(&nodes[i], &rule, age, now)
		if evaluateErr != nil {
			return evaluateErr
		}
		dispatchRoutingNodeAlertTransition(state, event)
	}
	return nil
}

func dispatchRoutingNodeAlertTransition(state *model.RoutingNodeAlertState, event *model.RoutingNodeAlertEvent) {
	if state == nil || event == nil {
		return
	}
	if err := common.PublishBusinessMonitorEvent("node-alert", state); err != nil {
		common.SysLog("failed to publish routing node alert: " + err.Error())
	}
	if state.SilencedUntil > event.CreatedAt {
		return
	}
	stateCopy := *state
	eventCopy := *event
	gopool.Go(func() {
		notifyRoutingNodeAlert(&stateCopy, &eventCopy)
	})
}

func notifyRoutingNodeAlert(state *model.RoutingNodeAlertState, event *model.RoutingNodeAlertEvent) {
	if state == nil || event == nil {
		return
	}
	subject := fmt.Sprintf("[%s] Node %s alert", strings.ToUpper(state.Severity), state.NodeName)
	if event.EventType == model.RoutingNodeAlertEventRecovered {
		subject = fmt.Sprintf("[RECOVERED] Node %s", state.NodeName)
	}
	content := fmt.Sprintf("Node: %s (%s)\nRule: %s\nMetric: %s\nCurrent: %.2f\nThreshold: %.2f\nEvent: %s", state.NodeName, state.NodeKey, state.RuleKey, state.Metric, event.Value, event.Threshold, event.EventType)
	notifyType := fmt.Sprintf("%s_%s_%s_%s_%d", dto.NotifyTypeNodeAlert, state.NodeKey, state.RuleKey, event.EventType, state.OccurrenceCount)
	NotifyRootUser(notifyType, subject, content)
}

func StartRoutingNodeAlertEvaluator() {
	routingNodeAlertEvaluatorOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}
		gopool.Go(func() {
			ticker := time.NewTicker(15 * time.Second)
			defer ticker.Stop()
			for {
				if err := EvaluateRoutingNodeHeartbeatAlerts(); err != nil {
					common.SysLog("routing node heartbeat alert evaluation failed: " + err.Error())
				}
				<-ticker.C
			}
		})
	})
}
