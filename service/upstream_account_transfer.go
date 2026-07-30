package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

const (
	upstreamAccountExportVersion = 1
	upstreamAccountExportType    = "sub2api-data"
	upstreamLegacyExportType     = "starnexus-upstream-accounts"
	upstreamLegacySub2ExportType = "sub2api-bundle"
	crsSourceSystem              = "crs"
	maxCRSResponseBytes          = 5 << 20
)

type UpstreamProxyExportItem struct {
	ProxyKey        string `json:"proxy_key"`
	Name            string `json:"name"`
	Protocol        string `json:"protocol"`
	Host            string `json:"host"`
	Port            int    `json:"port"`
	Username        string `json:"username,omitempty"`
	Password        string `json:"password,omitempty"`
	Status          string `json:"status"`
	ExpiresAt       *int64 `json:"expires_at,omitempty"`
	FallbackMode    string `json:"fallback_mode,omitempty"`
	BackupProxyName string `json:"backup_proxy_name,omitempty"`
	ExpiryWarnDays  int    `json:"expiry_warn_days,omitempty"`
}

type UpstreamAccountExportItem struct {
	Name               string         `json:"name"`
	Notes              *string        `json:"notes,omitempty"`
	Platform           string         `json:"platform"`
	Type               string         `json:"type"`
	Credentials        map[string]any `json:"credentials"`
	Extra              any            `json:"extra,omitempty"`
	ProxyKey           *string        `json:"proxy_key,omitempty"`
	ProxyId            *int           `json:"proxy_id,omitempty"`
	Concurrency        int            `json:"concurrency,omitempty"`
	Priority           int            `json:"priority,omitempty"`
	Weight             int            `json:"weight,omitempty"`
	LoadFactor         *int           `json:"load_factor,omitempty"`
	RateMultiplier     *float64       `json:"rate_multiplier,omitempty"`
	Status             string         `json:"status,omitempty"`
	Schedulable        *bool          `json:"schedulable,omitempty"`
	ExpiresAt          *int64         `json:"expires_at,omitempty"`
	AutoPauseOnExpired *bool          `json:"auto_pause_on_expired,omitempty"`
	OAuthRefreshOwner  string         `json:"oauth_refresh_owner,omitempty"`
	PoolIds            []int          `json:"pool_ids,omitempty"`
}

type UpstreamAccountExport struct {
	Type       string                      `json:"type"`
	Version    int                         `json:"version"`
	ExportedAt string                      `json:"exported_at"`
	Proxies    []UpstreamProxyExportItem   `json:"proxies"`
	Accounts   []UpstreamAccountExportItem `json:"accounts"`
}

type UpstreamDataImportError struct {
	Kind     string `json:"kind"`
	Name     string `json:"name,omitempty"`
	ProxyKey string `json:"proxy_key,omitempty"`
	Message  string `json:"message"`
}

type UpstreamDataImportResult struct {
	ProxyCreated   int                       `json:"proxy_created"`
	ProxyReused    int                       `json:"proxy_reused"`
	ProxyFailed    int                       `json:"proxy_failed"`
	AccountCreated int                       `json:"account_created"`
	AccountFailed  int                       `json:"account_failed"`
	Errors         []UpstreamDataImportError `json:"errors,omitempty"`
}

