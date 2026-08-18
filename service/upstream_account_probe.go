package service

import (
	"bufio"
	"bytes"
	"context"
	_ "embed"
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
	"github.com/google/uuid"
)

const upstreamManagementTestBodyLimit = 64 * 1024
const upstreamAccountProbeOutputLimit = 8 * 1024

const (
	upstreamAccountProbeOpenAIUserAgent    = CodexDefaultOriginator + "/" + CodexFallbackVersion + " (Ubuntu 22.4.0; x86_64) xterm-256color"
	upstreamAccountProbeOpenAIVersion      = CodexFallbackVersion
	legacyOpenAIAPIKeyProbeUserAgent       = "codex_cli_rs/0.144.1 (Ubuntu 22.4.0; x86_64) xterm-256color"
	legacyOpenAIAPIKeyProbeVersion         = "0.144.1"
	upstreamAccountProbeDefaultOpenAIModel = "gpt-5.4"
	UpstreamAccountTestModeDefault         = "default"
	UpstreamAccountTestModeCompact         = "compact"
)

//go:embed upstream_account_probe_instructions.txt
var upstreamAccountProbeInstructions string

type UpstreamAccountTestResult struct {
	AccountId            int      `json:"account_id"`
	Success              bool     `json:"success"`
	StatusCode           int      `json:"status_code"`
	LatencyMs            int64    `json:"latency_ms"`
	FirstOutputLatencyMs int64    `json:"first_output_latency_ms"`
	ProxyId              int      `json:"proxy_id"`
	Result               string   `json:"result"`
	CredentialType       string   `json:"credential_type"`
	Protocol             string   `json:"protocol"`
	Model                string   `json:"model"`
	TerminalType         string   `json:"terminal_type,omitempty"`
	EventTypes           []string `json:"event_types,omitempty"`
	HTTPVersion          string   `json:"http_version,omitempty"`
	ContentType          string   `json:"content_type,omitempty"`
	ContentEncoding      string   `json:"content_encoding,omitempty"`
	BodyBytes            int      `json:"body_bytes,omitempty"`
	Mode                 string   `json:"mode"`
	OutputText           string   `json:"output_text,omitempty"`
}

type UpstreamAccountTestOptions struct {
	Model string
	Mode  string
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
	Completed            bool
	TerminalType         string
	EventTypes           []string
	HTTPVersion          string
	ContentType          string
	ContentEncoding      string
	BodyBytes            int
	OutputText           string
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
		recordUpstreamAccountTestState(&account, false, 0, result.Result, nil, nil)
		return result, nil
	}
	var client *http.Client
	if account.Platform == constant.UpstreamPlatformOpenAI {
		client, err = NewOpenAIUpstreamHttpClient(proxyURL)
	} else {
		client, err = NewProxyHttpClient(proxyURL)
	}
	if err != nil {
		result := &UpstreamAccountTestResult{AccountId: account.Id, Result: "proxy_configuration_invalid", CredentialType: account.Type}
		recordUpstreamAccountTestState(&account, false, 0, result.Result, nil, nil)
		return result, nil
	}
	resultCode := "connection_failed"
	options := UpstreamAccountTestOptions{}
	if len(testOptions) > 0 {
		options = testOptions[0]
	}
	options.Mode = normalizeUpstreamAccountTestMode(options.Mode)
	probe, err := probeUpstreamAccountGeneration(ctx, client, &account, credentials, options.Model, options.Mode)
	if probe == nil {
		probe = &upstreamGenerationProbeResult{}
	}
	if err == nil {
		resultCode = "upstream_rejected"
		if probe.StatusCode >= http.StatusOK && probe.StatusCode < http.StatusMultipleChoices && probe.Completed && !probe.Failed {
			resultCode = "ok"
		} else if probe.StatusCode >= http.StatusOK && probe.StatusCode < http.StatusMultipleChoices && !probe.Failed {
			resultCode = "incomplete_response"
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
		TerminalType: probe.TerminalType, EventTypes: probe.EventTypes,
		HTTPVersion: probe.HTTPVersion, ContentType: probe.ContentType,
		ContentEncoding: probe.ContentEncoding, BodyBytes: probe.BodyBytes, Mode: options.Mode,
		OutputText: probe.OutputText,
	}
	if options.Mode == UpstreamAccountTestModeCompact {
		recordUpstreamAccountCompactProbeState(&account, success, probe.StatusCode, resultCode)
	}
	recordUpstreamAccountTestState(&account, success, probe.StatusCode, resultCode, probe.Header, probe.Body)
	recordUpstreamAccountEvent(account.Id, proxyId, "account_test", resultCode, resultCode, map[string]any{
		"status_code": probe.StatusCode, "latency_ms": probe.LatencyMs, "first_output_latency_ms": probe.FirstOutputLatencyMs,
		"protocol": probe.Protocol, "model": probe.Model, "terminal_type": probe.TerminalType, "event_types": probe.EventTypes,
		"http_version": probe.HTTPVersion, "content_type": probe.ContentType, "content_encoding": probe.ContentEncoding,
		"body_bytes": probe.BodyBytes, "mode": options.Mode,
	})
	return result, nil
}

