package common

import (
	"context"
	"time"

	"github.com/go-redis/redis/v8"
)

const BusinessMonitorLogStream = "new-api:business-monitor:logs"

// PublishBusinessMonitorEvent appends an event after its PostgreSQL write
// succeeds. Redis Streams retain a short replay window for reconnecting clients.
func PublishBusinessMonitorEvent(eventType string, event interface{}) error {
	if !RedisEnabled || RDB == nil {
		return nil
	}
	payload, err := Marshal(event)
	if err != nil {
		return err
	}
	_, err = RDB.XAdd(context.Background(), &redis.XAddArgs{
		Stream: BusinessMonitorLogStream,
		MaxLen: 10000,
		Approx: true,
		Values: map[string]interface{}{
			"type":  eventType,
			"event": string(payload),
		},
	}).Result()
	return err
}

func PublishBusinessMonitorLog(log interface{}) error {
	return PublishBusinessMonitorEvent("log", log)
}

func ReadBusinessMonitorLogStream(ctx context.Context, lastID string, count int64, block time.Duration) ([]redis.XMessage, error) {
	if !RedisEnabled || RDB == nil {
		return nil, redis.ErrClosed
	}
	if lastID == "" {
		lastID = "$"
	}
	result, err := RDB.XRead(ctx, &redis.XReadArgs{
		Streams: []string{BusinessMonitorLogStream, lastID},
		Count:   count,
		Block:   block,
	}).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		return nil, err
	}
	if len(result) == 0 {
		return nil, nil
	}
	return result[0].Messages, nil
}