func ExportUpstreamAccounts(ids []int) (*UpstreamAccountExport, error) {
	query := model.DB.Order("id ASC")
	if normalized := normalizePositiveIds(ids); len(ids) > 0 {
		if len(normalized) == 0 {
			return nil, errors.New("no valid upstream account ids")
		}
		query = query.Where("id IN ?", normalized)
	}
	var accounts []model.UpstreamAccount
	if err := query.Find(&accounts).Error; err != nil {
		return nil, err
	}
	var allProxies []model.UpstreamProxy
	if err := model.DB.Order("id ASC").Find(&allProxies).Error; err != nil {
		return nil, err
	}
	proxyById := make(map[int]model.UpstreamProxy, len(allProxies))
	for _, proxy := range allProxies {
		proxyById[proxy.Id] = proxy
	}
	selectedProxyIds := map[int]struct{}{}
	if len(ids) == 0 {
		for _, proxy := range allProxies {
			selectedProxyIds[proxy.Id] = struct{}{}
		}
	} else {
		for _, account := range accounts {
			if account.ProxyId != nil {
				selectedProxyIds[*account.ProxyId] = struct{}{}
			}
		}
		for changed := true; changed; {
			changed = false
			for id := range selectedProxyIds {
				proxy, ok := proxyById[id]
				if ok && proxy.BackupProxyId != nil {
					if _, exists := selectedProxyIds[*proxy.BackupProxyId]; !exists {
						selectedProxyIds[*proxy.BackupProxyId] = struct{}{}
						changed = true
					}
				}
			}
		}
	}
	result := &UpstreamAccountExport{
		Type: upstreamAccountExportType, Version: upstreamAccountExportVersion,
		ExportedAt: time.Now().UTC().Format(time.RFC3339),
		Proxies:    make([]UpstreamProxyExportItem, 0, len(selectedProxyIds)),
		Accounts:   make([]UpstreamAccountExportItem, 0, len(accounts)),
	}
	proxyKeyById := make(map[int]string, len(selectedProxyIds))
	proxyNameById := make(map[int]string, len(selectedProxyIds))
	for _, proxy := range allProxies {
		if _, ok := selectedProxyIds[proxy.Id]; !ok {
			continue
		}
		auth, err := DecryptUpstreamProxyAuth(&proxy)
		if err != nil {
			return nil, fmt.Errorf("read upstream proxy %d auth: %w", proxy.Id, err)
		}
		key := buildUpstreamDataProxyKey(proxy.Protocol, proxy.Host, proxy.Port, auth.Username, auth.Password)
		proxyKeyById[proxy.Id] = key
		proxyNameById[proxy.Id] = proxy.Name
	}
	for _, proxy := range allProxies {
		key, ok := proxyKeyById[proxy.Id]
		if !ok {
			continue
		}
		auth, err := DecryptUpstreamProxyAuth(&proxy)
		if err != nil {
			return nil, fmt.Errorf("read upstream proxy %d auth: %w", proxy.Id, err)
		}
		backupName := ""
		if proxy.BackupProxyId != nil {
			backupName = proxyNameById[*proxy.BackupProxyId]
		}
		result.Proxies = append(result.Proxies, UpstreamProxyExportItem{
			ProxyKey: key, Name: proxy.Name, Protocol: proxy.Protocol, Host: proxy.Host, Port: proxy.Port,
			Username: auth.Username, Password: auth.Password, Status: proxy.Status, ExpiresAt: proxy.ExpiresAt,
			FallbackMode: proxy.FallbackMode, BackupProxyName: backupName, ExpiryWarnDays: proxy.ExpiryWarnDays,
		})
	}
	for index := range accounts {
		account := &accounts[index]
		credentials, err := DecryptUpstreamAccountCredentials(account)
		if err != nil {
			return nil, fmt.Errorf("decrypt upstream account %d: %w", account.Id, err)
		}
		poolIds, err := listUpstreamAccountPoolIds(model.DB, account.Id)
		if err != nil {
			return nil, err
		}
		var extra map[string]any
		if err := common.UnmarshalJsonStr(account.Extra, &extra); err != nil {
			return nil, fmt.Errorf("parse upstream account %d extra: %w", account.Id, err)
		}
		normalizeSub2CredentialBackedOptions(credentials, extra)
		var proxyKey *string
		if account.ProxyId != nil {
			if key, ok := proxyKeyById[*account.ProxyId]; ok {
				proxyKey = &key
			}
		}
		schedulable := account.Schedulable
		autoPause := account.AutoPauseOnExpired
		result.Accounts = append(result.Accounts, UpstreamAccountExportItem{
			Name: account.Name, Notes: account.Notes, Platform: account.Platform, Type: account.Type,
			Credentials: credentials, Extra: extra, ProxyKey: proxyKey, ProxyId: account.ProxyId,
			Concurrency: account.Concurrency, Priority: account.Priority, Weight: account.Weight,
			LoadFactor: account.LoadFactor, RateMultiplier: account.RateMultiplier,
			Status: account.Status, Schedulable: &schedulable, ExpiresAt: account.ExpiresAt,
			AutoPauseOnExpired: &autoPause, OAuthRefreshOwner: account.OAuthRefreshOwner,
			PoolIds: poolIds,
		})
	}
	return result, nil
}

