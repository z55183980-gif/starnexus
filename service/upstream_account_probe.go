package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
)

const upstreamManagementTestBodyLimit = 64 * 1024

type UpstreamAccountTestResult struct {
	AccountId      int    `json:"account_id"`
	Success        bool   `json:"success"`
	StatusCode     int    `json:"status_code"`
	LatencyMs      int64  `json:"latency_ms"`
	ProxyId        int    `json:"proxy_id"`
	Result         string `json:"result"`
	CredentialType string `json:"credential_type"`
}

type UpstreamProxyTestResult struct {
	ProxyId   int    `json:"proxy_id"`
	Success   bool   `json:"success"`
	LatencyMs int64  `json:"latency_ms"`
	Result    string `json:"result"`
	IP        string `json:"ip"`
	Country   string `json:"country"`
	Region    string `json:"region"`
	City      string `json:"city"`
}

func TestUpstreamAccount(ctx context.Context, accountId int) (*UpstreamAccountTestResult, error) {
	if accountId <= 0 {
		return nil, errors.New("invalid upstream account id")
	}
	var account model.UpstreamAccount
	if err := model.DB.First(&account, accountId).Error; err != nil {
		return nil, err
	}
	credentials, err := DecryptUpstreamAccountCredentials(&account)
	if err != nil {
		return nil, errors.New("account credential cannot be decrypted")
	}
	proxy, proxyURL, err := resolveUpstreamProxy(ctx, &account, nil, "")
	if err != nil {
		result := &UpstreamAccountTestResult{AccountId: account.Id, Result: "proxy_unavailable", CredentialType: account.Type}
		recordUpstreamAccountTestState(account.Id, false, 0, result.Result)
		return result, nil
	}
	client, err := NewProxyHttpClient(proxyURL)
	if err != nil {
		result := &UpstreamAccountTestResult{AccountId: account.Id, Result: "proxy_configuration_invalid", CredentialType: account.Type}
		recordUpstreamAccountTestState(account.Id, false, 0, result.Result)
		return result, nil
	}
	startedAt := time.Now()
	statusCode := 0
	resultCode := "connection_failed"
	switch account.Type {
	case constant.UpstreamAccountTypeOAuth:
		accessToken := upstreamCredentialMapString(credentials, "access_token")
		accountExternalId := upstreamCredentialMapString(credentials, "account_id")
		baseURL := upstreamCredentialMapString(credentials, "base_url")
		if baseURL == "" {
			baseURL = "https://chatgpt.com"
		}
		statusCode, _, err = FetchCodexWhamUsage(ctx, client, baseURL, accessToken, accountExternalId)
	case constant.UpstreamAccountTypeAPIKey:
		statusCode, err = testUpstreamAPIKeyAccount(ctx, client, credentials)
	default:
		return nil, errors.New("unsupported upstream account type")
	}
	latencyMs := time.Since(startedAt).Milliseconds()
	if err == nil {
		resultCode = "upstream_rejected"
		if statusCode >= http.StatusOK && statusCode < http.StatusMultipleChoices {
			resultCode = "ok"
		}
	}
	success := resultCode == "ok"
	proxyId := 0
	if proxy != nil {
		proxyId = proxy.Id
	}
	result := &UpstreamAccountTestResult{
		AccountId: account.Id, Success: success, StatusCode: statusCode, LatencyMs: latencyMs,
		ProxyId: proxyId, Result: resultCode, CredentialType: account.Type,
	}
	recordUpstreamAccountTestState(account.Id, success, statusCode, resultCode)
	recordUpstreamAccountEvent(account.Id, proxyId, "account_test", resultCode, resultCode, map[string]any{
		"status_code": statusCode, "latency_ms": latencyMs,
	})
	return result, nil
}

func testUpstreamAPIKeyAccount(ctx context.Context, client *http.Client, credentials map[string]any) (int, error) {
	apiKey := upstreamCredentialMapString(credentials, "api_key")
	if apiKey == "" {
		return 0, errors.New("API key is missing")
	}
	baseURL := strings.TrimRight(upstreamCredentialMapString(credentials, "base_url"), "/")
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/v1/models", nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, upstreamManagementTestBodyLimit))
	return resp.StatusCode, nil
}

