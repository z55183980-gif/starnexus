package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

func TestCanManageTargetUserAgentScope(t *testing.T) {
	agentID := 100

	tests := []struct {
		name   string
		target *model.User
		want   bool
	}{
		{
			name: "allows direct common invitee",
			target: &model.User{
				Id:        200,
				Role:      common.RoleCommonUser,
				InviterId: agentID,
			},
			want: true,
		},
		{
			name: "rejects non-invitee common user",
			target: &model.User{
				Id:        201,
				Role:      common.RoleCommonUser,
				InviterId: 999,
			},
			want: false,
		},
		{
			name: "rejects admin even if invited",
			target: &model.User{
				Id:        202,
				Role:      common.RoleAdminUser,
				InviterId: agentID,
			},
			want: false,
		},
		{
			name:   "rejects nil target",
			target: nil,
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := canManageTargetUser(agentID, common.RoleAgentUser, tt.target)
			if got != tt.want {
				t.Fatalf("canManageTargetUser() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCanCreateTargetRole(t *testing.T) {
	tests := []struct {
		name       string
		actorRole  int
		targetRole int
		want       bool
	}{
		{name: "root creates agent", actorRole: common.RoleRootUser, targetRole: common.RoleAgentUser, want: true},
		{name: "root creates admin", actorRole: common.RoleRootUser, targetRole: common.RoleAdminUser, want: true},
		{name: "admin creates common", actorRole: common.RoleAdminUser, targetRole: common.RoleCommonUser, want: true},
		{name: "admin cannot create agent", actorRole: common.RoleAdminUser, targetRole: common.RoleAgentUser, want: false},
		{name: "agent cannot create common", actorRole: common.RoleAgentUser, targetRole: common.RoleCommonUser, want: false},
		{name: "root cannot create root", actorRole: common.RoleRootUser, targetRole: common.RoleRootUser, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := canCreateTargetRole(tt.actorRole, tt.targetRole)
			if got != tt.want {
				t.Fatalf("canCreateTargetRole() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCanUpdateTargetRole(t *testing.T) {
	tests := []struct {
		name       string
		actorRole  int
		targetRole int
		want       bool
	}{
		{name: "root updates to agent", actorRole: common.RoleRootUser, targetRole: common.RoleAgentUser, want: true},
		{name: "admin cannot update to agent", actorRole: common.RoleAdminUser, targetRole: common.RoleAgentUser, want: false},
		{name: "admin can keep common", actorRole: common.RoleAdminUser, targetRole: common.RoleCommonUser, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.actorRole == common.RoleRootUser || tt.targetRole == common.RoleCommonUser
			if got != tt.want {
				t.Fatalf("role update allowed = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCanAgentManageAction(t *testing.T) {
	tests := []struct {
		name   string
		action string
		mode   string
		want   bool
	}{
		{name: "agent cannot enable user", action: "enable", want: false},
		{name: "agent cannot disable user", action: "disable", want: false},
		{name: "agent can add quota", action: "add_quota", mode: "add", want: true},
		{name: "agent cannot subtract quota", action: "add_quota", mode: "subtract", want: false},
		{name: "agent cannot override quota", action: "add_quota", mode: "override", want: false},
		{name: "agent cannot delete user", action: "delete", want: false},
		{name: "agent cannot promote user", action: "promote", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := canAgentManageAction(tt.action, tt.mode)
			if got != tt.want {
				t.Fatalf("canAgentManageAction() = %v, want %v", got, tt.want)
			}
		})
	}
}