func ImportUpstreamData(payload UpstreamAccountExport) (*UpstreamDataImportResult, error) {
	if payload.Type != "" && payload.Type != upstreamAccountExportType && payload.Type != upstreamLegacySub2ExportType && payload.Type != upstreamLegacyExportType {
		return nil, fmt.Errorf("unsupported data type: %s", payload.Type)
	}
	if payload.Version != 0 && payload.Version != upstreamAccountExportVersion {
		return nil, fmt.Errorf("unsupported data version: %d", payload.Version)
	}
	if len(payload.Accounts) > 100 || len(payload.Proxies) > 100 {
		return nil, errors.New("import supports at most 100 accounts and 100 proxies")
	}
	result := &UpstreamDataImportResult{Errors: []UpstreamDataImportError{}}

	var existingProxies []model.UpstreamProxy
	if err := model.DB.Order("id ASC").Find(&existingProxies).Error; err != nil {
		return nil, err
	}
	proxyKeyToId := make(map[string]int, len(existingProxies)+len(payload.Proxies))
	proxyNameToId := make(map[string]int, len(existingProxies)+len(payload.Proxies))
	for index := range existingProxies {
		proxy := &existingProxies[index]
		auth, err := DecryptUpstreamProxyAuth(proxy)
		if err != nil {
			return nil, err
		}
		proxyKeyToId[buildUpstreamDataProxyKey(proxy.Protocol, proxy.Host, proxy.Port, auth.Username, auth.Password)] = proxy.Id
		proxyNameToId[proxy.Name] = proxy.Id
	}

	type importedProxy struct {
		id      int
		item    UpstreamProxyExportItem
		created bool
	}
	importedProxies := make([]importedProxy, 0, len(payload.Proxies))
	for _, item := range payload.Proxies {
		key := strings.TrimSpace(item.ProxyKey)
		if key == "" {
			key = buildUpstreamDataProxyKey(item.Protocol, item.Host, item.Port, item.Username, item.Password)
		}
		if id, ok := proxyKeyToId[key]; ok {
			result.ProxyReused++
			proxyNameToId[item.Name] = id
			importedProxies = append(importedProxies, importedProxy{id: id, item: item})
			continue
		}
		name := strings.TrimSpace(item.Name)
		if name == "" {
			name = "imported-proxy"
		}
		status := normalizeUpstreamDataProxyStatus(item.Status)
		proxy := model.UpstreamProxy{
			Name: name, Protocol: item.Protocol, Host: item.Host, Port: item.Port, Status: status,
			ExpiresAt: item.ExpiresAt, FallbackMode: constant.UpstreamProxyFallbackNone,
			ExpiryWarnDays: item.ExpiryWarnDays,
		}
		input := &UpstreamProxyCreateInput{Proxy: proxy, Auth: UpstreamProxyAuthInput{Username: item.Username, Password: item.Password}}
		if err := CreateUpstreamProxy(input); err != nil {
			result.ProxyFailed++
			result.Errors = append(result.Errors, UpstreamDataImportError{Kind: "proxy", Name: item.Name, ProxyKey: key, Message: err.Error()})
			continue
		}
		proxyKeyToId[key] = input.Proxy.Id
		proxyNameToId[name] = input.Proxy.Id
		result.ProxyCreated++
		importedProxies = append(importedProxies, importedProxy{id: input.Proxy.Id, item: item, created: true})
	}
	for _, imported := range importedProxies {
		if !imported.created || imported.item.FallbackMode == "" || imported.item.FallbackMode == constant.UpstreamProxyFallbackNone {
			continue
		}
		view, err := GetUpstreamProxy(imported.id)
		if err != nil {
			continue
		}
		proxy := view.UpstreamProxy
		proxy.FallbackMode = imported.item.FallbackMode
		if proxy.FallbackMode == constant.UpstreamProxyFallbackProxy {
			backupId, ok := proxyNameToId[imported.item.BackupProxyName]
			if !ok {
				result.Errors = append(result.Errors, UpstreamDataImportError{Kind: "proxy", Name: imported.item.Name, Message: "backup_proxy_name not found; fallback disabled"})
				proxy.FallbackMode = constant.UpstreamProxyFallbackNone
			} else {
				proxy.BackupProxyId = &backupId
			}
		}
		if err := UpdateUpstreamProxy(&UpstreamProxyUpdateInput{Proxy: proxy}); err != nil {
			result.Errors = append(result.Errors, UpstreamDataImportError{Kind: "proxy", Name: imported.item.Name, Message: err.Error()})
		}
	}

	for _, item := range payload.Accounts {
		credentials := copyAnyMap(item.Credentials)
		platform := strings.ToLower(strings.TrimSpace(item.Platform))
		accountType := strings.ToLower(strings.TrimSpace(item.Type))
		if platform == constant.UpstreamPlatformOpenAI && accountType == constant.UpstreamAccountTypeOAuth && upstreamDataCredentialString(credentials, "account_id") == "" {
			if legacyId := upstreamDataCredentialString(credentials, "chatgpt_account_id"); legacyId != "" {
				credentials["account_id"] = credentials["chatgpt_account_id"]
			}
		}
		extra, err := normalizeUpstreamDataExtra(item.Extra)
		if err != nil {
			result.AccountFailed++
			result.Errors = append(result.Errors, UpstreamDataImportError{Kind: "account", Name: item.Name, Message: err.Error()})
			continue
		}
		var extraObject map[string]any
		if err := common.UnmarshalJsonStr(extra, &extraObject); err != nil {
			result.AccountFailed++
			result.Errors = append(result.Errors, UpstreamDataImportError{Kind: "account", Name: item.Name, Message: err.Error()})
			continue
		}
		normalizeSub2CredentialBackedOptions(credentials, extraObject)
		extraRaw, err := common.Marshal(extraObject)
		if err != nil {
			result.AccountFailed++
			result.Errors = append(result.Errors, UpstreamDataImportError{Kind: "account", Name: item.Name, Message: err.Error()})
			continue
		}
		extra = string(extraRaw)
		proxyId := item.ProxyId
		if item.ProxyKey != nil && strings.TrimSpace(*item.ProxyKey) != "" {
			mappedId, ok := proxyKeyToId[strings.TrimSpace(*item.ProxyKey)]
			if !ok {
				result.AccountFailed++
				result.Errors = append(result.Errors, UpstreamDataImportError{Kind: "account", Name: item.Name, ProxyKey: *item.ProxyKey, Message: "proxy_key not found"})
				continue
			}
			proxyId = &mappedId
		}
		concurrency := item.Concurrency
		if concurrency <= 0 {
			concurrency = 1
		}
		weight := item.Weight
		if weight <= 0 {
			weight = 1
		}
		status := strings.TrimSpace(item.Status)
		if status == "" {
			status = constant.UpstreamStatusActive
		}
		schedulable := true
		if item.Schedulable != nil {
			schedulable = *item.Schedulable
		}
		autoPause := true
		if item.AutoPauseOnExpired != nil {
			autoPause = *item.AutoPauseOnExpired
		}
		refreshOwner := strings.TrimSpace(item.OAuthRefreshOwner)
		if refreshOwner == "" {
			refreshOwner = constant.UpstreamOAuthRefreshOwnerExternal
			if accountType == constant.UpstreamAccountTypeOAuth || accountType == constant.UpstreamAccountTypeSetupToken {
				refreshOwner = constant.UpstreamOAuthRefreshOwnerStarNexus
			}
		}
		account := model.UpstreamAccount{
			Name: item.Name, Notes: item.Notes, Platform: platform, Type: accountType, Extra: extra,
			ProxyId: proxyId, Concurrency: concurrency, Priority: item.Priority, Weight: weight,
			LoadFactor: item.LoadFactor, RateMultiplier: item.RateMultiplier, Status: status,
			Schedulable: schedulable, ExpiresAt: item.ExpiresAt, AutoPauseOnExpired: autoPause,
			OAuthRefreshOwner: refreshOwner,
		}
		input := &UpstreamAccountCreateInput{Account: account, Credentials: credentials}
		if err := CreateUpstreamAccount(input); err != nil {
			result.AccountFailed++
			result.Errors = append(result.Errors, UpstreamDataImportError{Kind: "account", Name: item.Name, Message: err.Error()})
			continue
		}
		result.AccountCreated++
	}
	return result, nil
}

