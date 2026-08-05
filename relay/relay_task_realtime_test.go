package relay

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
)

func TestSupportsRealtimeTaskFetch(t *testing.T) {
	for _, channelType := range []int{
		constant.ChannelTypeGemini,
		constant.ChannelTypeVertexAi,
		constant.ChannelTypeSora,
	} {
		if !supportsRealtimeTaskFetch(channelType) {
			t.Fatalf("channel type %d must support realtime task fetch", channelType)
		}
	}
	if supportsRealtimeTaskFetch(constant.ChannelTypeKling) {
		t.Fatal("Kling must keep using background polling only")
	}
}
