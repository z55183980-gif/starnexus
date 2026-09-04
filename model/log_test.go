package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestFormatUserLogsRemovesTokenPricingAdminFields(t *testing.T) {
	logs := []*Log{
		{
			UpstreamAccountId:   9,
			UpstreamAccountName: "supplier-account",
			AccountCost:         0.25,
			UserCost:            0.75,
			Content:             "token重算：tokens=50, modelRatio=1.00, rawTokens=100, rawPrompt=80, rawCompletion=20, inputRatio=0.5000, outputRatio=0.5000",
			Other: common.MapToJsonStr(map[string]interface{}{
				"admin_info": map[string]interface{}{
					"node_name": "xingyuapi-prod-1",
					"cache_billing": map[string]interface{}{
						"offset_bps":                300,
						"total_input_tokens":        5,
						"raw_cache_read_tokens":     5,
						"billing_cache_read_tokens": 2,
						"reclassified_tokens":       3,
					},
				},
				"token_pricing_enabled":      true,
				"token_pricing_input_ratio":  2,
				"token_pricing_output_ratio": 3,
				"token_pricing_rules":        []string{"vip", "user"},
				"raw_prompt_tokens":          100,
				"raw_completion_tokens":      10,
				"raw_total_tokens":           110,
				"billing_prompt_tokens":      200,
				"billing_completion_tokens":  30,
				"billing_total_tokens":       230,
				"cache_tokens":               5,
				"stream_status": map[string]interface{}{
					"status":      "error",
					"end_reason":  "handler_stop",
					"error_count": 1,
					"end_error":   "upstream https://api.example.com/v1 failed from 192.168.1.1",
					"errors":      []string{"internal retry detail"},
				},
			}),
		},
	}

	formatUserLogs(logs, 0)

	require.Zero(t, logs[0].UpstreamAccountId)
	require.Empty(t, logs[0].UpstreamAccountName)
	require.Zero(t, logs[0].AccountCost)
	require.Equal(t, 0.75, logs[0].UserCost)
	require.Contains(t, logs[0].Content, "tokens=50")
	require.NotContains(t, logs[0].Content, "rawTokens")
	require.NotContains(t, logs[0].Content, "rawPrompt")
	require.NotContains(t, logs[0].Content, "rawCompletion")
	require.NotContains(t, logs[0].Content, "inputRatio")
	require.NotContains(t, logs[0].Content, "outputRatio")

	other, err := common.StrToMap(logs[0].Other)
	require.NoError(t, err)
	require.NotContains(t, other, "token_pricing_enabled")
	require.Equal(t, float64(2), other["cache_tokens"])
	require.NotContains(t, other, "admin_info")
	require.NotContains(t, other, "token_pricing_input_ratio")
	require.NotContains(t, other, "token_pricing_output_ratio")
	require.NotContains(t, other, "token_pricing_rules")
	require.NotContains(t, other, "raw_prompt_tokens")
	require.NotContains(t, other, "raw_completion_tokens")
	require.NotContains(t, other, "raw_total_tokens")
	require.NotContains(t, other, "billing_prompt_tokens")
	require.NotContains(t, other, "billing_completion_tokens")
	require.NotContains(t, other, "billing_total_tokens")
	streamStatus, ok := other["stream_status"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, "error", streamStatus["status"])
	require.Equal(t, "handler_stop", streamStatus["end_reason"])
	require.Equal(t, float64(1), streamStatus["error_count"])
	require.NotContains(t, streamStatus, "errors")
	require.NotContains(t, streamStatus["end_error"], "api.example.com")
	require.NotContains(t, streamStatus["end_error"], "192.168.1.1")
	require.NotContains(t, logs[0].Other, "offset_bps")
	require.NotContains(t, logs[0].Other, "total_input_tokens")
	require.NotContains(t, logs[0].Other, "raw_cache_read_tokens")
	require.NotContains(t, logs[0].Other, "billing_cache_read_tokens")
	require.NotContains(t, logs[0].Other, "reclassified_tokens")
}

func TestFormatUserLogsOmitsSuccessfulStreamStatus(t *testing.T) {
	logs := []*Log{{
		Other: `{"stream_status":{"status":"ok","end_reason":"eof"}}`,
	}}

	formatUserLogs(logs, 0)

	other, err := common.StrToMap(logs[0].Other)
	require.NoError(t, err)
	require.NotContains(t, other, "stream_status")
}

