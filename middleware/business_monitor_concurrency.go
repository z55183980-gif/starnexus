package middleware

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/gin-gonic/gin"
)

const businessMonitorConcurrencyTTL = 2 * time.Minute

var businessMonitorRequestSeq atomic.Uint64

func newBusinessMonitorRequestID() string {
	return fmt.Sprintf("%s-%d-%d-%d", common.NodeName, os.Getpid(), time.Now().UnixNano(), businessMonitorRequestSeq.Add(1))
}

// RelayConcurrency tracks an authenticated, rate-limited relay request after
// channel distribution has succeeded. Mount it only on routes that perform
// real upstream work.
func RelayConcurrency() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !common.RedisEnabled || common.RDB == nil {
			c.Next()
			return
		}

		requestID := newBusinessMonitorRequestID()
		if _, err := common.BusinessMonitorConcurrencyAcquire(c.Request.Context(), requestID, businessMonitorConcurrencyTTL); err != nil {
			logger.LogWarn(c.Request.Context(), "business monitor concurrency acquire failed: "+err.Error())
			c.Next()
			return
		}

		var once sync.Once
		release := func() {
			once.Do(func() {
				releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if _, err := common.BusinessMonitorConcurrencyRelease(releaseCtx, requestID); err != nil {
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
