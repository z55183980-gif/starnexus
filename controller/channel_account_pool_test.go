package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestValidateChannelWithLocalAccountPool(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.Channel{},
		&model.UpstreamAccountPool{},
		&model.UpstreamAccountPoolMember{},
		&model.UpstreamAccount{},
		&model.UpstreamAccountEvent{},
	))
	originalDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = originalDB })

	pool := model.UpstreamAccountPool{
		Name: "codex", Platform: constant.UpstreamPlatformOpenAI, CredentialType: constant.UpstreamAccountTypeOAuth,
		Status: constant.UpstreamStatusActive, SchedulerConfig: "{}", CreatedAt: 1, UpdatedAt: 1,
	}
	require.NoError(t, db.Create(&pool).Error)

	channel := &model.Channel{
		Type: constant.ChannelTypeCodex, Name: "local-codex", Key: "",
		CredentialSource: constant.ChannelCredentialSourceAccountPool, UpstreamAccountPoolId: &pool.Id,
	}
	require.NoError(t, validateChannel(channel, true))
	require.Equal(t, constant.ChannelTypeOpenAI, channel.Type)

	channel.ChannelInfo.IsMultiKey = true
	require.Error(t, validateChannel(channel, true))
	channel.ChannelInfo.IsMultiKey = false
	channel.Type = constant.ChannelTypeAnthropic
	require.Error(t, validateChannel(channel, true))

	anthropicPool := model.UpstreamAccountPool{
		Name: "claude", Platform: constant.UpstreamPlatformAnthropic, CredentialType: "mixed",
		Status: constant.UpstreamStatusActive, SchedulerConfig: "{}", CreatedAt: 1, UpdatedAt: 1,
	}
	require.NoError(t, db.Create(&anthropicPool).Error)
	anthropicChannel := &model.Channel{
		Type: constant.ChannelTypeAnthropic, Name: "local-claude", Key: "",
		CredentialSource: constant.ChannelCredentialSourceAccountPool, UpstreamAccountPoolId: &anthropicPool.Id,
	}
	require.NoError(t, validateChannel(anthropicChannel, true))
	require.Equal(t, constant.ChannelTypeAnthropic, anthropicChannel.Type)

	publishedChannel := model.Channel{
		Type: constant.ChannelTypeOpenAI, Name: "published", Key: "",
		CredentialSource: constant.ChannelCredentialSourceAccountPool, UpstreamAccountPoolId: &pool.Id,
	}
	require.NoError(t, db.Create(&publishedChannel).Error)
	channel.Type = constant.ChannelTypeOpenAI
	require.ErrorContains(t, validateChannel(channel, true), "already published")
	require.NoError(t, db.Delete(&publishedChannel).Error)

	baseURL := "https://legacy.example.com"
	organization := "legacy-org"
	modelMapping := `{"public-model":"upstream-model"}`
	headerOverride := `{"x-legacy":"value"}`
	legacyLocal := &model.Channel{
		Type: constant.ChannelTypeOpenAI, Name: "legacy-local", Key: "legacy-key",
		CredentialSource: constant.ChannelCredentialSourceAccountPool, UpstreamAccountPoolId: &pool.Id,
		BaseURL: &baseURL, OpenAIOrganization: &organization, ModelMapping: &modelMapping, HeaderOverride: &headerOverride,
	}
	legacyLocal.SetSetting(dto.ChannelSettings{
		Proxy: "socks5://127.0.0.1:1080", PassThroughBodyEnabled: true,
		SystemPrompt: "keep-channel-policy",
	})
	legacyLocal.SetOtherSettings(dto.ChannelOtherSettings{
		ResponsesWebSocketV2Enabled: true, ResponsesWebSocketV2Mode: "passthrough",
		ResponsesWebSocketV2ReplayEnabled: true, AlphaSearchEnabled: true, AllowServiceTier: true,
	})
	require.NoError(t, validateChannel(legacyLocal, true))
	require.Empty(t, legacyLocal.Key)
	require.Nil(t, legacyLocal.BaseURL)
	require.Nil(t, legacyLocal.OpenAIOrganization)
	require.Nil(t, legacyLocal.ModelMapping)
	require.Nil(t, legacyLocal.HeaderOverride)
	require.Empty(t, legacyLocal.GetSetting().Proxy)
	require.False(t, legacyLocal.GetSetting().PassThroughBodyEnabled)
	require.Equal(t, "keep-channel-policy", legacyLocal.GetSetting().SystemPrompt)
	require.False(t, legacyLocal.GetOtherSettings().ResponsesWebSocketV2Enabled)
	require.False(t, legacyLocal.GetOtherSettings().AlphaSearchEnabled)
	require.True(t, legacyLocal.GetOtherSettings().AllowServiceTier)

	channel.Type = constant.ChannelTypeOpenAI
	channel.CredentialSource = constant.ChannelCredentialSourceKey
	channel.UpstreamAccountPoolId = nil
	require.ErrorContains(t, validateChannel(channel, false), "channel key is required")
	channel.Key = "sk-test"
	require.NoError(t, validateChannel(channel, false))
}

func TestApplyChannelUpdateDefaultsAllowsLocalPriorityPatch(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.Channel{},
		&model.UpstreamAccountPool{},
		&model.UpstreamAccountPoolMember{},
		&model.UpstreamAccount{},
		&model.UpstreamAccountEvent{},
	))
	originalDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = originalDB })

	pool := model.UpstreamAccountPool{
		Name: "codex", Platform: constant.UpstreamPlatformOpenAI, CredentialType: constant.UpstreamAccountTypeOAuth,
		Status: constant.UpstreamStatusActive, SchedulerConfig: "{}", CreatedAt: 1, UpdatedAt: 1,
	}
	require.NoError(t, db.Create(&pool).Error)

	priority := int64(0)
	origin := &model.Channel{
		Type: constant.ChannelTypeOpenAI, Name: "local-openai", Models: "gpt-4o", Group: "default",
		CredentialSource: constant.ChannelCredentialSourceAccountPool, UpstreamAccountPoolId: &pool.Id,
		Priority: &priority, OtherSettings: `{"allow_service_tier":true}`,
	}
	origin.SetSetting(dto.ChannelSettings{SystemPrompt: "keep-channel-policy"})
	require.NoError(t, db.Create(origin).Error)

	// List inline edit only sends id + priority, leaving type/credential fields zeroed.
	nextPriority := int64(10)
	patch := &model.Channel{Id: origin.Id, Priority: &nextPriority}
	applyChannelUpdateDefaults(patch, origin)
	require.Equal(t, constant.ChannelTypeOpenAI, patch.Type)
	require.Equal(t, constant.ChannelCredentialSourceAccountPool, patch.CredentialSource)
	require.Equal(t, pool.Id, *patch.UpstreamAccountPoolId)
	require.NoError(t, validateChannel(patch, false))
	require.Equal(t, "keep-channel-policy", patch.GetSetting().SystemPrompt)
	require.True(t, patch.GetOtherSettings().AllowServiceTier)
	require.Equal(t, int64(10), *patch.Priority)
}