func recordUpstreamAccountCompactProbeState(account *model.UpstreamAccount, success bool, statusCode int, result string) {
	if account == nil || account.Id <= 0 || account.Platform != constant.UpstreamPlatformOpenAI {
		return
	}
	extra := map[string]any{}
	if strings.TrimSpace(account.Extra) != "" {
		_ = common.UnmarshalJsonStr(account.Extra, &extra)
	}
	extra["openai_compact_checked_at"] = time.Now().UTC().Format(time.RFC3339)
	extra["openai_compact_last_status"] = statusCode
	extra["openai_compact_last_error"] = ""
	if success {
		extra["openai_compact_supported"] = true
	} else {
		extra["openai_compact_last_error"] = result
		if statusCode == http.StatusNotFound || statusCode == http.StatusMethodNotAllowed || statusCode == http.StatusNotImplemented {
			extra["openai_compact_supported"] = false
		}
	}
	encoded, err := common.Marshal(extra)
	if err != nil {
		return
	}
	account.Extra = string(encoded)
	_ = model.DB.Model(&model.UpstreamAccount{}).Where("id = ?", account.Id).Update("extra", account.Extra).Error
}

func normalizeUpstreamAccountTestMode(mode string) string {
	if strings.EqualFold(strings.TrimSpace(mode), UpstreamAccountTestModeCompact) {
		return UpstreamAccountTestModeCompact
	}
	return UpstreamAccountTestModeDefault
}

