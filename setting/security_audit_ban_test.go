package setting

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestParseSecurityAuditBanConfigJSON(t *testing.T) {
	config, err := ParseSecurityAuditBanConfigJSON(`{"channel_ids":[9,3,9],"duration_seconds":7200}`)
	require.NoError(t, err)
	require.Equal(t, []int{3, 9}, config.ChannelIds)
	require.Equal(t, SecurityAuditBanDurationTwoHours, config.DurationSeconds)

	_, err = ParseSecurityAuditBanConfigJSON(`{"channel_ids":[3],"duration_seconds":30}`)
	require.Error(t, err)
	_, err = ParseSecurityAuditBanConfigJSON(`{"channel_ids":[0],"duration_seconds":0}`)
	require.Error(t, err)
}

func TestSecurityAuditBanAppliesToChannel(t *testing.T) {
	original := GetSecurityAuditBanConfig()
	t.Cleanup(func() {
		data := securityAuditBanConfigJSONForTest(original)
		require.NoError(t, UpdateSecurityAuditBanConfigByJsonString(data))
	})
	require.NoError(t, UpdateSecurityAuditBanConfigByJsonString(`{"channel_ids":[4,7],"duration_seconds":86400}`))

	applies, duration := SecurityAuditBanAppliesToChannel(7)
	require.True(t, applies)
	require.Equal(t, SecurityAuditBanDurationOneDay, duration)
	applies, _ = SecurityAuditBanAppliesToChannel(8)
	require.False(t, applies)
}

func securityAuditBanConfigJSONForTest(config SecurityAuditBanConfig) string {
	data, _ := common.Marshal(config)
	return string(data)
}
