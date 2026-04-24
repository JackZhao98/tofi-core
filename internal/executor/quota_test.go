package executor

import (
	"context"
	"strings"
	"sync"
	"testing"
)

func TestIsInstallCommand(t *testing.T) {
	cases := []struct {
		cmd  string
		want bool
	}{
		{"pip install requests", true},
		{"pip3 install -U pandas", true},
		{"python3 -m pip install rich", true},
		{"uv pip install httpx", true},
		{"uv add pandas", true},
		{"uv tool install ruff", true},
		{"npm install --save foo", true},
		{"npm i lodash", true},
		{"pnpm add vite", true},
		{"yarn add react", true},
		{"cd /work && pip install numpy", true},
		{"python -c 'print(1)'", false},
		{"ls -la", false},
		{"pip list", false},
		{"npm run build", false},
		{"cat install.txt", false},
	}
	for _, c := range cases {
		if got := IsInstallCommand(c.cmd); got != c.want {
			t.Errorf("IsInstallCommand(%q) = %v, want %v", c.cmd, got, c.want)
		}
	}
}

// fakeGate records calls and returns a canned decision.
type fakeGate struct {
	mu         sync.Mutex
	decision   QuotaDecision
	decideIDs  []string
	bumpIDs    []string
	bumpedWG   sync.WaitGroup
	bumpsToWait int
}

func (f *fakeGate) Decide(userID string) QuotaDecision {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.decideIDs = append(f.decideIDs, userID)
	return f.decision
}

func (f *fakeGate) BumpInventory(userID string) {
	f.mu.Lock()
	f.bumpIDs = append(f.bumpIDs, userID)
	f.mu.Unlock()
	if f.bumpsToWait > 0 {
		f.bumpedWG.Done()
	}
}

// fakeExecutor captures the env map passed through so tests can verify
// TOFI_INSTALL_MODE injection without invoking a real sandbox.
type fakeExecutor struct {
	lastEnv     map[string]string
	lastCommand string
	returnOut   string
	returnErr   error
}

func (f *fakeExecutor) CreateSandbox(SandboxConfig) (string, error) { return "/tmp/sb", nil }
func (f *fakeExecutor) Cleanup(string)                              {}
func (f *fakeExecutor) Execute(_ context.Context, _, _ string, cmd string, _ int, env map[string]string) (string, error) {
	f.lastCommand = cmd
	f.lastEnv = env
	return f.returnOut, f.returnErr
}

func TestWithQuota_InstallInjectsModeAndBumpsInventory(t *testing.T) {
	inner := &fakeExecutor{returnOut: "Successfully installed requests"}
	gate := &fakeGate{
		decision: QuotaDecision{
			Mode:         InstallModePersistent,
			DiskUsagePct: 42,
		},
		bumpsToWait: 1,
	}
	gate.bumpedWG.Add(1)

	exec := WithQuota(inner, "alice", gate)
	out, err := exec.Execute(context.Background(), "/sb", "/u", "pip install requests", 60, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if inner.lastEnv["TOFI_INSTALL_MODE"] != string(InstallModePersistent) {
		t.Errorf("install mode not injected: env = %v", inner.lastEnv)
	}
	if !strings.Contains(out, "[tofi_meta]") || !strings.Contains(out, "disk_pct: 42") {
		t.Errorf("meta not appended to output:\n%s", out)
	}
	if !strings.Contains(out, "install_mode: persistent") {
		t.Errorf("install_mode not in meta:\n%s", out)
	}

	gate.bumpedWG.Wait()
	if len(gate.bumpIDs) != 1 || gate.bumpIDs[0] != "alice" {
		t.Errorf("BumpInventory calls = %v, want [alice]", gate.bumpIDs)
	}
}

func TestWithQuota_EphemeralModeHintsWhenQuotaFull(t *testing.T) {
	inner := &fakeExecutor{returnOut: "installed"}
	gate := &fakeGate{
		decision: QuotaDecision{
			Mode:         InstallModeEphemeral,
			DiskUsagePct: 100,
			Hint:         "Disk quota full — installs this run will not persist.",
		},
		bumpsToWait: 1,
	}
	gate.bumpedWG.Add(1)

	exec := WithQuota(inner, "bob", gate)
	out, _ := exec.Execute(context.Background(), "/sb", "/u", "uv add pandas", 60, nil)

	if inner.lastEnv["TOFI_INSTALL_MODE"] != "ephemeral" {
		t.Errorf("expected ephemeral mode injected, got %v", inner.lastEnv)
	}
	if !strings.Contains(out, "will not persist") {
		t.Errorf("hint missing from meta:\n%s", out)
	}
	gate.bumpedWG.Wait()
}

func TestWithQuota_NonInstallSkipsGate(t *testing.T) {
	inner := &fakeExecutor{returnOut: "hello"}
	gate := &fakeGate{decision: QuotaDecision{Mode: InstallModePersistent, DiskUsagePct: 99}}

	exec := WithQuota(inner, "alice", gate)
	out, _ := exec.Execute(context.Background(), "/sb", "/u", "python -c 'print(1)'", 60, nil)

	if strings.Contains(out, "[tofi_meta]") {
		t.Errorf("meta appended on non-install call:\n%s", out)
	}
	if _, ok := inner.lastEnv["TOFI_INSTALL_MODE"]; ok {
		t.Error("TOFI_INSTALL_MODE should not be injected for non-install commands")
	}
	if len(gate.decideIDs) != 0 {
		t.Errorf("Decide called for non-install command: %v", gate.decideIDs)
	}
	if len(gate.bumpIDs) != 0 {
		t.Errorf("BumpInventory called for non-install command: %v", gate.bumpIDs)
	}
}

func TestWithQuota_NilGateOrEmptyUserReturnsInnerUnchanged(t *testing.T) {
	inner := &fakeExecutor{}
	if WithQuota(inner, "alice", nil) != inner {
		t.Error("nil gate should return inner executor")
	}
	if WithQuota(inner, "", &fakeGate{}) != inner {
		t.Error("empty userID should return inner executor")
	}
}

func TestWithQuota_DoesNotMutateCallerEnv(t *testing.T) {
	inner := &fakeExecutor{returnOut: "ok"}
	gate := &fakeGate{
		decision:    QuotaDecision{Mode: InstallModeEphemeral},
		bumpsToWait: 1,
	}
	gate.bumpedWG.Add(1)

	callerEnv := map[string]string{"SECRET": "xyz"}
	exec := WithQuota(inner, "alice", gate)
	_, _ = exec.Execute(context.Background(), "/sb", "/u", "pip install x", 60, callerEnv)

	if _, ok := callerEnv["TOFI_INSTALL_MODE"]; ok {
		t.Error("caller env map was mutated — decorator must copy before injecting")
	}
	if inner.lastEnv["SECRET"] != "xyz" {
		t.Error("copied env lost caller fields")
	}
	gate.bumpedWG.Wait()
}
