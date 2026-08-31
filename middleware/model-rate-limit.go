package middleware

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/common/limiter"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
)

const (
	ModelRequestRateLimitCountMark        = "MRRL"
	ModelRequestRateLimitSuccessCountMark = "MRRLS"
)

// 检查Redis中的请求限制
func checkRedisRateLimit(ctx context.Context, rdb *redis.Client, key string, maxCount int, duration int64) (bool, error) {
	// 如果maxCount为0，表示不限制
	if maxCount == 0 {
		return true, nil
	}

	// 获取当前计数
	length, err := rdb.LLen(ctx, key).Result()
	if err != nil {
		return false, err
	}

	// 如果未达到限制，允许请求
	if length < int64(maxCount) {
		return true, nil
	}

	// 检查时间窗口
	oldTimeStr, _ := rdb.LIndex(ctx, key, -1).Result()
	oldTime, err := time.Parse(timeFormat, oldTimeStr)
	if err != nil {
		return false, err
	}

	nowTimeStr := time.Now().Format(timeFormat)
	nowTime, err := time.Parse(timeFormat, nowTimeStr)
	if err != nil {
		return false, err
	}
	// 如果在时间窗口内已达到限制，拒绝请求
	subTime := nowTime.Sub(oldTime).Seconds()
	if int64(subTime) < duration {
		rdb.Expire(ctx, key, time.Duration(setting.ModelRequestRateLimitDurationMinutes)*time.Minute)
		return false, nil
	}

	return true, nil
}

// 记录Redis请求
func recordRedisRequest(ctx context.Context, rdb *redis.Client, key string, maxCount int) {
	// 如果maxCount为0，不记录请求
	if maxCount == 0 {
		return
	}

	now := time.Now().Format(timeFormat)
	rdb.LPush(ctx, key, now)
	rdb.LTrim(ctx, key, 0, int64(maxCount-1))
	rdb.Expire(ctx, key, time.Duration(setting.ModelRequestRateLimitDurationMinutes)*time.Minute)
}

// Redis限流处理器
func redisRateLimitHandler(duration int64, totalMaxCount, successMaxCount int) gin.HandlerFunc {
	return func(c *gin.Context) {
		userId := strconv.Itoa(c.GetInt("id"))
		ctx := context.Background()
		rdb := common.RDB

		// 1. 检查成功请求数限制
		successKey := fmt.Sprintf("rateLimit:%s:%s", ModelRequestRateLimitSuccessCountMark, userId)
		allowed, err := checkRedisRateLimit(ctx, rdb, successKey, successMaxCount, duration)
		if err != nil {
			fmt.Println("检查成功请求数限制失败:", err.Error())
			abortWithOpenAiMessage(c, http.StatusInternalServerError, "rate_limit_check_failed")
			return
		}
		if !allowed {
			abortWithOpenAiMessage(c, http.StatusTooManyRequests, fmt.Sprintf("您已达到请求数限制：%d分钟内最多请求%d次", duration/60, successMaxCount), types.ErrorCodeGatewayRateLimit)
			return
		}

		//2.检查总请求数限制并记录总请求（当totalMaxCount为0时会自动跳过，使用令牌桶限流器
		if totalMaxCount > 0 {
			totalKey := fmt.Sprintf("rateLimit:%s", userId)
			// 初始化
			tb := limiter.New(ctx, rdb)
			allowed, err = tb.Allow(
				ctx,
				totalKey,
				limiter.WithCapacity(int64(totalMaxCount)*duration),
				limiter.WithRate(int64(totalMaxCount)),
				limiter.WithRequested(duration),
			)

			if err != nil {
				fmt.Println("检查总请求数限制失败:", err.Error())
				abortWithOpenAiMessage(c, http.StatusInternalServerError, "rate_limit_check_failed")
				return
			}

			if !allowed {
				abortWithOpenAiMessage(c, http.StatusTooManyRequests, fmt.Sprintf("您已达到总请求数限制：%d分钟内最多请求%d次，包括失败次数，请检查您的请求是否正确", duration/60, totalMaxCount), types.ErrorCodeGatewayRateLimit)
				return
			}
		}

		// 4. 处理请求
		c.Next()

		// 5. 如果请求成功，记录成功请求
		if c.Writer.Status() < 400 {
			recordRedisRequest(ctx, rdb, successKey, successMaxCount)
		}
	}
}

// 内存限流处理器
func memoryRateLimitHandler(duration int64, totalMaxCount, successMaxCount int) gin.HandlerFunc {
	inMemoryRateLimiter.Init(time.Duration(setting.ModelRequestRateLimitDurationMinutes) * time.Minute)

	return func(c *gin.Context) {
		userId := strconv.Itoa(c.GetInt("id"))
		totalKey := ModelRequestRateLimitCountMark + userId
		successKey := ModelRequestRateLimitSuccessCountMark + userId

		// 1. 预留成功请求额度。预留本身不是成功计数，最终由请求结果
		// 提交或回滚，避免失败请求污染成功额度，同时限制并发超发。
		successReservation, allowed := inMemoryRateLimiter.Reserve(successKey, successMaxCount, duration)
		if !allowed {
			abortWithOpenAiMessage(c, http.StatusTooManyRequests, fmt.Sprintf("您已达到请求数限制：%d分钟内最多请求%d次", duration/60, successMaxCount), types.ErrorCodeGatewayRateLimit)
			return
		}

		// 2. 检查总请求数限制（当totalMaxCount为0时跳过）。如果总量
		// 桶拒绝，释放上面的成功额度预留，因为请求尚未真正执行。
		if totalMaxCount > 0 && !inMemoryRateLimiter.Request(totalKey, totalMaxCount, duration) {
			inMemoryRateLimiter.Finalize(successKey, successReservation, false)
			abortWithOpenAiMessage(c, http.StatusTooManyRequests, fmt.Sprintf("您已达到总请求数限制：%d分钟内最多请求%d次，包括失败次数，请检查您的请求是否正确", duration/60, totalMaxCount), types.ErrorCodeGatewayRateLimit)
			return
		}
		defer func() {
			inMemoryRateLimiter.Finalize(successKey, successReservation, c.Writer.Status() < 400)
		}()

		// 3. 处理请求
		c.Next()
	}
}