func TestFormatUserLogsCacheBillingProjectionRejectsInvalidAudit(t *testing.T) {
	tests := []struct {
		name         string
		cacheBilling map[string]interface{}
		wantCache    float64
	}{
		{
			name:      "missing audit",
			wantCache: 5,
		},
		{
			name: "raw count mismatch",
			cacheBilling: map[string]interface{}{
				"raw_cache_read_tokens":     4,
				"billing_cache_read_tokens": 2,
			},
			wantCache: 5,
		},
		{
			name: "negative billing count",
			cacheBilling: map[string]interface{}{
				"raw_cache_read_tokens":     5,
				"billing_cache_read_tokens": -1,
			},
			wantCache: 5,
		},
		{
			name: "billing count exceeds raw",
			cacheBilling: map[string]interface{}{
				"raw_cache_read_tokens":     5,
				"billing_cache_read_tokens": 6,
			},
			wantCache: 5,
		},
		{
			name: "zero billing count is valid",
			cacheBilling: map[string]interface{}{
				"raw_cache_read_tokens":     5,
				"billing_cache_read_tokens": 0,
			},
			wantCache: 0,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			adminInfo := map[string]interface{}{"node_name": "internal-node"}
			if test.cacheBilling != nil {
				adminInfo["cache_billing"] = test.cacheBilling
			}
			logs := []*Log{{
				Other: common.MapToJsonStr(map[string]interface{}{
					"cache_tokens": 5,
					"admin_info":   adminInfo,
				}),
			}}

			formatUserLogs(logs, 0)

			other, err := common.StrToMap(logs[0].Other)
			require.NoError(t, err)
			require.Equal(t, test.wantCache, other["cache_tokens"])
			require.NotContains(t, other, "admin_info")
		})
	}
}

func TestAttachNodeNameToLogOther(t *testing.T) {
	originalNodeName := common.NodeName
	common.NodeName = "xingyuapi-prod-1"
	t.Cleanup(func() {
		common.NodeName = originalNodeName
	})

	other := attachNodeNameToLogOther(map[string]interface{}{
		"admin_info": map[string]interface{}{
			"use_channel": []int{42},
		},
	})

	adminInfo, ok := other["admin_info"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, "xingyuapi-prod-1", adminInfo["node_name"])
	require.Equal(t, []int{42}, adminInfo["use_channel"])
}

func TestResolveUseTimeMilliseconds(t *testing.T) {
	precise := int64(1234)
	require.Equal(t, int64(1234), *resolveUseTimeMilliseconds(1, &precise))
	require.Equal(t, int64(2000), *resolveUseTimeMilliseconds(2, nil))
	require.Nil(t, resolveUseTimeMilliseconds(0, nil))
}

func TestNormalizeLogUsageStats(t *testing.T) {
	log := &Log{Other: `{"usage_semantic":"anthropic","cache_tokens":1500,"cache_write_tokens":300,"cache_creation_tokens":400,"cache_creation_tokens_5m":100,"cache_creation_tokens_1h":200,"group_ratio":0.5,"billing_mode":"tiered_expr","billing_source":"subscription","admin_info":{"cache_billing":{"raw_cache_read_tokens":1500,"billing_cache_read_tokens":1400}}}`}
	require.True(t, NormalizeLogUsageStats(log))
	require.Equal(t, int8(1), log.UsageStatsReady)
	require.Equal(t, int64(1500), log.UsageCacheReadTokens)
	require.Equal(t, int64(1400), log.UsageBillingCacheRead)
	// cache_write_tokens has the same precedence as the existing JSON logic.
	require.Equal(t, int64(300), log.UsageCacheCreationTokens)
	require.Equal(t, int8(1), log.UsageIsAnthropic)
	require.InDelta(t, 0.5, log.UsageGroupRatio, 0.000001)
	require.Equal(t, "tiered_expr", log.UsageBillingMode)
	require.Equal(t, "subscription", log.UsageBillingSource)

	malformed := &Log{Other: `{not-json`}
	require.False(t, NormalizeLogUsageStats(malformed))
	require.Equal(t, int8(1), malformed.UsageStatsReady)
	require.Zero(t, malformed.UsageCacheReadTokens)
	require.Empty(t, malformed.UsageBillingMode)
}

func TestSQLiteDecimalAutoMigrateIsIdempotent(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/log-migration.db"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	models := []interface{}{&Log{}, &ContentModerationKeyUsage{}, &SubscriptionPlan{}, &UserAffiliate{}}
	require.NoError(t, db.AutoMigrate(models...))
	require.NoError(t, db.AutoMigrate(models...))
	require.True(t, db.Migrator().HasIndex(&Log{}, "idx_logs_token_usage_window"))
}

