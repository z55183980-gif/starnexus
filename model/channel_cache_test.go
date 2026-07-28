package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestInitChannelCacheKeepsLastGoodSnapshotOnQueryFailure(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Channel{}, &Ability{}))
	channel := Channel{Id: 44, Name: "cached", Status: common.ChannelStatusEnabled, Group: "test", Models: "gpt-test"}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&Ability{Group: "test", Model: "gpt-test", ChannelId: channel.Id, Enabled: true}).Error)

	originalDB := DB
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	originalGroups := group2model2channels
	originalChannels := channelsIDM
	DB = db
	common.MemoryCacheEnabled = true
	t.Cleanup(func() {
		DB = originalDB
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
		group2model2channels = originalGroups
		channelsIDM = originalChannels
	})

	InitChannelCache()
	cached, err := CacheGetChannel(channel.Id)
	require.NoError(t, err)
	require.Equal(t, channel.Name, cached.Name)

	require.NoError(t, db.Migrator().DropTable(&Channel{}))
	InitChannelCache()
	cached, err = CacheGetChannel(channel.Id)
	require.NoError(t, err)
	require.Equal(t, channel.Name, cached.Name)
}
