package doubao

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestPrepareZQBAPIAssetRunsUploadCreateAndWaitChain(t *testing.T) {
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