func buildUpstreamDataProxyKey(protocol, host string, port int, username, password string) string {
	return fmt.Sprintf("%s|%s|%d|%s|%s", strings.TrimSpace(protocol), strings.TrimSpace(host), port, strings.TrimSpace(username), strings.TrimSpace(password))
}

func upstreamDataCredentialString(credentials map[string]any, key string) string {
	value, ok := credentials[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%v", value))
}

func normalizeUpstreamDataProxyStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "", constant.UpstreamStatusActive:
		return constant.UpstreamStatusActive
	case constant.UpstreamStatusExpired:
		return constant.UpstreamStatusInactive
	default:
		return strings.ToLower(strings.TrimSpace(status))
	}
}

func normalizeUpstreamDataExtra(value any) (string, error) {
	if value == nil {
		return "{}", nil
	}
	if raw, ok := value.(string); ok {
		var object map[string]any
		if err := common.UnmarshalJsonStr(raw, &object); err != nil {
			return "", errors.New("account extra must be a JSON object")
		}
		return raw, nil
	}
	raw, err := common.Marshal(value)
	if err != nil {
		return "", errors.New("account extra must be a JSON object")
	}
	var object map[string]any
	if err := common.Unmarshal(raw, &object); err != nil {
		return "", errors.New("account extra must be a JSON object")
	}
	return string(raw), nil
}

