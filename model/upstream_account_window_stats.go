package model

type UpstreamAccountWindowStats struct {
	Requests int64   `json:"requests"`
	Tokens   int64   `json:"tokens"`
	Cost     float64 `json:"cost"`
	UserCost float64 `json:"user_cost"`
}

func GetUpstreamAccountWindowStats(accountId int, startTime int64) (*UpstreamAccountWindowStats, error) {
	stats := &UpstreamAccountWindowStats{}
	if accountId <= 0 {
		return stats, nil
	}
	if err := LOG_DB.Model(&Log{}).
		Select("COUNT(*) AS requests, COALESCE(SUM(prompt_tokens + completion_tokens), 0) AS tokens, COALESCE(SUM(account_cost), 0) AS cost, COALESCE(SUM(user_cost), 0) AS user_cost").
		Where("type = ? AND upstream_account_id = ? AND created_at >= ?", LogTypeConsume, accountId, startTime).
		Scan(stats).Error; err != nil {
		return nil, err
	}
	return stats, nil
}

// GetUpstreamAccountWindowStatsBatch aggregates consume-log stats for many accounts
// from the same window start. Missing accounts are filled with zero-value stats.
func GetUpstreamAccountWindowStatsBatch(accountIds []int, startTime int64) (map[int]*UpstreamAccountWindowStats, error) {
	result := make(map[int]*UpstreamAccountWindowStats, len(accountIds))
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
		result[accountId] = &UpstreamAccountWindowStats{}
	}
	if len(uniqueIds) == 0 {
		return result, nil
	}

	type row struct {
		UpstreamAccountId int     `gorm:"column:upstream_account_id"`
		Requests          int64   `gorm:"column:requests"`
		Tokens            int64   `gorm:"column:tokens"`
		Cost              float64 `gorm:"column:cost"`
		UserCost          float64 `gorm:"column:user_cost"`
	}
	var rows []row
	if err := LOG_DB.Model(&Log{}).
		Select("upstream_account_id, COUNT(*) AS requests, COALESCE(SUM(prompt_tokens + completion_tokens), 0) AS tokens, COALESCE(SUM(account_cost), 0) AS cost, COALESCE(SUM(user_cost), 0) AS user_cost").
		Where("type = ? AND upstream_account_id IN ? AND created_at >= ?", LogTypeConsume, uniqueIds, startTime).
		Group("upstream_account_id").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, item := range rows {
		result[item.UpstreamAccountId] = &UpstreamAccountWindowStats{
			Requests: item.Requests,
			Tokens:   item.Tokens,
			Cost:     item.Cost,
			UserCost: item.UserCost,
		}
	}
	return result, nil
}
