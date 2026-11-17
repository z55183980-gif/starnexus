package claude

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/google/uuid"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// sessionMetadataNamespace scopes derived session IDs to this feature, so the
// same seed can never collide with UUIDs generated for other purposes.
var sessionMetadataNamespace = uuid.MustParse("6f1d7c2e-4b8a-5d3f-9e61-0a2b3c4d5e6f")

type sessionMetadataUserID struct {
	DeviceID    string `json:"device_id"`
	AccountUUID string `json:"account_uuid"`
	SessionID   string `json:"session_id"`
}

// SessionMetadataEnabled reports whether the channel asks for a derived
// conversation identity on Claude requests that carry none.
func SessionMetadataEnabled(info *relaycommon.RelayInfo) bool {
	return info != nil && info.ChannelMeta != nil && info.ApiType == constant.APITypeAnthropic && info.ChannelOtherSettings.ClaudeSessionMetadata
}

// ApplySessionMetadata gives a Claude request without a usable metadata.user_id a stable
// per-conversation identity. A sticky-routing Claude gateway (Sub2API) otherwise
// hashes the whole message list, which changes every turn, so each turn lands
// on a different upstream account and rebuilds the full prompt cache.
//
// The identity is derived only from data that every turn of one conversation
// repeats: the token, the optional OpenAI `user` field, the system text and the
// first user message text. Caller-supplied metadata wins; a caller's session
// without a device only gains the device (see completeUserID).
func ApplySessionMetadata(info *relaycommon.RelayInfo, body []byte) ([]byte, error) {
	if !SessionMetadataEnabled(info) || !gjson.ValidBytes(body) {
		return body, nil
	}
	metadata := gjson.GetBytes(body, "metadata")
	if metadata.Exists() && metadata.Type != gjson.Null && !metadata.IsObject() {
		return body, nil
	}
	tokenID := strconv.Itoa(info.TokenId)
	if existing := metadata.Get("user_id"); existing.Exists() && strings.TrimSpace(existing.String()) != "" {
		if existing.Type != gjson.String {
			return body, nil
		}
		completed, ok := completeUserID(existing.String(), tokenID)
		if !ok {
			return body, nil
		}
		return sjson.SetBytes(body, "metadata.user_id", completed)
	}
	firstUser := firstUserMessageText(body)
	if firstUser == "" {
		return body, nil
	}
	seed := strings.Join([]string{
		"claude-session-v1",
		tokenID,
		gjson.GetBytes(body, "user").String(),
		systemText(body),
		firstUser,
	}, "\x00")
	userID, err := common.Marshal(sessionMetadataUserID{
		DeviceID:  sessionDeviceID(tokenID),
		SessionID: uuid.NewSHA1(sessionMetadataNamespace, []byte(seed)).String(),
	})
	if err != nil {
		return nil, err
	}
	return sjson.SetBytes(body, "metadata.user_id", string(userID))
}

func sessionDeviceID(tokenID string) string {
	device := sha256.Sum256([]byte("claude-device-v1\x00" + tokenID))
	return hex.EncodeToString(device[:])
}

// completeUserID adds a device_id to a JSON user_id that names a session but no
// device, such as {"session_id":"..."}. Sub2API rejects that shape and falls back
// to hashing cache-marked content, which differs between turns and moves the
// conversation between accounts. The caller's session and other fields stay.
func completeUserID(raw string, tokenID string) (string, bool) {
	if !gjson.Valid(raw) {
		return "", false
	}
	parsed := gjson.Parse(raw)
	session := parsed.Get("session_id")
	if !parsed.IsObject() || session.Type != gjson.String || strings.TrimSpace(session.String()) == "" ||
		strings.TrimSpace(parsed.Get("device_id").String()) != "" {
		return "", false
	}
	completed, err := sjson.Set(raw, "device_id", sessionDeviceID(tokenID))
	if err != nil {
		return "", false
	}
	return completed, true
}

// systemText covers both Claude top-level system and OpenAI system/developer
// messages, so passthrough bodies in either wire format derive the same way.
func systemText(body []byte) string {
	var parts []string
	if system := gjson.GetBytes(body, "system"); system.Exists() {
		parts = append(parts, contentText(system))
	}
	gjson.GetBytes(body, "messages").ForEach(func(_, message gjson.Result) bool {
		if role := message.Get("role").String(); role == "system" || role == "developer" {
			parts = append(parts, contentText(message.Get("content")))
		}
		return true
	})
	return strings.Join(parts, "\n")
}

func firstUserMessageText(body []byte) string {
	text := ""
	gjson.GetBytes(body, "messages").ForEach(func(_, message gjson.Result) bool {
		if message.Get("role").String() != "user" {
			return true
		}
		content := message.Get("content")
		if text = contentText(content); text == "" {
			// Non-text first turns (e.g. images) still repeat verbatim.
			text = content.Raw
		}
		return false
	})
	return text
}

// contentText joins text parts only. Cache markers and other block fields may
// move between turns and must not change the derived identity.
func contentText(content gjson.Result) string {
	if content.Type == gjson.String {
		return content.String()
	}
	if !content.IsArray() {
		return ""
	}
	var parts []string
	content.ForEach(func(_, block gjson.Result) bool {
		if text := block.Get("text"); text.Exists() {
			parts = append(parts, text.String())
		}
		return true
	})
	return strings.Join(parts, "\n")
}
