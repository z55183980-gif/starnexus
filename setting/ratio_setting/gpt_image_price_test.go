package ratio_setting

import "testing"

func TestNormalizeGPTImageTier(t *testing.T) {
	tests := map[string]string{
		"":          GPTImageTier2K,
		"auto":      GPTImageTier2K,
		"1024x1024": GPTImageTier1K,
		"1024x1536": GPTImageTier2K,
		"2048x2048": GPTImageTier2K,
		"3840x2160": GPTImageTier4K,
		"bad":       "",
	}
	for input, want := range tests {
		if got := NormalizeGPTImageTier(input); got != want {
			t.Fatalf("NormalizeGPTImageTier(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestGPTImageTierRankTreatsUnspecifiedAsUnset(t *testing.T) {
	if got := GPTImageTierRank(""); got != 0 {
		t.Fatalf("GPTImageTierRank(empty) = %d, want 0", got)
	}
	if got := GPTImageTierRank("auto"); got != 2 {
		t.Fatalf("GPTImageTierRank(auto) = %d, want 2", got)
	}
}

func TestGetGPTImagePriceUsesExactTierAndDatedModelFallback(t *testing.T) {
	if err := UpdateGPTImagePriceByJSONString(`{"gpt-image-2":{"1k":0.04,"2k":0.08,"4k":0.16}}`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = UpdateGPTImagePriceByJSONString("{}") })

	price, ok := GetGPTImagePrice("gpt-image-2-2026-04-21", "1024x1536")
	if !ok || price != 0.08 {
		t.Fatalf("price = %v, ok = %v", price, ok)
	}
	if _, ok := GetGPTImagePrice("gpt-image-2", "4k"); !ok {
		t.Fatal("expected explicitly configured 4k price")
	}
	if _, ok := GetGPTImagePrice("gpt-image-1", "1k"); ok {
		t.Fatal("must not fall back across gpt-image models")
	}
}

func TestUpdateGPTImagePriceRejectsUnrelatedModelsAndNegativePrices(t *testing.T) {
	t.Cleanup(func() { _ = UpdateGPTImagePriceByJSONString("{}") })
	if err := UpdateGPTImagePriceByJSONString(`{"dall-e-3":{"1k":0.04}}`); err == nil {
		t.Fatal("expected unrelated model validation error")
	}
	if err := UpdateGPTImagePriceByJSONString(`{"GPT-Image-2":{"1k":0.04,"2k":0.08,"4k":0.16}}`); err == nil {
		t.Fatal("expected non-normalized model name validation error")
	}
	_ = UpdateGPTImagePriceByJSONString("{}")
	if err := UpdateGPTImagePriceByJSONString(`{"gpt-image-2":{"1k":-1,"2k":0.08,"4k":0.16}}`); err == nil {
		t.Fatal("expected negative price validation error")
	}
	if err := UpdateGPTImagePriceByJSONString(`{"gpt-image-2":{"1k":0.04,"2k":0.08}}`); err == nil {
		t.Fatal("expected incomplete tier validation error")
	}
}
