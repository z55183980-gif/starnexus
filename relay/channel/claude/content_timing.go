package claude

import (
	"time"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/tidwall/gjson"
)

// Called only after a successful downstream write/flush. Keep transport activity
// (FirstResponseTime) independent so heartbeats still prevent unsafe replay.
func observeClaudeContentTiming(info *relaycommon.RelayInfo, data string) {
	if !info.ContentTimingTracked || info.FirstContentMs != nil {
		return
	}
	frame := gjson.Parse(data)
	content := false
	switch frame.Get("type").String() {
	case "content_block_start":
		block := frame.Get("content_block")
		switch block.Get("type").String() {
		case "text":
			content = block.Get("text").String() != ""
		case "tool_use", "server_tool_use":
			content = block.Get("name").String() != ""
		}
	case "content_block_delta":
		delta := frame.Get("delta")
		switch delta.Get("type").String() {
		case "text_delta":
			content = delta.Get("text").String() != ""
		case "input_json_delta":
			content = delta.Get("partial_json").String() != ""
		}
	}
	if content && !info.GetLogStartTime().IsZero() {
		ms := time.Since(info.GetLogStartTime()).Milliseconds()
		if ms >= 0 {
			info.FirstContentMs = &ms
		}
	}
}
