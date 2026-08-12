package system_setting

import (
	"strings"
	"sync/atomic"

	"github.com/QuantumNous/new-api/setting/config"
)

const (
	CodexFingerprintModeOff       = "off"
	CodexFingerprintModeDevice    = "device"
	CodexFingerprintModeSession   = "session"
	CodexFingerprintModeFull      = "full"
	CodexVersionSyncStatusIdle    = "idle"
	CodexVersionSyncStatusSyncing = "syncing"
	CodexVersionSyncStatusSuccess = "success"
	CodexVersionSyncStatusFailed  = "failed"
)

type CodexSetting struct {
	ClientVersion              string `json:"client_version"`
	SyncedClientVersion        string `json:"synced_client_version"`
	VersionSyncedAt            int64  `json:"version_synced_at"`
	VersionSyncStatus          string `json:"version_sync_status"`
	VersionSyncError           string `json:"version_sync_error"`
	VersionSyncAttemptedAt     int64  `json:"version_sync_attempted_at"`
	VersionAutoSyncEnabled     bool   `json:"version_auto_sync_enabled"`
	DisableIdentityEnforcement bool   `json:"disable_identity_enforcement"`
	RoutingHintEnabled         bool   `json:"routing_hint_enabled"`
	FingerprintDefaultMode     string `json:"fingerprint_default_mode"`
}

var (
	codexSetting = CodexSetting{
		VersionAutoSyncEnabled: true,
		VersionSyncStatus:      CodexVersionSyncStatusIdle,
		RoutingHintEnabled:     true,
		FingerprintDefaultMode: CodexFingerprintModeOff,
	}
	codexSettingSnapshot atomic.Value
)

func init() {
	config.GlobalConfig.Register("codex_setting", &codexSetting)
	UpdateAndSyncCodexSetting()
}

func UpdateAndSyncCodexSetting() {
	snapshot := codexSetting
	snapshot.ClientVersion = strings.TrimSpace(snapshot.ClientVersion)
	snapshot.SyncedClientVersion = strings.TrimSpace(snapshot.SyncedClientVersion)
	snapshot.VersionSyncStatus = normalizeCodexVersionSyncStatus(snapshot.VersionSyncStatus)
	snapshot.VersionSyncError = strings.TrimSpace(snapshot.VersionSyncError)
	snapshot.FingerprintDefaultMode = normalizeCodexFingerprintDefaultMode(snapshot.FingerprintDefaultMode)
	codexSettingSnapshot.Store(snapshot)
}

func GetCodexSetting() CodexSetting {
	if value := codexSettingSnapshot.Load(); value != nil {
		return value.(CodexSetting)
	}
	return codexSetting
}

func normalizeCodexFingerprintDefaultMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case CodexFingerprintModeDevice:
		return CodexFingerprintModeDevice
	case CodexFingerprintModeSession:
		return CodexFingerprintModeSession
	case CodexFingerprintModeFull:
		return CodexFingerprintModeFull
	default:
		return CodexFingerprintModeOff
	}
}

func normalizeCodexVersionSyncStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case CodexVersionSyncStatusSyncing:
		return CodexVersionSyncStatusSyncing
	case CodexVersionSyncStatusSuccess:
		return CodexVersionSyncStatusSuccess
	case CodexVersionSyncStatusFailed:
		return CodexVersionSyncStatusFailed
	default:
		return CodexVersionSyncStatusIdle
	}
}
