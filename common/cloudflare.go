package common

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
)

var (
	cfRayHeaderPattern = regexp.MustCompile(`(?i)cf-ray[:\s=]+([a-z0-9-]+)`)
	cfRayBodyPattern   = regexp.MustCompile(`(?i)cRay:\s*'([a-z0-9-]+)'`)
	cfRayQueryPattern  = regexp.MustCompile(`(?i)(?:\?|&)ray=([a-z0-9-]+)`)
	cfChallengeMarkers = []string{
		"window._cf_chl_opt",
		"just a moment",
		"enable javascript and cookies to continue",
		"__cf_chl_",
		"challenge-platform",
	}
)

// IsCloudflareChallengeResponse reports whether the upstream response looks like a Cloudflare challenge page.
func IsCloudflareChallengeResponse(statusCode int, headers http.Header, body []byte) bool {
	if statusCode != http.StatusForbidden && statusCode != http.StatusTooManyRequests {
		return false
	}
	if headers != nil && strings.EqualFold(strings.TrimSpace(headers.Get("cf-mitigated")), "challenge") {
		return true
	}

	preview := strings.ToLower(truncateUTF8(string(body), 4096))
	for _, marker := range cfChallengeMarkers {
		if strings.Contains(preview, marker) {
			return true
		}
	}

	contentType := ""
	if headers != nil {
		contentType = strings.ToLower(strings.TrimSpace(headers.Get("Content-Type")))
	}
	if strings.Contains(contentType, "text/html") &&
		(strings.Contains(preview, "<html") || strings.Contains(preview, "<!doctype html")) &&
		(strings.Contains(preview, "cloudflare") || strings.Contains(preview, "challenge")) {
		return true
	}
	return false
}

// ExtractCloudflareRayID extracts cf-ray from response headers or body.
func ExtractCloudflareRayID(headers http.Header, body []byte) string {
	if headers != nil {
		if rayID := strings.TrimSpace(headers.Get("cf-ray")); rayID != "" {
			return rayID
		}
	}
	preview := truncateUTF8(string(body), 8192)
	if matches := cfRayHeaderPattern.FindStringSubmatch(preview); len(matches) >= 2 {
		return strings.TrimSpace(matches[1])
	}
	if matches := cfRayBodyPattern.FindStringSubmatch(preview); len(matches) >= 2 {
		return strings.TrimSpace(matches[1])
	}
	if matches := cfRayQueryPattern.FindStringSubmatch(preview); len(matches) >= 2 {
		return strings.TrimSpace(matches[1])
	}
	return ""
}

// FormatCloudflareChallengeMessage returns a short operator-facing Cloudflare challenge message.
func FormatCloudflareChallengeMessage(statusCode int, headers http.Header, body []byte) string {
	message := fmt.Sprintf("upstream Cloudflare challenge (HTTP %d)", statusCode)
	if rayID := ExtractCloudflareRayID(headers, body); rayID != "" {
		return fmt.Sprintf("%s, cf-ray: %s", message, rayID)
	}
	return message
}

func truncateUTF8(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max]
}
