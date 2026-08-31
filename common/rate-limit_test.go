package common

import (
	"testing"
	"time"
)

func TestInMemoryRateLimiterFailedReservationDoesNotConsumeSuccessQuota(t *testing.T) {
	var limiter InMemoryRateLimiter
	limiter.Init(0)

	token, allowed := limiter.Reserve("success:user-1", 1, 60)
	if !allowed || token == 0 {
		t.Fatalf("first reservation should be allowed: token=%d allowed=%v", token, allowed)
	}
	limiter.Finalize("success:user-1", token, false)

	nextToken, allowed := limiter.Reserve("success:user-1", 1, 60)
	if !allowed || nextToken == 0 {
		t.Fatalf("failed request must release success quota: token=%d allowed=%v", nextToken, allowed)
	}
	limiter.Finalize("success:user-1", nextToken, false)
}

func TestInMemoryRateLimiterCommittedSuccessConsumesQuota(t *testing.T) {
	var limiter InMemoryRateLimiter
	limiter.Init(0)

	token, allowed := limiter.Reserve("success:user-2", 1, 60)
	if !allowed {
		t.Fatal("first reservation should be allowed")
	}
	limiter.Finalize("success:user-2", token, true)

	if _, allowed = limiter.Reserve("success:user-2", 1, 60); allowed {
		t.Fatal("committed success should consume the configured quota")
	}
}
