package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

const (
	upstreamAccountCredentialRecordKind = "account"
	upstreamProxyAuthRecordKind         = "proxy"
)

type UpstreamAccountListFilter struct {
	Platform    string
	Type        string
	Status      string
	Search      string
	PoolId      int
	ProxyId     int
	Schedulable *bool
	SortBy      string
	SortOrder   string
	Page        int
	PageSize    int
}

type UpstreamAccountView struct {
	model.UpstreamAccount
	CredentialConfigured bool                    `json:"credential_configured"`
	PoolIds              []int                   `json:"pool_ids"`
	CurrentConcurrency   int                     `json:"current_concurrency"`
	Metadata             UpstreamAccountMetadata `json:"metadata"`
}

type UpstreamAccountMetadata struct {
	Email                      string                        `json:"email,omitempty"`
	PlanType                   string                        `json:"plan_type,omitempty"`
	SubscriptionExpiresAt      string                        `json:"subscription_expires_at,omitempty"`
	PrivacyMode                string                        `json:"privacy_mode,omitempty"`
	CompactMode                string                        `json:"compact_mode,omitempty"`
	CompactSupported           bool                          `json:"compact_supported"`
	CredentialReadable         bool                          `json:"credential_readable"`
	CredentialReadError        string                        `json:"credential_read_error,omitempty"`
	BaseURL                    string                        `json:"base_url,omitempty"`
	ModelMapping               map[string]string             `json:"model_mapping,omitempty"`
	CompactModelMapping        map[string]string             `json:"compact_model_mapping,omitempty"`
	OpenAIEndpointCapabilities []string                      `json:"openai_capabilities,omitempty"`
	InterceptWarmupRequests    bool                          `json:"intercept_warmup_requests"`
	TempUnschedulableEnabled   bool                          `json:"temp_unschedulable_enabled"`
	TempUnschedulableRules     []model.TempUnschedulableRule `json:"temp_unschedulable_rules,omitempty"`
	HeaderOverrideEnabled      bool                          `json:"header_override_enabled"`
	HeaderOverrides            map[string]string             `json:"header_overrides,omitempty"`
	BedrockAuthMode            string                        `json:"bedrock_auth_mode,omitempty"`
	AWSRegion                  string                        `json:"aws_region,omitempty"`
	AWSAccessKeyID             string                        `json:"aws_access_key_id,omitempty"`
	VertexProjectID            string                        `json:"vertex_project_id,omitempty"`
	VertexClientEmail          string                        `json:"vertex_client_email,omitempty"`
	VertexLocation             string                        `json:"vertex_location,omitempty"`
}

type UpstreamAccountPoolView struct {
	model.UpstreamAccountPool
	AccountCount            int64  `json:"account_count"`
	ActiveCount             int64  `json:"active_count"`
	ReadyCount              int64  `json:"ready_count"`
	TemporarilyLimitedCount int64  `json:"temporarily_limited_count"`
	CurrentConcurrency      int    `json:"current_concurrency"`
	Concurrency             int    `json:"concurrency"`
	SchedulerAvailable      bool   `json:"scheduler_available"`
	ChannelCount            int64  `json:"channel_count"`
	PublishedChannelId      *int   `json:"published_channel_id"`
	PublishedChannelStatus  *int   `json:"published_channel_status"`
	PublishedModelCount     int    `json:"published_model_count"`
	AttemptCount24h         int64  `json:"attempt_count_24h"`
	SuccessCount24h         int64  `json:"success_count_24h"`
	ErrorCount24h           int64  `json:"error_count_24h"`
	LastEventAt             *int64 `json:"last_event_at"`
}

type UpstreamAccountPoolCapabilities struct {
	Models                      []string `json:"models"`
	AccountCount                int      `json:"account_count"`
	SchedulableAccountCount     int      `json:"schedulable_account_count"`
	UnreadableAccountCount      int      `json:"unreadable_account_count"`
	WildcardModelAccountCount   int      `json:"wildcard_model_account_count"`
	PassthroughAccountCount     int      `json:"passthrough_account_count"`
	HeaderOverrideAccountCount  int      `json:"header_override_account_count"`
	ProxyConfiguredAccountCount int      `json:"proxy_configured_account_count"`
	PublishedChannelId          *int     `json:"published_channel_id"`
	PublishedGroups             []string `json:"published_groups"`
	PublishedModels             []string `json:"published_models"`
}

type UpstreamAccountPoolPublishResult struct {
	ChannelId int      `json:"channel_id"`
	Created   bool     `json:"created"`
	Models    []string `json:"models"`
	Groups    []string `json:"groups"`
}

type UpstreamAccountPoolMemberInput struct {
	AccountId int `json:"account_id"`
	Priority  int `json:"priority"`
	Weight    int `json:"weight"`
}

type UpstreamAccountPoolMemberView struct {
	model.UpstreamAccountPoolMember
	Name        string `json:"name"`
	Platform    string `json:"platform"`
	Type        string `json:"type"`
	Status      string `json:"status"`
	Schedulable bool   `json:"schedulable"`
}

type upstreamAccountPoolCountRow struct {
	PoolId int
	Count  int64
}

type upstreamAccountPoolCapacityRow struct {
	PoolId      int
	AccountId   int
	Concurrency int
}

type upstreamAccountPoolEventCountRow struct {
	PoolId    int
	EventType string
	Count     int64
}

type upstreamAccountPoolLastEventRow struct {
	PoolId      int
	LastEventAt int64
}

type UpstreamProxyView struct {
	model.UpstreamProxy
	AuthConfigured bool   `json:"auth_configured"`
	AuthUsername   string `json:"username"`
	AuthPassword   string `json:"password,omitempty"`
	AccountCount   int64  `json:"account_count"`
	PoolCount      int64  `json:"pool_count"`
	BackupCount    int64  `json:"backup_count"`
}

type UpstreamAccountCreateInput struct {
	Account     model.UpstreamAccount
	Credentials map[string]any
	PoolIds     []int
}

type UpstreamAccountUpdateInput struct {
	Account         model.UpstreamAccount
	Credentials     *map[string]any
	CredentialPatch *map[string]any
	PoolIds         *[]int
}

type UpstreamAccountRecoveryScope string

const (
	UpstreamAccountRecoveryAll       UpstreamAccountRecoveryScope = "all"
	UpstreamAccountRecoveryRateLimit UpstreamAccountRecoveryScope = "rate_limit"
	UpstreamAccountRecoveryTemporary UpstreamAccountRecoveryScope = "temporary"
)

type UpstreamProxyAuthInput struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type UpstreamProxyCreateInput struct {
	Proxy model.UpstreamProxy
	Auth  UpstreamProxyAuthInput
}

type UpstreamProxyUpdateInput struct {
	Proxy model.UpstreamProxy
	Auth  *UpstreamProxyAuthInput
}

func ListUpstreamAccountPools() ([]UpstreamAccountPoolView, error) {
	var pools []model.UpstreamAccountPool
	if err := model.DB.Order("id ASC").Find(&pools).Error; err != nil {
		return nil, err
	}
	views := make([]UpstreamAccountPoolView, 0, len(pools))
	for _, pool := range pools {
		views = append(views, UpstreamAccountPoolView{UpstreamAccountPool: pool})
	}
	if err := populateUpstreamAccountPoolViews(views); err != nil {
		return nil, err
	}
	return views, nil
}

func GetUpstreamAccountPool(id int) (*UpstreamAccountPoolView, error) {
	if id <= 0 {
		return nil, errors.New("invalid upstream account pool id")
	}
	var pool model.UpstreamAccountPool
	if err := model.DB.First(&pool, id).Error; err != nil {
		return nil, err
	}
	views := []UpstreamAccountPoolView{{UpstreamAccountPool: pool}}
	if err := populateUpstreamAccountPoolViews(views); err != nil {
		return nil, err
	}
	return &views[0], nil
}

func GetUpstreamAccountPoolCapabilities(id int) (*UpstreamAccountPoolCapabilities, error) {
	return getUpstreamAccountPoolCapabilities(model.DB, id)
}

