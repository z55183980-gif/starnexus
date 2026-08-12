package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/bytedance/gopkg/util/gopool"
)

const (
	codexVersionSyncInterval = 6 * time.Hour
	codexVersionSyncTimeout  = 30 * time.Second
	codexReleaseBodyLimit    = 1 << 20
	codexLatestReleaseURL    = "https://api.github.com/repos/openai/codex/releases/latest"
	codexReleaseListURL      = "https://api.github.com/repos/openai/codex/releases?per_page=30"
)

var codexVersionSyncOnce sync.Once
var codexVersionSyncMutex sync.Mutex

type codexGitHubRelease struct {
	TagName    string `json:"tag_name"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
}

func StartCodexVersionSyncTask() {
	codexVersionSyncOnce.Do(func() {
		gopool.Go(func() {
			runCodexVersionSyncIfNeeded()
			ticker := time.NewTicker(codexVersionSyncInterval)
			defer ticker.Stop()
			for range ticker.C {
				runCodexVersionSyncIfNeeded()
			}
		})
	})
}

func runCodexVersionSyncIfNeeded() {
	settings := system_setting.GetCodexSetting()
	if !settings.VersionAutoSyncEnabled {
		return
	}
	if settings.VersionSyncedAt > 0 && time.Since(time.Unix(settings.VersionSyncedAt, 0)) < codexVersionSyncInterval {
		return
	}
	if err := syncCodexVersion(); err != nil {
		logger.LogWarn(context.Background(), fmt.Sprintf("Codex version synchronization failed: %v", err))
	}
}

// SyncCodexVersionNow performs an administrator-triggered synchronization and
// updates the persisted status so the management UI can show the result.
func SyncCodexVersionNow() error {
	return syncCodexVersion()
}

func syncCodexVersion() error {
	codexVersionSyncMutex.Lock()
	defer codexVersionSyncMutex.Unlock()

	startedAt := time.Now().Unix()
	_ = persistCodexVersionSyncState(system_setting.CodexVersionSyncStatusSyncing, "", startedAt, nil)
	ctx, cancel := context.WithTimeout(context.Background(), codexVersionSyncTimeout)
	defer cancel()
	latest, err := fetchLatestStableCodexVersion(ctx)
	if err != nil {
		_ = persistCodexVersionSyncState(system_setting.CodexVersionSyncStatusFailed, err.Error(), startedAt, nil)
		return err
	}
	settings := system_setting.GetCodexSetting()
	current := NormalizeCodexClientVersion(settings.SyncedClientVersion)
	if current != "" && compareCodexVersions(latest, current) < 0 {
		latest = current
	}
	if err := model.UpdateOptionsBulk(map[string]string{
		"codex_setting.synced_client_version":     latest,
		"codex_setting.version_synced_at":         fmt.Sprintf("%d", time.Now().Unix()),
		"codex_setting.version_sync_status":       system_setting.CodexVersionSyncStatusSuccess,
		"codex_setting.version_sync_error":        "",
		"codex_setting.version_sync_attempted_at": fmt.Sprintf("%d", startedAt),
	}); err != nil {
		return fmt.Errorf("persist Codex synchronized version failed: %w", err)
	}
	return nil
}

func persistCodexVersionSyncState(status, syncError string, attemptedAt int64, syncedAt *int64) error {
	values := map[string]string{
		"codex_setting.version_sync_status":       status,
		"codex_setting.version_sync_error":        syncError,
		"codex_setting.version_sync_attempted_at": fmt.Sprintf("%d", attemptedAt),
	}
	if syncedAt != nil {
		values["codex_setting.version_synced_at"] = fmt.Sprintf("%d", *syncedAt)
	}
	return model.UpdateOptionsBulk(values)
}

func fetchLatestStableCodexVersion(ctx context.Context) (string, error) {
	var latest codexGitHubRelease
	if err := requestCodexGitHubReleaseJSON(ctx, codexLatestReleaseURL, &latest); err == nil {
		if version := stableCodexReleaseVersion(latest); version != "" {
			return version, nil
		}
	}
	var releases []codexGitHubRelease
	if err := requestCodexGitHubReleaseJSON(ctx, codexReleaseListURL, &releases); err != nil {
		return "", err
	}
	best := ""
	for _, release := range releases {
		version := stableCodexReleaseVersion(release)
		if version != "" && (best == "" || compareCodexVersions(version, best) > 0) {
			best = version
		}
	}
	if best == "" {
		return "", errors.New("official Codex releases contain no stable version")
	}
	return best, nil
}

func stableCodexReleaseVersion(release codexGitHubRelease) string {
	if release.Draft || release.Prerelease {
		return ""
	}
	version := strings.TrimSpace(release.TagName)
	version = strings.TrimPrefix(version, "rust-v")
	version = strings.TrimPrefix(version, "v")
	version = NormalizeCodexClientVersion(version)
	if version == "" || strings.Contains(version, "-") {
		return ""
	}
	return version
}

func requestCodexGitHubReleaseJSON(ctx context.Context, endpoint string, target any) error {
	client, err := NewProxyHttpClient("")
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "new-api-codex-version-sync")
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("GitHub releases returned status %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, codexReleaseBodyLimit+1))
	if err != nil {
		return err
	}
	if len(body) > codexReleaseBodyLimit {
		return errors.New("GitHub releases response exceeds size limit")
	}
	return common.Unmarshal(body, target)
}
