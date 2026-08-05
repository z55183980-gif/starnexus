package ratio_setting

import "testing"

func TestNormalizeVideoResolution(t *testing.T) {
	cases := map[string]string{
		"":       "",
		"default": "default",
		"720P":   "720p",
		"1080":   "1080p",
		"2K":     "4k",
		"2k":     "4k",
		"4K":     "4k",
		"2160p":  "4k",
	}
	for in, want := range cases {
		if got := NormalizeVideoResolution(in); got != want {
			t.Fatalf("NormalizeVideoResolution(%q)=%q want %q", in, got, want)
		}
	}
}

func TestGetConfiguredVideoTokenRatio(t *testing.T) {
	raw := `{
		"dreamina-seedance-2-0-260128": {
			"default": {"base": 46, "with_video": 28},
			"480p": {"base": 46, "with_video": 28},
			"720p": {"base": 46, "with_video": 28},
			"1080p": {"base": 51, "with_video": 31},
			"4k": {"base": 26, "with_video": 16}
		}
	}`
	if err := UpdateVideoTokenPriceByJSONString(raw); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = UpdateVideoTokenPriceByJSONString("{}")
	})

	r, ok := GetConfiguredVideoTokenRatio("dreamina-seedance-2-0-260128", "720p", false)
	if !ok || r != 1.0 {
		t.Fatalf("720p no-video = %v ok=%v; want 1", r, ok)
	}
	r, ok = GetConfiguredVideoTokenRatio("dreamina-seedance-2-0-260128", "", true)
	if !ok || abs(r-28.0/46.0) > 1e-9 {
		t.Fatalf("default with-video = %v ok=%v", r, ok)
	}
	r, ok = GetConfiguredVideoTokenRatio("dreamina-seedance-2-0-260128", "1080p", false)
	if !ok || abs(r-51.0/46.0) > 1e-9 {
		t.Fatalf("1080p = %v ok=%v", r, ok)
	}
	r, ok = GetConfiguredVideoTokenRatio("dreamina-seedance-2-0-260128", "2K", true)
	if !ok || abs(r-16.0/46.0) > 1e-9 {
		t.Fatalf("2K with-video = %v ok=%v", r, ok)
	}
	if _, ok := GetConfiguredVideoTokenRatio("unknown", "720p", false); ok {
		t.Fatal("unknown should miss")
	}
}

func TestGetConfiguredVideoTokenRatioIncompleteFallsBack(t *testing.T) {
	// Only 4k configured — 720p / with-video miss must not shadow hardcoded.
	raw := `{
		"dreamina-seedance-2-0-260128": {
			"4k": {"base": 26, "with_video": 16}
		}
	}`
	if err := UpdateVideoTokenPriceByJSONString(raw); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = UpdateVideoTokenPriceByJSONString("{}")
	})
	if _, ok := GetConfiguredVideoTokenRatio("dreamina-seedance-2-0-260128", "720p", false); ok {
		t.Fatal("incomplete base must not claim configured hit")
	}
	if _, ok := GetConfiguredVideoTokenRatio("dreamina-seedance-2-0-260128", "4k", false); ok {
		t.Fatal("4k without default/720p base must not claim configured hit")
	}
}

func TestIsSeedanceVideoModel(t *testing.T) {
	if !IsSeedanceVideoModel("dreamina-seedance-2-0-260128") {
		t.Fatal("expected 2.0 dreamina true")
	}
	if IsSeedanceVideoModel("doubao-seedance-1-0-pro-250528") {
		t.Fatal("1.0 should not use 2.x matrix UI")
	}
	if IsSeedanceVideoModel("gpt-4o") {
		t.Fatal("unrelated model")
	}
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
