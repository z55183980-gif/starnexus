package model

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestSearchUsers(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&User{}); err != nil {
		t.Fatal(err)
	}
	originalDB := DB
	DB = db
	t.Cleanup(func() { DB = originalDB })
	sqlDB, _ := db.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })

	users := []User{
		{Id: 1, Username: "d"},
		{Id: 2, Username: "中"},
		{Id: 3, Username: "12"},
		{Id: 4, Username: "danny"},
		{Id: 5, Username: "mail", Email: "d@example.com"},
		{Id: 6, Username: "named", DisplayName: "d"},
		{Id: 7, Username: "中文"},
		{Id: 8, Username: "user12"},
		{Id: 9, Username: "d_other", InviterId: 200},
		{Id: 10, Username: "d_admin", Role: common.RoleAdminUser},
		{Id: 11, Username: "d_disabled", Status: common.UserStatusDisabled},
		{Id: 12, Username: "someone"},
		{Id: 13, Username: "d_group", Group: "other"},
		{Id: 14, Username: "d%_"},
	}
	for i := range users {
		u := &users[i]
		u.Password = "secret"
		u.AffCode = fmt.Sprintf("S%04d", u.Id)
		if u.InviterId == 0 {
			u.InviterId = 100
		}
		if u.Role == 0 {
			u.Role = common.RoleCommonUser
		}
		if u.Status == 0 {
			u.Status = common.UserStatusEnabled
		}
		if u.Group == "" {
			u.Group = "default"
		}
	}
	if err := db.Create(&users).Error; err != nil {
		t.Fatal(err)
	}
	enabled, disabled := common.UserStatusEnabled, common.UserStatusDisabled
	commonRole, adminRole := common.RoleCommonUser, common.RoleAdminUser
	tests := []struct {
		name    string
		keyword string
		exact   bool
		group   string
		status  *int
		role    *int
		inviter int
		offset  int
		limit   int
		ids     []int
		total   int64
	}{
		{name: "short name before pagination", keyword: "d", limit: 2, ids: []int{1, 14}, total: 9},
		{name: "stable second page", keyword: "d", offset: 2, limit: 2, ids: []int{13, 11}, total: 9},
		{name: "exact short name", keyword: "d", exact: true, ids: []int{1}, total: 1},
		{name: "normalized name", keyword: " D ", exact: true, ids: []int{1}, total: 1},
		{name: "normalized fuzzy priority", keyword: " D ", limit: 1, ids: []int{1}, total: 9},
		{name: "single Chinese character", keyword: "中", limit: 1, ids: []int{2}, total: 2},
		{name: "exact Chinese character", keyword: "中", exact: true, ids: []int{2}, total: 1},
		{name: "numeric username beats id", keyword: "12", ids: []int{3, 12, 8}, total: 3},
		{name: "exact numeric ignores id", keyword: "12", exact: true, ids: []int{3}, total: 1},
		{name: "exact ignores email", keyword: "d@example.com", exact: true},
		{name: "literal wildcard", keyword: "d%_", exact: true, ids: []int{14}, total: 1},
		{name: "parameterized keyword", keyword: "' OR 1=1 --", exact: true},
		{name: "combined filters", keyword: "d", exact: true, group: "default", status: &enabled, role: &commonRole, ids: []int{1}, total: 1},
		{name: "status excludes exact", keyword: "d", exact: true, status: &disabled},
		{name: "role excludes exact", keyword: "d", exact: true, role: &adminRole},
		{name: "group excludes exact", keyword: "d", exact: true, group: "other"},
		{name: "agent priority", keyword: "d", inviter: 100, limit: 2, ids: []int{1, 14}, total: 7},
		{name: "agent exact", keyword: "d", exact: true, inviter: 100, ids: []int{1}, total: 1},
		{name: "agent cannot see other owner", keyword: "d", exact: true, inviter: 200},
		{name: "agent cannot see admin", keyword: "d_admin", exact: true, inviter: 100},
		{name: "agent combined filters", keyword: "d", inviter: 100, group: "default", status: &enabled, role: &commonRole, ids: []int{1, 14, 6, 5, 4}, total: 5},
		{name: "empty search keeps descending id", keyword: " ", limit: 2, ids: []int{14, 13}, total: 14},
		{name: "empty exact keeps list", exact: true, limit: 2, ids: []int{14, 13}, total: 14},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			limit := tt.limit
			if limit == 0 {
				limit = 20
			}
			var result []*User
			var total int64
			var err error
			if tt.inviter != 0 {
				result, total, err = SearchUsersByInviterId(tt.inviter, tt.keyword, tt.group, tt.status, tt.role, tt.exact, tt.offset, limit)
			} else {
				result, total, err = SearchUsers(tt.keyword, tt.group, tt.status, tt.role, tt.exact, tt.offset, limit)
			}
			if err != nil {
				t.Fatal(err)
			}
			var ids []int
			for _, user := range result {
				ids = append(ids, user.Id)
				if user.Password != "" {
					t.Fatal("search exposed password")
				}
			}
			if !reflect.DeepEqual(ids, tt.ids) || total != tt.total {
				t.Fatalf("got ids=%v total=%d, want ids=%v total=%d", ids, total, tt.ids, tt.total)
			}
		})
	}
}
