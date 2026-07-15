package dto

import "testing"

func TestCacheCreationTokensTotal(t *testing.T) {
	tests := []struct {
		name    string
		details InputTokenDetails
		want    int
	}{
		{name: "legacy creation", details: InputTokenDetails{CachedCreationTokens: 12}, want: 12},
		{name: "native write", details: InputTokenDetails{CacheWriteTokens: 18}, want: 18},
		{name: "deduplicate by maximum", details: InputTokenDetails{CachedCreationTokens: 12, CacheWriteTokens: 18}, want: 18},
		{name: "negative upstream values", details: InputTokenDetails{CachedCreationTokens: -1, CacheWriteTokens: -2}, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.details.CacheCreationTokensTotal(); got != tt.want {
				t.Fatalf("CacheCreationTokensTotal() = %d, want %d", got, tt.want)
			}
		})
	}
}
