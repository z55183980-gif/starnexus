package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
)

func TestGetEndpointTypesForAbilityUsesAlphaSearchCapability(t *testing.T) {
	const modelName = "gpt-5.6-sol"

	withoutCapability := getEndpointTypesForAbility(AbilityWithChannel{
		Ability:     Ability{Model: modelName},
		ChannelType: constant.ChannelTypeOpenAI,
	})
	require.NotContains(t, withoutCapability, constant.EndpointTypeOpenAIAlphaSearch)

	settings, err := common.Marshal(dto.ChannelOtherSettings{AlphaSearchEnabled: true})
	require.NoError(t, err)
	settingsJSON := string(settings)
	withCapability := getEndpointTypesForAbility(AbilityWithChannel{
		Ability:         Ability{Model: modelName},
		ChannelType:     constant.ChannelTypeOpenAI,
		ChannelSettings: &settingsJSON,
	})
	require.Contains(t, withCapability, constant.EndpointTypeOpenAIAlphaSearch)
}

func TestGetModelSupportEndpointTypesRefreshesInvalidatedCapability(t *testing.T) {
	truncateTables(t)
	t.Cleanup(InvalidatePricingCache)

	const modelName = "gpt-alpha-cache-refresh"
	channel := &Channel{
		Name:   "alpha search cache test",
		Type:   constant.ChannelTypeOpenAI,
		Key:    "test-key",
		Models: modelName,
		Group:  "default",
		Status: common.ChannelStatusEnabled,
	}
	require.NoError(t, DB.Create(channel).Error)
	require.NoError(t, channel.AddAbilities(nil))

	InvalidatePricingCache()
	require.NotContains(t, GetModelSupportEndpointTypes(modelName), constant.EndpointTypeOpenAIAlphaSearch)

	settings, err := common.Marshal(dto.ChannelOtherSettings{AlphaSearchEnabled: true})
	require.NoError(t, err)
	require.NoError(t, DB.Model(&Channel{}).Where("id = ?", channel.Id).Update("settings", string(settings)).Error)
	InvalidatePricingCache()

	require.Contains(t, GetModelSupportEndpointTypes(modelName), constant.EndpointTypeOpenAIAlphaSearch)
}
