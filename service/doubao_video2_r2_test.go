package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestDecodeDoubaoVideo2InlineMedia(t *testing.T) {
	pngHeader := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
	source := "data:image/png;base64," + base64.StdEncoding.EncodeToString(pngHeader)
	data, contentType, extension, err := decodeDoubaoVideo2InlineMedia(source)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(pngHeader) || contentType != "image/png" || extension != "png" {
		t.Fatalf("data=%x type=%q extension=%q", data, contentType, extension)
	}
}

func TestDoubaoVideo2R2DirectUploadIntegration(t *testing.T) {
	if !DoubaoVideo2R2Configured() {
		t.Skip("DoubaoVideo2.0 R2 integration is not configured")
	}
	payload := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
	digest := sha256.Sum256(payload)
	checksum := base64.StdEncoding.EncodeToString(digest[:])
	upload, err := CreateDoubaoVideo2DirectUpload(t.Context(), 42, "image/png", int64(len(payload)), checksum)
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPut, upload.UploadURL, bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range upload.Headers {
		request.Header.Set(key, value)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	responseBody, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		t.Fatalf("R2 PUT status = %d body = %s", response.StatusCode, responseBody)
	}
	completed, err := CompleteDoubaoVideo2DirectUpload(t.Context(), 42, upload.ObjectID, upload.CompleteToken)
	if err != nil {
		t.Fatal(err)
	}
	getResponse, err := http.Get(completed.MediaURL)
	if err != nil {
		t.Fatal(err)
	}
	getResponse.Body.Close()
	if getResponse.StatusCode != http.StatusOK {
		t.Fatalf("R2 GET status = %d", getResponse.StatusCode)
	}
	config, err := loadDoubaoVideo2R2Config()
	if err != nil {
		t.Fatal(err)
	}
	if err := deleteDoubaoVideo2R2Object(t.Context(), config, doubaoVideo2R2Prefix+upload.ObjectID); err != nil {
		t.Fatal(err)
	}
}

func TestStoreDoubaoVideo2InlineMediaIntegration(t *testing.T) {
	if !DoubaoVideo2R2Configured() {
		t.Skip("DoubaoVideo2.0 R2 integration is not configured")
	}
	payload := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
	source := "data:image/png;base64," + base64.StdEncoding.EncodeToString(payload)
	mediaURL, err := StoreDoubaoVideo2InlineMedia(t.Context(), source)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(mediaURL)
	if err != nil {
		t.Fatal(err)
	}
	marker := "/" + doubaoVideo2R2Prefix
	markerIndex := strings.Index(parsed.Path, marker)
	if markerIndex < 0 {
		t.Fatalf("R2 media URL path does not contain the expected object prefix: %s", parsed.Path)
	}
	objectKey := strings.TrimPrefix(parsed.Path[markerIndex:], "/")
	config, err := loadDoubaoVideo2R2Config()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if deleteErr := deleteDoubaoVideo2R2Object(context.Background(), config, objectKey); deleteErr != nil {
			t.Errorf("delete automatic R2 test object: %v", deleteErr)
		}
	})
	response, err := http.Get(mediaURL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("R2 automatic GET status = %d", response.StatusCode)
	}
}

func TestCreateDoubaoVideo2DirectUploadSignsRequiredHeaders(t *testing.T) {
	t.Setenv("DOUBAO_VIDEO2_R2_ENDPOINT", "https://example.r2.cloudflarestorage.com")
	t.Setenv("DOUBAO_VIDEO2_R2_ACCESS_KEY_ID", "access-key")
	t.Setenv("DOUBAO_VIDEO2_R2_SECRET_ACCESS_KEY", "secret-key")
	t.Setenv("DOUBAO_VIDEO2_R2_BUCKET", "starnexus-video-inputs")
	digest := sha256.Sum256([]byte("test-media"))
	checksum := base64.StdEncoding.EncodeToString(digest[:])

	upload, err := CreateDoubaoVideo2DirectUpload(t.Context(), 42, "image/png", 10, checksum)
	if err != nil {
		t.Fatal(err)
	}
	if upload.Method != http.MethodPut || upload.ObjectID == "" || upload.CompleteToken == "" {
		t.Fatalf("unexpected upload response: %+v", upload)
	}
	if upload.Headers["Content-Type"] != "image/png" {
		t.Fatalf("required upload headers missing: %#v", upload.Headers)
	}
	if !strings.Contains(upload.UploadURL, "X-Amz-Signature=") || !strings.Contains(upload.UploadURL, "X-Amz-SignedHeaders=") {
		t.Fatalf("upload URL is not SigV4 presigned: %s", upload.UploadURL)
	}
	if _, ok := upload.Headers["Content-Length"]; ok {
		t.Fatal("Content-Length must be set by the HTTP client, not returned as a forbidden browser header")
	}
}

func TestCreateDoubaoVideo2DirectUploadRejectsBadChecksumAndOversize(t *testing.T) {
	t.Setenv("DOUBAO_VIDEO2_R2_ENDPOINT", "https://example.r2.cloudflarestorage.com")
	t.Setenv("DOUBAO_VIDEO2_R2_ACCESS_KEY_ID", "access-key")
	t.Setenv("DOUBAO_VIDEO2_R2_SECRET_ACCESS_KEY", "secret-key")
	t.Setenv("DOUBAO_VIDEO2_R2_BUCKET", "starnexus-video-inputs")
	if _, err := CreateDoubaoVideo2DirectUpload(t.Context(), 42, "image/png", 10, "not-a-checksum"); err == nil {
		t.Fatal("invalid checksum was accepted")
	}
	digest := sha256.Sum256([]byte("test-media"))
	checksum := base64.StdEncoding.EncodeToString(digest[:])
	if _, err := CreateDoubaoVideo2DirectUpload(t.Context(), 42, "image/png", doubaoVideo2R2MaximumMediaSize+1, checksum); err == nil {
		t.Fatal("oversized direct upload was accepted")
	}
}

func TestDoubaoVideo2PersistentMaterialPrefixIsOutsideTemporaryLifecycle(t *testing.T) {
	if strings.HasPrefix(doubaoVideo2R2MaterialPrefix, doubaoVideo2R2Prefix) {
		t.Fatalf("persistent prefix %q must not be covered by temporary lifecycle prefix %q", doubaoVideo2R2MaterialPrefix, doubaoVideo2R2Prefix)
	}
}

func TestPresignDoubaoVideo2PersistentMaterial(t *testing.T) {
	t.Setenv("DOUBAO_VIDEO2_R2_ENDPOINT", "https://example.r2.cloudflarestorage.com")
	t.Setenv("DOUBAO_VIDEO2_R2_ACCESS_KEY_ID", "access-key")
	t.Setenv("DOUBAO_VIDEO2_R2_SECRET_ACCESS_KEY", "secret-key")
	t.Setenv("DOUBAO_VIDEO2_R2_BUCKET", "starnexus-video-inputs")

	objectKey := doubaoVideo2R2MaterialPrefix + "42/object.png"
	mediaURL, err := PresignDoubaoVideo2PersistentMaterial(t.Context(), objectKey)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(mediaURL, "/"+objectKey) || !strings.Contains(mediaURL, "X-Amz-Signature=") {
		t.Fatalf("persistent material URL is not correctly presigned: %s", mediaURL)
	}
	if _, err := PresignDoubaoVideo2PersistentMaterial(t.Context(), doubaoVideo2R2Prefix+"temporary.png"); err == nil {
		t.Fatal("temporary object key was accepted as persistent material")
	}
}