func normalizeSub2CredentialBackedOptions(credentials map[string]any, extra map[string]any) {
	if credentials == nil || extra == nil {
		return
	}
	for _, key := range []string{"intercept_warmup_requests", "compact_model_mapping", "openai_capabilities"} {
		if _, exists := credentials[key]; !exists {
			if value, ok := extra[key]; ok {
				credentials[key] = value
			}
		}
		delete(extra, key)
	}
	if mode := strings.ToLower(strings.TrimSpace(upstreamDataCredentialString(credentials, "auth_mode"))); mode == "api_key" {
		credentials["auth_mode"] = "apikey"
	}
}

type CRSSyncInput struct {
	BaseURL            string   `json:"base_url"`
	Username           string   `json:"username"`
	Password           string   `json:"password"`
	SyncProxies        bool     `json:"sync_proxies"`
	SelectedAccountIDs []string `json:"selected_account_ids"`
}

type CRSPreviewAccount struct {
	CRSAccountID string `json:"crs_account_id"`
	Kind         string `json:"kind"`
	Name         string `json:"name"`
	Platform     string `json:"platform"`
	Type         string `json:"type"`
}

type CRSPreviewResult struct {
	NewAccounts      []CRSPreviewAccount `json:"new_accounts"`
	ExistingAccounts []CRSPreviewAccount `json:"existing_accounts"`
	Skipped          int                 `json:"skipped"`
}

type CRSSyncItemResult struct {
	CRSAccountID string `json:"crs_account_id"`
	Kind         string `json:"kind"`
	Name         string `json:"name"`
	Action       string `json:"action"`
	Error        string `json:"error,omitempty"`
}

type CRSSyncResult struct {
	Created int                 `json:"created"`
	Updated int                 `json:"updated"`
	Skipped int                 `json:"skipped"`
	Failed  int                 `json:"failed"`
	Items   []CRSSyncItemResult `json:"items"`
}

type crsLoginResponse struct {
	Success bool   `json:"success"`
	Token   string `json:"token"`
	Message string `json:"message"`
	Error   string `json:"error"`
}

type crsProxy struct {
	Protocol string `json:"protocol"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type crsAccount struct {
	Kind               string         `json:"kind"`
	ID                 string         `json:"id"`
	Name               string         `json:"name"`
	Description        string         `json:"description"`
	AuthType           string         `json:"authType"`
	IsActive           bool           `json:"isActive"`
	Schedulable        bool           `json:"schedulable"`
	Priority           int            `json:"priority"`
	Status             string         `json:"status"`
	MaxConcurrentTasks int            `json:"maxConcurrentTasks"`
	Proxy              *crsProxy      `json:"proxy"`
	Credentials        map[string]any `json:"credentials"`
	Extra              map[string]any `json:"extra"`
	targetPlatform     string
	targetType         string
}

type crsExportResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error"`
	Message string `json:"message"`
	Data    struct {
		ClaudeAccounts          []crsAccount `json:"claudeAccounts"`
		ClaudeConsoleAccounts   []crsAccount `json:"claudeConsoleAccounts"`
		OpenAIOAuthAccounts     []crsAccount `json:"openaiOAuthAccounts"`
		OpenAIResponsesAccounts []crsAccount `json:"openaiResponsesAccounts"`
		GeminiOAuthAccounts     []crsAccount `json:"geminiOAuthAccounts"`
		GeminiAPIKeyAccounts    []crsAccount `json:"geminiApiKeyAccounts"`
	} `json:"data"`
}

func PreviewUpstreamAccountsFromCRS(ctx context.Context, input CRSSyncInput) (*CRSPreviewResult, error) {
	sources, skipped, err := fetchSupportedCRSAccounts(ctx, input)
	if err != nil {
		return nil, err
	}
	existing, err := listCRSSourceIDs()
	if err != nil {
		return nil, err
	}
	result := &CRSPreviewResult{
		NewAccounts: []CRSPreviewAccount{}, ExistingAccounts: []CRSPreviewAccount{}, Skipped: skipped,
	}
	for _, source := range sources {
		item := CRSPreviewAccount{
			CRSAccountID: source.ID, Kind: source.Kind, Name: crsAccountName(source),
			Platform: source.targetPlatform, Type: source.targetType,
		}
		if _, ok := existing[crsSourceID(source.ID)]; ok {
			result.ExistingAccounts = append(result.ExistingAccounts, item)
		} else {
			result.NewAccounts = append(result.NewAccounts, item)
		}
	}
	return result, nil
}

