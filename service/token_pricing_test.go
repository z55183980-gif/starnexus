package service

import (
	"sync"
	"testing"
)

var tokenPricingConfigTestMu sync.Mutex

func lockTokenPricingConfigTest(t *testing.T) func() {
	t.Helper()
	tokenPricingConfigTestMu.Lock()
	return func() {
		tokenPricingConfigTestMu.Unlock()
	}
}
