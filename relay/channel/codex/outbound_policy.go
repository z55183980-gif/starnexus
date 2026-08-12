package codex

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	appconstant "github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tidwall/gjson"
	"golang.org/x/net/http/httpguts"
)

const codexRoutingHintHeader = "x-codex-routing-hint"

func (a *Adaptor) FinalizeOutboundJSONBody(c *gin.Context, info *relaycommon.RelayInfo, body []byte) ([]byte, error) {
	if info == nil || !gjson.ValidBytes(body) {
		return body, nil
	}
	accountID := common.GetContextKeyInt(c, appconstant.ContextKeyUpstreamAccountId)
	state := &relaycommon.CodexOutboundState{
		AccountID:   accountID,
		FinalModel:  strings.TrimSpace(gjson.GetBytes(body, "model").String()),
		ServiceTier: strings.TrimSpace(gjson.GetBytes(body, "service_tier").String()),
	}
	info.CodexOutboundState = state

	if info.RelayMode != relayconstant.RelayModeResponses || ResponsesUsesNativeWebSocket(c, info) {
		return body, nil
	}
	selection, ok := currentOpenAIOAuthSelection(c)
	if !ok {
		return body, nil
	}
	options, err := model.ParseUpstreamAccountOptionsWithCredentials(selection.Account.Extra, selection.Credentials)
	if err != nil {
		return nil, err
	}
	mode := options.EffectiveCodexFingerprintMode(system_setting.GetCodexSetting().FingerprintDefaultMode)
	if mode == model.UpstreamCodexFingerprintModeOff {
		return body, nil
	}
	fingerprint := resolveCodexFingerprint(c, body, selection, mode)
	if fingerprint == nil {
		return body, nil
	}
	state.Fingerprint = fingerprint
	return applyCodexFingerprintBody(body, fingerprint)
}

func (a *Adaptor) FinalizeOutboundRequest(c *gin.Context, info *relaycommon.RelayInfo, request *http.Request) error {
	if request == nil || request.Header == nil || info == nil {
		return nil
	}
	currentAccountID := common.GetContextKeyInt(c, appconstant.ContextKeyUpstreamAccountId)
	state := info.CodexOutboundState
	if state != nil && state.AccountID > 0 && currentAccountID > 0 && state.AccountID != currentAccountID {
		state = nil
		info.CodexOutboundState = nil
	}

	applyCodexEndpointBetaPolicy(request.Header, c, info)
	applyCodexOAuthCredentials(request.Header, c)
	service.ApplyCodexOutboundIdentity(request.Header)
	if state != nil && state.Fingerprint != nil {
		applyCodexFingerprintHeaders(request.Header, state.Fingerprint)
	}
	applyCodexRoutingHint(request.Header, info, state)
	return nil
}

func applyCodexOAuthCredentials(header http.Header, c *gin.Context) {
	selection, ok := currentOpenAIOAuthSelection(c)
	if !ok || selection == nil || header == nil {
		return
	}
	accessToken := firstCredentialString(selection.Credentials, "access_token")
	accountID := firstCredentialString(selection.Credentials, "account_id", "chatgpt_account_id")
	if accessToken != "" {
		header.Set("Authorization", "Bearer "+accessToken)
	}
	if accountID != "" {
		header.Set("chatgpt-account-id", accountID)
	}
}

func currentOpenAIOAuthSelection(c *gin.Context) (*service.UpstreamAccountSelection, bool) {
	selection, ok := common.GetContextKeyType[*service.UpstreamAccountSelection](c, appconstant.ContextKeyUpstreamAccountSelection)
	return selection, ok && selection != nil && selection.Account.Platform == appconstant.UpstreamPlatformOpenAI &&
		selection.Account.Type == appconstant.UpstreamAccountTypeOAuth
}

