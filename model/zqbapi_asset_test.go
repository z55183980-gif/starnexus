package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func setupZQBAPIRegistryTestDB(t *testing.T) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/registry.db"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&ZQBAPIAsset{}, &Task{}); err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	originalDB := DB
	DB = db
	t.Cleanup(func() {
		DB = originalDB
		_ = sqlDB.Close()
	})
}

func TestClaimZQBAPIAssetIsExclusiveAndCanTakeExpiredLease(t *testing.T) {
	setupZQBAPIRegistryTestDB(t)
	first, owner, err := ClaimZQBAPIAsset(&ZQBAPIAsset{CacheKey: "key-1"}, "node-a", time.Minute)
	if err != nil || !owner {
		t.Fatalf("first claim owner=%v err=%v", owner, err)
	}
	second, owner, err := ClaimZQBAPIAsset(&ZQBAPIAsset{CacheKey: "key-1"}, "node-b", time.Minute)
	if err != nil || owner || second.ID != first.ID || second.LeaseOwner != "node-a" {
		t.Fatalf("second claim owner=%v record=%+v err=%v", owner, second, err)
	}
	if err := UpdateZQBAPIAssetState(first.ID, map[string]any{"lease_until": time.Now().Add(-time.Minute).Unix()}); err != nil {
		t.Fatal(err)
	}
	taken, owner, err := ClaimZQBAPIAsset(&ZQBAPIAsset{CacheKey: "key-1"}, "node-b", time.Minute)
	if err != nil || !owner || taken.LeaseOwner != "node-b" {
		t.Fatalf("expired lease claim owner=%v record=%+v err=%v", owner, taken, err)
	}
}

func TestClaimZQBAPIAssetReusesActiveRecord(t *testing.T) {
	setupZQBAPIRegistryTestDB(t)
	record, owner, err := ClaimZQBAPIAsset(&ZQBAPIAsset{CacheKey: "key-active"}, "node-a", time.Minute)
	if err != nil || !owner {
		t.Fatal(err)
	}
	if _, err := UpdateZQBAPIAssetOwned(record.ID, "node-a", map[string]any{
		"status": ZQBAPIAssetStatusActive, "asset_id": "asset-1", "lease_owner": "", "lease_until": 0,
		"expires_at": time.Now().Add(time.Hour).Unix(),
	}); err != nil {
		t.Fatal(err)
	}
	reused, owner, err := ClaimZQBAPIAsset(&ZQBAPIAsset{CacheKey: "key-active"}, "node-b", time.Minute)
	if err != nil || owner || reused.AssetID != "asset-1" {
		t.Fatalf("reuse owner=%v record=%+v err=%v", owner, reused, err)
	}
}

func TestUpdateTaskResultFileOnlyChangesDedicatedColumn(t *testing.T) {
	setupZQBAPIRegistryTestDB(t)
	task := &Task{TaskID: "task-cache", Status: TaskStatusSuccess, PrivateData: TaskPrivateData{ResultURL: "https://example.com/original.mp4"}}
	if err := DB.Create(task).Error; err != nil {
		t.Fatal(err)
	}
	if err := UpdateTaskResultFile(task.ID, "zqbapi-result.mp4"); err != nil {
		t.Fatal(err)
	}
	var got Task
	if err := DB.First(&got, task.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.ResultFile != "zqbapi-result.mp4" || got.PrivateData.ResultURL != task.PrivateData.ResultURL {
		t.Fatalf("unexpected task after cache update: %+v", got)
	}
}

func TestListRecentTaskResultsForArchiveUsesPortableStableCursor(t *testing.T) {
	setupZQBAPIRegistryTestDB(t)
	platform := constant.TaskPlatform("61")
	now := time.Now().Unix()
	tasks := []*Task{
		{TaskID: "older", Platform: platform, Status: TaskStatusSuccess, UpdatedAt: now - 10},
		{TaskID: "newer", Platform: platform, Status: TaskStatusPendingSettlement, UpdatedAt: now},
		{TaskID: "failed", Platform: platform, Status: TaskStatusFailure, UpdatedAt: now},
		{TaskID: "other-platform", Platform: constant.TaskPlatform("1"), Status: TaskStatusSuccess, UpdatedAt: now},
	}
	for _, task := range tasks {
		if err := DB.Create(task).Error; err != nil {
			t.Fatal(err)
		}
	}

	first, err := ListRecentTaskResultsForArchive(platform, 0, now-60, 1)
	if err != nil || len(first) != 1 || first[0].TaskID != "newer" {
		t.Fatalf("first=%v err=%v", first, err)
	}
	second, err := ListRecentTaskResultsForArchive(platform, first[0].ID, now-60, 10)
	if err != nil || len(second) != 1 || second[0].TaskID != "older" {
		t.Fatalf("second=%v err=%v", second, err)
	}
}
