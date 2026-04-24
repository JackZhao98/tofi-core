package executor

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildOCIConfig_PersistentMode(t *testing.T) {
	opts := OCISpecOptions{
		Command:     "python3 -c 'print(1)'",
		UID:         1000,
		GID:         1000,
		RunDir:      "/var/tofi/runs/run123",
		UserDir:     "/var/tofi/users/alice",
		PkgDir:      "/var/tofi/packages",
		InstallMode: InstallModePersistent,
		TimeoutSec:  60,
		ExtraEnv:    []string{"SKILL_KEY=secret"},
	}
	raw, err := BuildOCIConfig(opts)
	if err != nil {
		t.Fatalf("BuildOCIConfig returned error: %v", err)
	}

	var spec map[string]any
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatalf("config.json is not valid JSON: %v", err)
	}

	if spec["ociVersion"] != "1.0.2" {
		t.Errorf("ociVersion = %v, want 1.0.2", spec["ociVersion"])
	}

	process := spec["process"].(map[string]any)
	args := process["args"].([]any)
	if len(args) != 3 || args[2] != opts.Command {
		t.Errorf("process.args = %v, want [/bin/sh -c %q]", args, opts.Command)
	}

	envList := process["env"].([]any)
	if !envContains(envList, "TOFI_INSTALL_MODE=persistent") {
		t.Error("env should contain TOFI_INSTALL_MODE=persistent")
	}
	if !envContains(envList, "SKILL_KEY=secret") {
		t.Error("env should forward ExtraEnv entries")
	}
	if !envContains(envList, "HOME=/home/tofi") {
		t.Error("env should set HOME=/home/tofi")
	}

	if !process["noNewPrivileges"].(bool) {
		t.Error("process.noNewPrivileges must be true")
	}

	root := spec["root"].(map[string]any)
	if !root["readonly"].(bool) {
		t.Error("root.readonly must be true")
	}

	mounts := spec["mounts"].([]any)
	localMount := findMount(mounts, sandboxLocalPath)
	if localMount == nil {
		t.Fatalf("missing mount at %s", sandboxLocalPath)
	}
	if localMount["source"] != opts.UserDir {
		t.Errorf("persistent mode: %s source = %v, want %v",
			sandboxLocalPath, localMount["source"], opts.UserDir)
	}

	cacheMount := findMount(mounts, sandboxCachePath)
	if cacheMount == nil || cacheMount["source"] != opts.PkgDir {
		t.Errorf("cache mount wrong: %+v", cacheMount)
	}
	cacheOpts := cacheMount["options"].([]any)
	if !optsContain(cacheOpts, "ro") {
		t.Error("cache mount must be read-only")
	}

	linux := spec["linux"].(map[string]any)
	namespaces := linux["namespaces"].([]any)
	wantNS := map[string]bool{"pid": true, "network": true, "ipc": true, "uts": true, "mount": true}
	gotNS := map[string]bool{}
	for _, ns := range namespaces {
		gotNS[ns.(map[string]any)["type"].(string)] = true
	}
	for name := range wantNS {
		if !gotNS[name] {
			t.Errorf("missing %s namespace", name)
		}
	}
}

func TestBuildOCIConfig_EphemeralMode(t *testing.T) {
	opts := OCISpecOptions{
		Command:     "pip install requests",
		UID:         1000,
		GID:         1000,
		RunDir:      "/var/tofi/runs/run456",
		UserDir:     "/var/tofi/users/bob",
		PkgDir:      "/var/tofi/packages",
		InstallMode: InstallModeEphemeral,
	}
	raw, err := BuildOCIConfig(opts)
	if err != nil {
		t.Fatalf("BuildOCIConfig: %v", err)
	}

	var spec map[string]any
	_ = json.Unmarshal(raw, &spec)

	mounts := spec["mounts"].([]any)
	localMount := findMount(mounts, sandboxLocalPath)
	if localMount == nil {
		t.Fatalf("missing %s mount", sandboxLocalPath)
	}
	wantSource := opts.RunDir + "/overlay/local"
	if localMount["source"] != wantSource {
		t.Errorf("ephemeral mode: %s source = %v, want %v",
			sandboxLocalPath, localMount["source"], wantSource)
	}

	envList := spec["process"].(map[string]any)["env"].([]any)
	if !envContains(envList, "TOFI_INSTALL_MODE=ephemeral") {
		t.Error("env should contain TOFI_INSTALL_MODE=ephemeral")
	}
}

func TestBuildOCIConfig_MissingRequiredFields(t *testing.T) {
	cases := []struct {
		name string
		opts OCISpecOptions
	}{
		{"no command", OCISpecOptions{RunDir: "/r", PkgDir: "/p", UserDir: "/u", InstallMode: InstallModePersistent}},
		{"no runDir", OCISpecOptions{Command: "ls", PkgDir: "/p", UserDir: "/u", InstallMode: InstallModePersistent}},
		{"no pkgDir", OCISpecOptions{Command: "ls", RunDir: "/r", UserDir: "/u", InstallMode: InstallModePersistent}},
		{"persistent without userDir", OCISpecOptions{Command: "ls", RunDir: "/r", PkgDir: "/p", InstallMode: InstallModePersistent}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := BuildOCIConfig(c.opts); err == nil {
				t.Errorf("expected error for %s, got nil", c.name)
			}
		})
	}
}

func TestBuildOCIConfig_NoCPULimitWhenTimeoutZero(t *testing.T) {
	opts := OCISpecOptions{
		Command:     "true",
		RunDir:      "/r",
		UserDir:     "/u",
		PkgDir:      "/p",
		InstallMode: InstallModePersistent,
		TimeoutSec:  0,
	}
	raw, _ := BuildOCIConfig(opts)
	if strings.Contains(string(raw), "RLIMIT_CPU") {
		t.Error("RLIMIT_CPU must be omitted when TimeoutSec is zero")
	}
}

func envContains(envList []any, target string) bool {
	for _, e := range envList {
		if e.(string) == target {
			return true
		}
	}
	return false
}

func optsContain(opts []any, target string) bool {
	for _, o := range opts {
		if o.(string) == target {
			return true
		}
	}
	return false
}

func findMount(mounts []any, destination string) map[string]any {
	for _, m := range mounts {
		mm := m.(map[string]any)
		if mm["destination"] == destination {
			return mm
		}
	}
	return nil
}