func recordUpstreamAccountTestState(accountId int, success bool, statusCode int, result string) {
	now := common.GetTimestamp()
	updates := map[string]any{"error_message": result, "updated_at": now}
	if success {
		updates["status"] = constant.UpstreamStatusActive
		updates["schedulable"] = true
		updates["error_message"] = ""
		updates["temp_unschedulable_until"] = nil
		updates["temp_unschedulable_reason"] = ""
		updates["rate_limit_reset_at"] = nil
	} else if statusCode == http.StatusUnauthorized {
		updates["status"] = constant.UpstreamStatusError
		updates["schedulable"] = false
		updates["temp_unschedulable_reason"] = "authentication_failed"
	} else {
		updates["temp_unschedulable_until"] = now + int64((time.Minute).Seconds())
		updates["temp_unschedulable_reason"] = result
	}
	_ = model.DB.Model(&model.UpstreamAccount{}).Where("id = ?", accountId).Updates(updates).Error
}

func TestUpstreamProxy(ctx context.Context, proxyId int) (*UpstreamProxyTestResult, error) {
	if proxyId <= 0 {
		return nil, errors.New("invalid upstream proxy id")
	}
	var proxy model.UpstreamProxy
	if err := model.DB.First(&proxy, proxyId).Error; err != nil {
		return nil, err
	}
	auth, err := DecryptUpstreamProxyAuth(&proxy)
	if err != nil {
		return nil, errors.New("proxy credential cannot be decrypted")
	}
	proxyURL, err := proxy.URL(auth.Username, auth.Password)
	if err != nil {
		return nil, errors.New("proxy configuration is invalid")
	}
	client, err := NewProxyHttpClient(proxyURL)
	if err != nil {
		return nil, errors.New("proxy client cannot be created")
	}
	testURL := strings.TrimSpace(os.Getenv("UPSTREAM_PROXY_TEST_URL"))
	if testURL == "" {
		testURL = "https://ipinfo.io/json"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, testURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	startedAt := time.Now()
	resp, err := client.Do(req)
	latencyMs := time.Since(startedAt).Milliseconds()
	result := &UpstreamProxyTestResult{ProxyId: proxy.Id, LatencyMs: latencyMs, Result: "connection_failed"}
	if err == nil && resp != nil {
		defer resp.Body.Close()
		if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
			limited := io.LimitReader(resp.Body, upstreamManagementTestBodyLimit)
			var payload struct {
				IP      string `json:"ip"`
				Country string `json:"country"`
				Region  string `json:"region"`
				City    string `json:"city"`
			}
			if decodeErr := common.DecodeJson(limited, &payload); decodeErr == nil && strings.TrimSpace(payload.IP) != "" {
				result.Success = true
				result.Result = "ok"
				result.IP = strings.TrimSpace(payload.IP)
				result.Country = strings.TrimSpace(payload.Country)
				result.Region = strings.TrimSpace(payload.Region)
				result.City = strings.TrimSpace(payload.City)
			} else {
				result.Result = "invalid_probe_response"
			}
		} else {
			result.Result = fmt.Sprintf("probe_status_%d", resp.StatusCode)
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, upstreamManagementTestBodyLimit))
		}
	}
	recordUpstreamProxyTestState(&proxy, result)
	recordUpstreamAccountEvent(0, proxy.Id, "proxy_test", result.Result, result.Result, map[string]any{
		"latency_ms": latencyMs, "country": result.Country,
	})
	return result, nil
}

func recordUpstreamProxyTestState(proxy *model.UpstreamProxy, result *UpstreamProxyTestResult) {
	if proxy == nil || result == nil {
		return
	}
	now := common.GetTimestamp()
	updates := map[string]any{
		"last_test_at": now, "latency_ms": result.LatencyMs, "latency_status": result.Result,
		"latency_message": result.Result, "observed_ip": result.IP, "observed_country": result.Country,
		"observed_region": result.Region, "observed_city": result.City, "updated_at": now,
	}
	_ = model.DB.Model(&model.UpstreamProxy{}).Where("id = ?", proxy.Id).Updates(updates).Error
}