func SyncUpstreamAccountsFromCRS(ctx context.Context, input CRSSyncInput) (*CRSSyncResult, error) {
	sources, unsupported, err := fetchSupportedCRSAccounts(ctx, input)
	if err != nil {
		return nil, err
	}
	var selected map[string]struct{}
	if input.SelectedAccountIDs != nil {
		selected = make(map[string]struct{}, len(input.SelectedAccountIDs))
		for _, id := range input.SelectedAccountIDs {
			selected[id] = struct{}{}
		}
	}
	result := &CRSSyncResult{Skipped: unsupported, Items: []CRSSyncItemResult{}}
	for _, source := range sources {
		item := CRSSyncItemResult{CRSAccountID: source.ID, Kind: source.Kind, Name: crsAccountName(source)}
		existing, findErr := findCRSAccount(source.ID)
		if findErr != nil {
			item.Action, item.Error = "failed", findErr.Error()
			result.Failed++
			result.Items = append(result.Items, item)
			continue
		}
		if existing == nil {
			if _, ok := selected[source.ID]; selected != nil && !ok {
				item.Action, item.Error = "skipped", "not selected"
				result.Skipped++
				result.Items = append(result.Items, item)
				continue
			}
		}
		if err := syncCRSAccount(source, existing, input.SyncProxies); err != nil {
			item.Action, item.Error = "failed", err.Error()
			result.Failed++
		} else if existing == nil {
			item.Action = "created"
			result.Created++
		} else {
			item.Action = "updated"
			result.Updated++
		}
		result.Items = append(result.Items, item)
	}
	return result, nil
}

func fetchSupportedCRSAccounts(ctx context.Context, input CRSSyncInput) ([]crsAccount, int, error) {
	exported, err := fetchCRSExport(ctx, input.BaseURL, input.Username, input.Password)
	if err != nil {
		return nil, 0, err
	}
	sources := make([]crsAccount, 0,
		len(exported.Data.ClaudeAccounts)+len(exported.Data.ClaudeConsoleAccounts)+
			len(exported.Data.OpenAIOAuthAccounts)+len(exported.Data.OpenAIResponsesAccounts))
	unsupported := len(exported.Data.GeminiOAuthAccounts) + len(exported.Data.GeminiAPIKeyAccounts)
	appendSources := func(items []crsAccount, platform, accountType string) {
		for _, item := range items {
			item.targetPlatform = platform
			item.targetType = accountType
			if platform == constant.UpstreamPlatformAnthropic && accountType == constant.UpstreamAccountTypeOAuth {
				authType := strings.TrimSpace(item.AuthType)
				if authType == constant.UpstreamAccountTypeSetupToken {
					item.targetType = constant.UpstreamAccountTypeSetupToken
				} else if authType != "" && authType != constant.UpstreamAccountTypeOAuth {
					unsupported++
					continue
				}
			}
			if strings.TrimSpace(item.ID) == "" {
				unsupported++
				continue
			}
			sources = append(sources, item)
		}
	}
	appendSources(exported.Data.ClaudeAccounts, constant.UpstreamPlatformAnthropic, constant.UpstreamAccountTypeOAuth)
	appendSources(exported.Data.ClaudeConsoleAccounts, constant.UpstreamPlatformAnthropic, constant.UpstreamAccountTypeAPIKey)
	appendSources(exported.Data.OpenAIOAuthAccounts, constant.UpstreamPlatformOpenAI, constant.UpstreamAccountTypeOAuth)
	appendSources(exported.Data.OpenAIResponsesAccounts, constant.UpstreamPlatformOpenAI, constant.UpstreamAccountTypeAPIKey)
	return sources, unsupported, nil
}