func getUpstreamAccountPoolCapabilities(db *gorm.DB, id int) (*UpstreamAccountPoolCapabilities, error) {
	if id <= 0 {
		return nil, errors.New("invalid upstream account pool id")
	}
	var pool model.UpstreamAccountPool
	if err := db.First(&pool, id).Error; err != nil {
		return nil, err
	}
	var accounts []model.UpstreamAccount
	if err := db.
		Where("id IN (?)", db.Model(&model.UpstreamAccountPoolMember{}).Select("account_id").Where("pool_id = ?", id)).
		Order("id ASC").
		Find(&accounts).Error; err != nil {
		return nil, err
	}

	capabilities := &UpstreamAccountPoolCapabilities{
		AccountCount:    len(accounts),
		Models:          []string{},
		PublishedGroups: []string{},
		PublishedModels: []string{},
	}
	models := make(map[string]struct{})
	now := common.GetTimestamp()
	for i := range accounts {
		account := &accounts[i]
		if !account.IsSchedulableAt(now) {
			continue
		}
		capabilities.SchedulableAccountCount++
		credentials, err := DecryptUpstreamAccountCredentials(account)
		if err != nil {
			capabilities.UnreadableAccountCount++
			continue
		}
		options, optionsErr := model.ParseUpstreamAccountOptionsWithCredentials(account.Extra, credentials)
		passthrough := optionsErr == nil && (options.OpenAIPassthrough || options.AnthropicPassthrough)
		if passthrough {
			capabilities.PassthroughAccountCount++
			capabilities.WildcardModelAccountCount++
		} else {
			mapping := upstreamAccountModelMapping(credentials)
			hasWildcard := false
			hasExplicitRules := len(mapping) > 0
			for rawModel := range mapping {
				addPublishedAccountModel(models, rawModel, &hasWildcard)
			}

			var accountCapabilities upstreamAccountCapabilities
			if strings.TrimSpace(account.Extra) != "" && strings.TrimSpace(account.Extra) != "{}" &&
				common.UnmarshalJsonStr(account.Extra, &accountCapabilities) == nil {
				hasExplicitRules = hasExplicitRules || len(accountCapabilities.Models) > 0 ||
					len(accountCapabilities.SupportedModels) > 0 || len(accountCapabilities.ModelPrefixes) > 0
				for _, rawModel := range accountCapabilities.Models {
					addPublishedAccountModel(models, rawModel, &hasWildcard)
				}
				for _, rawModel := range accountCapabilities.SupportedModels {
					addPublishedAccountModel(models, rawModel, &hasWildcard)
				}
				if len(accountCapabilities.ModelPrefixes) > 0 {
					hasWildcard = true
				}
			}
			if hasWildcard || !hasExplicitRules {
				capabilities.WildcardModelAccountCount++
			}
		}
		if enabled, _ := credentials["header_override_enabled"].(bool); enabled && len(upstreamCredentialStringMap(credentials["header_overrides"])) > 0 {
			capabilities.HeaderOverrideAccountCount++
		}
		if account.ProxyId != nil || pool.DefaultProxyId != nil {
			capabilities.ProxyConfiguredAccountCount++
		}
	}

	capabilities.Models = make([]string, 0, len(models))
	for modelName := range models {
		capabilities.Models = append(capabilities.Models, modelName)
	}
	sort.Strings(capabilities.Models)

	var published model.Channel
	err := db.Where("credential_source = ? AND upstream_account_pool_id = ?", constant.ChannelCredentialSourceAccountPool, id).
		Order("id ASC").First(&published).Error
	if err == nil {
		channelId := published.Id
		capabilities.PublishedChannelId = &channelId
		capabilities.PublishedGroups = published.GetGroups()
		capabilities.PublishedModels = published.GetModels()
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	return capabilities, nil
}

func addPublishedAccountModel(models map[string]struct{}, rawModel string, hasWildcard *bool) {
	modelName := strings.TrimSpace(rawModel)
	if modelName == "" {
		return
	}
	if strings.Contains(modelName, "*") {
		*hasWildcard = true
		return
	}
	models[modelName] = struct{}{}
}

func PublishUpstreamAccountPoolChannel(id int, groups []string, requestedModels ...[]string) (*UpstreamAccountPoolPublishResult, error) {
	normalizedGroups, err := normalizePublishedChannelGroups(groups)
	if err != nil {
		return nil, err
	}

	result := &UpstreamAccountPoolPublishResult{Groups: normalizedGroups}
	channelsChanged := false
	err = model.DB.Transaction(func(tx *gorm.DB) error {
		var pool model.UpstreamAccountPool
		if err := tx.First(&pool, id).Error; err != nil {
			return err
		}
		if pool.Status != constant.UpstreamStatusActive {
			return errors.New("only active account pools can be published")
		}

		capabilities, err := getUpstreamAccountPoolCapabilities(tx, id)
		if err != nil {
			return err
		}
		if capabilities.SchedulableAccountCount == 0 {
			return errors.New("account pool has no schedulable accounts")
		}
		if len(capabilities.Models) == 0 {
			return errors.New("account pool has no concrete account models to publish")
		}
		publishedModels := capabilities.Models
		if len(requestedModels) > 0 {
			publishedModels, err = normalizePublishedChannelModels(requestedModels[0], capabilities.Models)
			if err != nil {
				return err
			}
		}

		channel, created, err := upsertPublishedAccountPoolChannel(tx, pool, publishedModels, normalizedGroups)
		if err != nil {
			return err
		}
		result.ChannelId = channel.Id
		result.Created = created
		result.Models = append([]string(nil), publishedModels...)
		channelsChanged = true
		return nil
	})
	if err != nil {
		return nil, err
	}
	if channelsChanged {
		refreshPublishedAccountPoolChannelCaches()
	}
	return result, nil
}

func normalizePublishedChannelModels(models []string, availableModels []string) ([]string, error) {
	if len(models) == 0 {
		return nil, errors.New("at least one channel model is required")
	}
	available := make(map[string]struct{}, len(availableModels))
	for _, modelName := range availableModels {
		available[modelName] = struct{}{}
	}
	selected := make(map[string]struct{}, len(models))
	for _, rawModel := range models {
		modelName := strings.TrimSpace(rawModel)
		if modelName == "" {
			continue
		}
		if strings.Contains(modelName, ",") {
			return nil, errors.New("channel model names cannot contain commas")
		}
		if _, ok := available[modelName]; !ok {
			return nil, fmt.Errorf("model %q is not available from this account pool", modelName)
		}
		selected[modelName] = struct{}{}
	}
	if len(selected) == 0 {
		return nil, errors.New("at least one channel model is required")
	}
	normalized := make([]string, 0, len(selected))
	for _, modelName := range availableModels {
		if _, ok := selected[modelName]; ok {
			normalized = append(normalized, modelName)
		}
	}
	return normalized, nil
}

func intersectPublishedChannelModels(selectedModels []string, availableModels []string) []string {
	available := make(map[string]struct{}, len(availableModels))
	for _, modelName := range availableModels {
		available[modelName] = struct{}{}
	}
	selected := make(map[string]struct{}, len(selectedModels))
	for _, modelName := range selectedModels {
		selected[modelName] = struct{}{}
	}
	models := make([]string, 0, len(selected))
	for _, modelName := range availableModels {
		if _, ok := selected[modelName]; ok {
			models = append(models, modelName)
		}
	}
	return models
}

func UnpublishUpstreamAccountPoolChannel(id int) error {
	if id <= 0 {
		return errors.New("invalid upstream account pool id")
	}
	channelsChanged := false
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		var pool model.UpstreamAccountPool
		if err := tx.First(&pool, id).Error; err != nil {
			return err
		}
		var channels []model.Channel
		if err := tx.Where("credential_source = ? AND upstream_account_pool_id = ?", constant.ChannelCredentialSourceAccountPool, id).
			Order("id ASC").Find(&channels).Error; err != nil {
			return err
		}
		if len(channels) == 0 {
			return nil
		}
		channelIds := make([]int, 0, len(channels))
		for _, channel := range channels {
			channelIds = append(channelIds, channel.Id)
		}
		if err := tx.Where("channel_id IN ?", channelIds).Delete(&model.Ability{}).Error; err != nil {
			return err
		}
		if err := tx.Where("id IN ?", channelIds).Delete(&model.Channel{}).Error; err != nil {
			return err
		}
		channelsChanged = true
		return nil
	})
	if err == nil && channelsChanged {
		refreshPublishedAccountPoolChannelCaches()
	}
	return err
}

func normalizePublishedChannelGroups(groups []string) ([]string, error) {
	seen := make(map[string]struct{}, len(groups))
	normalized := make([]string, 0, len(groups))
	for _, rawGroup := range groups {
		group := strings.TrimSpace(rawGroup)
		if group == "" {
			continue
		}
		if strings.Contains(group, ",") {
			return nil, errors.New("channel group names cannot contain commas")
		}
		if _, exists := seen[group]; exists {
			continue
		}
		seen[group] = struct{}{}
		normalized = append(normalized, group)
	}
	if len(normalized) == 0 {
		return nil, errors.New("at least one channel group is required")
	}
	if len(strings.Join(normalized, ",")) > 64 {
		return nil, errors.New("published channel groups exceed the supported length")
	}
	return normalized, nil
}

func upsertPublishedAccountPoolChannel(tx *gorm.DB, pool model.UpstreamAccountPool, models []string, groups []string) (*model.Channel, bool, error) {
	modelList := strings.Join(models, ",")
	groupList := strings.Join(groups, ",")
	channelType := constant.ChannelTypeOpenAI
	if pool.Platform == constant.UpstreamPlatformAnthropic {
		channelType = constant.ChannelTypeAnthropic
	}

	var channels []model.Channel
	err := tx.Where("credential_source = ? AND upstream_account_pool_id = ?", constant.ChannelCredentialSourceAccountPool, pool.Id).
		Order("id ASC").Find(&channels).Error
	if err != nil {
		return nil, false, err
	}
	if len(channels) == 0 {
		autoBan := 0
		priority := int64(0)
		weight := uint(0)
		emptyJSON := "{}"
		channel := model.Channel{
			Type: channelType, Key: "", Status: common.ChannelStatusEnabled, Name: pool.Name,
			Weight: &weight, CreatedTime: common.GetTimestamp(), Models: modelList, Group: groupList,
			Priority: &priority, AutoBan: &autoBan, Setting: &emptyJSON, OtherSettings: "{}",
			CredentialSource: constant.ChannelCredentialSourceAccountPool, UpstreamAccountPoolId: &pool.Id,
		}
		if err := tx.Create(&channel).Error; err != nil {
			return nil, false, err
		}
		updates := publishedAccountPoolChannelUpdates(pool, channelType, modelList)
		updates["group"] = groupList
		if err := tx.Model(&model.Channel{}).Where("id = ?", channel.Id).Updates(updates).Error; err != nil {
			return nil, false, err
		}
		if err := tx.First(&channel, channel.Id).Error; err != nil {
			return nil, false, err
		}
		if err := channel.AddAbilities(tx); err != nil {
			return nil, false, err
		}
		return &channel, true, nil
	}
	if len(channels) > 1 {
		return nil, false, fmt.Errorf("account pool is referenced by %d local channels; unpublish it before publishing again", len(channels))
	}

	channel := channels[0]
	updates := publishedAccountPoolChannelUpdates(pool, channelType, modelList)
	updates["group"] = groupList
	if channel.Status == common.ChannelStatusAutoDisabled {
		updates["status"] = common.ChannelStatusEnabled
	}
	if err := tx.Model(&model.Channel{}).Where("id = ?", channel.Id).Updates(updates).Error; err != nil {
		return nil, false, err
	}
	if err := tx.First(&channel, channel.Id).Error; err != nil {
		return nil, false, err
	}
	if err := channel.UpdateAbilities(tx); err != nil {
		return nil, false, err
	}
	return &channel, false, nil
}

