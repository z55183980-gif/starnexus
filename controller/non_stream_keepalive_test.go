package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newKeepAliveTestContext() (*gin.Context, *httptest.ResponseRecorder) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v11/responses", nil)
	return c, recorder
}

func TestNonStreamKeepAliveLeavesFastJSONUnchanged(t *testing.T) {
	c, recorder := newKeepAliveTestContext()
	stop := startNonStreamJSONKeepAliveWithIntervals(c, time.Hour, time.Hour)

	c.JSON(http.StatusCreated, gin.H{"ok": true})
	stop()

	require.Equal(t, http.StatusCreated, recorder.Code)
	require.JSONEq(t, `{"ok":true}`, recorder.Body.String())
	require.NotContains(t, recorder.Header(), nonStreamKeepAliveHeader)
}

func TestNonStreamKeepAlivePrefixesSlowJSONAndStopsBeforeBody(t *testing.T) {
	c, recorder := newKeepAliveTestContext()
	stop := startNonStreamJSONKeepAliveWithIntervals(c, 5*time.Millisecond, time.Hour)
	writer := c.Writer.(*nonStreamKeepAliveWriter)
	require.Eventually(t, func() bool {
		writer.mu.Lock()
		defer writer.mu.Unlock()
		return recorder.Header().Get(nonStreamKeepAliveHeader) == "active"
	}, time.Second, time.Millisecond)

	c.JSON(http.StatusOK, gin.H{"ok": true})
	stop()
	bodyAfterStop := recorder.Body.String()
	time.Sleep(15 * time.Millisecond)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "active", recorder.Header().Get(nonStreamKeepAliveHeader))
	require.Equal(t, bodyAfterStop, recorder.Body.String())
	require.JSONEq(t, `{"ok":true}`, recorder.Body.String())
}

func TestNonStreamKeepAliveStopsAfterApplicationHeader(t *testing.T) {
	c, recorder := newKeepAliveTestContext()
	stop := startNonStreamJSONKeepAliveWithIntervals(c, 20*time.Millisecond, time.Millisecond)

	c.Status(http.StatusNoContent)
	c.Writer.WriteHeaderNow()
	time.Sleep(40 * time.Millisecond)
	stop()

	require.Equal(t, http.StatusNoContent, recorder.Code)
	require.Empty(t, recorder.Body.String())
	require.NotContains(t, recorder.Header(), nonStreamKeepAliveHeader)
}

func TestNonStreamKeepAlivePreservesErrorJSONAfterHeartbeat(t *testing.T) {
	c, recorder := newKeepAliveTestContext()
	stop := startNonStreamJSONKeepAliveWithIntervals(c, 5*time.Millisecond, time.Hour)
	writer := c.Writer.(*nonStreamKeepAliveWriter)
	require.Eventually(t, func() bool {
		writer.mu.Lock()
		defer writer.mu.Unlock()
		return recorder.Header().Get(nonStreamKeepAliveHeader) == "active"
	}, time.Second, time.Millisecond)

	c.JSON(http.StatusBadGateway, gin.H{"error": "upstream failed"})
	stop()

	require.Equal(t, http.StatusOK, recorder.Code)
	require.JSONEq(t, `{"error":"upstream failed"}`, recorder.Body.String())
}
