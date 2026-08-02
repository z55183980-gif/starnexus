package service

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
)

func TestBuildAnthropicUsageWindow(t *testing.T) {
	now := time.Date(2026, time.August, 2, 12, 0, 0, 0, time.UTC)
	window := buildAnthropicUsageWindow(upstreamAnthropicUsageWindow{
		Utilization: 42.5,
		ResetsAt:    now.Add(90 * time.Minute).Format(time.RFC3339),
	}, 5*time.Hour, now)
	if window == nil || window.UsedPercent != 42.5 || window.ResetAfterSeconds != 5400 {
		t.Fatalf("unexpected usage window: %+v", window)
	}
	if empty := buildAnthropicUsageWindow(upstreamAnthropicUsageWindow{}, 5*time.Hour, now); empty != nil {
		t.Fatalf("expected an absent window, got %+v", empty)
	}
}

func TestBuildAnthropicSetupTokenUsagePreservesExplicitZero(t *testing.T) {
	now := time.Date(2026, time.August, 2, 12, 0, 0, 0, time.UTC)
	start := now.Unix()
	end := now.Add(5 * time.Hour).Unix()
	account := &model.UpstreamAccount{
		Extra:               `{"session_window_utilization":0}`,
		SessionWindowStart:  &start,
		SessionWindowEnd:    &end,
		SessionWindowStatus: "rejected",
	}
	usage := buildAnthropicSetupTokenUsage(account, now)
	if usage.FiveHour == nil || usage.FiveHour.UsedPercent != 0 {
		t.Fatalf("explicit zero utilization must override status fallback: %+v", usage.FiveHour)
	}
	if usage.FiveHour.ResetAfterSeconds != int64((5 * time.Hour).Seconds()) {
		t.Fatalf("unexpected reset countdown: %+v", usage.FiveHour)
	}
}
