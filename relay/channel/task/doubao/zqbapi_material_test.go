package doubao

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"

	"golang.org/x/sync/singleflight"
	"gorm.io/gorm"
)

func setupZQBAPIAssetTestDB(t *testing.T) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/zqbapi-assets.db"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.ZQBAPIAsset{}); err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	originalDB := model.DB
	model.DB = db
	zqbapiAssetCache = sync.Map{}
	zqbapiAssetGroup = singleflight.Group{}
	t.Cleanup(func() {
		model.DB = originalDB
		zqbapiAssetCache = sync.Map{}
		zqbapiAssetGroup = singleflight.Group{}
		_ = sqlDB.Close()
	})
}

func TestPrepareZQBAPIAssetRunsUploadCreateAndWaitChain(t *testing.T) {
	setupZQBAPIAssetTestDB(t)
	var mu sync.Mutex
	actions := make([]string, 0, 3)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		action := r.URL.Query().Get("Action")
		mu.Lock()
		actions = append(actions, action)
		mu.Unlock()

		if r.URL.Query().Get("Version") != zqbapiMaterialVersion {
			t.Errorf("Version = %q", r.URL.Query().Get("Version"))
		}
		if !strings.HasPrefix(r.Header.Get("Authorization"), "HMAC-SHA256 Credential=ak-test/") {
			t.Errorf("unexpected Authorization header")
		}
		if r.Header.Get("X-Date") == "" || r.Header.Get("X-Content-Sha256") == "" {
			t.Errorf("signed headers are missing")
		}

		switch action {
		case "UploadAssetFile":
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Errorf("parse upload: %v", err)
			}
			if r.FormValue("AssetType") != "Image" {
				t.Errorf("AssetType = %q", r.FormValue("AssetType"))
			}
			_, _ = io.WriteString(w, `{"ResponseMetadata":{"RequestId":"upload"},"Result":{"Url":"https://material.example/image.jpg","FileName":"image.jpg"}}`)
		case "CreateAsset":
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), `"GroupId":"group-test"`) || !strings.Contains(string(body), `"URL":"https://material.example/image.jpg"`) {
				t.Errorf("unexpected CreateAsset body: %s", body)
			}
			_, _ = io.WriteString(w, `{"ResponseMetadata":{"RequestId":"create"},"Result":{"Id":"asset-test"}}`)
		case "GetAsset":
			_, _ = io.WriteString(w, `{"ResponseMetadata":{"RequestId":"get"},"Result":{"Id":"asset-test","Status":"Active"}}`)
		default:
			http.Error(w, "unexpected action", http.StatusBadRequest)
		}
	}))
	defer server.Close()

	assetURL, err := prepareZQBAPIAsset(context.Background(), zqbapiMaterialConfig{
		AccessKeyID: "ak-test",
		SecretKey:   "secret-test",
		GroupID:     "group-test",
		ProjectName: "default",
		Region:      "cn-beijing",
		ProviderURL: server.URL,
	}, "data:image/jpeg;base64,dGVzdA==", &zqbapiImageInspection{
		Data:     []byte("unique-test-image"),
		MIMEType: "image/jpeg",
	})
	if err != nil {
		t.Fatal(err)
	}
	if assetURL != "asset://asset-test" {
		t.Fatalf("asset URL = %q", assetURL)
	}
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(actions, ",") != "UploadAssetFile,CreateAsset,GetAsset" {
		t.Fatalf("actions = %v", actions)
	}
}

func TestPrepareZQBAPIAssetDoesNotRetryCreateAsset(t *testing.T) {
	setupZQBAPIAssetTestDB(t)
	createCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("Action") {
		case "UploadAssetFile":
			_, _ = io.WriteString(w, `{"ResponseMetadata":{"RequestId":"upload"},"Result":{"Url":"https://material.example/image.jpg"}}`)
		case "CreateAsset":
			createCalls++
			http.Error(w, `{"error":"temporary"}`, http.StatusInternalServerError)
		default:
			http.Error(w, "unexpected action", http.StatusBadRequest)
		}
	}))
	defer server.Close()

	_, err := prepareZQBAPIAsset(context.Background(), zqbapiMaterialConfig{
		AccessKeyID: "ak-test", SecretKey: "secret-test", GroupID: "group-test",
		ProjectName: "default", Region: "cn-beijing", ProviderURL: server.URL,
	}, "", &zqbapiImageInspection{Data: []byte("no-create-retry-image"), MIMEType: "image/jpeg"})
	if err == nil {
		t.Fatal("expected CreateAsset error")
	}
	if createCalls != 1 {
		t.Fatalf("CreateAsset calls = %d, want 1", createCalls)
	}
	var temporary interface{ Temporary() bool }
	if !errors.As(err, &temporary) || !temporary.Temporary() {
		t.Fatalf("error should be temporary: %v", err)
	}
}

func TestNormalizeZQBAPISmallImage(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 200, 400))
	for y := 0; y < 400; y++ {
		for x := 0; x < 200; x++ {
			img.Set(x, y, color.NRGBA{R: 20, G: 40, B: 60, A: 255})
		}
	}
	var source bytes.Buffer
	if err := png.Encode(&source, img); err != nil {
		t.Fatal(err)
	}
	data, mimeType, normalizedImage, normalized, err := normalizeZQBAPIImage(source.Bytes(), "image/png", img, false)
	if err != nil {
		t.Fatal(err)
	}
	if !normalized || mimeType != "image/jpeg" || len(data) == 0 {
		t.Fatalf("normalized=%v mime=%q bytes=%d", normalized, mimeType, len(data))
	}
	bounds := normalizedImage.Bounds()
	if bounds.Dx() != 512 || bounds.Dy() != 1024 {
		t.Fatalf("normalized dimensions = %dx%d", bounds.Dx(), bounds.Dy())
	}
}

func TestValidateZQBAPIImageRejectsUnsupportedRatio(t *testing.T) {
	if err := validateZQBAPIImageConfig(300, 1000); err == nil {
		t.Fatal("expected unsupported aspect ratio error")
	}
}

func TestZQBAPIEXIFOrientationIsApplied(t *testing.T) {
	jpegWithOrientationSix := []byte{
		0xff, 0xd8, 0xff, 0xe1, 0x00, 0x22,
		'E', 'x', 'i', 'f', 0, 0,
		'I', 'I', 0x2a, 0x00, 0x08, 0x00, 0x00, 0x00,
		0x01, 0x00,
		0x12, 0x01, 0x03, 0x00, 0x01, 0x00, 0x00, 0x00, 0x06, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
		0xff, 0xd9,
	}
	if orientation := zqbapiJPEGEXIFOrientation(jpegWithOrientationSix); orientation != 6 {
		t.Fatalf("orientation = %d, want 6", orientation)
	}
	source := image.NewNRGBA(image.Rect(0, 0, 2, 3))
	rotated, changed := applyZQBAPIEXIFOrientation(jpegWithOrientationSix, source)
	if !changed || rotated.Bounds().Dx() != 3 || rotated.Bounds().Dy() != 2 {
		t.Fatalf("changed=%v dimensions=%dx%d", changed, rotated.Bounds().Dx(), rotated.Bounds().Dy())
	}
}
