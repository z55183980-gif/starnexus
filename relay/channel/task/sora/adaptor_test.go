package sora

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

func TestParseTaskResultIncludesDuration(t *testing.T) {
	adaptor := &TaskAdaptor{}
	result, err := adaptor.ParseTaskResult([]byte(`{"id":"upstream-task","status":"completed","progress":100,"seconds":"5"}`))
	if err != nil {
		t.Fatalf("ParseTaskResult() error = %v", err)
	}
	if result.DurationSeconds != 5 {
		t.Fatalf("DurationSeconds = %d, want 5", result.DurationSeconds)
	}
}

func TestAdjustBillingOnCompleteReconcilesDuration(t *testing.T) {
	task := &model.Task{
		Quota: 1_487_500,
		PrivateData: model.TaskPrivateData{
			BillingContext: &model.TaskBillingContext{
				OtherRatios: map[string]float64{"seconds": 4, "size": 1},
			},
		},
	}
	adaptor := &TaskAdaptor{}

	actualQuota := adaptor.AdjustBillingOnComplete(task, &relaycommon.TaskInfo{DurationSeconds: 5})
	if actualQuota != 1_859_375 {
		t.Fatalf("actual quota = %d, want 1859375", actualQuota)
	}
	if got := task.PrivateData.BillingContext.OtherRatios["seconds"]; got != 5 {
		t.Fatalf("seconds ratio = %v, want 5", got)
	}
}

func TestAdjustBillingOnCompleteKeepsMatchingDuration(t *testing.T) {
	task := &model.Task{
		Quota: 1_487_500,
		PrivateData: model.TaskPrivateData{
			BillingContext: &model.TaskBillingContext{
				OtherRatios: map[string]float64{"seconds": 4},
			},
		},
	}
	adaptor := &TaskAdaptor{}

	if actualQuota := adaptor.AdjustBillingOnComplete(task, &relaycommon.TaskInfo{DurationSeconds: 4}); actualQuota != 0 {
		t.Fatalf("actual quota = %d, want no adjustment", actualQuota)
	}
}
