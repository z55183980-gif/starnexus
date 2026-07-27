package advancedcustom

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"

	"github.com/stretchr/testify/require"
)

func TestGetRequestURLAlphaSearchUsesConfiguredRoute(t *testing.T) {
	adaptor := &Adaptor{}
	url, err := adaptor.GetRequestURL(&relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeAlphaSearch,
		OriginModelName: "gpt-5.6-sol",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl: "https://proxy.example.com",
			ChannelType:    constant.ChannelTypeAdvancedCustom,
			ChannelOtherSettings: dto.ChannelOtherSettings{AdvancedCustom: &dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{{
				IncomingPath: "/v1/alpha/search",
				UpstreamPath: "/custom/search",
				Models:       []string{"gpt-5.6-sol"},
			}}}},
		},
	})
	require.NoError(t, err)
	require.Equal(t, "https://proxy.example.com/custom/search", url)
}

func TestGetRequestURLAlphaSearchRejectsUnmatchedModel(t *testing.T) {
	adaptor := &Adaptor{}
	_, err := adaptor.GetRequestURL(&relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeAlphaSearch,
		OriginModelName: "gpt-other",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl: "https://proxy.example.com",
			ChannelType:    constant.ChannelTypeAdvancedCustom,
			ChannelOtherSettings: dto.ChannelOtherSettings{AdvancedCustom: &dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{{
				IncomingPath: "/v1/alpha/search",
				UpstreamPath: "/custom/search",
				Models:       []string{"gpt-5.6-sol"},
			}}}},
		},
	})
	require.Error(t, err)
}