// ModelRequestRateLimit 模型请求限流中间件
func ModelRequestRateLimit() func(c *gin.Context) {
	return func(c *gin.Context) {
		// 在每个请求时检查是否启用限流
		if !setting.ModelRequestRateLimitEnabled {
			c.Next()
			return
		}

		// 计算限流参数
		duration := int64(setting.ModelRequestRateLimitDurationMinutes * 60)
		totalMaxCount := setting.ModelRequestRateLimitCount
		successMaxCount := setting.ModelRequestRateLimitSuccessCount

		// 获取分组
		group := common.GetContextKeyString(c, constant.ContextKeyTokenGroup)
		if group == "" {
			group = common.GetContextKeyString(c, constant.ContextKeyUserGroup)
		}

		//获取分组的限流配置
		groupTotalCount, groupSuccessCount, found := setting.GetGroupRateLimit(group)
		if found {
			totalMaxCount = groupTotalCount
			successMaxCount = groupSuccessCount
		}

		// 根据存储类型选择并执行限流处理器
		if common.RedisEnabled {
			redisRateLimitHandler(duration, totalMaxCount, successMaxCount)(c)
		} else {
			memoryRateLimitHandler(duration, totalMaxCount, successMaxCount)(c)
		}
	}
}

// AcquireModelRequestRateLimit checks and records the total-request limit for
// one relay turn. The returned callback records a successful turn when called
// with true.
func AcquireModelRequestRateLimit(c *gin.Context) (func(bool), *types.NewAPIError) {
	if !setting.ModelRequestRateLimitEnabled {
		return func(bool) {}, nil
	}

	duration := int64(setting.ModelRequestRateLimitDurationMinutes * 60)
	totalMaxCount := setting.ModelRequestRateLimitCount
	successMaxCount := setting.ModelRequestRateLimitSuccessCount
	group := common.GetContextKeyString(c, constant.ContextKeyTokenGroup)
	if group == "" {
		group = common.GetContextKeyString(c, constant.ContextKeyUserGroup)
	}
	if groupTotalCount, groupSuccessCount, found := setting.GetGroupRateLimit(group); found {
		totalMaxCount = groupTotalCount
		successMaxCount = groupSuccessCount
	}

	userID := strconv.Itoa(c.GetInt("id"))
	tooMany := func(message string) *types.NewAPIError {
		return types.NewErrorWithStatusCode(errors.New(message), types.ErrorCodeGatewayRateLimit, http.StatusTooManyRequests, types.ErrOptionWithSkipRetry())
	}
	checkFailed := func(err error) *types.NewAPIError {
		return types.NewErrorWithStatusCode(err, types.ErrorCodeAccessDenied, http.StatusInternalServerError, types.ErrOptionWithSkipRetry())
	}

	if common.RedisEnabled {
		ctx := c.Request.Context()
		rdb := common.RDB
		successKey := fmt.Sprintf("rateLimit:%s:%s", ModelRequestRateLimitSuccessCountMark, userID)
		allowed, err := checkRedisRateLimit(ctx, rdb, successKey, successMaxCount, duration)
		if err != nil {
			return nil, checkFailed(fmt.Errorf("rate limit check failed: %w", err))
		}
		if !allowed {
			return nil, tooMany(fmt.Sprintf("您已达到请求数限制：%d分钟内最多请求%d次", setting.ModelRequestRateLimitDurationMinutes, successMaxCount))
		}

		if totalMaxCount > 0 {
			totalKey := fmt.Sprintf("rateLimit:%s", userID)
			tb := limiter.New(ctx, rdb)
			allowed, err = tb.Allow(ctx, totalKey,
				limiter.WithCapacity(int64(totalMaxCount)*duration),
				limiter.WithRate(int64(totalMaxCount)),
				limiter.WithRequested(duration),
			)
			if err != nil {
				return nil, checkFailed(fmt.Errorf("rate limit check failed: %w", err))
			}
			if !allowed {
				return nil, tooMany(fmt.Sprintf("您已达到总请求数限制：%d分钟内最多请求%d次，包括失败次数，请检查您的请求是否正确", setting.ModelRequestRateLimitDurationMinutes, totalMaxCount))
			}
		}

		return func(success bool) {
			if success {
				recordRedisRequest(context.Background(), rdb, successKey, successMaxCount)
			}
		}, nil
	}

	inMemoryRateLimiter.Init(time.Duration(setting.ModelRequestRateLimitDurationMinutes) * time.Minute)
	totalKey := ModelRequestRateLimitCountMark + userID
	successKey := ModelRequestRateLimitSuccessCountMark + userID
	successReservation, allowed := inMemoryRateLimiter.Reserve(successKey, successMaxCount, duration)
	if !allowed {
		return nil, tooMany("model successful request rate limit exceeded")
	}
	if totalMaxCount > 0 && !inMemoryRateLimiter.Request(totalKey, totalMaxCount, duration) {
		inMemoryRateLimiter.Finalize(successKey, successReservation, false)
		return nil, tooMany("model request rate limit exceeded")
	}
	return func(success bool) {
		inMemoryRateLimiter.Finalize(successKey, successReservation, success)
	}, nil
}
