package model

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/require"
)

func TestChannelInfoDatabaseValueUsesJSONString(t *testing.T) {
	want := ChannelInfo{
		IsMultiKey:           true,
		MultiKeySize:         2,
		MultiKeyStatusList:   map[int]int{0: 1, 1: 1},
		MultiKeyPollingIndex: 1,
		MultiKeyMode:         constant.MultiKeyModePolling,
	}

	value, err := want.Value()
	require.NoError(t, err)
	jsonText, ok := value.(string)
	require.True(t, ok, "JSON database values must be strings, got %T", value)

	var got ChannelInfo
	require.NoError(t, got.Scan(jsonText))
	require.Equal(t, want, got)
}

func TestChannelInfoScanSupportsBytesAndNil(t *testing.T) {
	const jsonText = `{"is_multi_key":false,"multi_key_size":1,"multi_key_status_list":{"0":1},"multi_key_polling_index":0,"multi_key_mode":"random"}`

	var got ChannelInfo
	require.NoError(t, got.Scan([]byte(jsonText)))
	require.Equal(t, 1, got.MultiKeySize)
	require.Equal(t, constant.MultiKeyModeRandom, got.MultiKeyMode)

	require.NoError(t, got.Scan(nil))
	require.Equal(t, ChannelInfo{}, got)
}

func TestTaskJSONDatabaseValuesUseStrings(t *testing.T) {
	properties := Properties{
		Input:             "prompt",
		UpstreamModelName: "upstream-model",
		OriginModelName:   "origin-model",
	}
	propertiesValue, err := properties.Value()
	require.NoError(t, err)
	propertiesJSON, ok := propertiesValue.(string)
	require.True(t, ok, "properties database value must be a string, got %T", propertiesValue)

	var scannedProperties Properties
	require.NoError(t, scannedProperties.Scan(propertiesJSON))
	require.Equal(t, properties, scannedProperties)

	privateData := TaskPrivateData{
		Key:            "secret",
		UpstreamTaskID: "upstream-task",
		BillingSource:  "wallet",
	}
	privateValue, err := privateData.Value()
	require.NoError(t, err)
	privateJSON, ok := privateValue.(string)
	require.True(t, ok, "private data database value must be a string, got %T", privateValue)

	var scannedPrivateData TaskPrivateData
	require.NoError(t, scannedPrivateData.Scan([]byte(privateJSON)))
	require.Equal(t, privateData, scannedPrivateData)
}

func TestTaskJSONDatabaseScansResetOnNil(t *testing.T) {
	properties := Properties{Input: "stale"}
	require.NoError(t, properties.Scan(nil))
	require.Equal(t, Properties{}, properties)

	privateData := TaskPrivateData{Key: "stale"}
	require.NoError(t, privateData.Scan(nil))
	require.Equal(t, TaskPrivateData{}, privateData)
}

func TestJSONDatabaseScanRejectsUnsupportedTypes(t *testing.T) {
	var channelInfo ChannelInfo
	err := channelInfo.Scan(123)
	require.ErrorContains(t, err, "unsupported JSON database value type int")
}

func TestPrefillJSONValueUsesJSONString(t *testing.T) {
	want := JSONValue(`["gpt-5", "gpt-5-mini"]`)
	value, err := want.Value()
	require.NoError(t, err)
	require.IsType(t, "", value)

	var got JSONValue
	require.NoError(t, got.Scan(value))
	require.JSONEq(t, string(want), string(got))
}