func publishedAccountPoolChannelUpdates(pool model.UpstreamAccountPool, channelType int, modelList string) map[string]any {
	return map[string]any{
		"type": channelType, "key": "", "name": pool.Name, "models": modelList,
		"base_url": nil, "open_ai_organization": nil, "model_mapping": nil,
		"header_override":   nil,
		"credential_source": constant.ChannelCredentialSourceAccountPool, "upstream_account_pool_id": pool.Id,
	}
}

func publishedAccountPoolChannelMetadataUpdates(pool model.UpstreamAccountPool, channelType int) map[string]any {
	return map[string]any{
		"type": channelType, "key": "", "name": pool.Name,
		"base_url": nil, "open_ai_organization": nil, "model_mapping": nil,
		"header_override":   nil,
		"credential_source": constant.ChannelCredentialSourceAccountPool, "upstream_account_pool_id": pool.Id,
	}
}

func syncPublishedAccountPoolChannelsTx(tx *gorm.DB, poolIds []int) (bool, error) {
	changed := false
	for _, poolId := range normalizePositiveIds(poolIds) {
		var channels []model.Channel
		if err := tx.Where("credential_source = ? AND upstream_account_pool_id = ?", constant.ChannelCredentialSourceAccountPool, poolId).
			Order("id ASC").Find(&channels).Error; err != nil {
			return false, err
		}
		if len(channels) == 0 {
			continue
		}

		var pool model.UpstreamAccountPool
		if err := tx.First(&pool, poolId).Error; err != nil {
			return false, err
		}
		channelType := constant.ChannelTypeOpenAI
		if pool.Platform == constant.UpstreamPlatformAnthropic {
			channelType = constant.ChannelTypeAnthropic
		}
		for i := range channels {
			channel := &channels[i]
			// Account availability and discovered capabilities are runtime state. They
			// must not overwrite the administrator's published model selection or the
			// local channel status. An unavailable pool is handled by the scheduler at
			// request time and recovers automatically when an account becomes usable.
			updates := publishedAccountPoolChannelMetadataUpdates(pool, channelType)
			if err := tx.Model(&model.Channel{}).Where("id = ?", channel.Id).Updates(updates).Error; err != nil {
				return false, err
			}
			if err := tx.First(channel, channel.Id).Error; err != nil {
				return false, err
			}
			if err := channel.UpdateAbilities(tx); err != nil {
				return false, err
			}
			changed = true
		}
	}
	return changed, nil
}

func refreshPublishedAccountPoolChannelCaches() {
	model.InitChannelCache()
	model.InvalidatePricingCache()
	ResetProxyClientCache()
}

func populateUpstreamAccountPoolViews(views []UpstreamAccountPoolView) error {
	if len(views) == 0 {
		return nil
	}
	router, routerErr := GetConfiguredUpstreamAccountRouter()
	schedulerAvailable := routerErr == nil && router != nil && router.leaseManager != nil
	poolIds := make([]int, 0, len(views))
	viewsById := make(map[int]*UpstreamAccountPoolView, len(views))
	for i := range views {
		if views[i].Id <= 0 {
			return errors.New("invalid upstream account pool stats request")
		}
		views[i].SchedulerAvailable = schedulerAvailable
		poolIds = append(poolIds, views[i].Id)
		viewsById[views[i].Id] = &views[i]
	}

	var memberCounts []upstreamAccountPoolCountRow
	if err := model.DB.Model(&model.UpstreamAccountPoolMember{}).
		Select("pool_id, COUNT(*) AS count").
		Where("pool_id IN ?", poolIds).
		Group("pool_id").Scan(&memberCounts).Error; err != nil {
		return err
	}
	for _, row := range memberCounts {
		viewsById[row.PoolId].AccountCount = row.Count
	}

	baseAccountCountQuery := func() *gorm.DB {
		return model.DB.Table("upstream_account_pool_members AS pool_members").
			Select("pool_members.pool_id AS pool_id, COUNT(*) AS count").
			Joins("JOIN upstream_accounts AS accounts ON accounts.id = pool_members.account_id").
			Where("pool_members.pool_id IN ?", poolIds).
			Where("accounts.status = ? AND accounts.schedulable = ?", constant.UpstreamStatusActive, true)
	}
	var activeCounts []upstreamAccountPoolCountRow
	if err := baseAccountCountQuery().Group("pool_members.pool_id").Scan(&activeCounts).Error; err != nil {
		return err
	}
	for _, row := range activeCounts {
		viewsById[row.PoolId].ActiveCount = row.Count
	}

	now := common.GetTimestamp()
	eligibleAccountCountQuery := func() *gorm.DB {
		return baseAccountCountQuery().
			Where("(accounts.auto_pause_on_expired = ? OR accounts.expires_at IS NULL OR accounts.expires_at > ?)", false, now)
	}
	readyAccountQuery := func() *gorm.DB {
		return eligibleAccountCountQuery().
			Where("(accounts.rate_limit_reset_at IS NULL OR accounts.rate_limit_reset_at <= ?)", now).
			Where("(accounts.overload_until IS NULL OR accounts.overload_until <= ?)", now).
			Where("(accounts.temp_unschedulable_until IS NULL OR accounts.temp_unschedulable_until <= ?)", now).
			Where("accounts.temp_unschedulable_reason NOT IN ?", []string{
				constant.UpstreamAccountReasonOAuthRefreshPending,
				constant.UpstreamAccountReasonOAuthRefreshFailed,
				constant.UpstreamAccountReasonOAuthRefreshPermanent,
			})
	}
	var readyCounts []upstreamAccountPoolCountRow
	if err := readyAccountQuery().
		Group("pool_members.pool_id").Scan(&readyCounts).Error; err != nil {
		return err
	}
	for _, row := range readyCounts {
		viewsById[row.PoolId].ReadyCount = row.Count
	}

	var capacityRows []upstreamAccountPoolCapacityRow
	if err := readyAccountQuery().
		Select("pool_members.pool_id AS pool_id, accounts.id AS account_id, accounts.concurrency AS concurrency").
		Scan(&capacityRows).Error; err != nil {
		return err
	}
	capacityAccountIds := make([]int, 0, len(capacityRows))
	for _, row := range capacityRows {
		capacityAccountIds = append(capacityAccountIds, row.AccountId)
	}
	applyUpstreamAccountPoolCapacities(viewsById, capacityRows, currentUpstreamAccountLeaseCounts(normalizePositiveIds(capacityAccountIds)))

	var temporarilyLimitedCounts []upstreamAccountPoolCountRow
	if err := eligibleAccountCountQuery().
		Where("(accounts.rate_limit_reset_at > ? OR accounts.overload_until > ? OR accounts.temp_unschedulable_until > ? OR accounts.temp_unschedulable_reason IN ?)",
			now, now, now, []string{
				constant.UpstreamAccountReasonOAuthRefreshPending,
				constant.UpstreamAccountReasonOAuthRefreshFailed,
				constant.UpstreamAccountReasonOAuthRefreshPermanent,
			}).
		Group("pool_members.pool_id").Scan(&temporarilyLimitedCounts).Error; err != nil {
		return err
	}
	for _, row := range temporarilyLimitedCounts {
		viewsById[row.PoolId].TemporarilyLimitedCount = row.Count
	}

	var channels []model.Channel
	if err := model.DB.
		Where("upstream_account_pool_id IN ?", poolIds).
		Order("id ASC").Find(&channels).Error; err != nil {
		return err
	}
	for i := range channels {
		channel := &channels[i]
		if channel.UpstreamAccountPoolId == nil {
			continue
		}
		view := viewsById[*channel.UpstreamAccountPoolId]
		if view == nil {
			continue
		}
		view.ChannelCount++
		if channel.CredentialSource == constant.ChannelCredentialSourceAccountPool && view.PublishedChannelId == nil {
			channelId := channel.Id
			channelStatus := channel.Status
			view.PublishedChannelId = &channelId
			view.PublishedChannelStatus = &channelStatus
			view.PublishedModelCount = len(channel.GetModels())
		}
	}

	cutoff := now - int64((24 * time.Hour).Seconds())
	eventTypes := []string{"request_selected", "request_success", "request_error"}
	var eventCounts []upstreamAccountPoolEventCountRow
	if err := model.DB.Model(&model.UpstreamAccountEvent{}).
		Select("pool_id, event_type, COUNT(*) AS count").
		Where("pool_id IN ? AND event_type IN ? AND created_at >= ?", poolIds, eventTypes, cutoff).
		Group("pool_id, event_type").Scan(&eventCounts).Error; err != nil {
		return err
	}
	for _, row := range eventCounts {
		view := viewsById[row.PoolId]
		switch row.EventType {
		case "request_selected":
			view.AttemptCount24h = row.Count
		case "request_success":
			view.SuccessCount24h = row.Count
		case "request_error":
			view.ErrorCount24h = row.Count
		}
	}

	var lastEvents []upstreamAccountPoolLastEventRow
	if err := model.DB.Model(&model.UpstreamAccountEvent{}).
		Select("pool_id, MAX(created_at) AS last_event_at").
		Where("pool_id IN ?", poolIds).
		Group("pool_id").Scan(&lastEvents).Error; err != nil {
		return err
	}
	for _, row := range lastEvents {
		lastEventAt := row.LastEventAt
		viewsById[row.PoolId].LastEventAt = &lastEventAt
	}
	return nil
}

