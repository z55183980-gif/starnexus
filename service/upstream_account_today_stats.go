package service

import (
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/model"
)

const upstreamTodayStatsCacheTTL = time.Minute

type upstreamTodayStatsCacheEntry struct {
	stats    UpstreamAccountWindowStats
	cachedAt time.Time
}

var upstreamTodayStatsCache sync.Map

func upstreamTodayStartUnix(now time.Time) int64 {
	local := now.In(time.Local)
	start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, local.Location())
	return start.Unix()
}

func GetUpstreamAccountTodayStats(accountId int) (*UpstreamAccountWindowStats, error) {
	if accountId <= 0 {
		return &UpstreamAccountWindowStats{}, nil
	}
	startUnix := upstreamTodayStartUnix(time.Now())
	cacheKey := fmt.Sprintf("%d:%d", accountId, startUnix)
	if value, ok := upstreamTodayStatsCache.Load(cacheKey); ok {
		if entry, ok := value.(upstreamTodayStatsCacheEntry); ok && time.Since(entry.cachedAt) < upstreamTodayStatsCacheTTL {
			stats := entry.stats
			return &stats, nil
		}
		upstreamTodayStatsCache.Delete(cacheKey)
	}
	stats, err := model.GetUpstreamAccountWindowStats(accountId, startUnix)
	if err != nil {
		return nil, err
	}
	upstreamTodayStatsCache.Store(cacheKey, upstreamTodayStatsCacheEntry{stats: *stats, cachedAt: time.Now()})
	return stats, nil
}

func GetUpstreamAccountTodayStatsBatch(accountIds []int) (map[int]*UpstreamAccountWindowStats, error) {
	uniqueIds := make([]int, 0, len(accountIds))
	seen := make(map[int]struct{}, len(accountIds))
	for _, accountId := range accountIds {
		if accountId <= 0 {
			continue
		}
		if _, ok := seen[accountId]; ok {
			continue
		}
		seen[accountId] = struct{}{}
		uniqueIds = append(uniqueIds, accountId)
	}

	result := make(map[int]*UpstreamAccountWindowStats, len(uniqueIds))
	if len(uniqueIds) == 0 {
		return result, nil
	}

	startUnix := upstreamTodayStartUnix(time.Now())
	missing := make([]int, 0, len(uniqueIds))
	for _, accountId := range uniqueIds {
		cacheKey := fmt.Sprintf("%d:%d", accountId, startUnix)
		if value, ok := upstreamTodayStatsCache.Load(cacheKey); ok {
			if entry, ok := value.(upstreamTodayStatsCacheEntry); ok && time.Since(entry.cachedAt) < upstreamTodayStatsCacheTTL {
				stats := entry.stats
				result[accountId] = &stats
				continue
			}
			upstreamTodayStatsCache.Delete(cacheKey)
		}
		missing = append(missing, accountId)
	}

	if len(missing) == 0 {
		return result, nil
	}

	batch, err := model.GetUpstreamAccountWindowStatsBatch(missing, startUnix)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	for _, accountId := range missing {
		stats := batch[accountId]
		if stats == nil {
			stats = &UpstreamAccountWindowStats{}
		}
		result[accountId] = stats
		upstreamTodayStatsCache.Store(
			fmt.Sprintf("%d:%d", accountId, startUnix),
			upstreamTodayStatsCacheEntry{stats: *stats, cachedAt: now},
		)
	}
	return result, nil
}
