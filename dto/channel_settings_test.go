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
