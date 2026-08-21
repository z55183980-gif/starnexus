package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
)

func TestApplyMinimumTokenConsumeQuota(t *testing.T) {
	minimumQuota := common.QuotaRound(minimumTokenConsumeUSD * common.QuotaPerUnit)

	if got := applyMinimumTokenConsumeQuota(1, 1, true); got != minimumQuota {
		t.Fatalf("minimum token charge = %d, want %d", got, minimumQuota)
	}
	if got := applyMinimumTokenConsumeQuota(minimumQuota+1, 1, true); got != minimumQuota+1 {
		t.Fatalf("charge above minimum changed to %d", got)
	}
	if got := applyMinimumTokenConsumeQuota(1, 0, true); got != 1 {
		t.Fatalf("zero-token charge changed to %d", got)
	}
	if got := applyMinimumTokenConsumeQuota(1, 1, false); got != 1 {
		t.Fatalf("non-billable charge changed to %d", got)
	}
}
