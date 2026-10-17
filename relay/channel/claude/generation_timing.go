package claude

import (
	"math"
	"time"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// consumeGatewayGenerationTiming changes only display telemetry. It never marks
// a response as sent, changes retry eligibility, or releases buffered content.
func consumeGatewayGenerationTiming(info *relaycommon.RelayInfo, data string) string {
	metadata := gjson.Get(data, "gateway_timing")
	if !metadata.Exists() {
		return data
	}
	clean, err := sjson.Delete(data, "gateway_timing")
	if err != nil {
		return data
	}
	if info == nil || gjson.Get(data, "type").String() != "message_stop" {
		return clean
	}
	version := metadata.Get("version")
	age := metadata.Get("first_content_age_ms")
	start := info.GetLogStartTime()
	if start.IsZero() || version.Type != gjson.Number || version.Float() != 1 || age.Type != gjson.Number {
		return clean
	}
	elapsed := time.Since(start).Milliseconds()
	value := age.Float()
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || math.Trunc(value) != value || value > float64(elapsed) {
		return clean
	}
	first := elapsed - int64(value)
	info.GenerationFirstTokenMs = &first
	return clean
}
