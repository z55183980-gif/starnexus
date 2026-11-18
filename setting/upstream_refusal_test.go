package setting

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func restoreUpstreamRefusalNoRetry(t *testing.T) {
	original := UpstreamRefusalNoRetryConfig2JsonString()
	t.Cleanup(func() {
		require.NoError(t, UpdateUpstreamRefusalNoRetryConfigByJsonString(original))
	})
}

func TestUpstreamRefusalNoRetryDefaultsOff(t *testing.T) {
	restoreUpstreamRefusalNoRetry(t)
	require.NoError(t, UpdateUpstreamRefusalNoRetryConfigByJsonString(""))
	require.False(t, UpstreamRefusalNoRetryAppliesToChannel(92))
	require.Equal(t, `{"channel_ids":[]}`, UpstreamRefusalNoRetryConfig2JsonString())
}

func TestUpstreamRefusalNoRetryNormalizesChannels(t *testing.T) {
	restoreUpstreamRefusalNoRetry(t)
	config, err := ParseUpstreamRefusalNoRetryConfigJSON(`{"channel_ids":[92,15,92]}`)
	require.NoError(t, err)
	require.Equal(t, []int{15, 92}, config.ChannelIds)

	require.NoError(t, UpdateUpstreamRefusalNoRetryConfigByJsonString(`{"channel_ids":[92]}`))
	require.True(t, UpstreamRefusalNoRetryAppliesToChannel(92))
	require.False(t, UpstreamRefusalNoRetryAppliesToChannel(15))
	require.False(t, UpstreamRefusalNoRetryAppliesToChannel(0))

	// Clearing the list is the rollback path.
	require.NoError(t, UpdateUpstreamRefusalNoRetryConfigByJsonString(`{"channel_ids":[]}`))
	require.False(t, UpstreamRefusalNoRetryAppliesToChannel(92))
}

func TestUpstreamRefusalNoRetryRejectsInvalidChannels(t *testing.T) {
	for _, value := range []string{`{"channel_ids":[0]}`, `{"channel_ids":[-1]}`, `not json`} {
		_, err := ParseUpstreamRefusalNoRetryConfigJSON(value)
		require.Error(t, err, value)
	}
}
