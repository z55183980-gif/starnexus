package service

import (
	"strconv"
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

func TestBuildAnthropicPassiveUsageFromExtra(t *testing.T) {
	now := time.Date(2026, time.August, 7, 15, 0, 0, 0, time.UTC)
	start := now.Add(-time.Hour).Unix()
	end := now.Add(4 * time.Hour).Unix()
	sampledAt := now.Add(-20 * time.Minute).UTC().Format(time.RFC3339)
	account := &model.UpstreamAccount{
		SessionWindowStart: &start,
		SessionWindowEnd:   &end,
		Extra: `{
			"session_window_utilization":0.5,
			"passive_usage_7d_utilization":0.25,
			"passive_usage_7d_reset":` + strconv.FormatInt(now.Add(48*time.Hour).Unix(), 10) + `,
			"passive_usage_7d_oi_utilization":0.1,
			"passive_usage_7d_oi_reset":` + strconv.FormatInt(now.Add(72*time.Hour).Unix(), 10) + `,
			"passive_usage_sampled_at":"` + sampledAt + `"
		}`,
	}
	usage := buildAnthropicPassiveUsage(account, now)
	if usage == nil || usage.Source != "passive" {
		t.Fatalf("expected passive usage, got %+v", usage)
	}
	if usage.FiveHour == nil || usage.FiveHour.UsedPercent != 50 {
		t.Fatalf("unexpected 5h: %+v", usage.FiveHour)
	}
	if usage.SevenDay == nil || usage.SevenDay.UsedPercent != 25 {
		t.Fatalf("unexpected 7d: %+v", usage.SevenDay)
	}
	if usage.SevenDayFable == nil || usage.SevenDayFable.UsedPercent != 10 {
		t.Fatalf("unexpected 7d F: %+v", usage.SevenDayFable)
	}
	if usage.FetchedAt != now.Add(-20*time.Minute).Unix() {
		t.Fatalf("unexpected fetched_at: %d", usage.FetchedAt)
	}
}

func TestBuildAnthropicPassiveUsageReturnsNilWithoutSignal(t *testing.T) {
	now := time.Now()
	if usage := buildAnthropicPassiveUsage(&model.UpstreamAccount{Extra: "{}"}, now); usage != nil {
		t.Fatalf("expected nil without signal, got %+v", usage)
	}
}