func probeUpstreamAccountGeneration(ctx context.Context, client *http.Client, account *model.UpstreamAccount, credentials map[string]any, requestedModel string, mode string) (*upstreamGenerationProbeResult, error) {
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
	accountOptions, _ := model.ParseUpstreamAccountOptionsWithCredentials(account.Extra, credentials)
	modelMapping := upstreamAccountModelMapping(credentials)
	if mode == UpstreamAccountTestModeCompact {
		if account.Platform != constant.UpstreamPlatformOpenAI {
			return nil, errors.New("compact account test only supports OpenAI accounts")
		}
		modelMapping = accountOptions.CompactModelMapping
	}
	if mappedModel, matched := resolveUpstreamAccountModelMapping(modelMapping, modelName); matched {
		modelName = mappedModel
	}
	modelName = normalizeUpstreamAccountProbeModel(account, modelName)
	request, protocol, err := buildUpstreamGenerationProbeRequest(ctx, account, credentials, modelName, mode)
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
		HTTPVersion: response.Proto, ContentType: response.Header.Get("Content-Type"),
		ContentEncoding: response.Header.Get("Content-Encoding"),
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		result.Body, _ = io.ReadAll(io.LimitReader(response.Body, upstreamManagementTestBodyLimit))
		result.BodyBytes = len(result.Body)
		result.LatencyMs = time.Since(startedAt).Milliseconds()
		return result, nil
	}
	// SUB2API parses a successful streaming probe as SSE even when an upstream
	// proxy strips Content-Type. The probe payload explicitly requests a stream,
	// so protocol intent is more reliable than the optional response header.
	if !strings.HasPrefix(protocol, "http_sse_") {
		result.Body, err = io.ReadAll(io.LimitReader(response.Body, upstreamManagementTestBodyLimit))
		result.BodyBytes = len(result.Body)
		result.LatencyMs = time.Since(startedAt).Milliseconds()
		if err == nil {
			result.OutputText = generationProbeJSONOutputText(protocol, result.Body)
		}
		if strings.TrimSpace(result.OutputText) != "" {
			result.FirstOutputLatencyMs = maxInt64(1, result.LatencyMs)
		}
		if err == nil {
			result.Completed, result.Failed, result.TerminalType = generationProbeJSONTerminal(protocol, result.Body)
			if protocol == "http_json_responses_compact" && !result.Failed {
				// Match SUB2API: a 2xx compact probe is a capability success even
				// when the compact response has no generation status field.
				result.Completed = true
				if result.TerminalType == "" {
					result.TerminalType = "http.2xx"
				}
			}
		}
		return result, err
	}

	visibleOutput := false
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		result.BodyBytes += len(line) + 1
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		data := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
		if len(data) == 0 {
			continue
		}
		if bytes.Equal(data, []byte("[DONE]")) {
			result.TerminalType = "[DONE]"
			result.EventTypes = appendProbeEventType(result.EventTypes, "[DONE]")
			if protocol == "http_sse_chat" || protocol == "http_sse_messages" {
				result.Completed = true
			}
			break
		}
		result.EventTypes = appendProbeEventType(result.EventTypes, generationProbeEventType(data))
		delta := generationProbeEventOutputText(protocol, data)
		if strings.TrimSpace(delta) != "" && !visibleOutput {
			visibleOutput = true
			result.FirstOutputLatencyMs = maxInt64(1, time.Since(startedAt).Milliseconds())
		}
		result.OutputText = appendGenerationProbeOutput(result.OutputText, delta)
		completed, failed, terminalType := generationProbeEventTerminal(protocol, data)
		if completed || failed {
			result.Completed = completed
			result.Failed = failed
			result.TerminalType = terminalType
		}
		if failed {
			if len(data) > upstreamManagementTestBodyLimit {
				data = data[:upstreamManagementTestBodyLimit]
			}
			result.Body = append(result.Body[:0], data...)
		}
		if completed || failed {
			break
		}
	}
	if protocol == "http_sse_messages" && !result.Failed && !result.Completed {
		// SUB2API treats a clean Claude SSE EOF as a successful account probe.
		result.Completed = true
		result.TerminalType = "eof"
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

func buildUpstreamGenerationProbeRequest(ctx context.Context, account *model.UpstreamAccount, credentials map[string]any, modelName string, mode string) (*http.Request, string, error) {
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
			if mode == UpstreamAccountTestModeCompact {
				path = "/backend-api/codex/responses/compact"
				protocol = "http_json_responses_compact"
				payload = openAICompactProbePayload(modelName)
			} else {
				path = "/backend-api/codex/responses"
				protocol = "http_sse_responses"
				payload = openAIResponsesProbePayload(modelName, prompt, true)
			}
		} else if account.Type == constant.UpstreamAccountTypeAPIKey {
			if baseURL == "" {
				baseURL = "https://api.openai.com"
			}
			if mode == UpstreamAccountTestModeCompact {
				path = "/v1/responses/compact"
				protocol = "http_json_responses_compact"
				payload = openAICompactProbePayload(modelName)
			} else if options.EffectiveOpenAIResponsesMode() == model.UpstreamOpenAIResponsesModeForceChatCompletions {
				path = "/v1/chat/completions"
				protocol = "http_sse_chat"
				payload = map[string]any{"model": modelName, "stream": true, "messages": []map[string]any{{"role": "user", "content": prompt}}}
			} else {
				path = "/v1/responses"
				protocol = "http_sse_responses"
				payload = openAIResponsesProbePayload(modelName, prompt, false)
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
	if mode == UpstreamAccountTestModeCompact {
		request.Header.Set("Accept", "application/json")
		probeSessionID := fmt.Sprintf("probe_compact_%d", account.Id)
		request.Header.Set("Session_ID", probeSessionID)
		request.Header.Set("Conversation_ID", probeSessionID)
	} else {
		request.Header.Set("Accept", "text/event-stream")
	}
	if account.Platform == constant.UpstreamPlatformOpenAI {
		if account.Type == constant.UpstreamAccountTypeOAuth {
			accountID := upstreamCredentialMapString(credentials, "account_id")
			if accountID == "" {
				accountID = upstreamCredentialMapString(credentials, "chatgpt_account_id")
			}
			request.Header.Set("Authorization", "Bearer "+upstreamCredentialMapString(credentials, "access_token"))
			request.Header.Set("chatgpt-account-id", accountID)
			// The probe endpoint still accepts the experimental capability token,
			// but its client identity must match production OAuth traffic.
			request.Header.Set("OpenAI-Beta", "responses=experimental")
		} else {
			request.Header.Set("Authorization", "Bearer "+upstreamCredentialMapString(credentials, "api_key"))
			applyUpstreamAccountOpenAIProbeHeaders(request.Header)
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
	ApplyUpstreamAccountHeaderOverrides(request.Header, account, credentials)
	// Account-level overrides are applied before the final OAuth identity
	// convergence so probe requests cannot drift from production Codex traffic.
	if account.Platform == constant.UpstreamPlatformOpenAI && account.Type == constant.UpstreamAccountTypeOAuth {
		ApplyCodexOutboundIdentity(request.Header)
	}
	return request, protocol, nil
}

func openAICompactProbePayload(modelName string) map[string]any {
	return map[string]any{
		"model":        modelName,
		"instructions": "You are a helpful coding assistant.",
		"input": []map[string]any{{
			"type": "message", "role": "user", "content": "Respond with OK.",
		}},
	}
}

func openAIResponsesProbePayload(modelName string, prompt string, isOAuth bool) map[string]any {
	payload := map[string]any{
		"model": modelName, "stream": true, "instructions": upstreamAccountProbeInstructions,
		"input": []map[string]any{{"role": "user", "content": []map[string]any{{"type": "input_text", "text": prompt}}}},
	}
	if isOAuth {
		payload["store"] = false
	}
	return payload
}

func upstreamAccountProbePrompt() string {
	return "hi"
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
	return upstreamAccountProbeDefaultOpenAIModel
}

func normalizeUpstreamAccountProbeModel(account *model.UpstreamAccount, modelName string) string {
	modelName = strings.TrimSpace(modelName)
	if account == nil || account.Platform != constant.UpstreamPlatformOpenAI || account.Type != constant.UpstreamAccountTypeOAuth {
		return modelName
	}
	normalized := strings.ToLower(modelName)
	if normalized == "gpt-5.6" || normalized == "openai/gpt-5.6" {
		return "gpt-5.6-sol"
	}
	return modelName
}

func applyUpstreamAccountOpenAIProbeHeaders(header http.Header) {
	if header == nil {
		return
	}
	header.Set("OpenAI-Beta", "responses=experimental")
	// API-key probes are not Codex OAuth traffic. Keep their historical
	// probe identity isolated from the OAuth outbound convergence policy.
	header.Set("originator", "codex_cli_rs")
	header.Set("User-Agent", legacyOpenAIAPIKeyProbeUserAgent)
	header.Set("version", legacyOpenAIAPIKeyProbeVersion)
	header.Set("X-Codex-Window-ID", uuid.NewString())
}

func generationProbeEventOutputText(protocol string, data []byte) string {
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
		return ""
	}
	switch protocol {
	case "http_sse_chat":
		var output strings.Builder
		for _, choice := range event.Choices {
			if choice.Delta.Content != nil {
				output.WriteString(*choice.Delta.Content)
			} else if choice.Delta.ReasoningContent != nil {
				output.WriteString(*choice.Delta.ReasoningContent)
			}
		}
		return output.String()
	case "http_sse_messages":
		if event.Type != "content_block_delta" {
			return ""
		}
		if delta, ok := event.Delta.(map[string]any); ok {
			if text := common.Interface2String(delta["text"]); text != "" {
				return text
			}
			return common.Interface2String(delta["thinking"])
		}
		return ""
	default:
		if strings.Contains(event.Type, ".delta") {
			return common.Interface2String(event.Delta)
		}
	}
	return ""
}

func appendGenerationProbeOutput(current string, delta string) string {
	if delta == "" || len([]rune(current)) >= upstreamAccountProbeOutputLimit {
		return current
	}
	combined := []rune(current + delta)
	if len(combined) > upstreamAccountProbeOutputLimit {
		combined = combined[:upstreamAccountProbeOutputLimit]
	}
	return string(combined)
}

func generationProbeEventType(data []byte) string {
	var event struct {
		Type string `json:"type"`
	}
	if common.Unmarshal(data, &event) != nil {
		return "invalid_json"
	}
	if strings.TrimSpace(event.Type) == "" {
		return "untyped"
	}
	return strings.TrimSpace(event.Type)
}

func appendProbeEventType(eventTypes []string, eventType string) []string {
	if eventType == "" || len(eventTypes) >= 32 {
		return eventTypes
	}
	if len(eventTypes) > 0 && eventTypes[len(eventTypes)-1] == eventType {
		return eventTypes
	}
	return append(eventTypes, eventType)
}

func generationProbeEventTerminal(protocol string, data []byte) (completed bool, failed bool, terminalType string) {
	var event struct {
		Type    string `json:"type"`
		Error   any    `json:"error"`
		Choices []struct {
			FinishReason *string `json:"finish_reason"`
		} `json:"choices"`
	}
	if common.Unmarshal(data, &event) != nil {
		return false, false, ""
	}
	if event.Type == "error" || event.Error != nil {
		return false, true, event.Type
	}
	switch protocol {
	case "http_sse_chat":
		for _, choice := range event.Choices {
			if choice.FinishReason != nil && strings.TrimSpace(*choice.FinishReason) != "" {
				return true, false, "finish_reason"
			}
		}
	case "http_sse_messages":
		if event.Type == "message_stop" {
			return true, false, event.Type
		}
	default:
		switch event.Type {
		case "response.completed", "response.done":
			return true, false, event.Type
		case "response.failed", "response.incomplete", "response.cancelled", "response.canceled":
			return false, true, event.Type
		}
	}
	return false, false, ""
}

func generationProbeJSONTerminal(protocol string, data []byte) (completed bool, failed bool, terminalType string) {
	if terminalCompleted, terminalFailed, eventType := generationProbeEventTerminal(protocol, data); terminalCompleted || terminalFailed {
		return terminalCompleted, terminalFailed, eventType
	}
	var response struct {
		Status     string `json:"status"`
		StopReason string `json:"stop_reason"`
		Error      any    `json:"error"`
		Choices    []struct {
			FinishReason *string `json:"finish_reason"`
		} `json:"choices"`
	}
	if common.Unmarshal(data, &response) != nil {
		return false, false, ""
	}
	if response.Error != nil {
		return false, true, "error"
	}
	switch protocol {
	case "http_sse_chat":
		for _, choice := range response.Choices {
			if choice.FinishReason != nil && strings.TrimSpace(*choice.FinishReason) != "" {
				return true, false, "finish_reason"
			}
		}
	case "http_sse_messages":
		if strings.TrimSpace(response.StopReason) != "" {
			return true, false, "stop_reason"
		}
	default:
		switch strings.ToLower(strings.TrimSpace(response.Status)) {
		case "completed", "done":
			return true, false, "status." + strings.ToLower(strings.TrimSpace(response.Status))
		case "failed", "incomplete", "cancelled", "canceled":
			return false, true, "status." + strings.ToLower(strings.TrimSpace(response.Status))
		}
	}
	return false, false, ""
}

func generationProbeJSONOutputText(protocol string, data []byte) string {
	if protocol == "http_sse_messages" {
		var response struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		}
		if common.Unmarshal(data, &response) == nil {
			var output string
			for _, content := range response.Content {
				output = appendGenerationProbeOutput(output, content.Text)
			}
			return output
		}
		return ""
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
		return ""
	}
	var result string
	for _, output := range response.Output {
		for _, content := range output.Content {
			result = appendGenerationProbeOutput(result, content.Text)
		}
	}
	if result != "" {
		return result
	}
	for _, choice := range response.Choices {
		result = appendGenerationProbeOutput(result, choice.Message.Content)
	}
	return result
}

func recordUpstreamAccountTestState(account *model.UpstreamAccount, success bool, statusCode int, result string, header http.Header, body []byte) {
	if account == nil || account.Id <= 0 {
		return
	}
	accountId := account.Id
	now := common.GetTimestamp()
	updates := map[string]any{"updated_at": now}
	if success {
		credentialRefreshBlocked := constant.UpstreamAccountOAuthRefreshBlocksScheduling(account.TempUnschedulableReason)
		if !credentialRefreshBlocked {
			// A successful test may recover an ordinary runtime error, but it
			// must not override an operator's manual pause or OAuth repair state.
			if account.Status == constant.UpstreamStatusError {
				updates["status"] = constant.UpstreamStatusActive
				updates["schedulable"] = true
				updates["error_message"] = ""
			}
			updates["temp_unschedulable_until"] = nil
			updates["temp_unschedulable_reason"] = ""
		}
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
