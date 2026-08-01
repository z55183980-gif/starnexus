package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
)

func TestNormalizeUserStatisticsRankingQuery(t *testing.T) {
	tests := []struct {
		name          string
		rankingType   string
		limit         string
		wantType      model.UserStatisticsRankingType
		wantLimit     int
		wantValidated bool
	}{
		{name: "recent defaults to ten", rankingType: "recent", wantType: model.UserStatisticsRankingRecent, wantLimit: 10, wantValidated: true},
		{name: "normalizes type and accepts twenty", rankingType: " Usage ", limit: "20", wantType: model.UserStatisticsRankingUsage, wantLimit: 20, wantValidated: true},
		{name: "accepts balance", rankingType: "balance", limit: "10", wantType: model.UserStatisticsRankingBalance, wantLimit: 10, wantValidated: true},
		{name: "rejects unknown type", rankingType: "quota", limit: "10"},
		{name: "rejects unsupported limit", rankingType: "recent", limit: "15"},
		{name: "rejects invalid limit", rankingType: "recent", limit: "many"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotType, gotLimit, gotValidated := normalizeUserStatisticsRankingQuery(tt.rankingType, tt.limit)
			if gotValidated != tt.wantValidated {
				t.Fatalf("normalizeUserStatisticsRankingQuery() validated = %v, want %v", gotValidated, tt.wantValidated)
			}
			if !tt.wantValidated {
				return
			}
			if gotType != tt.wantType || gotLimit != tt.wantLimit {
				t.Fatalf("normalizeUserStatisticsRankingQuery() = (%q, %d), want (%q, %d)", gotType, gotLimit, tt.wantType, tt.wantLimit)
			}
		})
	}
}
