package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/gin-gonic/gin"
)

const businessMonitorConcurrencyTTL = 2 * time.Minute

var businessMonitorRequestSeq atomic.Uint64

func isBusinessMonitorRelayRequest(c *gin.Context) bool {
	path := c.Request.URL.Path
	isRelayPath := strings.HasPrefix(path, "/v1/") ||
		strings.HasPrefix(path, "/v1beta/") ||
		strings.HasPrefix(path, "/kling/") ||
		strings.HasPrefix(path, "/jimeng") ||
		strings.HasPrefix(path, "/pg/")
	if !isRelayPath {
		return false
	}
	if c.Request.Method == http.MethodPost {
		return true
	}
	return strings.EqualFold(c.GetHeader("Upgrade"), "websocket")
}

func newBusinessMonitorRequestID() string {
	return fmt.Sprintf("%d-%d", time.Now().UnixNano(), businessMonitorRequestSeq.Add(1))
}

func GlobalRelayConcurrency() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !isBusinessMonitorRelayRequest(c) || !common.RedisEnabled || common.RDB == nil {
			c.Next()
			return
		}

		requestID := newBusinessMonitorRequestID()
		if err := common.BusinessMonitorConcurrencyAcquire(c.Request.Context(), requestID, businessMonitorConcurrencyTTL); err != nil {
			logger.LogWarn(c.Request.Context(), "business monitor concurrency acquire failed: "+err.Error())
			c.Next()
			return
		}

		var once sync.Once
		release := func() {
			once.Do(func() {
				releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := common.BusinessMonitorConcurrencyRelease(releaseCtx, requestID); err != nil {
					logger.LogWarn(releaseCtx, "business monitor concurrency release failed: "+err.Error())
				}
			})
		}

		stopRefresh := make(chan struct{})
		var refreshWG sync.WaitGroup
		refreshWG.Add(1)
		go func() {
			defer refreshWG.Done()
			ticker := time.NewTicker(businessMonitorConcurrencyTTL / 3)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					refreshCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					if err := common.BusinessMonitorConcurrencyRefresh(refreshCtx, requestID, businessMonitorConcurrencyTTL); err != nil {
						logger.LogWarn(refreshCtx, "business monitor concurrency refresh failed: "+err.Error())
					}
					cancel()
				case <-stopRefresh:
					return
				}
			}
		}()

		defer func() {
			close(stopRefresh)
			refreshWG.Wait()
			release()
		}()
		c.Next()
	}
}
