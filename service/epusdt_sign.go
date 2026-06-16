package service

import (
	"crypto/md5"
	"encoding/hex"
	"sort"
	"strings"
)

// EpusdtSign builds the EpUSDT gateway signature:
// non-empty params (excluding signature), sorted by key, joined as k=v&..., append API token, MD5 lowercase.
func EpusdtSign(params map[string]string, apiToken string) string {
	keys := make([]string, 0, len(params))
	for key, value := range params {
		if key == "signature" || strings.TrimSpace(value) == "" {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+params[key])
	}

	raw := strings.Join(parts, "&") + apiToken
	sum := md5.Sum([]byte(raw))
	return strings.ToLower(hex.EncodeToString(sum[:]))
}

// EpusdtVerifySign returns true when the provided signature matches.
func EpusdtVerifySign(params map[string]string, apiToken string) bool {
	expected := strings.TrimSpace(params["signature"])
	if expected == "" {
		return false
	}
	return strings.EqualFold(expected, EpusdtSign(params, apiToken))
}
