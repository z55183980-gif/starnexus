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
		release := AcquireRelayConcurrency(c.Request.Context())
		defer release()
		c.Next()
	}
}

// AcquireRelayConcurrency tracks one upstream turn in the business monitor.
// It intentionally fails open because this metric must not take down relays.
func AcquireRelayConcurrency(ctx context.Context) func() {
	if !common.RedisEnabled || common.RDB == nil {
		return func() {}
	}

	requestID := newBusinessMonitorRequestID()
	if _, err := common.BusinessMonitorConcurrencyAcquire(ctx, requestID, businessMonitorConcurrencyTTL); err != nil {
		logger.LogWarn(ctx, "business monitor concurrency acquire failed: "+err.Error())
		return func() {}
	}

	var once sync.Once
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

	return func() {
		once.Do(func() {
			close(stopRefresh)
			refreshWG.Wait()
			releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if _, err := common.BusinessMonitorConcurrencyRelease(releaseCtx, requestID); err != nil {
				logger.LogWarn(releaseCtx, "business monitor concurrency release failed: "+err.Error())
			}
		})
	}
}
