//go:build integration

// Package executor integration tests — run with:
//
//	go test -tags=integration ./internal/executor/ -v -run TestGvisorT
//
// Requires `runsc` in PATH. CI wires these as a separate job. On macOS
// the file is excluded entirely by the build tag.
package executor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// testExecutor spins up a GvisorExecutor rooted in a temp TOFI_HOME so
// tests can create "users", fill packages dirs, and tear everything
// down by deleting the temp root.
func testExecutor(t *testing.T) (*GvisorExecutor, string) {
	t.Helper()
	runsc, err := exec.LookPath("runsc")
	if err != nil {
		t.Skip("runsc not installed; skipping gVisor integration test")
	}
	home := t.TempDir()
	g, err := NewGvisorExecutor(home, runsc)
	if err != nil {
		t.Fatalf("NewGvisorExecutor: %v", err)
	}
	return g, home
}

// runCmd is a T-helper that creates a per-user sandbox, runs one command,
// and cleans up. Returns trimmed stdout.
func runCmd(t *testing.T, g *GvisorExecutor, userID, command string, envOverrides map[string]string) string {
	t.Helper()
	runID := fmt.Sprintf("test-%s-%d", userID, time.Now().UnixNano())
	path, err := g.CreateSandbox(SandboxConfig{
		HomeDir: g.homeDir,
		UserID:  userID,
		CardID:  runID,
	})
	if err != nil {
		t.Fatalf("CreateSandbox(%s): %v", userID, err)
	}
	defer g.Cleanup(path)

	userDir := filepath.Join(g.homeDir, "users", userID)
	env := map[string]string{"TOFI_INSTALL_MODE": "persistent"}
	for k, v := range envOverrides {
		env[k] = v
	}
	out, err := g.Execute(context.Background(), path, userDir, command, 90, env)
	if err != nil {
		t.Fatalf("Execute(%s) failed: %v\nOutput: %s", command, err, out)
	}
	return strings.TrimSpace(out)
}

// ─── T1 — mount isolation ─────────────────────────────────────────
// Alice installs pandas, Bob installs numpy. Verify A cannot import
// numpy and B cannot import pandas.
func TestGvisorT1_MountIsolation(t *testing.T) {
	g, _ := testExecutor(t)
	runCmd(t, g, "alice", "python3 -m pip install --quiet pandas", nil)
	runCmd(t, g, "bob", "python3 -m pip install --quiet numpy", nil)

	outA := runCmd(t, g, "alice", `python3 -c 'try:
  import numpy; print("LEAK")
except ImportError:
  print("ISOLATED")'`, nil)
	if !strings.Contains(outA, "ISOLATED") {
		t.Errorf("alice should not see bob's numpy: %s", outA)
	}

	outB := runCmd(t, g, "bob", `python3 -c 'try:
  import pandas; print("LEAK")
except ImportError:
  print("ISOLATED")'`, nil)
	if !strings.Contains(outB, "ISOLATED") {
		t.Errorf("bob should not see alice's pandas: %s", outB)
	}
}

// ─── T2 — persistence across runs ─────────────────────────────────
func TestGvisorT2_PersistenceAcrossRuns(t *testing.T) {
	g, _ := testExecutor(t)
	runCmd(t, g, "alice", "python3 -m pip install --quiet requests", nil)
	start := time.Now()
	out := runCmd(t, g, "alice", `python3 -c 'import requests; print(requests.__version__)'`, nil)
	elapsed := time.Since(start)
	if out == "" {
		t.Errorf("requests import empty: %s", out)
	}
	if elapsed > 3*time.Second {
		t.Errorf("warm import took %v, expected <3s (install should have persisted)", elapsed)
	}
}

// ─── T3 — shared wheel cache hit ─────────────────────────────────
// Both users install the same package. Second install should not
// re-download (pip reports "Using cached" or similar).
func TestGvisorT3_SharedWheelCacheHit(t *testing.T) {
	g, _ := testExecutor(t)
	out1 := runCmd(t, g, "alice", "python3 -m pip install requests 2>&1", nil)
	out2 := runCmd(t, g, "bob", "python3 -m pip install requests 2>&1", nil)
	if !strings.Contains(out2, "cached") && !strings.Contains(out2, "Already satisfied") {
		t.Errorf("bob's install should hit wheel cache.\nalice:\n%s\nbob:\n%s", out1, out2)
	}
}

