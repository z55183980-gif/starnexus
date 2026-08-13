package dto

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChannelSettingsShouldPassThroughBody(t *testing.T) {
	require.False(t, (ChannelSettings{}).ShouldPassThroughBody())
	require.True(t, (ChannelSettings{PassThroughBodyEnabled: true}).ShouldPassThroughBody())
	require.True(t, (ChannelSettings{AccountPassThroughBodyEnabled: true}).ShouldPassThroughBody())
}

func TestZQBAPIChannelSettingsNormalizeAndValidate(t *testing.T) {
	settings := &ZQBAPIChannelSettings{MaterialMode: " ALWAYS ", GroupID: " group-1 ", AssetGroupType: " REAL "}
	require.NoError(t, settings.Validate())
	require.Equal(t, ZQBAPIMaterialModeAlways, settings.MaterialMode)
	require.Equal(t, "group-1", settings.GroupID)
	require.Equal(t, "real", settings.AssetGroupType)

	require.Error(t, (&ZQBAPIChannelSettings{MaterialMode: "unknown"}).Validate())
	require.Error(t, (&ZQBAPIChannelSettings{AssetGroupType: "unknown"}).Validate())
}

func TestDoubaoVideo2ChannelSettingsNormalizeAndValidate(t *testing.T) {
	settings := &DoubaoVideo2ChannelSettings{MaterialMode: " ALWAYS ", GroupID: " 191014526315921433 "}
	require.NoError(t, settings.Validate())
	require.Equal(t, DoubaoVideo2MaterialModeAlways, settings.MaterialMode)
	require.Equal(t, "191014526315921433", settings.GroupID)
	require.NoError(t, (&DoubaoVideo2ChannelSettings{MaterialMode: "off"}).Validate())
	empty := &DoubaoVideo2ChannelSettings{}
	require.NoError(t, empty.Validate())
	require.Equal(t, DoubaoVideo2MaterialModeOff, empty.MaterialMode)
	require.Error(t, (&DoubaoVideo2ChannelSettings{MaterialMode: "always"}).Validate())
	require.Error(t, (&DoubaoVideo2ChannelSettings{MaterialMode: "unknown", GroupID: "group-1"}).Validate())
}
