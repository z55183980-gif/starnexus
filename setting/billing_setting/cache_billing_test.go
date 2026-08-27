package billing_setting

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetCacheBillingOffsetBps(t *testing.T) {
	restore := setCacheBillingOffsetBpsForTest(map[string]int{
		"gpt-5":    300,
		"disabled": 0,
		"invalid":  10001,
	})
	t.Cleanup(restore)

	require.Equal(t, 300, GetCacheBillingOffsetBps("gpt-5"))
	require.Zero(t, GetCacheBillingOffsetBps("disabled"))
	require.Zero(t, GetCacheBillingOffsetBps("invalid"))
	require.Zero(t, GetCacheBillingOffsetBps("missing"))
}