func TestGetBusinessMonitorCacheStats(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	originalLogDB := LOG_DB
	LOG_DB = db
	t.Cleanup(func() { LOG_DB = originalLogDB })
	require.NoError(t, db.AutoMigrate(&Log{}))

	logs := []Log{
		// OpenAI prompt totals include cache reads, so 2,000 becomes 500 uncached + 1,500 read.
		{Type: LogTypeConsume, CreatedAt: 150, ModelName: "gpt-test", PromptTokens: 2000, Other: `{"cache_tokens":1500,"admin_info":{"cache_billing":{"raw_cache_read_tokens":1500,"billing_cache_read_tokens":1400}}}`},
		// Anthropic reports input, cache read, and cache creation separately.
		{Type: LogTypeConsume, CreatedAt: 160, PromptTokens: 200, Other: `{"usage_semantic":"anthropic","cache_tokens":500,"cache_creation_tokens_5m":100,"cache_creation_tokens_1h":200}`},
		{Type: LogTypeConsume, CreatedAt: 170, PromptTokens: 100, Other: `{}`},
		{Type: LogTypeError, CreatedAt: 180, PromptTokens: 900, Other: `{"cache_tokens":800}`},
		{Type: LogTypeConsume, CreatedAt: 99, PromptTokens: 900, Other: `{"cache_tokens":800}`},
		{Type: LogTypeConsume, CreatedAt: 201, PromptTokens: 900, Other: `{"cache_tokens":800}`},
	}
	for i := range logs {
		NormalizeLogUsageStats(&logs[i])
	}
	require.NoError(t, db.Create(&logs).Error)

	stats, err := GetBusinessMonitorCacheStats(100, 200, "")
	require.NoError(t, err)
	require.Equal(t, int64(800), stats.InputTokens)
	require.Equal(t, int64(2000), stats.CacheReadTokens)
	require.Equal(t, int64(1900), stats.BillingCacheReadTokens)
	require.Equal(t, int64(300), stats.CacheCreationTokens)

	filteredStats, err := GetBusinessMonitorCacheStats(100, 200, "gpt-test")
	require.NoError(t, err)
	require.Equal(t, int64(500), filteredStats.InputTokens)
	require.Equal(t, int64(1500), filteredStats.CacheReadTokens)
	require.Equal(t, int64(1400), filteredStats.BillingCacheReadTokens)
}

func TestGetUsageDetailsSummaryUsesDatabaseAggregate(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	originalDB := DB
	originalLogDB := LOG_DB
	originalQuotaPerUnit := common.QuotaPerUnit
	DB = db
	LOG_DB = db
	common.QuotaPerUnit = 100
	t.Cleanup(func() {
		DB = originalDB
		LOG_DB = originalLogDB
		common.QuotaPerUnit = originalQuotaPerUnit
	})
	require.NoError(t, db.AutoMigrate(&Log{}))
	log := &Log{
		Type:             LogTypeConsume,
		CreatedAt:        150,
		PromptTokens:     2000,
		CompletionTokens: 100,
		Quota:            100,
		AccountCost:      0.25,
		UserCost:         0.75,
		Other:            `{"cache_tokens":1500,"admin_info":{"cache_billing":{"raw_cache_read_tokens":1500,"billing_cache_read_tokens":1400}},"group_ratio":0.5}`,
	}
	NormalizeLogUsageStats(log)
	require.NoError(t, db.Create(log).Error)

	summary, err := GetUsageDetailsSummary(LogTypeConsume, 100, 200, "", "", "", "")
	require.NoError(t, err)
	require.Equal(t, int64(500), summary.InputTokens)
	require.Equal(t, int64(100), summary.OutputTokens)
	require.Equal(t, int64(1500), summary.CacheTokens)
	require.Equal(t, int64(2100), summary.TotalTokens)
	require.InDelta(t, 0.75, summary.ActualCostUSD, 0.000001)
	require.InDelta(t, 0.25, summary.AccountCostUSD, 0.000001)
	require.InDelta(t, 1.5, summary.StandardCostUSD, 0.000001)
}

