package service

import (
	"bufio"
	"bytes"
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
	"github.com/QuantumNous/new-api/types"
)

const upstreamManagementTestBodyLimit = 64 * 1024

type UpstreamAccountTestResult struct {
	AccountId            int    `json:"account_id"`
	Success              bool   `json:"success"`
	StatusCode           int    `json:"status_code"`
	LatencyMs            int64  `json:"latency_ms"`
	FirstOutputLatencyMs int64  `json:"first_output_latency_ms"`
	ProxyId              int    `json:"proxy_id"`
	Result               string `json:"result"`
	CredentialType       string `json:"credential_type"`
	Protocol             string `json:"protocol"`
	Model                string `json:"model"`
}

type UpstreamAccountTestOptions struct {
	Model string
}

type upstreamGenerationProbeResult struct {
	StatusCode           int
	LatencyMs            int64
	FirstOutputLatencyMs int64
	Header               http.Header
	Body                 []byte
	Protocol             string
	Model                string
	Failed               bool
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

func TestUpstreamAccount(ctx context.Context, accountId int, testOptions ...UpstreamAccountTestOptions) (*UpstreamAccountTestResult, error) {
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
		recordUpstreamAccountTestState(account.Id, false, 0, result.Result, nil, nil)
		return result, nil
	}
	client, err := NewProxyHttpClient(proxyURL)
	if err != nil {
		result := &UpstreamAccountTestResult{AccountId: account.Id, Result: "proxy_configuration_invalid", CredentialType: account.Type}
		recordUpstreamAccountTestState(account.Id, false, 0, result.Result, nil, nil)
		return result, nil
	}
	resultCode := "connection_failed"
	options := UpstreamAccountTestOptions{}
	if len(testOptions) > 0 {
		options = testOptions[0]
	}
	probe, err := probeUpstreamAccountGeneration(ctx, client, &account, credentials, options.Model)
	if probe == nil {
		probe = &upstreamGenerationProbeResult{}
	}
	if err == nil {
		resultCode = "upstream_rejected"
		if probe.StatusCode >= http.StatusOK && probe.StatusCode < http.StatusMultipleChoices && probe.FirstOutputLatencyMs > 0 && !probe.Failed {
			resultCode = "ok"
		} else if probe.StatusCode >= http.StatusOK && probe.StatusCode < http.StatusMultipleChoices && !probe.Failed {
			resultCode = "no_visible_output"
		}
	}
	success := resultCode == "ok"
	proxyId := 0
	if proxy != nil {
		proxyId = proxy.Id
	}
	result := &UpstreamAccountTestResult{
		AccountId: account.Id, Success: success, StatusCode: probe.StatusCode, LatencyMs: probe.LatencyMs,
		FirstOutputLatencyMs: probe.FirstOutputLatencyMs, ProxyId: proxyId, Result: resultCode,
		CredentialType: account.Type, Protocol: probe.Protocol, Model: probe.Model,
	}
	recordUpstreamAccountTestState(account.Id, success, probe.StatusCode, resultCode, probe.Header, probe.Body)
	recordUpstreamAccountEvent(account.Id, proxyId, "account_test", resultCode, resultCode, map[string]any{
		"status_code": probe.StatusCode, "latency_ms": probe.LatencyMs, "first_output_latency_ms": probe.FirstOutputLatencyMs,
		"protocol": probe.Protocol, "model": probe.Model,
	})
	return result, nil
}