func applyUpstreamAccountPoolCapacities(viewsById map[int]*UpstreamAccountPoolView, rows []upstreamAccountPoolCapacityRow, leaseCounts map[int]int) {
	for _, row := range rows {
		view := viewsById[row.PoolId]
		if view == nil || row.Concurrency <= 0 {
			continue
		}
		view.Concurrency += row.Concurrency
		view.CurrentConcurrency += leaseCounts[row.AccountId]
	}
}

func CreateUpstreamAccountPool(pool *model.UpstreamAccountPool) error {
	if err := model.ValidateUpstreamAccountPool(pool); err != nil {
		return err
	}
	return model.DB.Transaction(func(tx *gorm.DB) error {
		if err := validatePoolProxy(tx, pool.DefaultProxyId); err != nil {
			return err
		}
		now := common.GetTimestamp()
		pool.CreatedAt = now
		pool.UpdatedAt = now
		return tx.Create(pool).Error
	})
}

func UpdateUpstreamAccountPool(pool *model.UpstreamAccountPool) error {
	if pool == nil || pool.Id <= 0 {
		return errors.New("invalid upstream account pool")
	}
	if err := model.ValidateUpstreamAccountPool(pool); err != nil {
		return err
	}
	channelsChanged := false
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		var current model.UpstreamAccountPool
		if err := tx.First(&current, pool.Id).Error; err != nil {
			return err
		}
		if current.Platform != pool.Platform {
			var members int64
			if err := tx.Model(&model.UpstreamAccountPoolMember{}).Where("pool_id = ?", pool.Id).Count(&members).Error; err != nil {
				return err
			}
			if members > 0 {
				return errors.New("cannot change the platform of a non-empty account pool")
			}
		}
		if pool.CredentialType != "mixed" {
			var incompatibleMembers int64
			if err := tx.Model(&model.UpstreamAccountPoolMember{}).
				Joins("JOIN upstream_accounts ON upstream_accounts.id = upstream_account_pool_members.account_id").
				Where("upstream_account_pool_members.pool_id = ? AND (upstream_accounts.platform <> ? OR upstream_accounts.type <> ?)",
					pool.Id, pool.Platform, pool.CredentialType).
				Count(&incompatibleMembers).Error; err != nil {
				return err
			}
			if incompatibleMembers > 0 {
				return fmt.Errorf("account pool contains %d incompatible account(s)", incompatibleMembers)
			}
		}
		if err := validatePoolProxy(tx, pool.DefaultProxyId); err != nil {
			return err
		}
		if err := tx.Model(&model.UpstreamAccountPool{}).Where("id = ?", pool.Id).Updates(map[string]any{
			"name":             pool.Name,
			"description":      pool.Description,
			"platform":         pool.Platform,
			"credential_type":  pool.CredentialType,
			"status":           pool.Status,
			"default_proxy_id": pool.DefaultProxyId,
			"scheduler_config": pool.SchedulerConfig,
			"updated_at":       common.GetTimestamp(),
		}).Error; err != nil {
			return err
		}
		changed, err := syncPublishedAccountPoolChannelsTx(tx, []int{pool.Id})
		channelsChanged = changed
		return err
	})
	if err == nil && channelsChanged {
		refreshPublishedAccountPoolChannelCaches()
	}
	return err
}

func DeleteUpstreamAccountPool(id int) error {
	if id <= 0 {
		return errors.New("invalid upstream account pool id")
	}
	return model.DB.Transaction(func(tx *gorm.DB) error {
		var pool model.UpstreamAccountPool
		if err := tx.First(&pool, id).Error; err != nil {
			return err
		}
		var channelCount int64
		if err := tx.Model(&model.Channel{}).Where("upstream_account_pool_id = ?", id).Count(&channelCount).Error; err != nil {
			return err
		}
		if channelCount > 0 {
			return fmt.Errorf("account pool is referenced by %d channel(s)", channelCount)
		}
		var memberCount int64
		if err := tx.Model(&model.UpstreamAccountPoolMember{}).Where("pool_id = ?", id).Count(&memberCount).Error; err != nil {
			return err
		}
		if memberCount > 0 {
			return fmt.Errorf("account pool still contains %d account(s)", memberCount)
		}
		return tx.Delete(&pool).Error
	})
}

func ListUpstreamAccountPoolMembers(poolId int) ([]UpstreamAccountPoolMemberView, error) {
	if poolId <= 0 {
		return nil, errors.New("invalid upstream account pool id")
	}
	var views []UpstreamAccountPoolMemberView
	if err := model.DB.Table("upstream_account_pool_members AS pool_members").
		Select("pool_members.pool_id, pool_members.account_id, pool_members.priority, pool_members.weight, pool_members.created_at, accounts.name, accounts.platform, accounts.type, accounts.status, accounts.schedulable").
		Joins("JOIN upstream_accounts AS accounts ON accounts.id = pool_members.account_id").
		Where("pool_members.pool_id = ?", poolId).
		Order("pool_members.priority ASC").Order("pool_members.account_id ASC").
		Scan(&views).Error; err != nil {
		return nil, err
	}
	return views, nil
}

func ReplaceUpstreamAccountPoolMembers(poolId int, inputs []UpstreamAccountPoolMemberInput) error {
	if poolId <= 0 {
		return errors.New("invalid upstream account pool id")
	}
	channelsChanged := false
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		var pool model.UpstreamAccountPool
		if err := tx.First(&pool, poolId).Error; err != nil {
			return err
		}
		seen := make(map[int]struct{}, len(inputs))
		accountIds := make([]int, 0, len(inputs))
		for _, input := range inputs {
			if input.AccountId <= 0 {
				return errors.New("account pool member account_id must be positive")
			}
			if input.Priority < 0 {
				return fmt.Errorf("account %d pool priority cannot be negative", input.AccountId)
			}
			if _, exists := seen[input.AccountId]; exists {
				return fmt.Errorf("account %d is duplicated in the account pool", input.AccountId)
			}
			seen[input.AccountId] = struct{}{}
			accountIds = append(accountIds, input.AccountId)
		}

		var accounts []model.UpstreamAccount
		if len(accountIds) > 0 {
			if err := tx.Where("id IN ?", accountIds).Find(&accounts).Error; err != nil {
				return err
			}
		}
		accountsById := make(map[int]model.UpstreamAccount, len(accounts))
		for _, account := range accounts {
			accountsById[account.Id] = account
		}
		members := make([]model.UpstreamAccountPoolMember, 0, len(inputs))
		now := common.GetTimestamp()
		for _, input := range inputs {
			account, exists := accountsById[input.AccountId]
			if !exists {
				return fmt.Errorf("account %d is invalid", input.AccountId)
			}
			if account.Platform != pool.Platform {
				return fmt.Errorf("account %d platform does not match pool platform", input.AccountId)
			}
			if pool.CredentialType != "mixed" && account.Type != pool.CredentialType {
				return fmt.Errorf("account %d credential type does not match pool", input.AccountId)
			}
			weight := input.Weight
			if weight <= 0 {
				weight = 1
			}
			members = append(members, model.UpstreamAccountPoolMember{
				PoolId: poolId, AccountId: input.AccountId, Priority: input.Priority, Weight: weight, CreatedAt: now,
			})
		}

		var existing []model.UpstreamAccountPoolMember
		if err := tx.Where("pool_id = ?", poolId).Find(&existing).Error; err != nil {
			return err
		}
		existingByAccountId := make(map[int]model.UpstreamAccountPoolMember, len(existing))
		removeIds := make([]int, 0)
		for _, member := range existing {
			existingByAccountId[member.AccountId] = member
			if _, keep := seen[member.AccountId]; !keep {
				removeIds = append(removeIds, member.AccountId)
			}
		}
		if len(removeIds) > 0 {
			if err := tx.Where("pool_id = ? AND account_id IN ?", poolId, removeIds).Delete(&model.UpstreamAccountPoolMember{}).Error; err != nil {
				return err
			}
		}
		newMembers := make([]model.UpstreamAccountPoolMember, 0)
		for _, member := range members {
			current, exists := existingByAccountId[member.AccountId]
			if !exists {
				newMembers = append(newMembers, member)
				continue
			}
			if current.Priority == member.Priority && current.Weight == member.Weight {
				continue
			}
			if err := tx.Model(&model.UpstreamAccountPoolMember{}).
				Where("pool_id = ? AND account_id = ?", poolId, member.AccountId).
				Updates(map[string]any{"priority": member.Priority, "weight": member.Weight}).Error; err != nil {
				return err
			}
		}
		if len(newMembers) > 0 {
			if err := tx.Create(&newMembers).Error; err != nil {
				return err
			}
		}
		if err := tx.Model(&model.UpstreamAccountPool{}).Where("id = ?", poolId).Update("updated_at", now).Error; err != nil {
			return err
		}
		changed, err := syncPublishedAccountPoolChannelsTx(tx, []int{poolId})
		channelsChanged = changed
		return err
	})
	if err == nil && channelsChanged {
		refreshPublishedAccountPoolChannelCaches()
	}
	return err
}

