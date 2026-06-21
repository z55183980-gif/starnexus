package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestFormatUserLogsRemovesTokenPricingAdminFields(t *testing.T) {
	logs := []*Log{
		{
			Content: "token重算：tokens=50, modelRatio=1.00, rawTokens=100, rawPrompt=80, rawCompletion=20, inputRatio=0.5000, outputRatio=0.5000",
			Other: common.MapToJsonStr(map[string]interface{}{
				"token_pricing_enabled":      true,
				"token_pricing_input_ratio":  2,
				"token_pricing_output_ratio": 3,
				"token_pricing_rules":        []string{"vip", "user"},
				"raw_prompt_tokens":          100,
				"raw_completion_tokens":      10,
				"raw_total_tokens":           110,
				"billing_prompt_tokens":      200,
				"billing_completion_tokens":  30,
				"billing_total_tokens":       230,
				"cache_tokens":               5,
			}),
		},
	}

	formatUserLogs(logs, 0)

	require.Contains(t, logs[0].Content, "tokens=50")
	require.NotContains(t, logs[0].Content, "rawTokens")
	require.NotContains(t, logs[0].Content, "rawPrompt")
	require.NotContains(t, logs[0].Content, "rawCompletion")
	require.NotContains(t, logs[0].Content, "inputRatio")
	require.NotContains(t, logs[0].Content, "outputRatio")

	other, err := common.StrToMap(logs[0].Other)
	require.NoError(t, err)
	require.Equal(t, true, other["token_pricing_enabled"])
	require.Equal(t, float64(5), other["cache_tokens"])
	require.NotContains(t, other, "token_pricing_input_ratio")
	require.NotContains(t, other, "token_pricing_output_ratio")
	require.NotContains(t, other, "token_pricing_rules")
	require.NotContains(t, other, "raw_prompt_tokens")
	require.NotContains(t, other, "raw_completion_tokens")
	require.NotContains(t, other, "raw_total_tokens")
	require.NotContains(t, other, "billing_prompt_tokens")
	require.NotContains(t, other, "billing_completion_tokens")
	require.NotContains(t, other, "billing_total_tokens")
}
