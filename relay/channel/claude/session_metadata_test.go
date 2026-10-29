package claude

import (
	"regexp"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func sessionMetadataInfo(tokenID int, enabled bool) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{TokenId: tokenID, ChannelMeta: &relaycommon.ChannelMeta{
		ApiType:              constant.APITypeAnthropic,
		ChannelOtherSettings: dto.ChannelOtherSettings{ClaudeSessionMetadata: enabled},
	}}
}

func derivedUserID(t *testing.T, info *relaycommon.RelayInfo, body string) sessionMetadataUserID {
	t.Helper()
	out, err := ApplySessionMetadata(info, []byte(body))
	require.NoError(t, err)
	raw := gjson.GetBytes(out, "metadata.user_id")
	require.True(t, raw.Exists(), string(out))
	var parsed sessionMetadataUserID
	require.NoError(t, common.UnmarshalJsonStr(raw.String(), &parsed))
	return parsed
}

const firstTurn = `{"model":"claude-opus-5-5","system":"You are helpful.","messages":[
	{"role":"user","content":[{"type":"text","text":"start the audit","cache_control":{"type":"ephemeral"}}]}]}`

// The second turn repeats the conversation start, moves the cache marker and
// adds turns; it must keep the same identity.
const secondTurn = `{"model":"claude-opus-5-5","system":[{"type":"text","text":"You are helpful."}],"messages":[
	{"role":"user","content":[{"type":"text","text":"start the audit"}]},
	{"role":"assistant","content":[{"type":"text","text":"done"}]},
	{"role":"user","content":[{"type":"text","text":"next","cache_control":{"type":"ephemeral"}}]}]}`

func TestApplySessionMetadataIsStableAcrossTurns(t *testing.T) {
	info := sessionMetadataInfo(42, true)
	first := derivedUserID(t, info, firstTurn)
	second := derivedUserID(t, info, secondTurn)

	require.Equal(t, first, second)
	require.Regexp(t, regexp.MustCompile(`^[0-9a-f]{64}$`), first.DeviceID)
	require.Regexp(t, regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-5[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`), first.SessionID)
	require.Empty(t, first.AccountUUID)
}

func TestApplySessionMetadataSeparatesConversationsAndTokens(t *testing.T) {
	base := derivedUserID(t, sessionMetadataInfo(42, true), firstTurn)

	otherConversation := derivedUserID(t, sessionMetadataInfo(42, true),
		`{"system":"You are helpful.","messages":[{"role":"user","content":"a different start"}]}`)
	require.Equal(t, base.DeviceID, otherConversation.DeviceID)
	require.NotEqual(t, base.SessionID, otherConversation.SessionID)

	otherToken := derivedUserID(t, sessionMetadataInfo(43, true), firstTurn)
	require.NotEqual(t, base.DeviceID, otherToken.DeviceID)
	require.NotEqual(t, base.SessionID, otherToken.SessionID)

	otherSystem := derivedUserID(t, sessionMetadataInfo(42, true),
		`{"system":"You are terse.","messages":[{"role":"user","content":"start the audit"}]}`)
	require.NotEqual(t, base.SessionID, otherSystem.SessionID)

	otherUser := derivedUserID(t, sessionMetadataInfo(42, true),
		`{"user":"end-user-7","system":"You are helpful.","messages":[{"role":"user","content":"start the audit"}]}`)
	require.NotEqual(t, base.SessionID, otherUser.SessionID)
}

func TestApplySessionMetadataSupportsOpenAIWireFormat(t *testing.T) {
	info := sessionMetadataInfo(42, true)
	first := derivedUserID(t, info, `{"messages":[{"role":"system","content":"rules"},{"role":"user","content":"hello"}]}`)
	second := derivedUserID(t, info, `{"messages":[{"role":"system","content":"rules"},{"role":"user","content":"hello"},{"role":"assistant","content":"hi"},{"role":"user","content":"more"}]}`)
	require.Equal(t, first, second)
}

func TestApplySessionMetadataPreservesCallerMetadata(t *testing.T) {
	info := sessionMetadataInfo(42, true)
	for _, body := range []string{
		`{"metadata":{"user_id":"user_abc_account__session_123"},"messages":[{"role":"user","content":"hello"}]}`,
		`{"metadata":"opaque","messages":[{"role":"user","content":"hello"}]}`,
		`{"messages":[{"role":"assistant","content":"no user turn"}]}`,
		`not json`,
	} {
		out, err := ApplySessionMetadata(info, []byte(body))
		require.NoError(t, err)
		require.Equal(t, body, string(out))
	}

	out, err := ApplySessionMetadata(info, []byte(`{"metadata":{"trace":"keep","user_id":""},"messages":[{"role":"user","content":"hello"}]}`))
	require.NoError(t, err)
	require.Equal(t, "keep", gjson.GetBytes(out, "metadata.trace").String())
	require.NotEmpty(t, gjson.GetBytes(out, "metadata.user_id").String())
}

func TestApplySessionMetadataRequiresAnthropicChannelOptIn(t *testing.T) {
	disabled := sessionMetadataInfo(42, false)
	otherAPI := sessionMetadataInfo(42, true)
	otherAPI.ApiType = constant.APITypeOpenAI
	for _, info := range []*relaycommon.RelayInfo{nil, {TokenId: 42}, disabled, otherAPI} {
		out, err := ApplySessionMetadata(info, []byte(firstTurn))
		require.NoError(t, err)
		require.Equal(t, firstTurn, string(out))
	}
}