func ListUpstreamAccounts(filter UpstreamAccountListFilter) ([]UpstreamAccountView, int64, error) {
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.PageSize < 1 {
		filter.PageSize = common.ItemsPerPage
	}
	if filter.PageSize > 100 {
		filter.PageSize = 100
	}
	query := model.DB.Model(&model.UpstreamAccount{})
	if platform := strings.ToLower(strings.TrimSpace(filter.Platform)); platform != "" {
		query = query.Where("platform = ?", platform)
	}
	if accountType := strings.ToLower(strings.TrimSpace(filter.Type)); accountType != "" {
		query = query.Where("type = ?", accountType)
	}
	if status := strings.ToLower(strings.TrimSpace(filter.Status)); status != "" {
		query = query.Where("status = ?", status)
	}
	if filter.Schedulable != nil {
		query = query.Where("schedulable = ?", *filter.Schedulable)
	}
	if search := strings.ToLower(strings.TrimSpace(filter.Search)); search != "" {
		query = query.Where("LOWER(name) LIKE ?", "%"+search+"%")
	}
	if filter.PoolId > 0 {
		query = query.Where("id IN (?)", model.DB.Model(&model.UpstreamAccountPoolMember{}).Select("account_id").Where("pool_id = ?", filter.PoolId))
	}
	if filter.ProxyId > 0 {
		query = query.Where("proxy_id = ?", filter.ProxyId)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	sortColumn := "id"
	if strings.EqualFold(strings.TrimSpace(filter.SortBy), "priority") {
		sortColumn = "priority"
	} else if strings.EqualFold(strings.TrimSpace(filter.SortBy), "schedulable") {
		sortColumn = "schedulable"
	}
	sortOrder := "ASC"
	if strings.EqualFold(strings.TrimSpace(filter.SortOrder), "desc") {
		sortOrder = "DESC"
	}
	var accounts []model.UpstreamAccount
	if err := query.Order(sortColumn + " " + sortOrder).Order("id ASC").Limit(filter.PageSize).Offset((filter.Page - 1) * filter.PageSize).Find(&accounts).Error; err != nil {
		return nil, 0, err
	}
	accountIds := make([]int, 0, len(accounts))
	for _, account := range accounts {
		accountIds = append(accountIds, account.Id)
	}
	leaseCounts := currentUpstreamAccountLeaseCounts(accountIds)
	poolIdsByAccount, err := listUpstreamAccountPoolIdsByAccount(model.DB, accountIds)
	if err != nil {
		return nil, 0, err
	}
	views := make([]UpstreamAccountView, 0, len(accounts))
	for _, account := range accounts {
		view := upstreamAccountView(account, poolIdsByAccount[account.Id])
		view.CurrentConcurrency = leaseCounts[account.Id]
		views = append(views, view)
	}
	return views, total, nil
}

func GetUpstreamAccount(id int) (*UpstreamAccountView, error) {
	if id <= 0 {
		return nil, errors.New("invalid upstream account id")
	}
	var account model.UpstreamAccount
	if err := model.DB.First(&account, id).Error; err != nil {
		return nil, err
	}
	poolIds, err := listUpstreamAccountPoolIds(model.DB, id)
	if err != nil {
		return nil, err
	}
	view := upstreamAccountView(account, poolIds)
	view.CurrentConcurrency = currentUpstreamAccountLeaseCounts([]int{id})[id]
	return &view, nil
}

func CreateUpstreamAccount(input *UpstreamAccountCreateInput) error {
	if input == nil {
		return errors.New("upstream account input is required")
	}
	restoreManagedOAuthExpiry(&input.Account, input.Credentials)
	if err := model.ValidateUpstreamAccount(&input.Account); err != nil {
		return err
	}
	if err := validateUpstreamCredentialPayload(input.Account.Platform, input.Account.Type, input.Credentials); err != nil {
		return err
	}
	if err := ValidateUpstreamAccountHeaderOverrides(&input.Account, input.Credentials); err != nil {
		return err
	}
	keyring, err := LoadUpstreamCredentialKeyringFromEnv()
	if err != nil {
		return err
	}
	poolIds := normalizePositiveIds(input.PoolIds)
	channelsChanged := false
	err = model.DB.Transaction(func(tx *gorm.DB) error {
		if err := validateAccountReferences(tx, &input.Account, poolIds); err != nil {
			return err
		}
		now := common.GetTimestamp()
		input.Account.CreatedAt = now
		input.Account.UpdatedAt = now
		input.Account.CredentialVersion = 1
		input.Account.CredentialKeyVersion = keyring.ActiveVersion()
		input.Account.CredentialCiphertext = ""
		input.Account.CredentialNonce = ""
		explicitPriority := input.Account.Priority
		explicitSchedulable := input.Account.Schedulable
		explicitAutoPauseOnExpired := input.Account.AutoPauseOnExpired
		explicitOAuthRefreshOwner := input.Account.OAuthRefreshOwner
		if err := tx.Create(&input.Account).Error; err != nil {
			return err
		}
		// GORM applies default tags to zero values on Create. Re-apply fields
		// where zero/false is an explicit administrative choice.
		input.Account.Priority = explicitPriority
		input.Account.Schedulable = explicitSchedulable
		input.Account.AutoPauseOnExpired = explicitAutoPauseOnExpired
		input.Account.OAuthRefreshOwner = explicitOAuthRefreshOwner
		if err := tx.Model(&model.UpstreamAccount{}).Where("id = ?", input.Account.Id).Updates(map[string]any{
			"priority":              explicitPriority,
			"schedulable":           explicitSchedulable,
			"auto_pause_on_expired": explicitAutoPauseOnExpired,
			"oauth_refresh_owner":   explicitOAuthRefreshOwner,
		}).Error; err != nil {
			return err
		}
		envelope, err := keyring.EncryptJSON(upstreamAccountCredentialRecordKind, input.Account.Id, input.Account.CredentialVersion, input.Credentials)
		if err != nil {
			return err
		}
		if err := tx.Model(&model.UpstreamAccount{}).Where("id = ?", input.Account.Id).Updates(map[string]any{
			"credential_ciphertext":  envelope.Ciphertext,
			"credential_nonce":       envelope.Nonce,
			"credential_key_version": envelope.KeyVersion,
		}).Error; err != nil {
			return err
		}
		input.Account.CredentialCiphertext = envelope.Ciphertext
		input.Account.CredentialNonce = envelope.Nonce
		input.Account.CredentialKeyVersion = envelope.KeyVersion
		if err := replaceUpstreamAccountMemberships(tx, input.Account.Id, poolIds, input.Account.Priority, input.Account.Weight); err != nil {
			return err
		}
		changed, err := syncPublishedAccountPoolChannelsTx(tx, poolIds)
		channelsChanged = changed
		return err
	})
	if err == nil && channelsChanged {
		refreshPublishedAccountPoolChannelCaches()
	}
	return err
}

func UpdateUpstreamAccount(input *UpstreamAccountUpdateInput) error {
	if input == nil || input.Account.Id <= 0 {
		return errors.New("invalid upstream account input")
	}
	if err := model.ValidateUpstreamAccount(&input.Account); err != nil {
		return err
	}
	if input.Credentials != nil && input.CredentialPatch != nil {
		return errors.New("credentials and credential_patch cannot be updated together")
	}
	credentialUpdateRequested := input.Credentials != nil || input.CredentialPatch != nil
	var keyring *UpstreamCredentialKeyring
	var err error
	if input.Credentials != nil {
		if err = validateUpstreamCredentialPayload(input.Account.Platform, input.Account.Type, *input.Credentials); err != nil {
			return err
		}
		if err = ValidateUpstreamAccountHeaderOverrides(&input.Account, *input.Credentials); err != nil {
			return err
		}
	}
	if credentialUpdateRequested {
		keyring, err = LoadUpstreamCredentialKeyringFromEnv()
		if err != nil {
			return err
		}
	}
	channelsChanged := false
	err = model.DB.Transaction(func(tx *gorm.DB) error {
		var current model.UpstreamAccount
		if err := tx.First(&current, input.Account.Id).Error; err != nil {
			return err
		}
		if current.Type != input.Account.Type && !credentialUpdateRequested {
			return errors.New("changing account type requires replacement credentials")
		}
		credentialsToStore := input.Credentials
		if input.CredentialPatch != nil {
			var merged map[string]any
			if err := keyring.DecryptJSON(UpstreamCredentialEnvelope{
				Ciphertext: current.CredentialCiphertext,
				Nonce:      current.CredentialNonce,
				KeyVersion: current.CredentialKeyVersion,
			}, upstreamAccountCredentialRecordKind, current.Id, current.CredentialVersion, &merged); err != nil {
				return err
			}
			if merged == nil {
				merged = map[string]any{}
			}
			for key, value := range *input.CredentialPatch {
				key = strings.TrimSpace(key)
				if key == "" {
					continue
				}
				if value == nil {
					delete(merged, key)
					continue
				}
				merged[key] = value
			}
			if err := validateUpstreamCredentialPayload(input.Account.Platform, input.Account.Type, merged); err != nil {
				return err
			}
			if err := ValidateUpstreamAccountHeaderOverrides(&input.Account, merged); err != nil {
				return err
			}
			credentialsToStore = &merged
		}
		if input.Account.ExpiresAt == nil &&
			input.Account.OAuthRefreshOwner == constant.UpstreamOAuthRefreshOwnerStarNexus &&
			isRefreshableUpstreamOAuthAccount(input.Account.Platform, input.Account.Type) {
			credentialsForExpiry := credentialsToStore
			if credentialsForExpiry == nil {
				currentCredentials, decryptErr := DecryptUpstreamAccountCredentials(&current)
				if decryptErr == nil {
					credentialsForExpiry = &currentCredentials
				}
			}
			if credentialsForExpiry != nil {
				restoreManagedOAuthExpiry(&input.Account, *credentialsForExpiry)
			}
		}
		existingPoolIds, err := listUpstreamAccountPoolIds(tx, current.Id)
		if err != nil {
			return err
		}
		poolIds := existingPoolIds
		if input.PoolIds != nil {
			poolIds = normalizePositiveIds(*input.PoolIds)
		}
		if err := validateAccountReferences(tx, &input.Account, poolIds); err != nil {
			return err
		}
		updates := map[string]any{
			"name": input.Account.Name, "notes": input.Account.Notes, "platform": input.Account.Platform,
			"type": input.Account.Type, "extra": input.Account.Extra, "proxy_id": input.Account.ProxyId,
			"proxy_fallback_origin_id": input.Account.ProxyFallbackOriginId,
			"concurrency":              input.Account.Concurrency, "priority": input.Account.Priority, "weight": input.Account.Weight,
			"load_factor": input.Account.LoadFactor, "rate_multiplier": input.Account.RateMultiplier,
			"status": input.Account.Status, "schedulable": input.Account.Schedulable,
			"expires_at": input.Account.ExpiresAt, "auto_pause_on_expired": input.Account.AutoPauseOnExpired,
			"oauth_refresh_owner": input.Account.OAuthRefreshOwner,
			"updated_at":          common.GetTimestamp(),
		}
		if credentialsToStore != nil {
			newVersion := current.CredentialVersion + 1
			envelope, err := keyring.EncryptJSON(upstreamAccountCredentialRecordKind, current.Id, newVersion, *credentialsToStore)
			if err != nil {
				return err
			}
			updates["credential_ciphertext"] = envelope.Ciphertext
			updates["credential_nonce"] = envelope.Nonce
			updates["credential_key_version"] = envelope.KeyVersion
			updates["credential_version"] = newVersion
		}
		result := tx.Model(&model.UpstreamAccount{}).Where("id = ? AND credential_version = ?", current.Id, current.CredentialVersion).Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("upstream account was updated concurrently")
		}
		if input.PoolIds != nil {
			if err := replaceUpstreamAccountMemberships(tx, current.Id, poolIds, input.Account.Priority, input.Account.Weight); err != nil {
				return err
			}
		}
		affectedPoolIds := append(append([]int{}, existingPoolIds...), poolIds...)
		changed, err := syncPublishedAccountPoolChannelsTx(tx, affectedPoolIds)
		channelsChanged = changed
		return err
	})
	if err == nil && channelsChanged {
		refreshPublishedAccountPoolChannelCaches()
	}
	return err
}

