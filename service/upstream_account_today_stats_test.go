package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestUpstreamTodayStartUnix(t *testing.T) {
	now := time.Date(2026, 8, 3, 15, 30, 0, 0, time.Local)
	start := time.Unix(upstreamTodayStartUnix(now), 0).In(time.Local)
	require.Equal(t, 2026, start.Year())
	require.Equal(t, time.August, start.Month())
	require.Equal(t, 3, start.Day())
	require.Equal(t, 0, start.Hour())
	require.Equal(t, 0, start.Minute())
	require.Equal(t, 0, start.Second())
}

func TestGetUpstreamAccountTodayStatsBatchEmpty(t *testing.T) {
	result, err := GetUpstreamAccountTodayStatsBatch(nil)
	require.NoError(t, err)
	require.Empty(t, result)

	result, err = GetUpstreamAccountTodayStatsBatch([]int{0, -1})
	require.NoError(t, err)
	require.Empty(t, result)
}
