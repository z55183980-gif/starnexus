package controller

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateCacheBillingOffsetBpsJSON(t *testing.T) {
	require.NoError(t, validateCacheBillingOffsetBpsJSON(`{}`))
	require.NoError(t, validateCacheBillingOffsetBpsJSON(`{"gpt-5":300,"claude-4":10000}`))
	require.Error(t, validateCacheBillingOffsetBpsJSON(`null`))
	require.Error(t, validateCacheBillingOffsetBpsJSON(`{"gpt-5":10001}`))
	require.Error(t, validateCacheBillingOffsetBpsJSON(`{" gpt-5":300}`))
	require.Error(t, validateCacheBillingOffsetBpsJSON(`{"gpt-5":3.5}`))
}
