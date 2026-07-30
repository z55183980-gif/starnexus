package service

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"golang.org/x/net/http/httpguts"
)

const upstreamAccountHeaderOverrideLimit = 64

var upstreamAccountBlockedHeaderOverrides = map[string]struct{}{
	"host": {}, "content-length": {}, "content-type": {}, "transfer-encoding": {},
	"connection": {}, "keep-alive": {}, "proxy-authenticate": {}, "proxy-authorization": {},
	"proxy-connection": {}, "te": {}, "trailer": {}, "upgrade": {},
	"authorization": {}, "x-api-key": {}, "x-goog-api-key": {}, "cookie": {}, "accept-encoding": {},
	"sec-websocket-key": {}, "sec-websocket-version": {}, "sec-websocket-extensions": {},
	"sec-websocket-protocol": {}, "sec-websocket-accept": {},
	"session_id": {}, "conversation_id": {}, "x-codex-turn-state": {}, "x-codex-turn-metadata": {},
	"chatgpt-account-id": {}, "x-claude-code-session-id": {}, "x-client-request-id": {},
}

func ValidateUpstreamAccountHeaderOverrides(account *model.UpstreamAccount, credentials map[string]any) error {
	if credentials == nil {
		return nil
	}
	enabledValue, hasEnabled := credentials["header_override_enabled"]
	if hasEnabled && enabledValue != nil {
		if _, ok := enabledValue.(bool); !ok {
			return errors.New("header_override_enabled must be a boolean")
		}
	}
	if enabled, _ := enabledValue.(bool); enabled && !upstreamAccountAllowsHeaderOverrides(account) {
		return errors.New("header overrides require an OpenAI or Anthropic API key account")
	}
	raw, exists := credentials["header_overrides"]
	if !exists || raw == nil {
		return nil
	}
	entries, err := upstreamAccountRawHeaderOverrides(raw)
	if err != nil {
		return err
	}
	if len(entries) > upstreamAccountHeaderOverrideLimit {
		return fmt.Errorf("header_overrides supports at most %d entries", upstreamAccountHeaderOverrideLimit)
	}
	seen := make(map[string]struct{}, len(entries))
	for name, value := range entries {
		name = strings.ToLower(strings.TrimSpace(name))
		value = strings.TrimSpace(value)
		if name == "" && value == "" {
			continue
		}
		if name == "" || !httpguts.ValidHeaderFieldName(name) {
			return fmt.Errorf("invalid header override name %q", name)
		}
		if _, blocked := upstreamAccountBlockedHeaderOverrides[name]; blocked {
			return fmt.Errorf("header %q is not allowed to be overridden", name)
		}
		if len(name) > 200 || len(value) > 8192 || !httpguts.ValidHeaderFieldValue(value) {
			return fmt.Errorf("invalid header override value for %q", name)
		}
		if _, duplicate := seen[name]; duplicate {
			return fmt.Errorf("duplicate header override %q", name)
		}
		seen[name] = struct{}{}
	}
	return nil
}

func ResolveUpstreamAccountHeaderOverrides(account *model.UpstreamAccount, credentials map[string]any) map[string]any {
	if !upstreamAccountAllowsHeaderOverrides(account) || credentials == nil {
		return nil
	}
	enabled, ok := credentials["header_override_enabled"].(bool)
	if !ok || !enabled {
		return nil
	}
	entries, err := upstreamAccountRawHeaderOverrides(credentials["header_overrides"])
	if err != nil || len(entries) > upstreamAccountHeaderOverrideLimit {
		return nil
	}
	result := make(map[string]any, len(entries))
	for name, value := range entries {
		name = strings.ToLower(strings.TrimSpace(name))
		value = strings.TrimSpace(value)
		if name == "" || value == "" || len(name) > 200 || len(value) > 8192 || !httpguts.ValidHeaderFieldName(name) || !httpguts.ValidHeaderFieldValue(value) {
			continue
		}
		if _, blocked := upstreamAccountBlockedHeaderOverrides[name]; blocked {
			continue
		}
		result[name] = value
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func MergeUpstreamAccountHeaderOverrides(base map[string]any, account *model.UpstreamAccount, credentials map[string]any) map[string]any {
	overrides := ResolveUpstreamAccountHeaderOverrides(account, credentials)
	if len(overrides) == 0 {
		return base
	}
	merged := make(map[string]any, len(base)+len(overrides))
	for name, value := range base {
		merged[name] = value
	}
	for name, value := range overrides {
		for existing := range merged {
			if strings.EqualFold(existing, name) {
				delete(merged, existing)
			}
		}
		merged[name] = value
	}
	return merged
}

func ApplyUpstreamAccountHeaderOverrides(header http.Header, account *model.UpstreamAccount, credentials map[string]any) {
	if header == nil {
		return
	}
	for name, rawValue := range ResolveUpstreamAccountHeaderOverrides(account, credentials) {
		value, _ := rawValue.(string)
		for existing := range header {
			if strings.EqualFold(existing, name) {
				delete(header, existing)
			}
		}
		header.Set(name, value)
	}
}

func upstreamAccountAllowsHeaderOverrides(account *model.UpstreamAccount) bool {
	if account == nil || account.Type != constant.UpstreamAccountTypeAPIKey {
		return false
	}
	return account.Platform == constant.UpstreamPlatformOpenAI || account.Platform == constant.UpstreamPlatformAnthropic
}

func upstreamAccountRawHeaderOverrides(raw any) (map[string]string, error) {
	result := map[string]string{}
	switch entries := raw.(type) {
	case nil:
		return result, nil
	case map[string]string:
		for name, value := range entries {
			result[name] = value
		}
	case map[string]any:
		for name, rawValue := range entries {
			value, ok := rawValue.(string)
			if !ok {
				return nil, fmt.Errorf("header %q override value must be a string", name)
			}
			result[name] = value
		}
	default:
		return nil, errors.New("header_overrides must be an object of header name to string value")
	}
	return result, nil
}
