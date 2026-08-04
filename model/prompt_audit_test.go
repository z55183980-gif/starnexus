package model

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestDeletePromptAuditPolicyAndLogsSameDatabase(t *testing.T) {
	db := openPromptAuditTestDB(t)
	setPromptAuditTestDatabases(t, db, db)

	policy := createPromptAuditTestData(t, db, db, 7)
	require.NoError(t, DeletePromptAuditPolicyAndLogs(policy.Id))
	assertPromptAuditDataDeleted(t, db, db, policy.Id, 7)

	var otherUserLogs int64
	require.NoError(t, db.Model(&PromptAuditLog{}).Where("user_id = ?", 8).Count(&otherUserLogs).Error)
	require.Equal(t, int64(1), otherUserLogs)
}

func TestDeletePromptAuditPolicyAndLogsSeparateDatabases(t *testing.T) {
	mainDB := openPromptAuditTestDB(t)
	logDB := openPromptAuditTestDB(t)
	setPromptAuditTestDatabases(t, mainDB, logDB)

	policy := createPromptAuditTestData(t, mainDB, logDB, 9)
	require.NoError(t, DeletePromptAuditPolicyAndLogs(policy.Id))
	assertPromptAuditDataDeleted(t, mainDB, logDB, policy.Id, 9)
}

func TestDeletePromptAuditLogsByUserIdKeepsPolicyAndAllowsNewLogs(t *testing.T) {
	mainDB := openPromptAuditTestDB(t)
	logDB := openPromptAuditTestDB(t)
	setPromptAuditTestDatabases(t, mainDB, logDB)

	policy := createPromptAuditTestData(t, mainDB, logDB, 11)
	require.NoError(t, logDB.Create(&PromptAuditLog{
		UserId: 12, Prompt: "other", PromptHash: "c", MatchedWords: "[]", CreatedAt: 3,
	}).Error)

	deleted, err := DeletePromptAuditLogsByUserId(policy.UserId)
	require.NoError(t, err)
	require.Equal(t, int64(2), deleted)

	var policyCount int64
	require.NoError(t, mainDB.Model(&PromptAuditPolicy{}).Where("id = ?", policy.Id).Count(&policyCount).Error)
	require.Equal(t, int64(1), policyCount)

	var otherUserLogs int64
	require.NoError(t, logDB.Model(&PromptAuditLog{}).Where("user_id = ?", 12).Count(&otherUserLogs).Error)
	require.Equal(t, int64(1), otherUserLogs)

	require.NoError(t, CreatePromptAuditLog(&PromptAuditLog{
		UserId: policy.UserId, Prompt: "new", PromptHash: "d", MatchedWords: "[]", CreatedAt: 4,
	}))
	var newLogs int64
	require.NoError(t, logDB.Model(&PromptAuditLog{}).Where("user_id = ?", policy.UserId).Count(&newLogs).Error)
	require.Equal(t, int64(1), newLogs)
}

func TestListPromptAuditLogsByCursor(t *testing.T) {
	db := openPromptAuditTestDB(t)
	setPromptAuditTestDatabases(t, db, db)

	logs := []PromptAuditLog{
		{UserId: 21, Prompt: "one", PromptHash: "1", MatchedWords: "[]", CreatedAt: 10},
		{UserId: 21, Prompt: "two", PromptHash: "2", MatchedWords: "[]", CreatedAt: 10},
		{UserId: 21, Prompt: "three", PromptHash: "3", MatchedWords: "[]", CreatedAt: 11},
		{UserId: 22, Prompt: "other", PromptHash: "4", MatchedWords: "[]", CreatedAt: 12},
	}
	require.NoError(t, db.Create(&logs).Error)

	latest, hasMore, err := ListPromptAuditLogsBeforeId(21, 0, 2)
	require.NoError(t, err)
	require.True(t, hasMore)
	require.Equal(t, []int{logs[2].Id, logs[1].Id}, promptAuditLogIds(latest))

	older, hasMore, err := ListPromptAuditLogsBeforeId(21, logs[1].Id, 2)
	require.NoError(t, err)
	require.False(t, hasMore)
	require.Equal(t, []int{logs[0].Id}, promptAuditLogIds(older))

	newer, hasMore, err := ListPromptAuditLogsAfterId(21, logs[0].Id, 1)
	require.NoError(t, err)
	require.True(t, hasMore)
	require.Equal(t, []int{logs[1].Id}, promptAuditLogIds(newer))

	newest, hasMore, err := ListPromptAuditLogsAfterId(21, logs[1].Id, 2)
	require.NoError(t, err)
	require.False(t, hasMore)
	require.Equal(t, []int{logs[2].Id}, promptAuditLogIds(newest))
}

