package newapi

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"

	"github.com/stretchr/testify/require"
)

func TestGetRequestURLAlphaSearch(t *testing.T) {
	adaptor := &Adaptor{}
	url, err := adaptor.GetRequestURL(&relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeAlphaSearch,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl: "https://proxy.example.com",
			ChannelType:    constant.ChannelTypeNewAPI,
		},
	})
	require.NoError(t, err)
	require.Equal(t, "https://proxy.example.com/v1/alpha/search", url)
}
