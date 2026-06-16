package service

import "testing"

func TestEpusdtSign(t *testing.T) {
	params := map[string]string{
		"amount":       "100",
		"notify_url":   "https://xxx/notify",
		"order_id":     "123",
		"redirect_url": "https://xxx/success",
	}
	got := EpusdtSign(params, "YOUR_TOKEN")
	want := "e1803cbedb37d78a0fc158c487b002fb"
	if got != want {
		t.Fatalf("EpusdtSign() = %q, want %q", got, want)
	}
}
