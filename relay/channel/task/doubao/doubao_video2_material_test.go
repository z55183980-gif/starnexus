package doubao

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
)

func TestDoubaoVideo2MaterialUsesChannelApiKeyAndAssetURL(t *testing.T) {
	var createCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("ApiKey") != "kz-test-key" {
			t.Fatalf("ApiKey header = %q", r.Header.Get("ApiKey"))
		}
		switch r.URL.Query().Get("Action") {
		case "CreateAsset":
			createCalls.Add(1)
			_, _ = w.Write([]byte(`{"ResponseMetadata":{"RequestId":"create-1"},"Result":{"Id":"asset-62"}}`))
		case "GetAsset":
			_, _ = w.Write([]byte(`{"ResponseMetadata":{"RequestId":"get-1"},"Result":{"Id":"asset-62","Status":"Active"}}`))
		default:
			t.Fatalf("unexpected action %q", r.URL.Query().Get("Action"))
		}
	}))
	defer server.Close()

	config := doubaoVideo2MaterialConfig{ChannelID: 62, APIKey: "kz-test-key", GroupID: "group-62", ProviderURL: server.URL}
	inlineImage := "data:image/png;base64," + base64.StdEncoding.EncodeToString(testDoubaoVideo2PNG(t))
	inspection, err := inspectDoubaoVideo2Image(nil, inlineImage)
	if err != nil {
		t.Fatal(err)
	}
	assetURL, err := prepareDoubaoVideo2Asset(context.Background(), config, server.URL+"/image.png", inspection)
	if err != nil {
		t.Fatal(err)
	}
	if assetURL != "asset://asset-62" || createCalls.Load() != 1 {
		t.Fatalf("asset URL/calls = %q/%d", assetURL, createCalls.Load())
	}
}

func TestDoubaoVideo2CreateAssetIsNotBlindlyRetried(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "temporary", http.StatusBadGateway)
	}))
	defer server.Close()
	config := doubaoVideo2MaterialConfig{APIKey: "kz-test-key", GroupID: "group-62", ProviderURL: server.URL}
	_, err := createDoubaoVideo2Asset(context.Background(), config, "https://example.com/image.png", "test")
	if err == nil || calls.Load() != 1 {
		t.Fatalf("err/calls = %v/%d", err, calls.Load())
	}
	classified := classifyDoubaoVideo2CreateError(err)
	var buildErr *doubaoVideo2BuildError
	if !errors.As(classified, &buildErr) || buildErr.Kind != doubaoVideo2ErrorMaterialAmbiguous || buildErr.Temporary() {
		t.Fatalf("classified error = %#v", classified)
	}
}

func TestDoubaoVideo2RetryPrecheckStoresOnlyPublicURLs(t *testing.T) {
	settings := dto.ChannelSettings{DoubaoVideo2: &dto.DoubaoVideo2ChannelSettings{MaterialMode: dto.DoubaoVideo2MaterialModeRetryOnly, GroupID: "group-62"}}
	body := &requestPayload{Content: []ContentItem{{Type: "image_url", ImageURL: &MediaURL{URL: "https://example.com/image.png"}}}}
	if !doubaoVideo2RequestCanRetry(body) {
		t.Fatal("public URL should be retryable")
	}
	if _, err := loadDoubaoVideo2MaterialConfig(62, "kz-key", "https://aiopenapi.kuaizi.cn/ai-open-platform-api", settings); err != nil {
		t.Fatal(err)
	}
	body.Content[0].ImageURL.URL = "data:image/png;base64,AAAA"
	if doubaoVideo2RequestCanRetry(body) {
		t.Fatal("inline data URL must not be persisted for retry")
	}
}

func TestDoubaoVideo2MaterialEndpointPreservesBasePath(t *testing.T) {
	base, _ := url.Parse("https://example.com/ai-open-platform-api")
	endpoint, _ := url.Parse(strings.TrimRight(base.String(), "/") + "/api/support/v1/asset")
	if endpoint.Path != "/ai-open-platform-api/api/support/v1/asset" {
		t.Fatalf("endpoint path = %q", endpoint.Path)
	}
	if constant.ChannelTypeDoubaoVideo2 != 62 {
		t.Fatal("unexpected channel type")
	}
}

func testDoubaoVideo2PNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 512, 512))
	for y := 0; y < 512; y++ {
		for x := 0; x < 512; x++ {
			img.Set(x, y, color.RGBA{R: 32, G: 64, B: 96, A: 255})
		}
	}
	var output bytes.Buffer
	if err := png.Encode(&output, img); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
