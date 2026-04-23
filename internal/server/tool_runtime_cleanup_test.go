package server

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCleanOldTree_RemovesExpiredEntriesOnly(t *testing.T) {
	root := t.TempDir()
	oldFile := filepath.Join(root, "old.bin")
	newFile := filepath.Join(root, "new.bin")
	oldDir := filepath.Join(root, "old-dir")

	if err := os.WriteFile(oldFile, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newFile, []byte("new"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(oldDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldDir, "nested.txt"), []byte("nested"), 0644); err != nil {
		t.Fatal(err)
	}

	oldTime := time.Now().Add(-8 * 24 * time.Hour)
	if err := os.Chtimes(oldFile, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(oldDir, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	removed, _ := cleanOldTree(root, time.Now(), 7*24*time.Hour)
	if removed != 2 {
		t.Fatalf("expected 2 removed entries, got %d", removed)
	}
	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Fatalf("old file should be removed")
	}
	if _, err := os.Stat(oldDir); !os.IsNotExist(err) {
		t.Fatalf("old dir should be removed")
	}
	if _, err := os.Stat(newFile); err != nil {
		t.Fatalf("new file should be preserved: %v", err)
	}
}

func TestEvictOldestUntilUnderQuota(t *testing.T) {
	root := t.TempDir()
	oldFile := filepath.Join(root, "old.bin")
	newFile := filepath.Join(root, "new.bin")

	if err := os.WriteFile(oldFile, []byte("12345"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newFile, []byte("67890"), 0644); err != nil {
		t.Fatal(err)
	}

	oldTime := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(oldFile, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	removed, _ := evictOldestUntilUnderQuota(5, root)
	if removed != 1 {
		t.Fatalf("expected 1 removed entry, got %d", removed)
	}
	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Fatalf("old file should be evicted first")
	}
	if _, err := os.Stat(newFile); err != nil {
		t.Fatalf("new file should remain: %v", err)
	}
}
