package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/shirou/gopsutil/cpu"
	gopsutildisk "github.com/shirou/gopsutil/disk"
	"github.com/shirou/gopsutil/host"
	"github.com/shirou/gopsutil/load"
	"github.com/shirou/gopsutil/mem"
	gopsutilnet "github.com/shirou/gopsutil/net"
	"gorm.io/gorm"
)

var (
	ErrRoutingNodeMonitorUnauthorized  = errors.New("routing node monitor unauthorized")
	ErrInvalidRoutingNodeMonitorReport = errors.New("invalid routing node monitor report")

	routingNodeMonitorReporterOnce sync.Once
	routingNodeMonitorHTTPClient   = &http.Client{Timeout: 10 * time.Second}
	routingNodeMonitorNetworkState struct {
		sync.Mutex
		sampledAt     time.Time
		bytesSent     uint64
		bytesReceived uint64
		source        string
	}
)

type RoutingNodeMonitorReport struct {
	NodeName                        string  `json:"node_name"`
	CPUUsage                        float64 `json:"cpu_usage"`
	CPUCores                        int     `json:"cpu_cores"`
	LoadOne                         float64 `json:"load_one"`
	LoadPercent                     float64 `json:"load_percent"`
	MemoryUsed                      uint64  `json:"memory_used"`
	MemoryTotal                     uint64  `json:"memory_total"`
	MemoryPercent                   float64 `json:"memory_percent"`
	DiskUsed                        uint64  `json:"disk_used"`
	DiskTotal                       uint64  `json:"disk_total"`
	DiskPercent                     float64 `json:"disk_percent"`
	NetworkBytesSent                uint64  `json:"network_bytes_sent"`
	NetworkBytesReceived            uint64  `json:"network_bytes_received"`
	NetworkUploadBps                float64 `json:"network_upload_bps"`
	NetworkDownloadBps              float64 `json:"network_download_bps"`
	DatabaseMetricsEnabled          bool    `json:"database_metrics_enabled"`
	PostgreSQLStatus                string  `json:"postgresql_status"`
	PostgreSQLConnections           int     `json:"postgresql_connections"`
	PostgreSQLMaxConnections        int     `json:"postgresql_max_connections"`
	PostgreSQLDatabaseSize          uint64  `json:"postgresql_database_size"`
	PostgreSQLCacheHitPercent       float64 `json:"postgresql_cache_hit_percent"`
	PostgreSQLReplicationStatus     string  `json:"postgresql_replication_status"`
	PostgreSQLReplicationLagSeconds float64 `json:"postgresql_replication_lag_seconds"`
	RedisStatus                     string  `json:"redis_status"`
	RedisMemoryUsed                 uint64  `json:"redis_memory_used"`
	RedisMemoryMax                  uint64  `json:"redis_memory_max"`
	PgBouncerStatus                 string  `json:"pgbouncer_status"`
	BackupLastAt                    int64   `json:"backup_last_at"`
	BackupSize                      uint64  `json:"backup_size"`
	UptimeSeconds                   uint64  `json:"uptime_seconds"`
	AppVersion                      string  `json:"app_version"`
}

type RoutingNodeMonitorEnrollmentRequest struct {
	NodeKey string `json:"node_key"`
}

type RoutingNodeMonitorEnrollmentResult struct {
	Token      string `json:"token"`
	NodeKey    string `json:"node_key"`
	NodeName   string `json:"node_name"`
	ReportPath string `json:"report_path"`
}

type routingNodeMonitorReporterConfig struct {
	ReportURL       string
	Token           string
	EnrollmentToken string
	NodeKey         string
}

func hashRoutingNodeMonitorToken(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}

func generateRoutingNodeMonitorToken() (string, error) {
	randomBytes := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, randomBytes); err != nil {
		return "", errors.New("failed to generate routing node monitor token")
	}
	return base64.RawURLEncoding.EncodeToString(randomBytes), nil
}

func RotateRoutingNodeMonitorToken(nodeId int) (string, error) {
	if _, err := model.GetRoutingNodeById(nodeId); err != nil {
		return "", err
	}
	token, err := generateRoutingNodeMonitorToken()
	if err != nil {
		return "", err
	}
	if err := model.SaveRoutingNodeMonitorToken(nodeId, hashRoutingNodeMonitorToken(token)); err != nil {
		return "", err
	}
	return token, nil
}

