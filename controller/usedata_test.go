package controller

import (
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func quotaDataRangeContext(rawQuery string) *gin.Context {
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest("GET", "/api/data?"+rawQuery, nil)
	return context
}

func TestNormalizeQuotaDataCacheQueryIgnoresDisplayGranularity(t *testing.T) {
	values := url.Values{
		"start_timestamp": {"100"},
		"end_timestamp":   {"200"},
		"default_time":    {"week"},
	}
	normalized := normalizeQuotaDataCacheQuery(values)
	require.Empty(t, normalized.Get("default_time"))
	require.Equal(t, "100", normalized.Get("start_timestamp"))
}

func TestParseQuotaDataRange(t *testing.T) {
	start, end, err := parseQuotaDataRange(quotaDataRangeContext("start_timestamp=100&end_timestamp=200"))
	require.NoError(t, err)
	require.Equal(t, int64(100), start)
	require.Equal(t, int64(200), end)
}

func TestParseQuotaDataRangeRejectsInvalidAndOversizedRanges(t *testing.T) {
	testCases := []string{
		"start_timestamp=&end_timestamp=200",
		"start_timestamp=200&end_timestamp=100",
		"start_timestamp=1&end_timestamp=2678402",
	}
	for _, rawQuery := range testCases {
		_, _, err := parseQuotaDataRange(quotaDataRangeContext(rawQuery))
		require.Error(t, err, rawQuery)
	}
}
