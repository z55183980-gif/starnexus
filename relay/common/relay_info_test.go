package common

import (
	"testing"

	appcommon "github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRelayInfoGetFinalRequestRelayFormatPrefersExplicitFinal(t *testing.T) {
	info := &RelayInfo{
		RelayFormat:             types.RelayFormatOpenAI,
		RequestConversionChain:  []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatClaude},
		FinalRequestRelayFormat: types.RelayFormatOpenAIResponses,
	}

	require.Equal(t, types.RelayFormat(types.RelayFormatOpenAIResponses), info.GetFinalRequestRelayFormat())
}

func TestRelayInfoGetFinalRequestRelayFormatFallsBackToConversionChain(t *testing.T) {
	info := &RelayInfo{
		RelayFormat:            types.RelayFormatOpenAI,
		RequestConversionChain: []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatClaude},
	}

	require.Equal(t, types.RelayFormat(types.RelayFormatClaude), info.GetFinalRequestRelayFormat())
}

func TestRelayInfoGetFinalRequestRelayFormatFallsBackToRelayFormat(t *testing.T) {
	info := &RelayInfo{
		RelayFormat: types.RelayFormatGemini,
	}

	require.Equal(t, types.RelayFormat(types.RelayFormatGemini), info.GetFinalRequestRelayFormat())
}

func TestRelayInfoGetFinalRequestRelayFormatNilReceiver(t *testing.T) {
	var info *RelayInfo
	require.Equal(t, types.RelayFormat(""), info.GetFinalRequestRelayFormat())
}

func TestRelayInfoGetAccountRateMultiplierDefaultsToOne(t *testing.T) {
	info := &RelayInfo{}
	require.Equal(t, 1.0, info.GetAccountRateMultiplier())
}

func TestRelayInfoGetAccountRateMultiplierPreservesZero(t *testing.T) {
	multiplier := 0.0
	info := &RelayInfo{AccountRateMultiplier: &multiplier}
	require.Equal(t, 0.0, info.GetAccountRateMultiplier())
}

func TestRelayInfoRefreshesAccountRateMultiplierAfterFailover(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(nil)
	appcommon.SetContextKey(ctx, constant.ContextKeyUpstreamAccountRateMultiplier, 2.5)
	info := &RelayInfo{}
	info.SetAccountRateMultiplierFromContext(ctx)
	require.Equal(t, 2.5, info.GetAccountRateMultiplier())

	appcommon.SetContextKey(ctx, constant.ContextKeyUpstreamAccountRateMultiplier, 0.75)
	info.SetAccountRateMultiplierFromContext(ctx)
	require.Equal(t, 0.75, info.GetAccountRateMultiplier())
}