func fetchCRSExport(ctx context.Context, rawBaseURL, username, password string) (*crsExportResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	baseURL, err := normalizeCRSBaseURL(rawBaseURL)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(username) == "" || strings.TrimSpace(password) == "" {
		return nil, errors.New("CRS username and password are required")
	}
	client := GetSSRFProtectedHTTPClient()
	if client == nil {
		return nil, errors.New("HTTP client is not initialized")
	}
	loginBody, err := common.Marshal(map[string]any{"username": username, "password": password})
	if err != nil {
		return nil, err
	}
	loginRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/web/auth/login", bytes.NewReader(loginBody))
	if err != nil {
		return nil, err
	}
	loginRequest.Header.Set("Content-Type", "application/json")
	loginRaw, status, err := doLimitedCRSRequest(client, loginRequest, 1<<20)
	if err != nil {
		return nil, err
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("CRS login failed with status %d", status)
	}
	var login crsLoginResponse
	if err := common.Unmarshal(loginRaw, &login); err != nil {
		return nil, fmt.Errorf("parse CRS login response: %w", err)
	}
	if !login.Success || strings.TrimSpace(login.Token) == "" {
		message := strings.TrimSpace(login.Message)
		if message == "" {
			message = strings.TrimSpace(login.Error)
		}
		if message == "" {
			message = "unknown error"
		}
		return nil, errors.New("CRS login failed: " + message)
	}
	exportRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/admin/sync/export-accounts?include_secrets=true", nil)
	if err != nil {
		return nil, err
	}
	exportRequest.Header.Set("Authorization", "Bearer "+login.Token)
	exportRaw, status, err := doLimitedCRSRequest(client, exportRequest, maxCRSResponseBytes)
	if err != nil {
		return nil, err
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("CRS export failed with status %d", status)
	}
	var exported crsExportResponse
	if err := common.Unmarshal(exportRaw, &exported); err != nil {
		return nil, fmt.Errorf("parse CRS export response: %w", err)
	}
	if !exported.Success {
		message := strings.TrimSpace(exported.Message)
		if message == "" {
			message = strings.TrimSpace(exported.Error)
		}
		return nil, errors.New("CRS export failed: " + message)
	}
	return &exported, nil
}

func normalizeCRSBaseURL(raw string) (string, error) {
	value := strings.TrimRight(strings.TrimSpace(raw), "/")
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", errors.New("invalid CRS service URL")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return "", errors.New("CRS service URL must not contain a path, query, or fragment")
	}
	if err := ValidateSSRFProtectedFetchURL(value); err != nil {
		return "", fmt.Errorf("CRS service URL is not allowed: %w", err)
	}
	return value, nil
}

func doLimitedCRSRequest(client *http.Client, request *http.Request, limit int64) ([]byte, int, error) {
	response, err := client.Do(request)
	if err != nil {
		return nil, 0, err
	}
	defer response.Body.Close()
	reader := io.LimitReader(response.Body, limit+1)
	raw, err := io.ReadAll(reader)
	if err != nil {
		return nil, response.StatusCode, err
	}
	if int64(len(raw)) > limit {
		return nil, response.StatusCode, errors.New("CRS response is too large")
	}
	return raw, response.StatusCode, nil
}

