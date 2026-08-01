package controller

import (
	"strings"
	"testing"
)

func TestNormalizeBatchUpdateUserGroupRequest(t *testing.T) {
	tests := []struct {
		name      string
		req       *BatchUpdateUserGroupRequest
		wantValid bool
		wantIds   int
		wantGroup string
	}{
		{
			name:      "trims group and removes duplicate ids",
			req:       &BatchUpdateUserGroupRequest{Ids: []int{3, 1, 3}, Group: " premium "},
			wantValid: true,
			wantIds:   2,
			wantGroup: "premium",
		},
		{
			name:      "rejects empty ids",
			req:       &BatchUpdateUserGroupRequest{Group: "default"},
			wantValid: false,
		},
		{
			name:      "rejects non-positive id",
			req:       &BatchUpdateUserGroupRequest{Ids: []int{1, 0}, Group: "default"},
			wantValid: false,
		},
		{
			name:      "rejects blank group",
			req:       &BatchUpdateUserGroupRequest{Ids: []int{1}, Group: "   "},
			wantValid: false,
		},
		{
			name:      "rejects group longer than 64 unicode characters",
			req:       &BatchUpdateUserGroupRequest{Ids: []int{1}, Group: strings.Repeat("组", 65)},
			wantValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeBatchUpdateUserGroupRequest(tt.req)
			if got != tt.wantValid {
				t.Fatalf("normalizeBatchUpdateUserGroupRequest() = %v, want %v", got, tt.wantValid)
			}
			if tt.wantValid {
				if len(tt.req.Ids) != tt.wantIds {
					t.Fatalf("normalized id count = %d, want %d", len(tt.req.Ids), tt.wantIds)
				}
				if tt.req.Group != tt.wantGroup {
					t.Fatalf("normalized group = %q, want %q", tt.req.Group, tt.wantGroup)
				}
			}
		})
	}
}

func TestNormalizeBatchUpdateUserGroupRequestRejectsOversizedBatch(t *testing.T) {
	ids := make([]int, maxBatchUserCount+1)
	for index := range ids {
		ids[index] = index + 1
	}
	req := &BatchUpdateUserGroupRequest{Ids: ids, Group: "default"}
	if normalizeBatchUpdateUserGroupRequest(req) {
		t.Fatal("expected oversized batch to be rejected")
	}
}