// ─── T4 — cross-user path access blocked ─────────────────────────
// Alice tries to read bob's venv via a guessed host path. gVisor mount
// isolation means bob's dir isn't in alice's filesystem view.
func TestGvisorT4_CrossUserReadBlocked(t *testing.T) {
	g, _ := testExecutor(t)
	runCmd(t, g, "bob", "echo bobs-secret > /home/tofi/.local/bin/secret.txt", nil)

	// From alice, try to access bob's file via every plausible path.
	probe := `for p in /home/tofi/.local/../../users/bob /home/tofi/../bob /users/bob; do
  ls "$p" 2>/dev/null && echo "LEAK:$p"
done
echo DONE`
	out := runCmd(t, g, "alice", probe, nil)
	if strings.Contains(out, "LEAK:") {
		t.Errorf("alice could see a path into bob's dir:\n%s", out)
	}
}

// ─── T5 — malicious delete stays contained ───────────────────────
func TestGvisorT5_DeleteStaysContained(t *testing.T) {
	g, _ := testExecutor(t)
	runCmd(t, g, "bob", "python3 -m pip install --quiet requests", nil)
	_ = runCmd(t, g, "alice", "rm -rf /home/tofi/.local/bin/* 2>/dev/null; echo done", nil)

	// Bob's venv must still work.
	out := runCmd(t, g, "bob", `python3 -c 'import requests; print("BOB_OK")'`, nil)
	if !strings.Contains(out, "BOB_OK") {
		t.Errorf("bob's venv damaged by alice's rm: %s", out)
	}
}

// ─── T6 — quota enforcement via gate ─────────────────────────────
// Runs in-process through the WithQuota decorator, feeding a gate that
// reports 100% after the first install. Verifies the second install
// flips to ephemeral mode.
func TestGvisorT6_QuotaEnforcement(t *testing.T) {
	g, _ := testExecutor(t)
	gate := &countingGate{
		decisions: []QuotaDecision{
			{Mode: InstallModePersistent, DiskUsagePct: 50},
			{Mode: InstallModeEphemeral, DiskUsagePct: 100, Hint: "full"},
		},
	}
	rec := &recordingExecutor{inner: g}
	wrapped := WithQuota(rec, "alice", gate)

	for i := 0; i < 2; i++ {
		runID := fmt.Sprintf("quota-%d-%d", i, time.Now().UnixNano())
		path, err := g.CreateSandbox(SandboxConfig{
			HomeDir: g.homeDir, UserID: "alice", CardID: runID,
		})
		if err != nil {
			t.Fatalf("CreateSandbox[%d]: %v", i, err)
		}
		userDir := filepath.Join(g.homeDir, "users", "alice")
		out, err := wrapped.Execute(context.Background(), path, userDir,
			"python3 -m pip install --quiet charset-normalizer",
			90, map[string]string{})
		if err != nil {
			t.Fatalf("Execute[%d]: %v\n%s", i, err, out)
		}
		g.Cleanup(path)
	}

	if len(gate.observed) < 2 {
		t.Fatalf("gate only observed %d decisions, want 2", len(gate.observed))
	}
	if rec.lastEnv["TOFI_INSTALL_MODE"] != "ephemeral" {
		t.Errorf("second install env = %v, want ephemeral", rec.lastEnv)
	}
}

// ─── T7 — ephemeral install doesn't persist ─────────────────────
func TestGvisorT7_EphemeralDoesNotPersist(t *testing.T) {
	g, _ := testExecutor(t)
	runCmd(t, g, "alice", "python3 -m pip install --quiet six",
		map[string]string{"TOFI_INSTALL_MODE": "ephemeral"})

	out := runCmd(t, g, "alice", `python3 -c 'try:
  import six; print("LEAK")
except ImportError:
  print("GONE")'`, nil)
	if !strings.Contains(out, "GONE") {
		t.Errorf("ephemeral install leaked into next run: %s", out)
	}
}

