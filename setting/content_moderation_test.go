package setting

import (
	"strings"
	"testing"
)

func TestParseContentModerationConfigJSONDefaults(t *testing.T) {
	cfg, err := ParseContentModerationConfigJSON("", nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Enabled {
		t.Fatal("expected disabled by default")
	}
	if cfg.Mode != ContentModerationModePreBlock {
		t.Fatalf("unexpected mode: %s", cfg.Mode)
	}
	if cfg.Model != defaultContentModerationModel {
		t.Fatalf("unexpected model: %s", cfg.Model)
	}
	if cfg.TimeoutMS != defaultContentModerationTimeoutMS {
		t.Fatalf("unexpected timeout: %d", cfg.TimeoutMS)
	}
	if len(cfg.Thresholds) == 0 {
		t.Fatal("expected default thresholds")
	}
}

func TestMergeContentModerationAPIKeysKeepsMasked(t *testing.T) {
	existing := []string{"sk-live-real-key-aaaa", "sk-live-real-key-bbbb"}
	incoming := []string{"****aaaa", "sk-live-new-key-cccc"}
	got := mergeContentModerationAPIKeys(existing, incoming)
	if len(got) != 2 {
		t.Fatalf("unexpected keys: %#v", got)
	}
	if got[0] != existing[0] {
		t.Fatalf("expected first key preserved, got %q", got[0])
	}
	if got[1] != "sk-live-new-key-cccc" {
		t.Fatalf("expected second key replaced, got %q", got[1])
	}
}

func TestContentModerationConfigToViewMasksKeys(t *testing.T) {
	view := ContentModerationConfigToView(ContentModerationConfig{
		Enabled: true,
		Mode:    ContentModerationModeObserve,
		APIKeys: []string{"sk-abcdefghijklmnopqrstuvwxyz"},
	})
	if view.Mode != ContentModerationModeObserve {
		t.Fatalf("unexpected mode in view: %q", view.Mode)
	}
	if !view.KeysConfigured || view.APIKeyCount != 1 {
		t.Fatalf("unexpected view meta: %#v", view)
	}
	if strings.Contains(view.APIKeys[0], "abcdefghijklmnop") {
		t.Fatalf("api key leaked in view: %q", view.APIKeys[0])
	}
	if !strings.HasPrefix(view.APIKeys[0], "****") {
		t.Fatalf("expected masked placeholder, got %q", view.APIKeys[0])
	}
}

func TestContentModerationModeNormalize(t *testing.T) {
	cfg, err := ParseContentModerationConfigJSON(`{"enabled":true,"mode":"observe"}`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Mode != ContentModerationModeObserve {
		t.Fatalf("expected observe, got %q", cfg.Mode)
	}

	cfg, err = ParseContentModerationConfigJSON(`{"enabled":true,"mode":"unknown"}`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Mode != ContentModerationModePreBlock {
		t.Fatalf("expected pre_block default, got %q", cfg.Mode)
	}
}
