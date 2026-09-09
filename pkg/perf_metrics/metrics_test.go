package perfmetrics

import (
	"testing"
	"time"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

func TestRelaySampleUsesAttemptTimingAndSnapshotsCompletion(t *testing.T) {
	start := time.Now().Add(-time.Minute)
	info := &relaycommon.RelayInfo{
		StartTime:         start,
		LogStartTime:      start.Add(20 * time.Second),
		FirstResponseTime: start.Add(22 * time.Second),
		IsStream:          true,
		OriginModelName:   "timing-test",
		UsingGroup:        "default",
	}
	completedAt := start.Add(25 * time.Second)
	sample := NewRelaySample(info, true, 30, completedAt)

	// Recording may run long after forwarding, including after the source info
	// changes. Neither can alter the captured attempt timing.
	info.LogStartTime = start.Add(40 * time.Second)
	info.FirstResponseTime = start.Add(41 * time.Second)
	require.EqualValues(t, 5000, sample.LatencyMs)
	require.EqualValues(t, 2000, sample.TtftMs)
	require.EqualValues(t, 3000, sample.GenerationMs)
	require.True(t, sample.HasTtft)
	require.True(t, sample.Success)
	require.EqualValues(t, 30, sample.OutputTokens)
	require.Equal(t, start, info.StartTime)
}

func TestRelaySampleWithoutFirstToken(t *testing.T) {
	start := time.Now().Add(-time.Minute)
	for _, stream := range []bool{false, true} {
		info := &relaycommon.RelayInfo{
			StartTime:         start,
			LogStartTime:      start.Add(20 * time.Second),
			FirstResponseTime: start.Add(-time.Second),
			IsStream:          stream,
		}
		sample := NewRelaySample(info, false, 0, start.Add(25*time.Second))
		require.EqualValues(t, 5000, sample.LatencyMs)
		require.EqualValues(t, 5000, sample.GenerationMs)
		require.False(t, sample.HasTtft)
		require.False(t, sample.Success)
	}
}

func TestRelaySampleFallbackAndInvalidTiming(t *testing.T) {
	start := time.Now().Add(-time.Minute)
	info := &relaycommon.RelayInfo{StartTime: start}
	sample := NewRelaySample(info, true, 0, start.Add(time.Second))
	require.EqualValues(t, 1000, sample.LatencyMs)

	info.LogStartTime = start.Add(2 * time.Second)
	info.FirstResponseTime = start.Add(time.Second)
	info.IsStream = true
	sample = NewRelaySample(info, false, 0, start.Add(time.Second))
	require.Zero(t, sample.LatencyMs)
	require.False(t, sample.HasTtft)
	require.Equal(t, Sample{}, NewRelaySample(nil, false, 0, start))
}