// ─── T8 — parallel runs ─────────────────────────────────────────
// Two concurrent sandboxes for different users. Both must succeed.
func TestGvisorT8_ParallelDifferentUsers(t *testing.T) {
	g, _ := testExecutor(t)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, uid := range []string{"alice", "bob"} {
		wg.Add(1)
		go func(userID string) {
			defer wg.Done()
			runID := fmt.Sprintf("parallel-%s-%d", userID, time.Now().UnixNano())
			path, err := g.CreateSandbox(SandboxConfig{
				HomeDir: g.homeDir, UserID: userID, CardID: runID,
			})
			if err != nil {
				errs <- err
				return
			}
			defer g.Cleanup(path)
			userDir := filepath.Join(g.homeDir, "users", userID)
			_, err = g.Execute(context.Background(), path, userDir,
				"python3 -c 'import time; time.sleep(1); print(\"OK\")'",
				10, map[string]string{"TOFI_INSTALL_MODE": "persistent"})
			errs <- err
		}(uid)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("parallel run failed: %v", err)
		}
	}
}

// ─── T9 — cleanup removes run dir but not user dir ───────────────
func TestGvisorT9_CleanupSeparation(t *testing.T) {
	g, home := testExecutor(t)
	runID := fmt.Sprintf("cleanup-%d", time.Now().UnixNano())
	path, err := g.CreateSandbox(SandboxConfig{
		HomeDir: home, UserID: "alice", CardID: runID,
	})
	if err != nil {
		t.Fatalf("CreateSandbox: %v", err)
	}
	userDir := filepath.Join(home, "users", "alice")

	_, _ = g.Execute(context.Background(), path, userDir,
		"echo marker > /home/tofi/.local/bin/marker.txt",
		30, map[string]string{"TOFI_INSTALL_MODE": "persistent"})

	g.Cleanup(path)

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("Cleanup did not remove %s", path)
	}
	if _, err := os.Stat(filepath.Join(userDir, "bin", "marker.txt")); err != nil {
		t.Errorf("user dir marker should survive cleanup: %v", err)
	}
}

// ─── T10 — network reachable (host mode) ─────────────────────────
// Deferred: when we add a host-side iptables egress allowlist branch,
// this flips to expecting BLOCKED. Right now --rootless forces
// --network=host, so outbound succeeds. The test is kept as a smoke
// check that pip can reach PyPI.
func TestGvisorT10_NetworkReachable(t *testing.T) {
	g, _ := testExecutor(t)
	out := runCmd(t, g, "alice",
		`curl -s -o /dev/null -w "%{http_code}" --max-time 5 https://1.1.1.1 || echo FAIL`,
		nil)
	if strings.Contains(out, "FAIL") || !strings.Contains(out, "200") && !strings.Contains(out, "301") && !strings.Contains(out, "302") && !strings.Contains(out, "400") {
		t.Errorf("expected reachable Cloudflare (host network), got: %s", out)
	}
}

// ─── Helpers ────────────────────────────────────────────────────

// countingGate is a QuotaGate for T6 that hands out decisions in order
// and records what the decorator injected into the inner executor's env.
type countingGate struct {
	mu        sync.Mutex
	decisions []QuotaDecision
	observed  []QuotaDecision
	bumpIDs   []string
}

func (g *countingGate) Decide(userID string) QuotaDecision {
	g.mu.Lock()
	defer g.mu.Unlock()
	idx := len(g.observed)
	if idx >= len(g.decisions) {
		idx = len(g.decisions) - 1
	}
	d := g.decisions[idx]
	g.observed = append(g.observed, d)
	return d
}

func (g *countingGate) BumpInventory(userID string) {
	g.mu.Lock()
	g.bumpIDs = append(g.bumpIDs, userID)
	g.mu.Unlock()
}

// recordingExecutor wraps another Executor to capture the env map passed
// through on each Execute call (used via countingGate in integration
// scaffolding where we need to observe what the decorator injected).
type recordingExecutor struct {
	inner     Executor
	mu        sync.Mutex
	lastEnv   map[string]string
	lastCmd   string
}

func (r *recordingExecutor) CreateSandbox(cfg SandboxConfig) (string, error) {
	return r.inner.CreateSandbox(cfg)
}
func (r *recordingExecutor) Cleanup(p string) { r.inner.Cleanup(p) }
func (r *recordingExecutor) Execute(ctx context.Context, sb, ud, cmd string, to int, env map[string]string) (string, error) {
	r.mu.Lock()
	r.lastEnv = env
	r.lastCmd = cmd
	r.mu.Unlock()
	return r.inner.Execute(ctx, sb, ud, cmd, to, env)
}
