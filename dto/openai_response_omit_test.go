package dto

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestResponsesOutputOmitsEmptyQualityAndSize(t *testing.T) {
	t.Parallel()
	raw, err := common.Marshal(ResponsesOutput{
		Type:   "message",
		ID:     "msg_1",
		Status: "completed",
		Role:   "assistant",
		Content: []ResponsesOutputContent{{
			Type: "output_text",
			Text: "hello",
		}},
	})
	require.NoError(t, err)
	require.False(t, gjson.GetBytes(raw, "quality").Exists())
	require.False(t, gjson.GetBytes(raw, "size").Exists())
	require.Equal(t, "hello", gjson.GetBytes(raw, "content.0.text").String())
}

func TestResponsesOutputKeepsImageGenerationQualityAndSize(t *testing.T) {
	t.Parallel()
	raw, err := common.Marshal(ResponsesOutput{
		Type:    ResponsesOutputTypeImageGenerationCall,
		ID:      "img_1",
		Status:  "completed",
		Quality: "high",
		Size:    "1024x1024",
	})
	require.NoError(t, err)
	require.Equal(t, "high", gjson.GetBytes(raw, "quality").String())
	require.Equal(t, "1024x1024", gjson.GetBytes(raw, "size").String())
}
