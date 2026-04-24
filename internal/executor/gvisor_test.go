package executor

import (
	"os"
	"path/filepath"
	"testing"
)

// TestGvisorExecutor_CreateSandboxDirTree verifies that CreateSandbox
// materialises every directory referenced by the OCI spec. Does not invoke
// runsc, so this runs on any platform.
func TestGvisorExecutor_CreateSandboxDirTree(t *testing.T) {
	home := t.TempDir()
	g, err := NewGvisorExecutor(home, "/usr/bin/runsc")
	if err != nil {
		t.Fatalf("NewGvisorExecutor: %v", err)
	}

	path, err := g.CreateSandbox(SandboxConfig{
		HomeDir: home,
		UserID:  "alice",
		CardID:  "run-abc",
	})
	if err != nil {
		t.Fatalf("CreateSandbox: %v", err)
	}

	wantSandbox := filepath.Join(home, "runs", "run-abc")
	if path != wantSandbox {
		t.Errorf("returned path = %s, want %s", path, wantSandbox)
	}

	expected := []string{
		filepath.Join(home, "packages", "cache", "pip"),
		filepath.Join(home, "packages", "cache", "uv"),
		filepath.Join(home, "packages", "cache", "npm"),
		filepath.Join(home, "packages", "wheels"),
		filepath.Join(home, "packages", "artifacts"),
		filepath.Join(home, "users", "alice", "bin"),
		filepath.Join(home, "users", "alice", "python", "venvs"),
		filepath.Join(home, "users", "alice", "uv", "tools"),
		filepath.Join(home, "users", "alice", "state", "tool-runs"),
		filepath.Join(path, "tmp"),
		filepath.Join(path, "overlay", "local", "bin"),
		filepath.Join(path, "overlay", "local", "python", "venvs"),
		filepath.Join(path, "rootfs"),
	}
	for _, dir := range expected {
		if _, err := os.Stat(dir); err != nil {
			t.Errorf("expected dir missing: %s (%v)", dir, err)
		}
	}
}

// TestGvisorExecutor_CleanupRefusesNonSandboxPath prevents the safety valve
// in Cleanup from ever rm-rf'ing a path that wasn't created by CreateSandbox.
func TestGvisorExecutor_CleanupRefusesNonSandboxPath(t *testing.T) {
	home := t.TempDir()
	g, _ := NewGvisorExecutor(home, "/usr/bin/runsc")

	// Create a directory that looks tempting but is not under runs/.
	bad := filepath.Join(home, "something-important")
	if err := os.MkdirAll(bad, 0755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(bad, "DO_NOT_DELETE")
	if err := os.WriteFile(marker, []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}

	g.Cleanup(bad)

	if _, err := os.Stat(marker); err != nil {
		t.Errorf("Cleanup removed path it should have refused: %s", bad)
	}
}

func TestGvisorExecutor_CreateSandboxRejectsEmptyCardID(t *testing.T) {
	g, _ := NewGvisorExecutor(t.TempDir(), "/usr/bin/runsc")
	_, err := g.CreateSandbox(SandboxConfig{UserID: "alice"})
	if err == nil {
		t.Error("expected error for empty CardID, got nil")
	}
}

func TestNewGvisorExecutor_RejectsMissingFields(t *testing.T) {
	if _, err := NewGvisorExecutor("", "/bin/runsc"); err == nil {
		t.Error("expected error for empty homeDir")
	}
	if _, err := NewGvisorExecutor(t.TempDir(), ""); err == nil {
		t.Error("expected error for empty runscBin")
	}
}
