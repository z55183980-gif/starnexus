package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
)

func TestMemoryModelRateLimitFailedRequestDoesNotCauseSuccess429(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalDuration := setting.ModelRequestRateLimitDurationMinutes
	originalRedisEnabled := common.RedisEnabled
	t.Cleanup(func() {
		setting.ModelRequestRateLimitDurationMinutes = originalDuration
		common.RedisEnabled = originalRedisEnabled
		inMemoryRateLimiter.Reset()
	})
	inMemoryRateLimiter.Reset()
	common.RedisEnabled = false
	setting.ModelRequestRateLimitDurationMinutes = 1

	requestCount := 0
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("id", 9001)
		memoryRateLimitHandler(60, 0, 1)(c)
	})
	router.GET("/", func(c *gin.Context) {
		requestCount++
		if requestCount == 1 {
			c.Status(http.StatusBadRequest)
			return
		}
		c.Status(http.StatusOK)
	})

	first := httptest.NewRecorder()
	router.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/", nil))
	if first.Code != http.StatusBadRequest {
		t.Fatalf("first request status=%d, want %d", first.Code, http.StatusBadRequest)
	}

	second := httptest.NewRecorder()
	router.ServeHTTP(second, httptest.NewRequest(http.MethodGet, "/", nil))
	if second.Code != http.StatusOK {
		t.Fatalf("failed first request polluted success quota: second status=%d, want %d", second.Code, http.StatusOK)
	}
}
