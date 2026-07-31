package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestBuildChannelsForInsertUsesIndependentBaseNames(t *testing.T) {
	channels := buildChannelsForInsert(model.Channel{
		Name:             "GPT",
		CredentialSource: constant.ChannelCredentialSourceKey,
	}, []string{"abcdefgh-first", "ijklmnop-second"}, true)

	require.Len(t, channels, 2)
	require.Equal(t, "GPT abcdefgh", channels[0].Name)
	require.Equal(t, "GPT ijklmnop", channels[1].Name)
}
