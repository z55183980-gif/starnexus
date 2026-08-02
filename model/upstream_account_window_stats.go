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
