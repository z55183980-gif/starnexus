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