func TestUsageAggregatesDoNotParseOtherJSON(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	originalDB := DB
	originalLogDB := LOG_DB
	originalQuotaPerUnit := common.QuotaPerUnit
	DB = db
	LOG_DB = db
	common.QuotaPerUnit = 100
	t.Cleanup(func() {
		DB = originalDB
		LOG_DB = originalLogDB
		common.QuotaPerUnit = originalQuotaPerUnit
	})
	require.NoError(t, db.AutoMigrate(&Log{}))

	// Ready=1 deliberately skips the create hook. If either aggregate regresses
	// to a JSON expression, this malformed payload makes the SQLite query fail.
	log := &Log{
		Type:                     LogTypeConsume,
		CreatedAt:                150,
		PromptTokens:             1000,
		CompletionTokens:         100,
		Quota:                    100,
		Other:                    `{not-json`,
		UsageStatsReady:          1,
		UsageCacheReadTokens:     600,
		UsageBillingCacheRead:    550,
		UsageCacheCreationTokens: 100,
		UsageGroupRatio:          0.5,
		UsageBillingMode:         "tiered_expr",
		UsageBillingSource:       "subscription",
	}
	require.NoError(t, db.Create(log).Error)

	cacheStats, err := GetBusinessMonitorCacheStats(100, 200, "")
	require.NoError(t, err)
	require.Equal(t, int64(300), cacheStats.InputTokens)
	require.Equal(t, int64(600), cacheStats.CacheReadTokens)
	require.Equal(t, int64(550), cacheStats.BillingCacheReadTokens)
	require.Equal(t, int64(100), cacheStats.CacheCreationTokens)

	billingType := 1
	summary, err := GetUsageDetailsSummary(
		LogTypeConsume, 100, 200, "", "", "", "",
		LogQueryOptions{BillingMode: "tiered_expr", BillingType: &billingType},
	)
	require.NoError(t, err)
	require.Equal(t, int64(300), summary.InputTokens)
	require.Equal(t, int64(100), summary.OutputTokens)
	require.Equal(t, int64(700), summary.CacheTokens)
	require.Equal(t, int64(1100), summary.TotalTokens)
	require.InDelta(t, 1.0, summary.ActualCostUSD, 0.000001)
	require.InDelta(t, 2.0, summary.StandardCostUSD, 0.000001)

	logs, total, err := GetAllLogs(
		LogTypeConsume, 100, 200, "", "", "", 0, 20, 0, "", "", "", nil,
		LogQueryOptions{BillingMode: "tiered_expr", BillingType: &billingType},
	)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, logs, 1)
}

func TestUsageAggregatesRequireHistoricalBackfill(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	originalDB := DB
	originalLogDB := LOG_DB
	DB = db
	LOG_DB = db
	t.Cleanup(func() {
		DB = originalDB
		LOG_DB = originalLogDB
	})
	require.NoError(t, db.AutoMigrate(&Log{}))
	require.NoError(t, db.Session(&gorm.Session{SkipHooks: true}).Create(&Log{
		Type:         LogTypeConsume,
		CreatedAt:    150,
		PromptTokens: 1000,
		Other:        `{"cache_tokens":600}`,
	}).Error)

	_, err = GetBusinessMonitorCacheStats(100, 200, "")
	require.ErrorIs(t, err, ErrLogUsageStatsBackfillRequired)
	_, err = GetUsageDetailsSummary(LogTypeConsume, 100, 200, "", "", "", "")
	require.ErrorIs(t, err, ErrLogUsageStatsBackfillRequired)
}

func TestGetAllLogsIncludesUpstreamAccountName(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	originalDB := DB
	originalLogDB := LOG_DB
	DB = db
	LOG_DB = db
	t.Cleanup(func() {
		DB = originalDB
		LOG_DB = originalLogDB
	})

	require.NoError(t, db.AutoMigrate(&Log{}, &UpstreamAccount{}))
	account := UpstreamAccount{
		Name:                 "primary-account",
		Platform:             "openai",
		Type:                 "apikey",
		CredentialCiphertext: "encrypted",
		CredentialNonce:      "nonce",
		Extra:                "{}",
		Status:               "active",
		Schedulable:          true,
		CreatedAt:            common.GetTimestamp(),
		UpdatedAt:            common.GetTimestamp(),
	}
	require.NoError(t, db.Create(&account).Error)
	require.NoError(t, db.Create(&Log{
		Type:              LogTypeConsume,
		CreatedAt:         common.GetTimestamp(),
		UpstreamAccountId: account.Id,
	}).Error)

	logs, total, err := GetAllLogs(
		LogTypeUnknown, 0, 0, "", "", "", 0, 20, 0, "", "", "", nil,
	)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, logs, 1)
	require.Equal(t, account.Id, logs[0].UpstreamAccountId)
	require.Equal(t, "primary-account", logs[0].UpstreamAccountName)
}

