package setting

import (
	"errors"
	"sort"
	"sync"

	"github.com/QuantumNous/new-api/common"
)

const (
	SecurityAuditBanOptionKey = "SecurityAuditBanConfig"

	SecurityAuditBanDurationPermanent = 0
	SecurityAuditBanDurationOneHour   = 60 * 60
	SecurityAuditBanDurationTwoHours  = 2 * 60 * 60
	SecurityAuditBanDurationOneDay    = 24 * 60 * 60
)

// SecurityAuditBanConfig controls which upstream channels may trigger a user
// API suspension after a structured cyber_policy response.
type SecurityAuditBanConfig struct {
	ChannelIds      []int `json:"channel_ids"`
	DurationSeconds int   `json:"duration_seconds"`
}

var (
	securityAuditBanMu sync.RWMutex
	securityAuditBan   = SecurityAuditBanConfig{
		ChannelIds:      []int{},
		DurationSeconds: SecurityAuditBanDurationPermanent,
	}
)

func GetSecurityAuditBanConfig() SecurityAuditBanConfig {
	securityAuditBanMu.RLock()
	defer securityAuditBanMu.RUnlock()
	return cloneSecurityAuditBanConfig(securityAuditBan)
}

func SecurityAuditBanConfig2JsonString() string {
	data, err := common.Marshal(GetSecurityAuditBanConfig())
	if err != nil {
		common.SysLog("error marshalling security audit ban config: " + err.Error())
		return "{}"
	}
	return string(data)
}

func ParseSecurityAuditBanConfigJSON(value string) (SecurityAuditBanConfig, error) {
	config := SecurityAuditBanConfig{}
	if value != "" {
		if err := common.UnmarshalJsonStr(value, &config); err != nil {
			return SecurityAuditBanConfig{}, err
		}
	}
	if !validSecurityAuditBanDuration(config.DurationSeconds) {
		return SecurityAuditBanConfig{}, errors.New("invalid security audit ban duration")
	}
	seen := make(map[int]struct{}, len(config.ChannelIds))
	channelIds := make([]int, 0, len(config.ChannelIds))
	for _, channelId := range config.ChannelIds {
		if channelId <= 0 {
			return SecurityAuditBanConfig{}, errors.New("invalid security audit ban channel")
		}
		if _, exists := seen[channelId]; exists {
			continue
		}
		seen[channelId] = struct{}{}
		channelIds = append(channelIds, channelId)
	}
	sort.Ints(channelIds)
	config.ChannelIds = channelIds
	return config, nil
}

func UpdateSecurityAuditBanConfigByJsonString(value string) error {
	config, err := ParseSecurityAuditBanConfigJSON(value)
	if err != nil {
		return err
	}
	securityAuditBanMu.Lock()
	securityAuditBan = config
	securityAuditBanMu.Unlock()
	return nil
}

func SecurityAuditBanAppliesToChannel(channelId int) (bool, int) {
	config := GetSecurityAuditBanConfig()
	for _, configuredId := range config.ChannelIds {
		if configuredId == channelId {
			return true, config.DurationSeconds
		}
	}
	return false, config.DurationSeconds
}

func validSecurityAuditBanDuration(seconds int) bool {
	switch seconds {
	case SecurityAuditBanDurationPermanent,
		SecurityAuditBanDurationOneHour,
		SecurityAuditBanDurationTwoHours,
		SecurityAuditBanDurationOneDay:
		return true
	default:
		return false
	}
}

func cloneSecurityAuditBanConfig(config SecurityAuditBanConfig) SecurityAuditBanConfig {
	config.ChannelIds = append([]int(nil), config.ChannelIds...)
	return config
}