func resolveCodexFingerprint(c *gin.Context, body []byte, selection *service.UpstreamAccountSelection, mode string) *relaycommon.CodexFingerprintState {
	if selection == nil {
		return nil
	}
	externalAccountID := firstCredentialString(selection.Credentials, "account_id", "chatgpt_account_id")
	if externalAccountID == "" {
		externalAccountID = fmt.Sprintf("local-account:%d", selection.Account.Id)
	}
	installationID := firstCredentialString(selection.Credentials, "openai_device_id", "device_id", "codex_installation_id")
	if installationID == "" {
		installationID = stableCodexUUID("new-api:codex-installation:v1:" + externalAccountID)
	} else if parsed, err := uuid.Parse(installationID); err == nil {
		installationID = parsed.String()
	} else {
		installationID = stableCodexUUID("new-api:codex-installation:explicit:v1:" + installationID)
	}
	result := &relaycommon.CodexFingerprintState{Mode: mode, InstallationID: installationID}
	if mode == model.UpstreamCodexFingerprintModeDevice {
		return result
	}
	result.SessionID = stableCodexUUID("new-api:codex-session:v1:" + externalAccountID)
	clientSessionID := ""
	if c != nil {
		clientSessionID = strings.TrimSpace(c.GetHeader("session-id"))
		if clientSessionID == "" {
			clientSessionID = strings.TrimSpace(c.GetHeader("session_id"))
		}
	}
	if clientSessionID == "" {
		clientSessionID = strings.TrimSpace(gjson.GetBytes(body, "prompt_cache_key").String())
	}
	if mode == model.UpstreamCodexFingerprintModeFull || clientSessionID == "" {
		result.ThreadID = result.SessionID
	} else {
		result.ThreadID = stableCodexUUID("new-api:codex-thread:v1:" + externalAccountID + ":" + clientSessionID)
	}
	turnID, err := uuid.NewV7()
	if err != nil {
		turnID = uuid.New()
	}
	result.TurnID = turnID.String()
	result.WindowID = result.ThreadID + ":0"
	return result
}

func stableCodexUUID(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	var id uuid.UUID
	copy(id[:], sum[:16])
	id[6] = (id[6] & 0x0f) | 0x40
	id[8] = (id[8] & 0x3f) | 0x80
	return id.String()
}