func syncCRSAccount(source crsAccount, existing *model.UpstreamAccount, syncProxy bool) error {
	credentials := copyAnyMap(source.Credentials)
	if existing != nil {
		current, err := DecryptUpstreamAccountCredentials(existing)
		if err != nil {
			return err
		}
		credentials = mergeAnyMap(current, credentials)
	}
	if err := validateUpstreamCredentialPayload(source.targetPlatform, source.targetType, credentials); err != nil {
		return err
	}
	extra := map[string]any{}
	if existing != nil && strings.TrimSpace(existing.Extra) != "" {
		_ = common.UnmarshalJsonStr(existing.Extra, &extra)
	}
	extra = mergeAnyMap(extra, source.Extra)
	normalizeSub2CredentialBackedOptions(credentials, extra)
	extra["crs_account_id"] = source.ID
	extra["crs_kind"] = source.Kind
	extra["crs_synced_at"] = time.Now().UTC().Format(time.RFC3339)
	extraRaw, err := common.Marshal(extra)
	if err != nil {
		return err
	}
	proxyId := (*int)(nil)
	if existing != nil {
		proxyId = existing.ProxyId
	}
	if syncProxy && source.Proxy != nil {
		proxyId, err = syncCRSProxy(source.Proxy, crsAccountName(source))
		if err != nil {
			return err
		}
	}
	concurrency := 3
	if source.MaxConcurrentTasks > 0 {
		concurrency = source.MaxConcurrentTasks
	}
	priority := source.Priority
	if priority < 0 {
		priority = 0
	}
	name := crsAccountName(source)
	notes := strings.TrimSpace(source.Description)
	status := constant.UpstreamStatusInactive
	if source.IsActive && strings.ToLower(strings.TrimSpace(source.Status)) != constant.UpstreamStatusError {
		status = constant.UpstreamStatusActive
	}
	sourceSystem, sourceId := crsSourceSystem, crsSourceID(source.ID)
	refreshOwner := constant.UpstreamOAuthRefreshOwnerExternal
	if source.targetType == constant.UpstreamAccountTypeOAuth || source.targetType == constant.UpstreamAccountTypeSetupToken {
		refreshOwner = constant.UpstreamOAuthRefreshOwnerStarNexus
	}
	if existing == nil {
		account := model.UpstreamAccount{
			Name: name, Platform: source.targetPlatform, Type: source.targetType, Extra: string(extraRaw),
			ProxyId: proxyId, Concurrency: concurrency, Priority: priority, Weight: 1,
			Status: status, Schedulable: source.Schedulable, AutoPauseOnExpired: true,
			OAuthRefreshOwner: refreshOwner,
			SourceSystem:      &sourceSystem, SourceId: &sourceId,
		}
		if notes != "" {
			account.Notes = &notes
		}
		return CreateUpstreamAccount(&UpstreamAccountCreateInput{Account: account, Credentials: credentials})
	}
	existing.Name = name
	existing.Platform = source.targetPlatform
	existing.Type = source.targetType
	existing.Extra = string(extraRaw)
	existing.ProxyId = proxyId
	existing.Concurrency = concurrency
	existing.Priority = priority
	existing.Status = status
	existing.Schedulable = source.Schedulable
	existing.OAuthRefreshOwner = refreshOwner
	if notes != "" {
		existing.Notes = &notes
	}
	return UpdateUpstreamAccount(&UpstreamAccountUpdateInput{Account: *existing, Credentials: &credentials})
}

func syncCRSProxy(source *crsProxy, accountName string) (*int, error) {
	if source == nil || strings.TrimSpace(source.Host) == "" || source.Port <= 0 {
		return nil, nil
	}
	protocol := strings.ToLower(strings.TrimSpace(source.Protocol))
	if protocol == "" {
		protocol = constant.UpstreamProxyProtocolHTTP
	}
	var existing model.UpstreamProxy
	err := model.DB.Where("protocol = ? AND host = ? AND port = ?", protocol, strings.TrimSpace(source.Host), source.Port).First(&existing).Error
	if err == nil {
		return &existing.Id, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	proxy := model.UpstreamProxy{
		Name: "CRS " + accountName, Protocol: protocol, Host: strings.TrimSpace(source.Host), Port: source.Port,
		Status: constant.UpstreamStatusActive, FallbackMode: constant.UpstreamProxyFallbackNone, ExpiryWarnDays: 7,
	}
	input := &UpstreamProxyCreateInput{Proxy: proxy, Auth: UpstreamProxyAuthInput{Username: source.Username, Password: source.Password}}
	if err := CreateUpstreamProxy(input); err != nil {
		return nil, err
	}
	return &input.Proxy.Id, nil
}

func listCRSSourceIDs() (map[int64]struct{}, error) {
	var accounts []model.UpstreamAccount
	if err := model.DB.Select("source_id").Where("source_system = ?", crsSourceSystem).Find(&accounts).Error; err != nil {
		return nil, err
	}
	result := make(map[int64]struct{}, len(accounts))
	for _, account := range accounts {
		if account.SourceId != nil {
			result[*account.SourceId] = struct{}{}
		}
	}
	return result, nil
}

func findCRSAccount(id string) (*model.UpstreamAccount, error) {
	var account model.UpstreamAccount
	err := model.DB.Where("source_system = ? AND source_id = ?", crsSourceSystem, crsSourceID(id)).First(&account).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &account, err
}

func crsSourceID(id string) int64 {
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(id))
	return int64(hash.Sum64() & 0x7fffffffffffffff)
}

func crsAccountName(source crsAccount) string {
	if name := strings.TrimSpace(source.Name); name != "" {
		return name
	}
	return "CRS " + source.ID
}

func copyAnyMap(source map[string]any) map[string]any {
	return mergeAnyMap(nil, source)
}

func mergeAnyMap(base, updates map[string]any) map[string]any {
	result := make(map[string]any, len(base)+len(updates))
	for key, value := range base {
		result[key] = value
	}
	for key, value := range updates {
		result[key] = value
	}
	return result
}
