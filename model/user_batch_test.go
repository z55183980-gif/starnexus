package model

import (
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestBatchUpdateUserGroup(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&User{}); err != nil {
		t.Fatalf("migrate users: %v", err)
	}

	originalDB := DB
	DB = db
	t.Cleanup(func() { DB = originalDB })

	users := []User{
		{Username: "batch-one", Password: "password", AffCode: "B001", Group: "default"},
		{Username: "batch-two", Password: "password", AffCode: "B002", Group: "default"},
		{Username: "untouched", Password: "password", AffCode: "B003", Group: "default"},
	}
	if err := db.Create(&users).Error; err != nil {
		t.Fatalf("create users: %v", err)
	}

	ids := []int{users[0].Id, users[1].Id}
	if err := BatchUpdateUserGroup(ids, "premium"); err != nil {
		t.Fatalf("BatchUpdateUserGroup() error = %v", err)
	}

	var updated []User
	if err := db.Where("id IN ?", ids).Order("id").Find(&updated).Error; err != nil {
		t.Fatalf("load updated users: %v", err)
	}
	for _, user := range updated {
		if user.Group != "premium" {
			t.Fatalf("user %d group = %q, want premium", user.Id, user.Group)
		}
	}

	var untouched User
	if err := db.First(&untouched, users[2].Id).Error; err != nil {
		t.Fatalf("load untouched user: %v", err)
	}
	if untouched.Group != "default" {
		t.Fatalf("untouched user group = %q, want default", untouched.Group)
	}
}

func TestGetUsersByIdsUnscopedIncludesSoftDeletedUsers(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&User{}); err != nil {
		t.Fatalf("migrate users: %v", err)
	}

	originalDB := DB
	DB = db
	t.Cleanup(func() { DB = originalDB })

	users := []User{
		{Username: "active-user", Password: "password", AffCode: "B011", Group: "default"},
		{Username: "deleted-user", Password: "password", AffCode: "B012", Group: "default"},
	}
	if err := db.Create(&users).Error; err != nil {
		t.Fatalf("create users: %v", err)
	}
	if err := db.Delete(&users[1]).Error; err != nil {
		t.Fatalf("soft delete user: %v", err)
	}

	got, err := GetUsersByIdsUnscoped([]int{users[0].Id, users[1].Id})
	if err != nil {
		t.Fatalf("GetUsersByIdsUnscoped() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("GetUsersByIdsUnscoped() returned %d users, want 2", len(got))
	}
}