func RotateRoutingNodeMonitorEnrollmentToken() (string, error) {
	token, err := generateRoutingNodeMonitorToken()
	if err != nil {
		return "", err
	}
	if err := model.SaveRoutingNodeMonitorEnrollmentToken(token, hashRoutingNodeMonitorToken(token)); err != nil {
		return "", err
	}
	return token, nil
}

func GetRoutingNodeMonitorEnrollmentToken() (string, error) {
	enrollment, err := model.GetRoutingNodeMonitorEnrollment()
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(enrollment.Token), nil
}

func EnrollRoutingNodeMonitor(enrollmentToken string, nodeKey string) (*RoutingNodeMonitorEnrollmentResult, error) {
	if strings.TrimSpace(enrollmentToken) == "" {
		return nil, ErrRoutingNodeMonitorUnauthorized
	}
	enrollment, err := model.GetRoutingNodeMonitorEnrollment()
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrRoutingNodeMonitorUnauthorized
		}
		return nil, err
	}
	actualHash := hashRoutingNodeMonitorToken(enrollmentToken)
	if subtle.ConstantTimeCompare([]byte(actualHash), []byte(enrollment.TokenHash)) != 1 {
		return nil, ErrRoutingNodeMonitorUnauthorized
	}
	normalizedKey, err := model.NormalizeRoutingNodeKey(nodeKey)
	if err != nil {
		return nil, ErrRoutingNodeMonitorUnauthorized
	}
	node, err := model.GetRoutingNodeByKey(normalizedKey, true)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrRoutingNodeMonitorUnauthorized
		}
		return nil, err
	}
	if !node.MonitorEnabled {
		return nil, ErrRoutingNodeMonitorUnauthorized
	}
	token, err := RotateRoutingNodeMonitorToken(node.Id)
	if err != nil {
		return nil, err
	}
	return &RoutingNodeMonitorEnrollmentResult{
		Token:      token,
		NodeKey:    node.Key,
		NodeName:   node.Name,
		ReportPath: "/api/node-monitor/report",
	}, nil
}

func SubmitRoutingNodeMonitorReport(token string, report *RoutingNodeMonitorReport) error {
	if strings.TrimSpace(token) == "" {
		return ErrRoutingNodeMonitorUnauthorized
	}
	if err := validateRoutingNodeMonitorReport(report); err != nil {
		return err
	}
	node, err := model.GetRoutingNodeByMonitorTokenHash(hashRoutingNodeMonitorToken(token))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrRoutingNodeMonitorUnauthorized
		}
		return err
	}
	now := common.GetTimestamp()
	status := &model.RoutingNodeMonitorStatus{
		NodeId:                          node.Id,
		NodeName:                        strings.TrimSpace(report.NodeName),
		CPUUsage:                        report.CPUUsage,
		CPUCores:                        report.CPUCores,
		LoadOne:                         report.LoadOne,
		LoadPercent:                     report.LoadPercent,
		MemoryUsed:                      report.MemoryUsed,
		MemoryTotal:                     report.MemoryTotal,
		MemoryPercent:                   report.MemoryPercent,
		DiskUsed:                        report.DiskUsed,
		DiskTotal:                       report.DiskTotal,
		DiskPercent:                     report.DiskPercent,
		NetworkBytesSent:                report.NetworkBytesSent,
		NetworkBytesReceived:            report.NetworkBytesReceived,
		NetworkUploadBps:                report.NetworkUploadBps,
		NetworkDownloadBps:              report.NetworkDownloadBps,
		DatabaseMetricsEnabled:          report.DatabaseMetricsEnabled,
		PostgreSQLStatus:                report.PostgreSQLStatus,
		PostgreSQLConnections:           report.PostgreSQLConnections,
		PostgreSQLMaxConnections:        report.PostgreSQLMaxConnections,
		PostgreSQLDatabaseSize:          report.PostgreSQLDatabaseSize,
		PostgreSQLCacheHitPercent:       report.PostgreSQLCacheHitPercent,
		PostgreSQLReplicationStatus:     report.PostgreSQLReplicationStatus,
		PostgreSQLReplicationLagSeconds: report.PostgreSQLReplicationLagSeconds,
		RedisStatus:                     report.RedisStatus,
		RedisMemoryUsed:                 report.RedisMemoryUsed,
		RedisMemoryMax:                  report.RedisMemoryMax,
		PgBouncerStatus:                 report.PgBouncerStatus,
		BackupLastAt:                    report.BackupLastAt,
		BackupSize:                      report.BackupSize,
		UptimeSeconds:                   report.UptimeSeconds,
		AppVersion:                      strings.TrimSpace(report.AppVersion),
		ReportedAt:                      now,
		UpdatedAt:                       now,
	}
	if err := model.SaveRoutingNodeMonitorStatus(status); err != nil {
		return err
	}
	if common.IsMasterNode {
		if err := evaluateRoutingNodeMonitorAlerts(node, status); err != nil {
			common.SysLog("routing node alert evaluation failed: " + err.Error())
		}
	}
	return nil
}

