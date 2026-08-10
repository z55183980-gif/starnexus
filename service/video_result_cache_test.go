package service

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenVideoResultCacheRestrictsFilesToGeneratedNames(t *testing.T) {
	root := t.TempDir()
	t.Setenv("VIDEO_RESULT_CACHE_DIR", root)
	name := videoResultCachePrefix + "0123456789abcdef.mp4"
	if err := os.WriteFile(filepath.Join(root, name), []byte("video"), 0o600); err != nil {
		t.Fatal(err)
	}
	file, info, err := OpenVideoResultCache(name)
	if err != nil {
		t.Fatal(err)
	}
	_ = file.Close()
	if info.Size() != 5 {
		t.Fatalf("cached size = %d", info.Size())
	}
	if _, _, err := OpenVideoResultCache("../secret"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("path traversal error = %v", err)
	}
}

func TestVideoResultExtensionUsesSafeAllowlist(t *testing.T) {
	if got := videoResultExtension("video/webm; charset=binary", "https://example.com/a.mp4"); got != ".webm" {
		t.Fatalf("webm extension = %q", got)
	}
	if got := videoResultExtension("application/octet-stream", "https://example.com/a.exe"); got != ".mp4" {
		t.Fatalf("unsafe extension fallback = %q", got)
	}
}
