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
			Content: "token重算：tokens=50, modelRatio=1.00, rawTokens=100, rawPrompt=80, rawCompletion=20, inputRatio=0.5000, outputRatio=0.5000",
			Other: common.MapToJsonStr(map[string]interface{}{
				"admin_info": map[string]interface{}{
					"node_name": "xingyuapi-prod-1",
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
			}),
		},
	}

	formatUserLogs(logs, 0)

	require.Contains(t, logs[0].Content, "tokens=50")
	require.NotContains(t, logs[0].Content, "rawTokens")
	require.NotContains(t, logs[0].Content, "rawPrompt")
	require.NotContains(t, logs[0].Content, "rawCompletion")
	require.NotContains(t, logs[0].Content, "inputRatio")
	require.NotContains(t, logs[0].Content, "outputRatio")

	other, err := common.StrToMap(logs[0].Other)
	require.NoError(t, err)
	require.Equal(t, true, other["token_pricing_enabled"])
	require.Equal(t, float64(5), other["cache_tokens"])
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

func TestSQLiteDecimalAutoMigrateIsIdempotent(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/log-migration.db"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	models := []interface{}{&Log{}, &ContentModerationKeyUsage{}, &SubscriptionPlan{}, &UserAffiliate{}}
	require.NoError(t, db.AutoMigrate(models...))
	require.NoError(t, db.AutoMigrate(models...))
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
