package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/bytedance/gopkg/util/gopool"
)

const (
	videoResultCacheDefaultMaxBytes      = int64(1024 * 1024 * 1024)
	videoResultCacheDefaultRetentionDays = 7
	videoResultCacheDownloadTimeout      = 15 * time.Minute
	videoResultCacheCleanupInterval      = 24 * time.Hour
	videoResultCacheRepairInterval       = 5 * time.Minute
	videoResultCacheRepairBatchSize      = 100
	videoResultCacheRepairMaxTasks       = 1000
	videoResultCachePrefix               = "zqbapi-"
)

var (
	videoResultArchiveInFlight sync.Map
	videoResultArchiveSlots    = make(chan struct{}, 4)
)

func videoResultCacheRoot() (string, bool, error) {
	configured := strings.TrimSpace(os.Getenv("VIDEO_RESULT_CACHE_DIR"))
	if configured == "" {
		return "", false, nil
	}
	root, err := filepath.Abs(configured)
	if err != nil {
		return "", false, fmt.Errorf("resolve VIDEO_RESULT_CACHE_DIR: %w", err)
	}
	if err := os.MkdirAll(root, 0o750); err != nil {
		return "", false, fmt.Errorf("create VIDEO_RESULT_CACHE_DIR: %w", err)
	}
	return filepath.Clean(root), true, nil
}

func videoResultCacheMaxBytes() int64 {
	value, err := strconv.ParseInt(strings.TrimSpace(os.Getenv("VIDEO_RESULT_CACHE_MAX_BYTES")), 10, 64)
	if err != nil || value < 1024*1024 || value > 10*1024*1024*1024 {
		return videoResultCacheDefaultMaxBytes
	}
	return value
}

func videoResultCacheRetention() time.Duration {
	days, err := strconv.Atoi(strings.TrimSpace(os.Getenv("VIDEO_RESULT_CACHE_RETENTION_DAYS")))
	if err != nil || days < 1 || days > 3650 {
		days = videoResultCacheDefaultRetentionDays
	}
	return time.Duration(days) * 24 * time.Hour
}

func archiveZQBAPIVideoResultAsync(task *model.Task) {
	if task == nil || strings.TrimSpace(task.PrivateData.UpstreamResultURL) == "" {
		return
	}
	if _, enabled, err := videoResultCacheRoot(); err != nil {
		logger.LogWarn(context.Background(), fmt.Sprintf("ZQBAPI result cache disabled by invalid configuration: %v", err))
		return
	} else if !enabled {
		return
	}
	if task.ResultFile != "" {
		if file, _, openErr := OpenVideoResultCache(task.ResultFile); openErr == nil {
			_ = file.Close()
			return
		}
	}
	inFlightKey := task.TaskID
	if inFlightKey == "" {
		inFlightKey = strconv.FormatInt(task.ID, 10)
	}
	if _, loaded := videoResultArchiveInFlight.LoadOrStore(inFlightKey, struct{}{}); loaded {
		return
	}
	taskCopy := *task
	gopool.Go(func() {
		defer videoResultArchiveInFlight.Delete(inFlightKey)
		select {
		case videoResultArchiveSlots <- struct{}{}:
			defer func() { <-videoResultArchiveSlots }()
		default:
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), videoResultCacheDownloadTimeout)
		defer cancel()
		channel, err := model.CacheGetChannel(taskCopy.ChannelId)
		if err != nil || channel.Type != constant.ChannelTypeZQBAPI {
			return
		}
		resultFile, err := persistVideoResult(ctx, taskCopy.TaskID, taskCopy.PrivateData.UpstreamResultURL, channel.GetSetting().Proxy)
		if err != nil {
			logger.LogWarn(ctx, fmt.Sprintf("archive ZQBAPI video result failed for task %s: %v", taskCopy.TaskID, err))
			return
		}
		if err := model.UpdateTaskResultFile(taskCopy.ID, resultFile); err != nil {
			logger.LogError(ctx, fmt.Sprintf("store ZQBAPI video result cache reference failed for task %s: %v", taskCopy.TaskID, err))
			return
		}
		logger.LogInfo(ctx, fmt.Sprintf("archived ZQBAPI video result for task %s", taskCopy.TaskID))
	})
}

