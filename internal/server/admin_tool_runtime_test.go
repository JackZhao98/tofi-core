package server

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCollectToolRuntimeItems(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "rh"), []byte("bin"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "venv-a"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "venv-a", "pyvenv.cfg"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	items, total, _ := collectToolRuntimeItems("bin", root)
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if total <= 0 {
		t.Fatalf("expected total size > 0")
	}
	if items[0].Category != "bin" {
		t.Fatalf("expected category bin, got %s", items[0].Category)
	}
}
