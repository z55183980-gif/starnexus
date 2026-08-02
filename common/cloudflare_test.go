package common

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsCloudflareChallengeResponse(t *testing.T) {
	t.Parallel()

	body := []byte(`<!DOCTYPE html><html><body><script>window._cf_chl_opt={};</script>/cdn-cgi/challenge-platform/h/g/orchestrate/chl_page/v1?ray=a24e033d8c125025</body></html>`)
	headers := http.Header{}
	headers.Set("Content-Type", "text/html; charset=UTF-8")
	headers.Set("cf-mitigated", "challenge")

	require.True(t, IsCloudflareChallengeResponse(http.StatusForbidden, headers, body))
	require.Equal(t, "a24e033d8c125025", ExtractCloudflareRayID(headers, body))
	require.Contains(t, FormatCloudflareChallengeMessage(http.StatusForbidden, headers, body), "cf-ray: a24e033d8c125025")
	require.False(t, IsCloudflareChallengeResponse(http.StatusOK, headers, body))
}
