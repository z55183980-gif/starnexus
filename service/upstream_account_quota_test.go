package service

import "testing"

func TestParseUpstreamAccountResetCredits(t *testing.T) {
	payload := map[string]any{
		"availableCount": float64(3),
		"rate_limit_reset_credits": []any{
			map[string]any{"resetType": "codex_rate_limits", "status": "available", "expiresAt": "2026-08-01T00:00:00Z"},
			map[string]any{"resetType": "other", "status": "available"},
		},
	}
	credits := parseUpstreamAccountResetCredits(payload)
	if credits == nil {
		t.Fatal("expected reset credit details")
	}
	if credits.AvailableCount != 3 {
		t.Fatalf("expected available count 3, got %d", credits.AvailableCount)
	}
	if len(credits.Credits) != 1 || credits.Credits[0].ExpiresAt != "2026-08-01T00:00:00Z" {
		t.Fatalf("unexpected credit details: %+v", credits.Credits)
	}
}

func TestParseUpstreamAccountResetCreditsArray(t *testing.T) {
	payload := []any{
		map[string]any{"reset_type": "codex_rate_limits", "status": "available"},
		map[string]any{"reset_type": "codex_rate_limits", "status": "redeemed"},
	}
	credits := parseUpstreamAccountResetCredits(payload)
	if credits == nil || credits.AvailableCount != 1 {
		t.Fatalf("unexpected reset credit count: %+v", credits)
	}
}