func TestListContentModerationLogsReturnsAllowedCountWithoutAllowedItems(t *testing.T) {
	db := openPromptAuditTestDB(t)
	setPromptAuditTestDatabases(t, db, db)

	logs := []PromptAuditLog{
		{UserId: 31, Prompt: "allowed", PromptHash: "allow", MatchedWords: `["moderation:allow"]`, Action: PromptAuditActionRecorded, CreatedAt: 1},
		{UserId: 31, Prompt: "observed hit", PromptHash: "hit", MatchedWords: `["moderation:sexual"]`, Hit: true, Action: PromptAuditActionHit, CreatedAt: 2},
		{UserId: 31, Prompt: "user audit", PromptHash: "user", MatchedWords: `[]`, Action: PromptAuditActionRecorded, CreatedAt: 3},
	}
	require.NoError(t, db.Create(&logs).Error)

	items, total, err := ListContentModerationLogs(ContentModerationLogFilter{}, nil)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Equal(t, []int{logs[1].Id}, promptAuditLogIds(items))

	items, total, err = ListContentModerationLogs(ContentModerationLogFilter{Action: PromptAuditActionRecorded}, nil)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Empty(t, items)
}

func TestSanitizeAllowedContentModerationCountsRemovesRequestDetails(t *testing.T) {
	db := openPromptAuditTestDB(t)
	setPromptAuditTestDatabases(t, db, db)
	require.NoError(t, db.Create(&PromptAuditLog{
		UserId: 7, Username: "alice", TokenId: 9, TokenName: "secret", RequestId: "req-1",
		ModelName: "gpt-test", Protocol: "openai", Endpoint: "/v1/responses",
		Prompt: "benign content", PromptHash: "hash", MatchedWords: `["moderation:allow"]`,
		Action: PromptAuditActionRecorded, CreatedAt: 100, Score: 0.1,
	}).Error)

	require.NoError(t, SanitizeAllowedContentModerationCounts())
	var count PromptAuditLog
	require.NoError(t, db.First(&count).Error)
	require.Zero(t, count.UserId)
	require.Zero(t, count.TokenId)
	require.Empty(t, count.Username)
	require.Empty(t, count.TokenName)
	require.Empty(t, count.RequestId)
	require.Empty(t, count.ModelName)
	require.Empty(t, count.Protocol)
	require.Empty(t, count.Endpoint)
	require.Empty(t, count.Prompt)
	require.Empty(t, count.PromptHash)
	require.Equal(t, `["moderation:allow"]`, count.MatchedWords)
	require.Equal(t, int64(100), count.CreatedAt)
}

func TestHasContentModerationObservedHit(t *testing.T) {
	db := openPromptAuditTestDB(t)
	setPromptAuditTestDatabases(t, db, db)

	logs := []PromptAuditLog{
		{UserId: 41, Prompt: "ordinary hit", PromptHash: "ordinary", MatchedWords: `["sensitive"]`, Hit: true, Action: PromptAuditActionHit, CreatedAt: 1},
		{UserId: 42, Prompt: "moderation hit", PromptHash: "moderation", ModerationPolicyHash: "policy-v2", MatchedWords: `["moderation:sexual"]`, Hit: true, Action: PromptAuditActionHit, CreatedAt: 2},
	}
	require.NoError(t, db.Create(&logs).Error)

	hasHit, err := HasContentModerationObservedHit(41, "policy-v2")
	require.NoError(t, err)
	require.False(t, hasHit)

	hasHit, err = HasContentModerationObservedHit(42, "policy-v2")
	require.NoError(t, err)
	require.True(t, hasHit)

	hasHit, err = HasContentModerationObservedHit(42, "new-policy")
	require.NoError(t, err)
	require.False(t, hasHit)
}