func restoreManagedOAuthExpiry(account *model.UpstreamAccount, credentials map[string]any) {
	if account == nil || account.ExpiresAt != nil ||
		account.OAuthRefreshOwner != constant.UpstreamOAuthRefreshOwnerStarNexus ||
		!isRefreshableUpstreamOAuthAccount(account.Platform, account.Type) {
		return
	}
	if expiresAt, ok := upstreamOAuthCredentialExpiry(credentials); ok {
		account.ExpiresAt = &expiresAt
	}
}

func DeleteUpstreamAccount(id int) error {
	if id <= 0 {
		return errors.New("invalid upstream account id")
	}
	channelsChanged := false
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		var account model.UpstreamAccount
		if err := tx.First(&account, id).Error; err != nil {
			return err
		}
		poolIds, err := listUpstreamAccountPoolIds(tx, id)
		if err != nil {
			return err
		}
		if err := tx.Where("account_id = ?", id).Delete(&model.UpstreamAccountPoolMember{}).Error; err != nil {
			return err
		}
		var planIds []int
		if err := tx.Model(&model.UpstreamAccountScheduledTestPlan{}).Where("account_id = ?", id).Pluck("id", &planIds).Error; err != nil {
			return err
		}
		if len(planIds) > 0 {
			if err := tx.Where("plan_id IN ?", planIds).Delete(&model.UpstreamAccountScheduledTestResult{}).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("account_id = ?", id).Delete(&model.UpstreamAccountScheduledTestPlan{}).Error; err != nil {
			return err
		}
		if err := tx.Delete(&account).Error; err != nil {
			return err
		}
		changed, err := syncPublishedAccountPoolChannelsTx(tx, poolIds)
		channelsChanged = changed
		return err
	})
	if err == nil && channelsChanged {
		refreshPublishedAccountPoolChannelCaches()
	}
	return err
}

func RecoverUpstreamAccountRuntimeState(id int, scope UpstreamAccountRecoveryScope) error {
	if id <= 0 {
		return errors.New("invalid upstream account id")
	}
	if scope == "" {
		scope = UpstreamAccountRecoveryAll
	}
	if scope != UpstreamAccountRecoveryAll && scope != UpstreamAccountRecoveryRateLimit && scope != UpstreamAccountRecoveryTemporary {
		return errors.New("unsupported upstream account recovery scope")
	}
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		var account model.UpstreamAccount
		if err := tx.First(&account, id).Error; err != nil {
			return err
		}
		if (scope == UpstreamAccountRecoveryAll || scope == UpstreamAccountRecoveryTemporary) &&
			constant.UpstreamAccountOAuthRefreshBlocksScheduling(account.TempUnschedulableReason) {
			return errors.New("OAuth credential refresh must succeed before scheduling can be restored")
		}
		updates := map[string]any{"updated_at": common.GetTimestamp()}
		if scope == UpstreamAccountRecoveryAll || scope == UpstreamAccountRecoveryRateLimit {
			updates["rate_limited_at"] = nil
			updates["rate_limit_reset_at"] = nil
			updates["session_window_start"] = nil
			updates["session_window_end"] = nil
			updates["session_window_status"] = ""
		}
		if scope == UpstreamAccountRecoveryAll || scope == UpstreamAccountRecoveryTemporary {
			updates["overload_until"] = nil
			updates["temp_unschedulable_until"] = nil
			updates["temp_unschedulable_reason"] = ""
		}
		if scope == UpstreamAccountRecoveryAll {
			updates["error_message"] = ""
			if account.ExpiresAt != nil && *account.ExpiresAt <= common.GetTimestamp() {
				updates["status"] = constant.UpstreamStatusExpired
				updates["schedulable"] = false
			} else {
				updates["status"] = constant.UpstreamStatusActive
				updates["schedulable"] = true
			}
		}
		return tx.Model(&model.UpstreamAccount{}).Where("id = ?", id).Updates(updates).Error
	})
	if err == nil {
		clearUpstreamAccountRuntimeBlock(id)
	}
	return err
}

func ListUpstreamProxies() ([]UpstreamProxyView, error) {
	var proxies []model.UpstreamProxy
	if err := model.DB.Order("id ASC").Find(&proxies).Error; err != nil {
		return nil, err
	}
	views := make([]UpstreamProxyView, 0, len(proxies))
	for _, proxy := range proxies {
		view, err := upstreamProxyView(model.DB, proxy)
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	return views, nil
}

func GetUpstreamProxy(id int) (*UpstreamProxyView, error) {
	if id <= 0 {
		return nil, errors.New("invalid upstream proxy id")
	}
	var proxy model.UpstreamProxy
	if err := model.DB.First(&proxy, id).Error; err != nil {
		return nil, err
	}
	view, err := upstreamProxyView(model.DB, proxy)
	if err != nil {
		return nil, err
	}
	return &view, nil
}

func CreateUpstreamProxy(input *UpstreamProxyCreateInput) error {
	if input == nil {
		return errors.New("upstream proxy input is required")
	}
	if err := model.ValidateUpstreamProxy(&input.Proxy); err != nil {
		return err
	}
	return model.DB.Transaction(func(tx *gorm.DB) error {
		if err := validateProxyFallback(tx, &input.Proxy); err != nil {
			return err
		}
		now := common.GetTimestamp()
		input.Proxy.CreatedAt = now
		input.Proxy.UpdatedAt = now
		input.Proxy.Username = input.Auth.Username
		input.Proxy.Password = input.Auth.Password
		input.Proxy.AuthCredentialVersion = 1
		input.Proxy.AuthKeyVersion = 0
		input.Proxy.AuthCiphertext = ""
		input.Proxy.AuthNonce = ""
		return tx.Create(&input.Proxy).Error
	})
}

func UpdateUpstreamProxy(input *UpstreamProxyUpdateInput) error {
	if input == nil || input.Proxy.Id <= 0 {
		return errors.New("invalid upstream proxy input")
	}
	if err := model.ValidateUpstreamProxy(&input.Proxy); err != nil {
		return err
	}
	return model.DB.Transaction(func(tx *gorm.DB) error {
		var current model.UpstreamProxy
		if err := tx.First(&current, input.Proxy.Id).Error; err != nil {
			return err
		}
		if err := validateProxyFallback(tx, &input.Proxy); err != nil {
			return err
		}
		updates := map[string]any{
			"name": input.Proxy.Name, "protocol": input.Proxy.Protocol, "host": input.Proxy.Host,
			"port": input.Proxy.Port, "status": input.Proxy.Status, "expires_at": input.Proxy.ExpiresAt,
			"fallback_mode": input.Proxy.FallbackMode, "backup_proxy_id": input.Proxy.BackupProxyId,
			"expiry_warn_days": input.Proxy.ExpiryWarnDays, "updated_at": common.GetTimestamp(),
		}
		if input.Auth != nil {
			username := strings.TrimSpace(input.Auth.Username)
			password := strings.TrimSpace(input.Auth.Password)
			if username != "" || password != "" {
				currentAuth, err := DecryptUpstreamProxyAuth(&current)
				if err != nil {
					return err
				}
				if username != "" {
					currentAuth.Username = username
				}
				if password != "" {
					currentAuth.Password = password
				}
				newVersion := current.AuthCredentialVersion + 1
				updates["username"] = currentAuth.Username
				updates["password"] = currentAuth.Password
				updates["auth_ciphertext"] = ""
				updates["auth_nonce"] = ""
				updates["auth_key_version"] = 0
				updates["auth_credential_version"] = newVersion
			}
		}
		result := tx.Model(&model.UpstreamProxy{}).Where("id = ? AND auth_credential_version = ?", current.Id, current.AuthCredentialVersion).Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("upstream proxy was updated concurrently")
		}
		return nil
	})
}