func validateRoutingNodeMonitorReport(report *RoutingNodeMonitorReport) error {
	if report == nil || report.CPUCores < 1 || report.CPUCores > 4096 || report.MemoryTotal == 0 || report.DiskTotal == 0 {
		return ErrInvalidRoutingNodeMonitorReport
	}
	if report.MemoryUsed > report.MemoryTotal || report.DiskUsed > report.DiskTotal {
		return ErrInvalidRoutingNodeMonitorReport
	}
	if len(strings.TrimSpace(report.NodeName)) > 128 || len(strings.TrimSpace(report.AppVersion)) > 64 {
		return ErrInvalidRoutingNodeMonitorReport
	}
	if report.PostgreSQLConnections < 0 || report.PostgreSQLMaxConnections < 0 || report.PostgreSQLConnections > 1_000_000 || report.PostgreSQLMaxConnections > 1_000_000 {
		return ErrInvalidRoutingNodeMonitorReport
	}
	for _, status := range []string{report.PostgreSQLStatus, report.RedisStatus, report.PgBouncerStatus} {
		if !validDatabaseServiceStatus(status) {
			return ErrInvalidRoutingNodeMonitorReport
		}
	}
	if !validDatabaseReplicationStatus(report.PostgreSQLReplicationStatus) || report.BackupLastAt < 0 {
		return ErrInvalidRoutingNodeMonitorReport
	}
	values := []struct {
		value float64
		max   float64
	}{
		{report.CPUUsage, 100},
		{report.LoadOne, 100000},
		{report.LoadPercent, 100000},
		{report.MemoryPercent, 100},
		{report.DiskPercent, 100},
		{report.NetworkUploadBps, 1e15},
		{report.NetworkDownloadBps, 1e15},
		{report.PostgreSQLCacheHitPercent, 100},
		{report.PostgreSQLReplicationLagSeconds, 31_536_000},
	}
	for _, item := range values {
		if math.IsNaN(item.value) || math.IsInf(item.value, 0) || item.value < 0 || item.value > item.max {
			return ErrInvalidRoutingNodeMonitorReport
		}
	}
	return nil
}

func StartRoutingNodeMonitorReporter() {
	routingNodeMonitorReporterOnce.Do(func() {
		if !common.GetEnvOrDefaultBool("NODE_MONITOR_ENABLED", false) {
			return
		}
		config, interval, err := loadRoutingNodeMonitorReporterConfig()
		if err != nil {
			common.SysLog("routing node monitor reporter is enabled but not configured")
			return
		}
		go runRoutingNodeMonitorReporter(config, interval)
	})
}

func RunRoutingNodeMonitorAgent() error {
	if !common.GetEnvOrDefaultBool("NODE_MONITOR_ENABLED", false) {
		return errors.New("routing node monitor agent is disabled")
	}
	config, interval, err := loadRoutingNodeMonitorReporterConfig()
	if err != nil {
		return err
	}
	common.SysLog("routing node monitor agent started for " + config.NodeKey)
	runRoutingNodeMonitorReporter(config, interval)
	return nil
}