func probeUpstreamAccountGeneration(ctx context.Context, client *http.Client, account *model.UpstreamAccount, credentials map[string]any, requestedModel string) (*upstreamGenerationProbeResult, error) {
	if account == nil {
		return nil, errors.New("upstream account is nil")
	}
	if client == nil {
		return nil, errors.New("upstream HTTP client is nil")
	}
	modelName := strings.TrimSpace(requestedModel)
	if modelName == "" {
		modelName = upstreamAccountProbeModel(account, credentials)
	}
	request, protocol, err := buildUpstreamGenerationProbeRequest(ctx, account, credentials, modelName)
	if err != nil {
		return nil, err
	}
	startedAt := time.Now()
	response, err := client.Do(request)
	if err != nil {
		return &upstreamGenerationProbeResult{LatencyMs: time.Since(startedAt).Milliseconds(), Protocol: protocol, Model: modelName}, err
	}
	defer response.Body.Close()
	result := &upstreamGenerationProbeResult{
		StatusCode: response.StatusCode, Header: response.Header.Clone(), Protocol: protocol, Model: modelName,
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		result.Body, _ = io.ReadAll(io.LimitReader(response.Body, upstreamManagementTestBodyLimit))
		result.LatencyMs = time.Since(startedAt).Milliseconds()
		return result, nil
	}
	if !strings.Contains(strings.ToLower(response.Header.Get("Content-Type")), "text/event-stream") {
		result.Body, err = io.ReadAll(io.LimitReader(response.Body, upstreamManagementTestBodyLimit))
		result.LatencyMs = time.Since(startedAt).Milliseconds()
		if err == nil && generationProbeJSONHasVisibleOutput(protocol, result.Body) {
			result.FirstOutputLatencyMs = maxInt64(1, result.LatencyMs)
		}
		return result, err
	}

	visibleOutput := false
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		data := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
		if len(data) == 0 || bytes.Equal(data, []byte("[DONE]")) {
			continue
		}
		if generationProbeEventHasVisibleOutput(protocol, data) && !visibleOutput {
			visibleOutput = true
			result.FirstOutputLatencyMs = maxInt64(1, time.Since(startedAt).Milliseconds())
		}
		if generationProbeEventFailed(protocol, data) {
			result.Failed = true
			if len(data) > upstreamManagementTestBodyLimit {
				data = data[:upstreamManagementTestBodyLimit]
			}
			result.Body = append(result.Body[:0], data...)
		}
	}
	result.LatencyMs = time.Since(startedAt).Milliseconds()
	return result, scanner.Err()
}

