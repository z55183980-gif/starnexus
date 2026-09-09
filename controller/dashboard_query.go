package controller

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

var errDashboardBusy = errors.New("dashboard aggregate is busy")

// Bound both database concurrency and pending work. A burst can wait briefly,
// but sustained overload must not accumulate unbounded goroutines.
var dashboardAggregateWaiters = make(chan struct{}, 8)
var quotaDataWaiters = make(chan struct{}, 8)
var adminLogListWaiters = make(chan struct{}, 8)

const dashboardQueueTimeout = 2 * time.Second

func acquireDashboardSlot(ctx context.Context, slots, waiters chan struct{}, wait time.Duration) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	release := func() { <-slots }
	select {
	case slots <- struct{}{}:
		return release, nil
	default:
	}
	select {
	case waiters <- struct{}{}:
		defer func() { <-waiters }()
	default:
		return nil, errDashboardBusy
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case slots <- struct{}{}:
		return release, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
		return nil, errDashboardBusy
	}
}

func dashboardQueryError(c *gin.Context, err error) {
	if errors.Is(err, errDashboardBusy) {
		// Preserve the existing response contract for classic clients. New
		// clients use the stable code rather than matching translated prose.
		c.Header("Retry-After", "1")
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error(), "code": "dashboard_busy"})
		return
	}
	common.ApiError(c, err)
}
