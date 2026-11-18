package setting

import (
	"errors"
	"sort"
	"sync"

	"github.com/QuantumNous/new-api/common"
)

const UpstreamRefusalNoRetryOptionKey = "UpstreamRefusalNoRetryConfig"

// UpstreamRefusalNoRetryConfig lists the channels whose explicit upstream
// refusals end the inbound request after one attempt: no account failover,
// no channel retry and no channel health penalty. It is empty by default so a
// release changes nothing until a channel is opted in, and clearing the list
// restores the previous retry behavior without touching global retry options.
type UpstreamRefusalNoRetryConfig struct {
	ChannelIds []int `json:"channel_ids"`
}

var (
	upstreamRefusalNoRetryMu sync.RWMutex
	upstreamRefusalNoRetry   = UpstreamRefusalNoRetryConfig{ChannelIds: []int{}}
)

func GetUpstreamRefusalNoRetryConfig() UpstreamRefusalNoRetryConfig {
	upstreamRefusalNoRetryMu.RLock()
	defer upstreamRefusalNoRetryMu.RUnlock()
	return UpstreamRefusalNoRetryConfig{ChannelIds: append([]int{}, upstreamRefusalNoRetry.ChannelIds...)}
}

func UpstreamRefusalNoRetryConfig2JsonString() string {
	data, err := common.Marshal(GetUpstreamRefusalNoRetryConfig())
	if err != nil {
		common.SysLog("error marshalling upstream refusal no-retry config: " + err.Error())
		return `{"channel_ids":[]}`
	}
	return string(data)
}

func ParseUpstreamRefusalNoRetryConfigJSON(value string) (UpstreamRefusalNoRetryConfig, error) {
	config := UpstreamRefusalNoRetryConfig{}
	if value != "" {
		if err := common.UnmarshalJsonStr(value, &config); err != nil {
			return UpstreamRefusalNoRetryConfig{}, err
		}
	}
	seen := make(map[int]struct{}, len(config.ChannelIds))
	channelIds := make([]int, 0, len(config.ChannelIds))
	for _, channelId := range config.ChannelIds {
		if channelId <= 0 {
			return UpstreamRefusalNoRetryConfig{}, errors.New("invalid upstream refusal no-retry channel")
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

func UpdateUpstreamRefusalNoRetryConfigByJsonString(value string) error {
	config, err := ParseUpstreamRefusalNoRetryConfigJSON(value)
	if err != nil {
		return err
	}
	upstreamRefusalNoRetryMu.Lock()
	upstreamRefusalNoRetry = config
	upstreamRefusalNoRetryMu.Unlock()
	return nil
}

func UpstreamRefusalNoRetryAppliesToChannel(channelId int) bool {
	if channelId <= 0 {
		return false
	}
	upstreamRefusalNoRetryMu.RLock()
	defer upstreamRefusalNoRetryMu.RUnlock()
	for _, configuredId := range upstreamRefusalNoRetry.ChannelIds {
		if configuredId == channelId {
			return true
		}
	}
	return false
}