func maxInt64(a int64, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func buildUpstreamGenerationProbeRequest(ctx context.Context, account *model.UpstreamAccount, credentials map[string]any, modelName string) (*http.Request, string, error) {
	prompt := upstreamAccountProbePrompt()
	baseURL := strings.TrimRight(upstreamCredentialMapString(credentials, "base_url"), "/")
	protocol := ""
	path := ""
	payload := map[string]any{}
	options, _ := model.ParseUpstreamAccountOptions(account.Extra)

	switch account.Platform {
	case constant.UpstreamPlatformOpenAI:
		if account.Type == constant.UpstreamAccountTypeOAuth {
			if baseURL == "" {
				baseURL = "https://chatgpt.com"
			}
			path = "/backend-api/codex/responses"
			protocol = "http_sse_responses"
			payload = openAIResponsesProbePayload(modelName, prompt)
		} else if account.Type == constant.UpstreamAccountTypeAPIKey {
			if baseURL == "" {
				baseURL = "https://api.openai.com"
			}
			if options.OpenAIResponsesMode == model.UpstreamOpenAIResponsesModeForceChatCompletions {
				path = "/v1/chat/completions"
				protocol = "http_sse_chat"
				payload = map[string]any{"model": modelName, "stream": true, "messages": []map[string]any{{"role": "user", "content": prompt}}}
			} else {
				path = "/v1/responses"
				protocol = "http_sse_responses"
				payload = openAIResponsesProbePayload(modelName, prompt)
			}
		} else {
			return nil, "", errors.New("unsupported OpenAI account type")
		}
	case constant.UpstreamPlatformAnthropic:
		if account.Type != constant.UpstreamAccountTypeOAuth && account.Type != constant.UpstreamAccountTypeSetupToken && account.Type != constant.UpstreamAccountTypeAPIKey {
			return nil, "", errors.New("use a local channel request to test Bedrock or Vertex credentials")
		}
		if baseURL == "" {
			baseURL = "https://api.anthropic.com"
		}
		path = "/v1/messages"
		protocol = "http_sse_messages"
		payload = map[string]any{"model": modelName, "max_tokens": 64, "stream": true, "messages": []map[string]any{{"role": "user", "content": []map[string]any{{"type": "text", "text": prompt}}}}}
	default:
		return nil, "", errors.New("unsupported upstream account platform")
	}

	body, err := common.Marshal(payload)
	if err != nil {
		return nil, "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, "", err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "text/event-stream")
	if account.Platform == constant.UpstreamPlatformOpenAI {
		if account.Type == constant.UpstreamAccountTypeOAuth {
			request.Header.Set("Authorization", "Bearer "+upstreamCredentialMapString(credentials, "access_token"))
			request.Header.Set("chatgpt-account-id", upstreamCredentialMapString(credentials, "account_id"))
			request.Header.Set("OpenAI-Beta", "responses=experimental")
			request.Header.Set("originator", "codex_cli_rs")
			request.Header.Set("User-Agent", "codex_cli_rs/starnexus-account-test")
		} else {
			request.Header.Set("Authorization", "Bearer "+upstreamCredentialMapString(credentials, "api_key"))
		}
	} else if account.Type == constant.UpstreamAccountTypeAPIKey {
		if options.AnthropicAPIKeyAuthScheme == model.UpstreamAnthropicAuthSchemeBearer {
			request.Header.Set("Authorization", "Bearer "+upstreamCredentialMapString(credentials, "api_key"))
		} else {
			request.Header.Set("x-api-key", upstreamCredentialMapString(credentials, "api_key"))
		}
		request.Header.Set("anthropic-version", "2023-06-01")
	} else {
		request.Header.Set("Authorization", "Bearer "+upstreamCredentialMapString(credentials, "access_token"))
		request.Header.Set("anthropic-version", "2023-06-01")
		request.Header.Set("anthropic-beta", "claude-code-20250219,oauth-2025-04-20")
		request.Header.Set("User-Agent", "claude-cli/2.1.220 (external, cli)")
		request.Header.Set("X-App", "cli")
	}
	return request, protocol, nil
}

func openAIResponsesProbePayload(modelName string, prompt string) map[string]any {
	return map[string]any{
		"model": modelName, "stream": true, "store": false, "instructions": "",
		"input": []map[string]any{{"role": "user", "content": []map[string]any{{"type": "input_text", "text": prompt}}}},
	}
}

func upstreamAccountProbePrompt() string {
	paragraph := "This is a latency and protocol verification request for an AI gateway. Read the following operational context carefully: the gateway performs account selection, proxy routing, request conversion, streaming response forwarding, usage accounting, and error recovery. The purpose is to measure time to the first generated output under a realistic prompt size rather than a one-token health check. No tools, web search, or external data are required. "
	return strings.Repeat(paragraph, 8) + "After reading the context, reply with exactly: STARNEXUS_PROBE_OK"
}

func upstreamAccountProbeModel(account *model.UpstreamAccount, credentials map[string]any) string {
	for _, key := range []string{"test_model", "model"} {
		if value := upstreamCredentialMapString(credentials, key); value != "" {
			return value
		}
	}
	var extra map[string]any
	if account != nil && common.UnmarshalJsonStr(account.Extra, &extra) == nil {
		if value := upstreamCredentialMapString(extra, "test_model"); value != "" {
			return value
		}
		for _, key := range []string{"supported_models", "models"} {
			if values, ok := extra[key].([]any); ok {
				for _, value := range values {
					if modelName, ok := value.(string); ok && strings.TrimSpace(modelName) != "" && !strings.Contains(modelName, "*") {
						return strings.TrimSpace(modelName)
					}
				}
			}
		}
	}
	if account != nil && account.Platform == constant.UpstreamPlatformAnthropic {
		if value := strings.TrimSpace(os.Getenv("UPSTREAM_ACCOUNT_TEST_ANTHROPIC_MODEL")); value != "" {
			return value
		}
		return "claude-sonnet-4-5-20250929"
	}
	if value := strings.TrimSpace(os.Getenv("UPSTREAM_ACCOUNT_TEST_OPENAI_MODEL")); value != "" {
		return value
	}
	return "gpt-5.6-sol"
}

func generationProbeEventHasVisibleOutput(protocol string, data []byte) bool {
	var event struct {
		Type    string `json:"type"`
		Delta   any    `json:"delta"`
		Choices []struct {
			Delta struct {
				Content          *string `json:"content"`
				ReasoningContent *string `json:"reasoning_content"`
			} `json:"delta"`
		} `json:"choices"`
	}
	if common.Unmarshal(data, &event) != nil {
		return false
	}
	switch protocol {
	case "http_sse_chat":
		for _, choice := range event.Choices {
			if (choice.Delta.Content != nil && *choice.Delta.Content != "") || (choice.Delta.ReasoningContent != nil && *choice.Delta.ReasoningContent != "") {
				return true
			}
		}
	case "http_sse_messages":
		if event.Type != "content_block_delta" {
			return false
		}
		if delta, ok := event.Delta.(map[string]any); ok {
			return strings.TrimSpace(common.Interface2String(delta["text"])) != "" || strings.TrimSpace(common.Interface2String(delta["thinking"])) != ""
		}
		return false
	default:
		return strings.Contains(event.Type, ".delta") && strings.TrimSpace(common.Interface2String(event.Delta)) != ""
	}
	return false
}

func generationProbeEventFailed(_ string, data []byte) bool {
	var event struct {
		Type string `json:"type"`
	}
	if common.Unmarshal(data, &event) != nil {
		return false
	}
	return event.Type == "error" || event.Type == "response.failed"
}

func generationProbeJSONHasVisibleOutput(protocol string, data []byte) bool {
	if protocol == "http_sse_messages" {
		var response struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		}
		if common.Unmarshal(data, &response) == nil {
			for _, content := range response.Content {
				if content.Text != "" {
					return true
				}
			}
		}
		return false
	}
	var response struct {
		Output []struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if common.Unmarshal(data, &response) != nil {
		return false
	}
	for _, output := range response.Output {
		for _, content := range output.Content {
			if content.Text != "" {
				return true
			}
		}
	}
	return len(response.Choices) > 0 && response.Choices[0].Message.Content != ""
}

func recordUpstreamAccountTestState(accountId int, success bool, statusCode int, result string, header http.Header, body []byte) {
	now := common.GetTimestamp()
	updates := map[string]any{"error_message": result, "updated_at": now}
	if success {
		updates["status"] = constant.UpstreamStatusActive
		updates["schedulable"] = true
		updates["error_message"] = ""
		updates["temp_unschedulable_until"] = nil
		updates["temp_unschedulable_reason"] = ""
		updates["rate_limit_reset_at"] = nil
		updates["rate_limited_at"] = nil
		updates["overload_until"] = nil
	} else if statusCode == http.StatusUnauthorized {
		apiErr := types.NewErrorWithStatusCode(errors.New("upstream authentication failed"), types.ErrorCodeBadResponseStatusCode, statusCode)
		apiErr.SetUpstreamResponse(header, body)
		ApplyUpstreamAccountError(accountId, 0, apiErr)
		return
	} else if statusCode == http.StatusTooManyRequests {
		apiErr := types.NewErrorWithStatusCode(errors.New("upstream rate limited"), types.ErrorCodeBadResponseStatusCode, statusCode)
		apiErr.SetUpstreamResponse(header, body)
		resetAt, windowStart, windowEnd := upstreamRateLimitState(apiErr, now)
		updates["rate_limited_at"] = now
		updates["rate_limit_reset_at"] = resetAt
		updates["temp_unschedulable_reason"] = "rate_limited"
		if windowStart != nil && windowEnd != nil {
			updates["session_window_start"] = *windowStart
			updates["session_window_end"] = *windowEnd
			updates["session_window_status"] = "rejected"
		}
	} else if statusCode == 529 {
		updates["overload_until"] = now + int64((10 * time.Minute).Seconds())
		updates["temp_unschedulable_reason"] = "upstream_overloaded"
	} else {
		updates["temp_unschedulable_until"] = now + int64((time.Minute).Seconds())
		updates["temp_unschedulable_reason"] = result
	}
	_ = model.DB.Model(&model.UpstreamAccount{}).Where("id = ?", accountId).Updates(updates).Error
	if success {
		recordUpstreamAccountRateLimitHeaders(accountId, header)
	}
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