func firstCredentialString(credentials map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, _ := credentials[key].(string); strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func applyCodexFingerprintBody(body []byte, fingerprint *relaycommon.CodexFingerprintState) ([]byte, error) {
	var payload map[string]any
	if err := common.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	metadata, _ := payload["client_metadata"].(map[string]any)
	if metadata == nil {
		metadata = make(map[string]any)
	}
	metadata["x-codex-installation-id"] = fingerprint.InstallationID
	turnFields := map[string]any{"installation_id": fingerprint.InstallationID}
	if fingerprint.Mode != model.UpstreamCodexFingerprintModeDevice {
		metadata["session_id"] = fingerprint.SessionID
		metadata["thread_id"] = fingerprint.ThreadID
		metadata["turn_id"] = fingerprint.TurnID
		metadata["x-codex-window-id"] = fingerprint.WindowID
		turnFields["session_id"] = fingerprint.SessionID
		turnFields["thread_id"] = fingerprint.ThreadID
		turnFields["turn_id"] = fingerprint.TurnID
		turnFields["window_id"] = fingerprint.WindowID
		turnFields["turn_started_at_unix_ms"] = time.Now().UnixMilli()
	}
	rewriteEmbeddedTurnMetadata(metadata, turnFields)
	payload["client_metadata"] = metadata
	return common.Marshal(payload)
}

func applyCodexFingerprintHeaders(header http.Header, fingerprint *relaycommon.CodexFingerprintState) {
	if header == nil || fingerprint == nil {
		return
	}
	header.Set("x-codex-installation-id", fingerprint.InstallationID)
	fields := map[string]any{"installation_id": fingerprint.InstallationID}
	if fingerprint.Mode != model.UpstreamCodexFingerprintModeDevice {
		header.Set("x-codex-window-id", fingerprint.WindowID)
		header.Set("x-client-request-id", fingerprint.ThreadID)
		header.Set("session-id", fingerprint.SessionID)
		header.Set("session_id", fingerprint.SessionID)
		header.Set("thread-id", fingerprint.ThreadID)
		fields["session_id"] = fingerprint.SessionID
		fields["thread_id"] = fingerprint.ThreadID
		fields["turn_id"] = fingerprint.TurnID
		fields["window_id"] = fingerprint.WindowID
		fields["turn_started_at_unix_ms"] = time.Now().UnixMilli()
	}
	rewriteHeaderTurnMetadata(header, fields)
}

func rewriteHeaderTurnMetadata(header http.Header, fields map[string]any) {
	raw := strings.TrimSpace(header.Get("x-codex-turn-metadata"))
	if raw == "" {
		return
	}
	metadata := map[string]any{}
	if common.UnmarshalJsonStr(raw, &metadata) != nil {
		return
	}
	for key, value := range fields {
		metadata[key] = value
	}
	if encoded, err := common.Marshal(metadata); err == nil {
		header.Set("x-codex-turn-metadata", string(encoded))
	}
}

func rewriteEmbeddedTurnMetadata(metadata map[string]any, fields map[string]any) {
	raw, _ := metadata["x-codex-turn-metadata"].(string)
	if strings.TrimSpace(raw) == "" {
		return
	}
	embedded := map[string]any{}
	if common.UnmarshalJsonStr(raw, &embedded) != nil {
		return
	}
	for key, value := range fields {
		embedded[key] = value
	}
	if encoded, err := common.Marshal(embedded); err == nil {
		metadata["x-codex-turn-metadata"] = string(encoded)
	}
}

func applyCodexEndpointBetaPolicy(header http.Header, c *gin.Context, info *relaycommon.RelayInfo) {
	switch info.RelayMode {
	case relayconstant.RelayModeAlphaSearch:
		deleteHeaderEqualFold(header, "OpenAI-Beta")
		for _, name := range []string{
			"session-id", "session_id", "conversation_id", "x-codex-window-id", "x-codex-installation-id",
			"x-codex-turn-state", "x-codex-turn-metadata", ResponsesLiteHeader,
		} {
			deleteHeaderEqualFold(header, name)
		}
	case relayconstant.RelayModeResponses:
		if ResponsesUsesNativeWebSocket(c, info) {
			deleteHeaderEqualFold(header, "OpenAI-Beta")
			header.Set("OpenAI-Beta", "responses_websockets=2026-02-06")
			return
		}
		// Ordinary OAuth Responses is no longer a beta protocol. Remove the
		// entire client-supplied beta field so websocket/experimental tokens
		// cannot cross the HTTP route accidentally.
		deleteHeaderEqualFold(header, "OpenAI-Beta")
	default:
		deleteHeaderEqualFold(header, "OpenAI-Beta")
	}
}

func applyCodexRoutingHint(header http.Header, info *relaycommon.RelayInfo, state *relaycommon.CodexOutboundState) {
	deleteHeaderEqualFold(header, codexRoutingHintHeader)
	if state == nil || !system_setting.GetCodexSetting().RoutingHintEnabled {
		return
	}
	if info.RelayMode != relayconstant.RelayModeResponses && info.RelayMode != relayconstant.RelayModeResponsesCompact {
		return
	}
	modelName := strings.TrimSpace(state.FinalModel)
	if modelName == "" || strings.ContainsAny(modelName, ";=") {
		return
	}
	tier := strings.ToLower(strings.TrimSpace(state.ServiceTier))
	if tier == "fast" {
		tier = "priority"
	}
	if tier != "priority" && tier != "flex" {
		tier = ""
	}
	hint := "model=" + modelName
	if tier != "" {
		hint += ";tier=" + tier
	}
	if httpguts.ValidHeaderFieldValue(hint) {
		header.Set(codexRoutingHintHeader, hint)
	}
}

func deleteHeaderEqualFold(header http.Header, name string) {
	for key := range header {
		if strings.EqualFold(strings.TrimSpace(key), strings.TrimSpace(name)) {
			delete(header, key)
		}
	}
}