func DeleteUpstreamProxy(id int) error {
	if id <= 0 {
		return errors.New("invalid upstream proxy id")
	}
	return model.DB.Transaction(func(tx *gorm.DB) error {
		var proxy model.UpstreamProxy
		if err := tx.First(&proxy, id).Error; err != nil {
			return err
		}
		checks := []struct {
			model any
			query string
			label string
		}{
			{&model.UpstreamAccount{}, "proxy_id = ? OR proxy_fallback_origin_id = ?", "account"},
			{&model.UpstreamAccountPool{}, "default_proxy_id = ?", "account pool"},
			{&model.UpstreamProxy{}, "backup_proxy_id = ?", "proxy fallback"},
		}
		for _, check := range checks {
			var count int64
			query := tx.Model(check.model)
			if strings.Contains(check.query, " OR ") {
				query = query.Where(check.query, id, id)
			} else {
				query = query.Where(check.query, id)
			}
			if err := query.Count(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				return fmt.Errorf("proxy is referenced by %d %s record(s)", count, check.label)
			}
		}
		return tx.Delete(&proxy).Error
	})
}

func DecryptUpstreamAccountCredentials(account *model.UpstreamAccount) (map[string]any, error) {
	if account == nil || account.Id <= 0 {
		return nil, errors.New("invalid upstream account")
	}
	keyring, err := LoadUpstreamCredentialKeyringFromEnv()
	if err != nil {
		return nil, err
	}
	var credentials map[string]any
	err = keyring.DecryptJSON(UpstreamCredentialEnvelope{
		Ciphertext: account.CredentialCiphertext,
		Nonce:      account.CredentialNonce,
		KeyVersion: account.CredentialKeyVersion,
	}, upstreamAccountCredentialRecordKind, account.Id, account.CredentialVersion, &credentials)
	return credentials, err
}

func DecryptUpstreamProxyAuth(proxy *model.UpstreamProxy) (*UpstreamProxyAuthInput, error) {
	if proxy == nil || proxy.Id <= 0 {
		return nil, errors.New("invalid upstream proxy")
	}
	if proxy.Username != "" || proxy.Password != "" || proxy.AuthCiphertext == "" {
		return &UpstreamProxyAuthInput{Username: proxy.Username, Password: proxy.Password}, nil
	}
	keyring, err := LoadUpstreamCredentialKeyringFromEnv()
	if err != nil {
		return nil, err
	}
	var auth UpstreamProxyAuthInput
	err = keyring.DecryptJSON(UpstreamCredentialEnvelope{
		Ciphertext: proxy.AuthCiphertext,
		Nonce:      proxy.AuthNonce,
		KeyVersion: proxy.AuthKeyVersion,
	}, upstreamProxyAuthRecordKind, proxy.Id, proxy.AuthCredentialVersion, &auth)
	return &auth, err
}

func validateUpstreamCredentialPayload(platform string, accountType string, credentials map[string]any) error {
	if credentials == nil {
		return errors.New("account credentials are required")
	}
	getString := func(key string) string {
		value, ok := credentials[key]
		if !ok || value == nil {
			return ""
		}
		return strings.TrimSpace(fmt.Sprintf("%v", value))
	}
	platform = strings.ToLower(strings.TrimSpace(platform))
	accountType = strings.ToLower(strings.TrimSpace(accountType))
	if !model.IsSupportedUpstreamAccountType(platform, accountType) {
		return errors.New("unsupported account type")
	}
	credentialOptions, err := model.ParseUpstreamAccountOptionsWithCredentials("{}", credentials)
	if err != nil {
		return err
	}
	if platform != constant.UpstreamPlatformOpenAI &&
		(len(credentialOptions.CompactModelMapping) > 0 || len(credentialOptions.OpenAIEndpointCapabilities) > 0) {
		return errors.New("OpenAI credential options require the OpenAI platform")
	}
	switch accountType {
	case constant.UpstreamAccountTypeOAuth:
		if getString("access_token") == "" {
			return errors.New("OAuth credentials require access_token")
		}
		if platform == constant.UpstreamPlatformOpenAI && getString("account_id") == "" {
			return errors.New("OpenAI OAuth credentials require account_id")
		}
	case constant.UpstreamAccountTypeSetupToken:
		if getString("access_token") == "" {
			return errors.New("setup token credentials require access_token")
		}
	case constant.UpstreamAccountTypeAPIKey:
		if getString("api_key") == "" {
			return errors.New("API key credentials require api_key")
		}
	case constant.UpstreamAccountTypeBedrock:
		authMethod := strings.ToLower(getString("auth_mode"))
		switch authMethod {
		case "apikey", "api_key":
			if getString("api_key") == "" || getString("aws_region") == "" {
				return errors.New("Bedrock API key credentials require api_key and region")
			}
		case "sigv4", "":
			if getString("aws_access_key_id") == "" || getString("aws_secret_access_key") == "" || getString("aws_region") == "" {
				return errors.New("Bedrock SigV4 credentials require access_key_id, secret_access_key, and region")
			}
		default:
			return errors.New("unsupported Bedrock authentication method")
		}
	case constant.UpstreamAccountTypeServiceAccount:
		raw := getString("service_account_json")
		if raw == "" {
			return errors.New("Vertex credentials require service_account_json")
		}
		var serviceAccount map[string]any
		if err := common.Unmarshal([]byte(raw), &serviceAccount); err != nil {
			return errors.New("Vertex service_account_json must be valid JSON")
		}
		for _, key := range []string{"project_id", "client_email", "private_key"} {
			value := strings.TrimSpace(fmt.Sprintf("%v", serviceAccount[key]))
			if value == "" || value == "<nil>" {
				return fmt.Errorf("Vertex service_account_json requires %s", key)
			}
		}
	default:
		return errors.New("unsupported account type")
	}
	return nil
}

func validateAccountReferences(tx *gorm.DB, account *model.UpstreamAccount, poolIds []int) error {
	if account.ProxyId != nil {
		var proxy model.UpstreamProxy
		if err := tx.First(&proxy, *account.ProxyId).Error; err != nil {
			return fmt.Errorf("account proxy is invalid: %w", err)
		}
	}
	for _, poolId := range poolIds {
		var pool model.UpstreamAccountPool
		if err := tx.First(&pool, poolId).Error; err != nil {
			return fmt.Errorf("account pool %d is invalid: %w", poolId, err)
		}
		if pool.Platform != account.Platform {
			return fmt.Errorf("account pool %d platform does not match account platform", poolId)
		}
		if pool.CredentialType != "mixed" && pool.CredentialType != account.Type {
			return fmt.Errorf("account pool %d credential type does not match account type", poolId)
		}
	}
	return nil
}

func validatePoolProxy(tx *gorm.DB, proxyId *int) error {
	if proxyId == nil {
		return nil
	}
	var proxy model.UpstreamProxy
	if err := tx.First(&proxy, *proxyId).Error; err != nil {
		return fmt.Errorf("default proxy is invalid: %w", err)
	}
	return nil
}

func validateProxyFallback(tx *gorm.DB, proxy *model.UpstreamProxy) error {
	if proxy.BackupProxyId == nil {
		return nil
	}
	visited := map[int]struct{}{}
	if proxy.Id > 0 {
		visited[proxy.Id] = struct{}{}
	}
	nextId := proxy.BackupProxyId
	for nextId != nil {
		if _, exists := visited[*nextId]; exists {
			return errors.New("proxy fallback chain contains a cycle")
		}
		visited[*nextId] = struct{}{}
		var next model.UpstreamProxy
		if err := tx.First(&next, *nextId).Error; err != nil {
			return fmt.Errorf("backup proxy is invalid: %w", err)
		}
		if next.FallbackMode != constant.UpstreamProxyFallbackProxy {
			break
		}
		nextId = next.BackupProxyId
	}
	return nil
}

