package zhipu_4v

import "testing"

func TestNormalizeBaseURL(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"https://open.bigmodel.cn", "https://open.bigmodel.cn"},
		{"https://open.bigmodel.cn/", "https://open.bigmodel.cn"},
		{"https://open.bigmodel.cn/api/paas/v4", "https://open.bigmodel.cn"},
		{"https://open.bigmodel.cn/api/paas/v4/", "https://open.bigmodel.cn"},
		{"https://open.bigmodel.cn/v4", "https://open.bigmodel.cn"},
		{"https://open.bigmodel.cn/api/coding/paas/v4", "https://open.bigmodel.cn"},
		{
			"https://open.bigmodel.cn/api/paas/v4/chat/completions",
			"https://open.bigmodel.cn",
		},
		{
			"https://open.bigmodel.cn/api/paas/v3/model-api/glm-4-flash",
			"https://open.bigmodel.cn",
		},
		{"https://api.z.ai/api/paas/v4", "https://api.z.ai"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := NormalizeBaseURL(tt.in); got != tt.want {
			t.Errorf("NormalizeBaseURL(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
