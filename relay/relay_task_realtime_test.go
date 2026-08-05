package relay

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
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

func TestShouldIgnoreEmptyRealtimeTaskStatus(t *testing.T) {
	videoPreAuthTask := &model.Task{PrivateData: model.TaskPrivateData{
		BillingContext: &model.TaskBillingContext{
			PreAuthorization:  true,
			VideoTokenBilling: true,
		},
	}}
	if !shouldIgnoreEmptyRealtimeTaskStatus(videoPreAuthTask, "") {
		t.Fatal("video token preauthorization must tolerate a transient empty realtime status")
	}
	if shouldIgnoreEmptyRealtimeTaskStatus(videoPreAuthTask, model.TaskStatusInProgress) {
		t.Fatal("a non-empty realtime status must still be applied")
	}
	if shouldIgnoreEmptyRealtimeTaskStatus(&model.Task{}, "") {
		t.Fatal("other tasks must keep the existing empty-status behavior")
	}
}