func replaceUpstreamAccountMemberships(tx *gorm.DB, accountId int, poolIds []int, defaultPriority int, defaultWeight int) error {
	var existing []model.UpstreamAccountPoolMember
	if err := tx.Where("account_id = ?", accountId).Find(&existing).Error; err != nil {
		return err
	}
	desired := make(map[int]struct{}, len(poolIds))
	for _, poolId := range poolIds {
		desired[poolId] = struct{}{}
	}
	existingByPool := make(map[int]struct{}, len(existing))
	removeIds := make([]int, 0)
	for _, member := range existing {
		existingByPool[member.PoolId] = struct{}{}
		if _, keep := desired[member.PoolId]; !keep {
			removeIds = append(removeIds, member.PoolId)
		}
	}
	if len(removeIds) > 0 {
		if err := tx.Where("account_id = ? AND pool_id IN ?", accountId, removeIds).Delete(&model.UpstreamAccountPoolMember{}).Error; err != nil {
			return err
		}
	}
	if defaultWeight <= 0 {
		defaultWeight = 1
	}
	now := common.GetTimestamp()
	members := make([]model.UpstreamAccountPoolMember, 0, len(poolIds))
	for _, poolId := range poolIds {
		if _, exists := existingByPool[poolId]; exists {
			continue
		}
		members = append(members, model.UpstreamAccountPoolMember{
			PoolId: poolId, AccountId: accountId, Priority: defaultPriority,
			Weight: defaultWeight, CreatedAt: now,
		})
	}
	if len(members) == 0 {
		return nil
	}
	return tx.Create(&members).Error
}

func listUpstreamAccountPoolIds(db *gorm.DB, accountId int) ([]int, error) {
	var members []model.UpstreamAccountPoolMember
	if err := db.Where("account_id = ?", accountId).Order("pool_id ASC").Find(&members).Error; err != nil {
		return nil, err
	}
	poolIds := make([]int, 0, len(members))
	for _, member := range members {
		poolIds = append(poolIds, member.PoolId)
	}
	return poolIds, nil
}

func listUpstreamAccountPoolIdsByAccount(db *gorm.DB, accountIds []int) (map[int][]int, error) {
	result := make(map[int][]int, len(accountIds))
	if len(accountIds) == 0 {
		return result, nil
	}
	for _, accountId := range accountIds {
		result[accountId] = []int{}
	}
	var members []model.UpstreamAccountPoolMember
	if err := db.Select("account_id", "pool_id").
		Where("account_id IN ?", accountIds).
		Order("account_id ASC").Order("pool_id ASC").
		Find(&members).Error; err != nil {
		return nil, err
	}
	for _, member := range members {
		result[member.AccountId] = append(result[member.AccountId], member.PoolId)
	}
	return result, nil
}

func normalizePositiveIds(ids []int) []int {
	set := make(map[int]struct{}, len(ids))
	for _, id := range ids {
		if id > 0 {
			set[id] = struct{}{}
		}
	}
	normalized := make([]int, 0, len(set))
	for id := range set {
		normalized = append(normalized, id)
	}
	sort.Ints(normalized)
	return normalized
}

func upstreamAccountView(account model.UpstreamAccount, poolIds []int) UpstreamAccountView {
	configured := account.CredentialCiphertext != ""
	metadata := upstreamAccountMetadata(&account)
	account.CredentialCiphertext = ""
	account.CredentialNonce = ""
	return UpstreamAccountView{UpstreamAccount: account, CredentialConfigured: configured, PoolIds: poolIds, Metadata: metadata}
}

func currentUpstreamAccountLeaseCounts(accountIds []int) map[int]int {
	counts := make(map[int]int, len(accountIds))
	if len(accountIds) == 0 {
		return counts
	}
	router, err := GetConfiguredUpstreamAccountRouter()
	if err != nil || router == nil || router.leaseManager == nil {
		return counts
	}
	current, err := router.leaseManager.CountBatch(context.Background(), accountIds)
	if err != nil {
		return counts
	}
	return current
}

func upstreamAccountMetadata(account *model.UpstreamAccount) UpstreamAccountMetadata {
	metadata := UpstreamAccountMetadata{}
	if account == nil {
		return metadata
	}
	credentials, credentialErr := DecryptUpstreamAccountCredentials(account)
	if credentialErr == nil {
		metadata.CredentialReadable = true
		metadata.BaseURL = upstreamCredentialMapString(credentials, "base_url")
		metadata.ModelMapping = upstreamCredentialStringMap(credentials["model_mapping"])
		metadata.HeaderOverrideEnabled, _ = credentials["header_override_enabled"].(bool)
		metadata.HeaderOverrides = upstreamCredentialStringMap(credentials["header_overrides"])
		metadata.BedrockAuthMode = upstreamCredentialMapString(credentials, "auth_mode")
		metadata.AWSRegion = upstreamCredentialMapString(credentials, "aws_region")
		metadata.AWSAccessKeyID = upstreamCredentialMapString(credentials, "aws_access_key_id")
		metadata.VertexProjectID = upstreamCredentialMapString(credentials, "project_id")
		metadata.VertexClientEmail = upstreamCredentialMapString(credentials, "client_email")
		metadata.VertexLocation = upstreamCredentialMapString(credentials, "location")
		metadata.Email = upstreamCredentialMapString(credentials, "email")
		metadata.PlanType = upstreamCredentialMapString(credentials, "plan_type")
		metadata.SubscriptionExpiresAt = upstreamCredentialMapString(credentials, "subscription_expires_at")
		if metadata.PlanType == "" {
			metadata.PlanType = upstreamCredentialMapString(credentials, "plan")
		}
		metadata.PrivacyMode = upstreamCredentialMapString(credentials, "privacy_mode")
		for _, key := range []string{"metadata", "live_identity"} {
			nested, _ := credentials[key].(map[string]any)
			if metadata.Email == "" {
				metadata.Email = upstreamCredentialMapString(nested, "email")
			}
			if metadata.PlanType == "" {
				metadata.PlanType = upstreamCredentialMapString(nested, "plan")
			}
			if metadata.PrivacyMode == "" {
				metadata.PrivacyMode = upstreamCredentialMapString(nested, "privacy_mode")
			}
		}
		if account.Platform == constant.UpstreamPlatformOpenAI && account.Type == constant.UpstreamAccountTypeOAuth {
			if identity, ok := extractCodexOAuthIdentityFromCredentials(credentials); ok {
				if metadata.Email == "" {
					metadata.Email = identity.Email
				}
				if metadata.PlanType == "" {
					metadata.PlanType = identity.PlanType
				}
				if metadata.SubscriptionExpiresAt == "" {
					metadata.SubscriptionExpiresAt = identity.SubscriptionExpiresAt
				}
			}
		}
	} else {
		metadata.CredentialReadError = credentialErr.Error()
	}
	var extra map[string]any
	if common.UnmarshalJsonStr(account.Extra, &extra) == nil {
		if metadata.PrivacyMode == "" {
			metadata.PrivacyMode = upstreamCredentialMapString(extra, "privacy_mode")
		}
	}
	if options, err := model.ParseUpstreamAccountOptionsWithCredentials(account.Extra, credentials); err == nil {
		metadata.CompactModelMapping = options.CompactModelMapping
		metadata.OpenAIEndpointCapabilities = options.OpenAIEndpointCapabilities
		metadata.InterceptWarmupRequests = options.InterceptWarmupRequests
		metadata.TempUnschedulableEnabled = options.TempUnschedulableEnabled
		metadata.TempUnschedulableRules = options.TempUnschedulableRules
	}
	if account.Platform == constant.UpstreamPlatformOpenAI {
		options, err := model.ParseUpstreamAccountOptions(account.Extra)
		if err != nil {
			return metadata
		}
		metadata.CompactMode = options.OpenAICompactMode
		metadata.CompactSupported = options.AllowsOpenAICompact()
	}
	return metadata
}

func upstreamCredentialStringMap(value any) map[string]string {
	result := map[string]string{}
	switch values := value.(type) {
	case map[string]string:
		for key, mapped := range values {
			key = strings.TrimSpace(key)
			mapped = strings.TrimSpace(mapped)
			if key != "" && mapped != "" {
				result[key] = mapped
			}
		}
	case map[string]any:
		for key, raw := range values {
			mapped, ok := raw.(string)
			key = strings.TrimSpace(key)
			mapped = strings.TrimSpace(mapped)
			if ok && key != "" && mapped != "" {
				result[key] = mapped
			}
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func upstreamProxyView(db *gorm.DB, proxy model.UpstreamProxy) (UpstreamProxyView, error) {
	auth := UpstreamProxyAuthInput{Username: proxy.Username, Password: proxy.Password}
	if auth.Username == "" && auth.Password == "" && proxy.AuthCiphertext != "" {
		if legacyAuth, err := DecryptUpstreamProxyAuth(&proxy); err == nil {
			auth = *legacyAuth
		}
	}
	view := UpstreamProxyView{
		UpstreamProxy: proxy,
		AuthConfigured: auth.Username != "" || auth.Password != "" ||
			(proxy.AuthCiphertext != "" && proxy.AuthNonce != "" && proxy.AuthKeyVersion > 0),
		AuthUsername: auth.Username,
		AuthPassword: auth.Password,
	}
	view.AuthCiphertext = ""
	view.AuthNonce = ""
	if err := db.Model(&model.UpstreamAccount{}).Where("proxy_id = ?", proxy.Id).Count(&view.AccountCount).Error; err != nil {
		return UpstreamProxyView{}, err
	}
	if err := db.Model(&model.UpstreamAccountPool{}).Where("default_proxy_id = ?", proxy.Id).Count(&view.PoolCount).Error; err != nil {
		return UpstreamProxyView{}, err
	}
	if err := db.Model(&model.UpstreamProxy{}).Where("backup_proxy_id = ?", proxy.Id).Count(&view.BackupCount).Error; err != nil {
		return UpstreamProxyView{}, err
	}
	return view, nil
}
