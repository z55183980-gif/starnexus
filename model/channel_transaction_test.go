package model

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func resetChannelTransactionTables(t *testing.T) {
	t.Helper()
	require.NoError(t, DB.Exec("DELETE FROM abilities").Error)
	require.NoError(t, DB.Exec("DELETE FROM channels").Error)
	t.Cleanup(func() {
		DB.Exec("DELETE FROM abilities")
		DB.Exec("DELETE FROM channels")
	})
}

func createTransactionTestChannel(t *testing.T) Channel {
	t.Helper()
	channel := Channel{
		Name:             "transaction-test",
		Type:             constant.ChannelTypeOpenAI,
		Key:              "sk-test",
		Status:           common.ChannelStatusEnabled,
		Models:           "model-old",
		Group:            "default",
		CredentialSource: constant.ChannelCredentialSourceKey,
	}
	require.NoError(t, BatchInsertChannels([]Channel{channel}))
	require.NoError(t, DB.First(&channel, "name = ?", channel.Name).Error)
	return channel
}

func TestChannelUpdateRollsBackWhenAbilityRebuildFails(t *testing.T) {
	resetChannelTransactionTables(t)
	channel := createTransactionTestChannel(t)

	callbackName := "test:fail_ability_create"
	require.NoError(t, DB.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "Ability" {
			tx.AddError(errors.New("forced ability create failure"))
		}
	}))
	t.Cleanup(func() {
		_ = DB.Callback().Create().Remove(callbackName)
	})

	channel.Models = "model-new"
	require.ErrorContains(t, channel.Update(), "forced ability create failure")

	var persisted Channel
	require.NoError(t, DB.First(&persisted, channel.Id).Error)
	require.Equal(t, "model-old", persisted.Models)

	var abilities []Ability
	require.NoError(t, DB.Where("channel_id = ?", channel.Id).Find(&abilities).Error)
	require.Len(t, abilities, 1)
	require.Equal(t, "model-old", abilities[0].Model)
}

func TestBatchSetChannelTagRebuildsAbilitiesWithNewTag(t *testing.T) {
	resetChannelTransactionTables(t)
	channel := createTransactionTestChannel(t)

	newTag := "new-tag"
	require.NoError(t, BatchSetChannelTag([]int{channel.Id}, &newTag))

	var persisted Channel
	require.NoError(t, DB.First(&persisted, channel.Id).Error)
	require.NotNil(t, persisted.Tag)
	require.Equal(t, newTag, *persisted.Tag)

	var ability Ability
	require.NoError(t, DB.Where("channel_id = ?", channel.Id).First(&ability).Error)
	require.NotNil(t, ability.Tag)
	require.Equal(t, newTag, *ability.Tag)
}