func persistVideoResult(ctx context.Context, taskID, sourceURL, proxy string) (string, error) {
	root, enabled, err := videoResultCacheRoot()
	if err != nil || !enabled {
		if err != nil {
			return "", err
		}
		return "", fmt.Errorf("video result cache is disabled")
	}
	if strings.TrimSpace(taskID) == "" {
		return "", fmt.Errorf("task ID is empty")
	}
	if err := validateVideoResultURL(sourceURL, proxy); err != nil {
		return "", err
	}
	client := GetSSRFProtectedHTTPClient()
	if proxy != "" {
		client, err = GetHttpClientWithProxy(proxy)
		if err != nil {
			return "", fmt.Errorf("create result cache proxy client: %w", err)
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return "", fmt.Errorf("create result cache request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download video result: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download video result returned HTTP %d", resp.StatusCode)
	}
	maxBytes := videoResultCacheMaxBytes()
	if resp.ContentLength > maxBytes {
		return "", fmt.Errorf("video result is larger than configured cache limit")
	}
	extension := videoResultExtension(resp.Header.Get("Content-Type"), sourceURL)
	digest := sha256.Sum256([]byte(taskID))
	fileName := videoResultCachePrefix + hex.EncodeToString(digest[:]) + extension
	targetPath := filepath.Join(root, fileName)
	if info, statErr := os.Stat(targetPath); statErr == nil && info.Mode().IsRegular() && info.Size() > 0 {
		return fileName, nil
	}
	temporary, err := os.CreateTemp(root, videoResultCachePrefix+"*.tmp")
	if err != nil {
		return "", fmt.Errorf("create temporary video result: %w", err)
	}
	temporaryPath := temporary.Name()
	keepTemporary := false
	defer func() {
		_ = temporary.Close()
		if !keepTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()
	written, err := io.Copy(temporary, io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return "", fmt.Errorf("write video result: %w", err)
	}
	if written > maxBytes {
		return "", fmt.Errorf("video result is larger than configured cache limit")
	}
	if err := temporary.Sync(); err != nil {
		return "", fmt.Errorf("sync video result: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return "", fmt.Errorf("close video result: %w", err)
	}
	if err := os.Rename(temporaryPath, targetPath); err != nil {
		if info, statErr := os.Stat(targetPath); statErr == nil && info.Mode().IsRegular() && info.Size() > 0 {
			return fileName, nil
		}
		return "", fmt.Errorf("publish video result: %w", err)
	}
	keepTemporary = true
	return fileName, nil
}

func validateVideoResultURL(sourceURL, proxy string) error {
	if proxy == "" {
		return ValidateSSRFProtectedFetchURL(sourceURL)
	}
	fetchSetting := system_setting.GetFetchSetting()
	return common.ValidateURLWithFetchSetting(sourceURL, fetchSetting.EnableSSRFProtection, fetchSetting.AllowPrivateIp, fetchSetting.DomainFilterMode, fetchSetting.IpFilterMode, fetchSetting.DomainList, fetchSetting.IpList, fetchSetting.AllowedPorts, fetchSetting.ApplyIPFilterForDomain)
}

func videoResultExtension(contentType, sourceURL string) string {
	mediaType, _, _ := mime.ParseMediaType(contentType)
	switch mediaType {
	case "video/webm":
		return ".webm"
	case "video/quicktime":
		return ".mov"
	case "video/x-matroska":
		return ".mkv"
	case "video/mp4":
		return ".mp4"
	}
	parsed, err := url.Parse(sourceURL)
	if err == nil {
		switch strings.ToLower(filepath.Ext(parsed.Path)) {
		case ".mp4", ".webm", ".mov", ".mkv":
			return strings.ToLower(filepath.Ext(parsed.Path))
		}
	}
	return ".mp4"
}

func OpenVideoResultCache(fileName string) (*os.File, os.FileInfo, error) {
	root, enabled, err := videoResultCacheRoot()
	if err != nil || !enabled {
		if err != nil {
			return nil, nil, err
		}
		return nil, nil, os.ErrNotExist
	}
	if fileName == "" || filepath.Base(fileName) != fileName || !strings.HasPrefix(fileName, videoResultCachePrefix) {
		return nil, nil, os.ErrNotExist
	}
	file, err := os.Open(filepath.Join(root, fileName))
	if err != nil {
		return nil, nil, err
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 {
		_ = file.Close()
		if err != nil {
			return nil, nil, err
		}
		return nil, nil, os.ErrNotExist
	}
	return file, info, nil
}

// DeleteVideoResultCache removes one validated generated cache file. Invalid
// names and already-missing files are treated as no-ops.
func DeleteVideoResultCache(fileName string) error {
	file, _, err := OpenVideoResultCache(fileName)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	path := file.Name()
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func StartZQBAPIHousekeepingTask() {
	gopool.Go(func() {
		runZQBAPIHousekeeping(context.Background())
		ticker := time.NewTicker(videoResultCacheCleanupInterval)
		defer ticker.Stop()
		for range ticker.C {
			runZQBAPIHousekeeping(context.Background())
		}
	})
	gopool.Go(func() {
		repairZQBAPIVideoResultCache(context.Background())
		ticker := time.NewTicker(videoResultCacheRepairInterval)
		defer ticker.Stop()
		for range ticker.C {
			repairZQBAPIVideoResultCache(context.Background())
		}
	})
}

// repairZQBAPIVideoResultCache lets every application node build the same
// deterministic local copy. ResultFile may already be populated by another
// node; archiveZQBAPIVideoResultAsync still verifies this node's filesystem.
func repairZQBAPIVideoResultCache(ctx context.Context) {
	if _, enabled, err := videoResultCacheRoot(); err != nil || !enabled {
		return
	}
	updatedAfter := time.Now().Add(-videoResultCacheRetention()).Unix()
	platform := constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeZQBAPI))
	beforeID := int64(0)
	scanned := 0
	for scanned < videoResultCacheRepairMaxTasks {
		tasks, err := model.ListRecentTaskResultsForArchive(platform, beforeID, updatedAfter, videoResultCacheRepairBatchSize)
		if err != nil {
			logger.LogWarn(ctx, fmt.Sprintf("scan video result archive candidates failed: %v", err))
			return
		}
		if len(tasks) == 0 {
			return
		}
		for _, task := range tasks {
			if task == nil {
				continue
			}
			beforeID = task.ID
			scanned++
			if task.Properties.OpenAIVideo && strings.TrimSpace(task.PrivateData.UpstreamResultURL) != "" {
				archiveZQBAPIVideoResultAsync(task)
			}
		}
		if len(tasks) < videoResultCacheRepairBatchSize {
			return
		}
	}
}

func runZQBAPIHousekeeping(ctx context.Context) {
	for {
		deleted, err := model.DeleteExpiredZQBAPIAssetRecords(time.Now().Unix(), 500)
		if err != nil {
			logger.LogWarn(ctx, fmt.Sprintf("cleanup expired ZQBAPI asset registry records failed: %v", err))
			break
		}
		if deleted < 500 {
			if deleted > 0 {
				logger.LogInfo(ctx, fmt.Sprintf("cleaned %d expired ZQBAPI asset registry records", deleted))
			}
			break
		}
	}
	for {
		deleted, err := model.DeleteExpiredDoubaoVideo2AssetRecords(time.Now().Unix(), 500)
		if err != nil {
			logger.LogWarn(ctx, fmt.Sprintf("cleanup expired DoubaoVideo2.0 asset registry records failed: %v", err))
			break
		}
		if deleted < 500 {
			if deleted > 0 {
				logger.LogInfo(ctx, fmt.Sprintf("cleaned %d expired DoubaoVideo2.0 asset registry records", deleted))
			}
			break
		}
	}
	root, enabled, err := videoResultCacheRoot()
	if err != nil || !enabled {
		return
	}
	cutoff := time.Now().Add(-videoResultCacheRetention())
	entries, err := os.ReadDir(root)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("scan ZQBAPI video result cache failed: %v", err))
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), videoResultCachePrefix) {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr == nil && info.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(root, entry.Name()))
		}
	}
}
