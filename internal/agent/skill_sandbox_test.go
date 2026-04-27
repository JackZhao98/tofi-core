package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopySkillToSandboxReplacesHostSymlink(t *testing.T) {
	src := t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, "scripts"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "scripts", "fetch.py"), []byte("print('ok')\n"), 0755); err != nil {
		t.Fatal(err)
	}

	sandbox := t.TempDir()
	target := filepath.Join(sandbox, "skills", "web-fetch")
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(src, target); err != nil {
		t.Fatal(err)
	}

	if err := copySkillToSandbox(sandbox, "web-fetch", src); err != nil {
		t.Fatalf("copySkillToSandbox: %v", err)
	}

	info, err := os.Lstat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("target is still a symlink: %s", target)
	}

	data, err := os.ReadFile(filepath.Join(target, "scripts", "fetch.py"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "print('ok')\n" {
		t.Fatalf("unexpected copied script content: %q", string(data))
	}
}
