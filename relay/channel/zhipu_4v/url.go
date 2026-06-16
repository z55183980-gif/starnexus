package zhipu_4v

import (
	"net/url"
	"strings"
)

var zhipuOfficialHosts = map[string]struct{}{
	"open.bigmodel.cn": {},
	"bigmodel.cn":      {},
	"api.z.ai":         {},
}

// NormalizeBaseURL trims user-provided Zhipu base URLs so adaptor paths are not duplicated.
// Users often paste https://open.bigmodel.cn/api/paas/v4 from documentation.
func NormalizeBaseURL(baseURL string) string {
	baseURL = strings.TrimSpace(baseURL)
	baseURL = strings.TrimRight(baseURL, "/")
	if baseURL == "" {
		return baseURL
	}

	suffixes := []string{
		"/api/paas/v4/chat/completions",
		"/api/paas/v4/embeddings",
		"/api/paas/v4/images/generations",
		"/api/paas/v4/models",
		"/api/coding/paas/v4",
		"/api/paas/v4",
		"/api/paas/v3/model-api",
		"/api/paas/v3",
	}

	for {
		trimmed := false
		for _, suffix := range suffixes {
			if strings.HasSuffix(baseURL, suffix) {
				baseURL = strings.TrimSuffix(baseURL, suffix)
				baseURL = strings.TrimRight(baseURL, "/")
				trimmed = true
				break
			}
		}
		if !trimmed {
			break
		}
	}

	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Host == "" {
		return trimVersionSuffix(baseURL)
	}

	if _, ok := zhipuOfficialHosts[strings.ToLower(parsed.Hostname())]; ok {
		parsed.Path = ""
		parsed.RawPath = ""
		parsed.RawQuery = ""
		parsed.Fragment = ""
		return strings.TrimRight(parsed.Scheme+"://"+parsed.Host, "/")
	}

	return trimVersionSuffix(baseURL)
}

func trimVersionSuffix(baseURL string) string {
	for _, suffix := range []string{"/v4", "/v3", "/v1"} {
		if strings.HasSuffix(baseURL, suffix) {
			return strings.TrimSuffix(baseURL, suffix)
		}
	}
	return baseURL
}
