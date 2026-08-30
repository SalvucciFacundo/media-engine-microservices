package localfs_test

import (
	"bytes"
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SalvucciFacundo/media-engine-microservices/internal/adapters/localfs"
)

func TestLocalFS_SaveAndOpen(t *testing.T) {
	tempDir := t.TempDir()
	store, err := localfs.NewFileStore(tempDir)
	if err != nil {
		t.Fatalf("failed to create FileStore: %v", err)
	}

	ctx := context.Background()
	content := []byte("hello media-engine")
	relPath := "uploads/2025/test.txt"

	fullPath, err := store.Save(ctx, relPath, bytes.NewReader(content))
	if err != nil {
		t.Fatalf("failed to save file: %v", err)
	}

	if !strings.HasPrefix(fullPath, tempDir) {
		t.Errorf("expected fullPath to start with %s, got %s", tempDir, fullPath)
	}

	rc, err := store.Open(ctx, relPath)
	if err != nil {
		t.Fatalf("failed to open saved file: %v", err)
	}
	defer rc.Close()

	readBytes, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("failed to read file content: %v", err)
	}
	if !bytes.Equal(readBytes, content) {
		t.Errorf("content mismatch: got %q, want %q", string(readBytes), string(content))
	}
}

func TestLocalFS_DeleteAndExists(t *testing.T) {
	tempDir := t.TempDir()
	store, err := localfs.NewFileStore(tempDir)
	if err != nil {
		t.Fatalf("failed to create FileStore: %v", err)
	}

	ctx := context.Background()
	relPath := "test-delete.png"
	_, err = store.Save(ctx, relPath, strings.NewReader("dummy-image-data"))
	if err != nil {
		t.Fatalf("failed to save file: %v", err)
	}

	exists, err := store.Exists(ctx, relPath)
	if err != nil || !exists {
		t.Errorf("expected file to exist, got exists=%v, err=%v", exists, err)
	}

	err = store.Delete(ctx, relPath)
	if err != nil {
		t.Fatalf("failed to delete file: %v", err)
	}

	exists, err = store.Exists(ctx, relPath)
	if err != nil || exists {
		t.Errorf("expected file not to exist after deletion, got exists=%v, err=%v", exists, err)
	}

	// Deleting already deleted file should not error out (idempotent)
	err = store.Delete(ctx, relPath)
	if err != nil {
		t.Errorf("deleting non-existent file returned error: %v", err)
	}
}

func TestLocalFS_PathTraversalProtection(t *testing.T) {
	tempDir := t.TempDir()
	store, err := localfs.NewFileStore(tempDir)
	if err != nil {
		t.Fatalf("failed to create FileStore: %v", err)
	}

	ctx := context.Background()
	_, err = store.Save(ctx, "../../../etc/passwd", strings.NewReader("malicious content"))
	if err == nil {
		t.Error("expected error for path traversal attempt, got nil")
	}

	_, err = store.Open(ctx, "../../../etc/passwd")
	if err == nil {
		t.Error("expected error opening path with traversal, got nil")
	}
}

func TestLocalFS_GetPath(t *testing.T) {
	tempDir := t.TempDir()
	store, err := localfs.NewFileStore(tempDir)
	if err != nil {
		t.Fatalf("failed to create FileStore: %v", err)
	}

	path := store.GetPath("sample.png")
	expected := filepath.Join(tempDir, "sample.png")
	if path != expected {
		t.Errorf("GetPath mismatch: got %s, want %s", path, expected)
	}
}

func TestLocalFS_NewFileStore_EmptyDir(t *testing.T) {
	_, err := localfs.NewFileStore("")
	if err == nil {
		t.Error("expected error for empty base directory, got nil")
	}
}