func loadRoutingNodeMonitorReporterConfig() (routingNodeMonitorReporterConfig, time.Duration, error) {
	config := routingNodeMonitorReporterConfig{
		ReportURL:       strings.TrimSpace(os.Getenv("NODE_MONITOR_REPORT_URL")),
		Token:           strings.TrimSpace(os.Getenv("NODE_MONITOR_TOKEN")),
		EnrollmentToken: strings.TrimSpace(os.Getenv("NODE_MONITOR_ENROLLMENT_TOKEN")),
		NodeKey: resolveRoutingNodeMonitorNodeKey(
			os.Getenv("NODE_MONITOR_NODE_KEY"),
			common.NodeName,
		),
	}
	if err := validateRoutingNodeMonitorReportURL(config.ReportURL); err != nil {
		return routingNodeMonitorReporterConfig{}, 0, err
	}
	if config.Token == "" && (config.EnrollmentToken == "" || config.NodeKey == "") {
		return routingNodeMonitorReporterConfig{}, 0, errors.New("routing node monitor token or enrollment configuration is required")
	}
	intervalSeconds := 15
	if raw := strings.TrimSpace(os.Getenv("NODE_MONITOR_INTERVAL_SECONDS")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			intervalSeconds = parsed
		}
	}
	if intervalSeconds < 5 {
		intervalSeconds = 5
	}
	if intervalSeconds > 300 {
		intervalSeconds = 300
	}
	return config, time.Duration(intervalSeconds) * time.Second, nil
}

func resolveRoutingNodeMonitorNodeKey(explicitKey string, nodeName string) string {
	if key := strings.TrimSpace(explicitKey); key != "" {
		return key
	}
	nodeName = strings.TrimSpace(nodeName)
	lowerNodeName := strings.ToLower(nodeName)
	const productionMarker = "-prod-"
	if markerIndex := strings.LastIndex(lowerNodeName, productionMarker); markerIndex >= 0 {
		suffix := lowerNodeName[markerIndex+len(productionMarker):]
		if number, err := strconv.Atoi(suffix); err == nil && number > 0 {
			return fmt.Sprintf("s%d", number)
		}
	}
	return nodeName
}

func validateRoutingNodeMonitorReportURL(rawURL string) error {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.User != nil {
		return errors.New("invalid routing node monitor report URL")
	}
	if parsed.Scheme == "http" && parsed.Hostname() != "localhost" && parsed.Hostname() != "127.0.0.1" && parsed.Hostname() != "::1" {
		return errors.New("routing node monitor report URL must use HTTPS")
	}
	return nil
}

func runRoutingNodeMonitorReporter(config routingNodeMonitorReporterConfig, interval time.Duration) {
	var previousError string
	activeToken := config.Token
	report := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if activeToken == "" {
			token, err := requestRoutingNodeMonitorEnrollment(ctx, config.ReportURL, config.EnrollmentToken, config.NodeKey)
			if err != nil {
				if err.Error() != previousError {
					common.SysLog("routing node monitor enrollment failed: " + err.Error())
					previousError = err.Error()
				}
				return
			}
			activeToken = token
		}
		err := sendRoutingNodeMonitorReport(ctx, config.ReportURL, activeToken)
		if errors.Is(err, ErrRoutingNodeMonitorUnauthorized) && config.EnrollmentToken != "" {
			activeToken = ""
			token, enrollmentErr := requestRoutingNodeMonitorEnrollment(ctx, config.ReportURL, config.EnrollmentToken, config.NodeKey)
			if enrollmentErr == nil {
				activeToken = token
				err = sendRoutingNodeMonitorReport(ctx, config.ReportURL, activeToken)
			} else {
				err = enrollmentErr
			}
		}
		if err != nil {
			if err.Error() != previousError {
				common.SysLog("routing node monitor report failed: " + err.Error())
				previousError = err.Error()
			}
			return
		}
		if previousError != "" {
			common.SysLog("routing node monitor reporting recovered")
			previousError = ""
		}
	}

	time.Sleep(3 * time.Second)
	report()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		report()
	}
}