func TestGetAllLogsProjectsCacheBillingDisplayAndPreservesAdminAudit(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	originalDB := DB
	originalLogDB := LOG_DB
	DB = db
	LOG_DB = db
	t.Cleanup(func() {
		DB = originalDB
		LOG_DB = originalLogDB
	})

	require.NoError(t, db.AutoMigrate(&Log{}))
	require.NoError(t, db.Create(&Log{
		Type:      LogTypeConsume,
		CreatedAt: common.GetTimestamp(),
		Other: common.MapToJsonStr(map[string]interface{}{
			"cache_tokens": 164237,
			"admin_info": map[string]interface{}{
				"node_name": "xingyuapi-prod-3",
				"cache_billing": map[string]interface{}{
					"offset_bps":                300,
					"total_input_tokens":        166941,
					"raw_cache_read_tokens":     164237,
					"billing_cache_read_tokens": 159229,
					"reclassified_tokens":       5008,
				},
			},
		}),
	}).Error)

	logs, total, err := GetAllLogs(
		LogTypeConsume, 0, 0, "", "", "", 0, 20, 0, "", "", "", nil,
	)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, logs, 1)

	other, err := common.StrToMap(logs[0].Other)
	require.NoError(t, err)
	require.Equal(t, float64(159229), other["cache_tokens"])
	adminInfo, ok := other["admin_info"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, "xingyuapi-prod-3", adminInfo["node_name"])
	require.Contains(t, adminInfo, "cache_billing")
}

func TestGetAgentUserLogsScopesToAgentAndInvitees(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	originalDB := DB
	originalLogDB := LOG_DB
	DB = db
	LOG_DB = db
	t.Cleanup(func() {
		DB = originalDB
		LOG_DB = originalLogDB
	})

	if err := db.AutoMigrate(&User{}, &Log{}); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}

	users := []User{
		{Id: 10, Username: "agent", Role: common.RoleAgentUser, AffCode: "agent-log"},
		{Id: 11, Username: "invitee", Role: common.RoleCommonUser, InviterId: 10, AffCode: "invitee-log"},
		{Id: 12, Username: "other", Role: common.RoleCommonUser, InviterId: 99, AffCode: "other-log"},
		{Id: 13, Username: "admin", Role: common.RoleAdminUser, InviterId: 10, AffCode: "admin-log"},
	}
	for _, user := range users {
		if err := db.Create(&user).Error; err != nil {
			t.Fatalf("seed user %d: %v", user.Id, err)
		}
	}

	now := common.GetTimestamp()
	logs := []Log{
		{UserId: 10, Username: "agent", Type: LogTypeConsume, CreatedAt: now, ModelName: "gpt", TokenName: "tk", Quota: 100, PromptTokens: 2, CompletionTokens: 3},
		{UserId: 11, Username: "invitee", Type: LogTypeConsume, CreatedAt: now, ModelName: "gpt", TokenName: "tk", Quota: 200, PromptTokens: 5, CompletionTokens: 7},
		{UserId: 12, Username: "other", Type: LogTypeConsume, CreatedAt: now, ModelName: "gpt", TokenName: "tk", Quota: 300, PromptTokens: 11, CompletionTokens: 13},
		{UserId: 13, Username: "admin", Type: LogTypeConsume, CreatedAt: now, ModelName: "gpt", TokenName: "tk", Quota: 400, PromptTokens: 17, CompletionTokens: 19},
	}
	for _, log := range logs {
		if err := db.Create(&log).Error; err != nil {
			t.Fatalf("seed log for user %d: %v", log.UserId, err)
		}
	}

	got, total, err := GetAgentUserLogs(10, LogTypeUnknown, 0, 0, "", "", "", 0, 20, "", "", "", nil)
	if err != nil {
		t.Fatalf("get agent logs: %v", err)
	}
	if total != 2 || len(got) != 2 {
		t.Fatalf("agent logs total=%d len=%d, want agent plus invitee", total, len(got))
	}
	seen := map[int]bool{}
	for _, log := range got {
		seen[log.UserId] = true
	}
	if !seen[10] || !seen[11] || seen[12] || seen[13] {
		t.Fatalf("agent log users = %v, want agent 10 and invitee 11 only", seen)
	}

	stat, err := SumAgentUsedQuota(10, LogTypeUnknown, 0, 0, "", "", "", "", nil)
	if err != nil {
		t.Fatalf("sum agent quota: %v", err)
	}
	if stat.Quota != 300 || stat.Rpm != 2 || stat.Tpm != 17 {
		t.Fatalf("agent stat = %+v, want quota=300 rpm=2 tpm=17", stat)
	}
}
