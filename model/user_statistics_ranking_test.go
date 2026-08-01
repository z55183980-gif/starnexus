package model

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func setupUserStatisticsRankingTestDB(t *testing.T) *gorm.DB {
	t.Helper()

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
	return db
}

func createUserStatisticsRankingUsers(t *testing.T, db *gorm.DB) []User {
	t.Helper()

	users := make([]User, 0, 12)
	for index := 1; index <= 12; index++ {
		users = append(users, User{
			Username:     fmt.Sprintf("ranking-%02d", index),
			Password:     "password",
			AffCode:      fmt.Sprintf("R%03d", index),
			Role:         common.RoleCommonUser,
			Status:       common.UserStatusEnabled,
			Group:        "default",
			Quota:        index * 100,
			UsedQuota:    (13 - index) * 200,
			RequestCount: index * 10,
			CreatedAt:    int64(index * 1_000),
		})
	}
	if err := db.Create(&users).Error; err != nil {
		t.Fatalf("create users: %v", err)
	}
	return users
}

func TestGetUserStatisticsRankingSortsAndLimitsResults(t *testing.T) {
	db := setupUserStatisticsRankingTestDB(t)
	users := createUserStatisticsRankingUsers(t, db)

	tests := []struct {
		name        string
		rankingType UserStatisticsRankingType
		wantFirstId int
	}{
		{name: "recent", rankingType: UserStatisticsRankingRecent, wantFirstId: users[11].Id},
		{name: "usage", rankingType: UserStatisticsRankingUsage, wantFirstId: users[0].Id},
		{name: "balance", rankingType: UserStatisticsRankingBalance, wantFirstId: users[11].Id},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := GetUserStatisticsRanking(nil, tt.rankingType, 10)
			if err != nil {
				t.Fatalf("GetUserStatisticsRanking() error = %v", err)
			}
			if len(result.Items) != 10 {
				t.Fatalf("item count = %d, want 10", len(result.Items))
			}
			if !result.HasMore {
				t.Fatal("HasMore = false, want true")
			}
			if result.Items[0].Id != tt.wantFirstId {
				t.Fatalf("first id = %d, want %d", result.Items[0].Id, tt.wantFirstId)
			}
		})
	}

	expanded, err := GetUserStatisticsRanking(nil, UserStatisticsRankingRecent, 20)
	if err != nil {
		t.Fatalf("expanded ranking error = %v", err)
	}
	if len(expanded.Items) != 12 || expanded.HasMore {
		t.Fatalf("expanded ranking = (%d items, has_more=%v), want (12, false)", len(expanded.Items), expanded.HasMore)
	}
}

func TestGetUserStatisticsRankingRespectsInviterScope(t *testing.T) {
	db := setupUserStatisticsRankingTestDB(t)
	inviterId := 99
	users := []User{
		{Username: "invited-one", Password: "password", AffCode: "RS01", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default", InviterId: inviterId, UsedQuota: 100},
		{Username: "invited-two", Password: "password", AffCode: "RS02", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default", InviterId: inviterId, UsedQuota: 200},
		{Username: "other-user", Password: "password", AffCode: "RS03", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default", InviterId: inviterId + 1, UsedQuota: 300},
	}
	if err := db.Create(&users).Error; err != nil {
		t.Fatalf("create scoped users: %v", err)
	}

	result, err := GetUserStatisticsRanking(&inviterId, UserStatisticsRankingUsage, 10)
	if err != nil {
		t.Fatalf("GetUserStatisticsRanking() error = %v", err)
	}
	if len(result.Items) != 2 {
		t.Fatalf("item count = %d, want 2", len(result.Items))
	}
	if result.Items[0].Username != "invited-two" {
		t.Fatalf("first username = %q, want invited-two", result.Items[0].Username)
	}
}
