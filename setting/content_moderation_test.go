package setting

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
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

func TestParseContentModerationConfigJSONLegacyAllGroupsDefault(t *testing.T) {
	cfg, err := ParseContentModerationConfigJSON(`{"enabled":true,"mode":"observe"}`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.AllGroups {
		t.Fatal("legacy config without all_groups should default to all groups")
	}
	if cfg.ModelFilter.Type != ContentModerationModelFilterAll {
		t.Fatalf("unexpected model filter: %#v", cfg.ModelFilter)
	}
}

func TestParseContentModerationConfigJSONAuditScope(t *testing.T) {
	cfg, err := ParseContentModerationConfigJSON(`{
		"enabled":true,
		"all_groups":false,
		"groups":[" vip ", "default", "vip"],
		"model_filter":{"type":"include","models":["gpt-4o","GPT-4o"]}
	}`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AllGroups {
		t.Fatal("expected selected groups")
	}
	if len(cfg.Groups) != 2 || cfg.Groups[0] != "vip" || cfg.Groups[1] != "default" {
		t.Fatalf("unexpected groups: %#v", cfg.Groups)
	}
	if cfg.ModelFilter.Type != ContentModerationModelFilterInclude {
		t.Fatalf("unexpected filter type: %s", cfg.ModelFilter.Type)
	}
	if len(cfg.ModelFilter.Models) != 1 || cfg.ModelFilter.Models[0] != "gpt-4o" {
		t.Fatalf("unexpected models: %#v", cfg.ModelFilter.Models)
	}
}

func TestContentModerationConfigJSONRoundTripAuditScope(t *testing.T) {
	in := `{
		"enabled":true,
		"mode":"pre_block",
		"base_url":"https://api.openai.com",
		"model":"omni-moderation-latest",
		"api_keys":["****abcd"],
		"timeout_ms":3000,
		"all_groups":false,
		"groups":["vip","default"],
		"model_filter":{"type":"include","models":["gpt-4o"]},
		"thresholds":{"sexual":0.65}
	}`
	cfg, err := ParseContentModerationConfigJSON(in, []string{"sk-real-key-abcd"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AllGroups {
		t.Fatal("expected all_groups=false after parse")
	}
	out := ContentModerationConfigJSON(cfg)
	if !strings.Contains(out, `"all_groups":false`) {
		t.Fatalf("persist JSON dropped all_groups=false: %s", out)
	}
	if !strings.Contains(out, `"vip"`) || !strings.Contains(out, `"default"`) {
		t.Fatalf("persist JSON dropped groups: %s", out)
	}
	if !strings.Contains(out, `"include"`) || !strings.Contains(out, `"gpt-4o"`) {
		t.Fatalf("persist JSON dropped model_filter: %s", out)
	}

	cfg2, err := ParseContentModerationConfigJSON(out, cfg.APIKeys)
	if err != nil {
		t.Fatal(err)
	}
	if cfg2.AllGroups || len(cfg2.Groups) != 2 || cfg2.ModelFilter.Type != ContentModerationModelFilterInclude {
		t.Fatalf("reload lost audit scope: %#v", cfg2)
	}

	view := ContentModerationConfigToView(cfg2)
	viewBytes, err := common.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	viewJSON := string(viewBytes)
	if !strings.Contains(viewJSON, `"all_groups":false`) {
		t.Fatalf("view JSON dropped all_groups=false: %s", viewJSON)
	}
	if !strings.Contains(viewJSON, `"vip"`) {
		t.Fatalf("view JSON dropped groups: %s", viewJSON)
	}
}

func TestContentModerationIncludesGroupAndModel(t *testing.T) {
	cfg := ContentModerationConfig{
		AllGroups: false,
		Groups:    []string{"vip"},
		ModelFilter: ContentModerationModelFilter{
			Type:   ContentModerationModelFilterExclude,
			Models: []string{"gpt-test"},
		},
	}
	if cfg.IncludesGroup("default") {
		t.Fatal("default should be out of group scope")
	}
	if !cfg.IncludesGroup("VIP") {
		t.Fatal("vip should match case-insensitively")
	}
	if cfg.IncludesGroup("") {
		t.Fatal("empty group should be out of scope")
	}
	if cfg.IncludesModel("gpt-test") {
		t.Fatal("excluded model should be out of scope")
	}
	if !cfg.IncludesModel("claude-opus") {
		t.Fatal("non-excluded model should be in scope")
	}

	all := ContentModerationConfig{AllGroups: true}
	if !all.IncludesGroup("") || !all.IncludesModel("anything") {
		t.Fatal("all groups / all models should include everything")
	}
}