func routingNodeMonitorEnrollmentURL(reportURL string) (string, error) {
	parsed, err := url.Parse(reportURL)
	if err != nil || !strings.HasSuffix(parsed.Path, "/report") {
		return "", errors.New("invalid routing node monitor report URL")
	}
	parsed.Path = strings.TrimSuffix(parsed.Path, "/report") + "/enroll"
	return parsed.String(), nil
}

func requestRoutingNodeMonitorEnrollment(ctx context.Context, reportURL string, enrollmentToken string, nodeKey string) (string, error) {
	enrollURL, err := routingNodeMonitorEnrollmentURL(reportURL)
	if err != nil {
		return "", err
	}
	body, err := common.Marshal(&RoutingNodeMonitorEnrollmentRequest{NodeKey: nodeKey})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, enrollURL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+enrollmentToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := routingNodeMonitorHTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	responseBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if readErr != nil {
		return "", readErr
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return "", ErrRoutingNodeMonitorUnauthorized
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("monitor enrollment endpoint returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	var result struct {
		Success bool                                `json:"success"`
		Message string                              `json:"message"`
		Data    *RoutingNodeMonitorEnrollmentResult `json:"data"`
	}
	if err := common.Unmarshal(responseBody, &result); err != nil || !result.Success || result.Data == nil || strings.TrimSpace(result.Data.Token) == "" {
		return "", errors.New("monitor enrollment endpoint returned an invalid response")
	}
	return strings.TrimSpace(result.Data.Token), nil
}

func sendRoutingNodeMonitorReport(ctx context.Context, reportURL string, token string) error {
	report, err := collectRoutingNodeMonitorReport()
	if err != nil {
		return err
	}
	body, err := common.Marshal(report)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reportURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := routingNodeMonitorHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	responseBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if readErr != nil {
		return readErr
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		if resp.StatusCode == http.StatusUnauthorized {
			return ErrRoutingNodeMonitorUnauthorized
		}
		return fmt.Errorf("monitor endpoint returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	var result struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	if err := common.Unmarshal(responseBody, &result); err != nil {
		return errors.New("monitor endpoint returned an invalid response")
	}
	if !result.Success {
		return fmt.Errorf("monitor endpoint rejected report: %s", result.Message)
	}
	return nil
}

func collectRoutingNodeMonitorReport() (*RoutingNodeMonitorReport, error) {
	percentages, err := cpu.Percent(500*time.Millisecond, false)
	if err != nil || len(percentages) == 0 {
		return nil, errors.New("failed to collect CPU usage")
	}
	cores, err := cpu.Counts(true)
	if err != nil || cores <= 0 {
		return nil, errors.New("failed to collect CPU core count")
	}
	memory, err := mem.VirtualMemory()
	if err != nil || memory.Total == 0 {
		return nil, errors.New("failed to collect memory usage")
	}
	diskTotal, diskUsed, diskPercent, err := collectRoutingNodeDiskMetrics()
	if err != nil || diskTotal == 0 {
		return nil, errors.New("failed to collect disk usage")
	}
	bytesSent, bytesReceived, uploadBps, downloadBps := collectRoutingNodeNetworkMetrics()
	uptime, err := host.Uptime()
	if err != nil {
		uptime = 0
	}
	loadOne := 0.0
	if average, loadErr := load.Avg(); loadErr == nil && average != nil {
		loadOne = math.Max(0, average.Load1)
	}
	nodeName := strings.TrimSpace(common.NodeName)
	if nodeName == "" {
		nodeName, _ = os.Hostname()
	}
	report := &RoutingNodeMonitorReport{
		NodeName:             nodeName,
		CPUUsage:             percentages[0],
		CPUCores:             cores,
		LoadOne:              loadOne,
		LoadPercent:          loadOne / float64(cores) * 100,
		MemoryUsed:           memory.Used,
		MemoryTotal:          memory.Total,
		MemoryPercent:        memory.UsedPercent,
		DiskUsed:             diskUsed,
		DiskTotal:            diskTotal,
		DiskPercent:          diskPercent,
		NetworkBytesSent:     bytesSent,
		NetworkBytesReceived: bytesReceived,
		NetworkUploadBps:     uploadBps,
		NetworkDownloadBps:   downloadBps,
		UptimeSeconds:        uptime,
		AppVersion:           common.Version,
	}
	collectRoutingNodeDatabaseMetrics(report)
	return report, nil
}

func collectRoutingNodeDiskMetrics() (uint64, uint64, float64, error) {
	path := strings.TrimSpace(os.Getenv("NODE_MONITOR_DISK_PATH"))
	if path == "" {
		info := common.GetDiskSpaceInfo()
		if info.Total == 0 {
			return 0, 0, 0, errors.New("disk usage is unavailable")
		}
		return info.Total, info.Used, info.UsedPercent, nil
	}
	usage, err := gopsutildisk.Usage(path)
	if err != nil || usage == nil || usage.Total == 0 {
		return 0, 0, 0, errors.New("disk usage is unavailable")
	}
	return usage.Total, usage.Used, usage.UsedPercent, nil
}

func collectRoutingNodeNetworkMetrics() (uint64, uint64, float64, float64) {
	bytesSent, bytesReceived, source := collectRoutingNodeNetworkCounters()
	if source == "" {
		return 0, 0, 0, 0
	}
	now := time.Now()

	routingNodeMonitorNetworkState.Lock()
	defer routingNodeMonitorNetworkState.Unlock()
	uploadBps := 0.0
	downloadBps := 0.0
	// A changed interface set (or a newly selected interface) has a different
	// counter origin. Do not compare it with the old aggregate counter value.
	if routingNodeMonitorNetworkState.source == source && !routingNodeMonitorNetworkState.sampledAt.IsZero() {
		elapsed := now.Sub(routingNodeMonitorNetworkState.sampledAt).Seconds()
		if elapsed > 0 && bytesSent >= routingNodeMonitorNetworkState.bytesSent {
			uploadBps = float64(bytesSent-routingNodeMonitorNetworkState.bytesSent) / elapsed
		}
		if elapsed > 0 && bytesReceived >= routingNodeMonitorNetworkState.bytesReceived {
			downloadBps = float64(bytesReceived-routingNodeMonitorNetworkState.bytesReceived) / elapsed
		}
	}
	routingNodeMonitorNetworkState.sampledAt = now
	routingNodeMonitorNetworkState.bytesSent = bytesSent
	routingNodeMonitorNetworkState.bytesReceived = bytesReceived
	routingNodeMonitorNetworkState.source = source
	return bytesSent, bytesReceived, uploadBps, downloadBps
}

// collectRoutingNodeNetworkCounters returns counters for public-facing
// interfaces only. IOCounters(false) aggregates every interface, so using it
// double-counts traffic that crosses loopback, Docker bridges, and veth pairs.
//
// NODE_MONITOR_NETWORK_INTERFACE can be set when a host has more than one
// physical interface. With no override, known virtual interfaces are ignored
// and the remaining physical-looking interfaces are summed.
func collectRoutingNodeNetworkCounters() (uint64, uint64, string) {
	counters, err := gopsutilnet.IOCounters(true)
	if err != nil || len(counters) == 0 {
		return 0, 0, ""
	}

	requested := strings.TrimSpace(os.Getenv("NODE_MONITOR_NETWORK_INTERFACE"))
	if requested != "" {
		for _, counter := range counters {
			if counter.Name == requested {
				return counter.BytesSent, counter.BytesRecv, "interface:" + counter.Name
			}
		}
		return 0, 0, ""
	}

	var bytesSent uint64
	var bytesReceived uint64
	selected := make([]string, 0, len(counters))
	for _, counter := range counters {
		if isRoutingNodeVirtualNetworkInterface(counter.Name) {
			continue
		}
		bytesSent += counter.BytesSent
		bytesReceived += counter.BytesRecv
		selected = append(selected, counter.Name)
	}
	if len(selected) == 0 {
		return 0, 0, ""
	}
	sort.Strings(selected)
	return bytesSent, bytesReceived, "auto:" + strings.Join(selected, ",")
}

func isRoutingNodeVirtualNetworkInterface(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" || name == "lo" {
		return true
	}
	for _, prefix := range []string{
		"br-", "docker", "veth", "virbr", "cni", "flannel", "cali",
		"tun", "tap", "wg", "tailscale",
	} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}
