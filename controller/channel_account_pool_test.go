package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
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

	channel.ChannelInfo.IsMultiKey = true
	require.Error(t, validateChannel(channel, true))
	channel.ChannelInfo.IsMultiKey = false
	channel.Type = constant.ChannelTypeAnthropic
	require.Error(t, validateChannel(channel, true))

	channel.Type = constant.ChannelTypeOpenAI
	channel.CredentialSource = constant.ChannelCredentialSourceKey
	channel.UpstreamAccountPoolId = nil
	require.ErrorContains(t, validateChannel(channel, false), "channel key is required")
	channel.Key = "sk-test"
	require.NoError(t, validateChannel(channel, false))
}