func TestEnsureSystemPromptAuditPolicyCreatesOnceAndPreservesExisting(t *testing.T) {
	db := openPromptAuditTestDB(t)
	require.NoError(t, db.AutoMigrate(&User{}))
	setPromptAuditTestDatabases(t, db, db)
	require.NoError(t, db.Create(&User{Id: 51, Username: "observed-user", Password: "password"}).Error)

	created, err := EnsureSystemPromptAuditPolicy(51)
	require.NoError(t, err)
	require.True(t, created)

	policy, err := GetPromptAuditPolicyByUserId(51)
	require.NoError(t, err)
	require.True(t, policy.MonitorEnabled)
	require.Zero(t, policy.CreatedBy)

	created, err = EnsureSystemPromptAuditPolicy(51)
	require.NoError(t, err)
	require.False(t, created)

	policy.CreatedBy = 99
	require.NoError(t, db.Model(policy).Update("created_by", policy.CreatedBy).Error)
	created, err = EnsureSystemPromptAuditPolicy(51)
	require.NoError(t, err)
	require.False(t, created)
	require.NoError(t, db.First(policy, policy.Id).Error)
	require.Equal(t, 99, policy.CreatedBy)
}

func promptAuditLogIds(logs []PromptAuditLog) []int {
	ids := make([]int, len(logs))
	for index, log := range logs {
		ids[index] = log.Id
	}
	return ids
}

func openPromptAuditTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&PromptAuditPolicy{}, &PromptAuditLog{}))
	return db
}

func setPromptAuditTestDatabases(t *testing.T, mainDB *gorm.DB, logDB *gorm.DB) {
	t.Helper()
	originalDB := DB
	originalLogDB := LOG_DB
	DB = mainDB
	LOG_DB = logDB
	t.Cleanup(func() {
		DB = originalDB
		LOG_DB = originalLogDB
	})
}

func createPromptAuditTestData(t *testing.T, mainDB *gorm.DB, logDB *gorm.DB, userId int) *PromptAuditPolicy {
	t.Helper()
	policy := &PromptAuditPolicy{
		UserId:         userId,
		MonitorEnabled: true,
		DelaySeconds:   PromptAuditDefaultDelaySeconds,
		CreatedAt:      1,
		UpdatedAt:      1,
	}
	require.NoError(t, mainDB.Create(policy).Error)
	logs := []PromptAuditLog{
		{UserId: userId, Prompt: "first", PromptHash: "a", MatchedWords: "[]", CreatedAt: 1},
		{UserId: userId, Prompt: "second", PromptHash: "b", MatchedWords: "[]", CreatedAt: 2},
	}
	require.NoError(t, logDB.Create(&logs).Error)
	if mainDB == logDB {
		require.NoError(t, logDB.Create(&PromptAuditLog{UserId: 8, Prompt: "other", PromptHash: "c", MatchedWords: "[]", CreatedAt: 3}).Error)
	}
	return policy
}

func assertPromptAuditDataDeleted(t *testing.T, mainDB *gorm.DB, logDB *gorm.DB, policyId int, userId int) {
	t.Helper()
	var policyCount int64
	require.NoError(t, mainDB.Model(&PromptAuditPolicy{}).Where("id = ?", policyId).Count(&policyCount).Error)
	require.Zero(t, policyCount)

	var logCount int64
	require.NoError(t, logDB.Model(&PromptAuditLog{}).Where("user_id = ?", userId).Count(&logCount).Error)
	require.Zero(t, logCount)
}
