package executor

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// MaxOutputBytes is the maximum output size from a sandbox command (1MB)
const MaxOutputBytes = 1 << 20

// MaxTimeout is the maximum allowed execution timeout in seconds
const MaxTimeout = 120

// blockedPrefixes are command prefixes that are never allowed (truly dangerous)
var blockedPrefixes = []string{
	"sudo ", "sudo\t",
	"su ", "su\t",
	"rm -rf /",
	"rm -rf /*",
	"mkfs",
	"fdisk",
	"mount ", "umount ",
	"shutdown", "reboot", "halt", "poweroff",
	"iptables", "systemctl ", "service ",
	"> /dev/", "< /dev/",
	"eval ", "exec ",
	"kill -9 1", "kill -9 -1",
	":(){ :|:& };:", // fork bomb
}

// blockedPatterns detects dangerous patterns anywhere in a command via regex.
// This is a best-effort layer — OS-level sandbox-exec is the true defense.
var blockedPatterns = []*regexp.Regexp{
	// Pipe-to-shell (remote code execution)
	regexp.MustCompile(`\|\s*(sh|bash|zsh|dash)\b`),
	regexp.MustCompile(`\|\s*python3?\s`),

	// Reverse shell patterns
	regexp.MustCompile(`\bnc\s+.*-e\b`),
	regexp.MustCompile(`\bncat\s+.*-e\b`),
	regexp.MustCompile(`/dev/tcp/`),
	regexp.MustCompile(`/dev/udp/`),
	regexp.MustCompile(`\bmkfifo\b.*\bnc\b`),
	regexp.MustCompile(`\bsocat\b.*\bexec\b`),

	// Data exfiltration (curl/wget with command substitution)
	regexp.MustCompile(`(curl|wget)\s+.*\$\(`),
	regexp.MustCompile(`(curl|wget)\s+.*-d\s+@/`),

	// Sensitive file access
	regexp.MustCompile(`\.(ssh|aws|gnupg|kube)/`),
	regexp.MustCompile(`Library/Keychains`),

	// Encoded bypass: base64 decode | shell
	regexp.MustCompile(`base64\s+-[dD]\b.*\|\s*(sh|bash)\b`),

	// Relative path traversal out of the sandbox
	regexp.MustCompile(`(^|[\s"'=;|&()<>:])\.\./`),
}

var (
	inlineEnvAssignPattern     = regexp.MustCompile(`(^|[;&|]\s*|&&\s*|\|\|\s*)[A-Za-z_][A-Za-z0-9_]*=`)
	absolutePathPattern        = regexp.MustCompile("(^|[\\s\"'=;|&()<>:])(/[^ \\t\\r\\n\"';|&()<>]+)")
	absoluteRuntimeExecPattern = regexp.MustCompile("(^|[;&|]\\s*|&&\\s*|\\|\\|\\s*)(/[^ \\t\\r\\n\"';|&()<>]+/(python3?|pip3?|npm|npx|node|uv))(\\s|$)")
)

// ─── DirectExecutor ─────────────────────────────────────────

// DirectExecutor runs commands directly on the host via sh -c.
// It inherits the full host PATH and uses a shared packages directory.
type DirectExecutor struct {
	homeDir string // tofi data directory for shared packages
}

// NewDirectExecutor creates a new DirectExecutor.
func NewDirectExecutor(homeDir string) *DirectExecutor {
	return &DirectExecutor{homeDir: homeDir}
}

// CreateSandbox creates an isolated directory structure for command execution.
// Creates a task-level sandbox and ensures the shared packages directory exists.
func (d *DirectExecutor) CreateSandbox(cfg SandboxConfig) (string, error) {
	// 1. Ensure shared packages directory exists
	pkgDir := filepath.Join(cfg.HomeDir, "packages")
	for _, sub := range []string{
		".local/bin",
		"node_modules/.bin",
		".pip",
		"runtime/bin",
		"artifacts",
		"cache/pip",
		"cache/uv",
		"cache/npm",
	} {
		if err := os.MkdirAll(filepath.Join(pkgDir, sub), 0755); err != nil {
			return "", fmt.Errorf("failed to create packages directory %s: %v", sub, err)
		}
	}

	// 2. Create task sandbox directory
	sandboxDir := filepath.Join(cfg.HomeDir, "sandbox", cfg.CardID)
	if err := os.MkdirAll(filepath.Join(sandboxDir, "tmp"), 0755); err != nil {
		return "", fmt.Errorf("failed to create sandbox directory: %v", err)
	}

	if cfg.UserID != "" {
		userRoot := filepath.Join(cfg.HomeDir, "users", cfg.UserID)
		if err := ensureUserRuntimeDirs(userRoot); err != nil {
			return "", fmt.Errorf("failed to create user runtime directories: %v", err)
		}
	}

	return sandboxDir, nil
}

// Execute runs a shell command inside the sandbox directory.
// Inherits the full host PATH with shared packages prepended.
func (d *DirectExecutor) Execute(ctx context.Context, sandboxPath, userDir, command string, timeoutSec int, env map[string]string) (string, error) {
	homeDir := d.homeDir
	if homeDir == "" {
		// Infer from sandboxPath: {homeDir}/sandbox/{cardID}
		homeDir = filepath.Dir(filepath.Dir(sandboxPath))
	}
	return executeInSandboxInternal(ctx, sandboxPath, homeDir, userDir, command, timeoutSec, env)
}

// Cleanup removes the task-level sandbox directory and all its contents.
func (d *DirectExecutor) Cleanup(sandboxPath string) {
	if sandboxPath == "" {
		return
	}
	// Safety: only remove paths that contain "/sandbox/"
	if !strings.Contains(sandboxPath, "/sandbox/") {
		return
	}
	os.RemoveAll(sandboxPath)
}

// ─── Shared internal implementation ─────────────────────────

// buildSafePATH constructs a whitelisted PATH instead of inheriting the full host PATH.
// Includes shared packages, standard system paths, and version managers (pyenv/nvm).
func buildSafePATH(pkgDir string) string {
	paths := []string{
		filepath.Join(pkgDir, ".local/bin"),
		filepath.Join(pkgDir, "node_modules/.bin"),
		"/opt/homebrew/bin",
		"/opt/homebrew/sbin",
		"/usr/local/bin",
		"/usr/bin",
		"/bin",
		"/usr/sbin",
		"/sbin",
	}
	// Preserve version manager paths (pyenv, nvm, volta) from host
	for _, p := range strings.Split(os.Getenv("PATH"), ":") {
		if strings.Contains(p, ".pyenv") ||
			strings.Contains(p, ".nvm") ||
			strings.Contains(p, ".volta") {
			paths = append(paths, p)
		}
	}
	return strings.Join(paths, ":")
}

// logCommandAudit writes a timestamped entry to the sandbox audit log.
func logCommandAudit(sandboxPath, command string) {
	auditPath := filepath.Join(sandboxPath, "audit.log")
	f, err := os.OpenFile(auditPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "[%s] %s\n", time.Now().Format(time.RFC3339), command)
}

func executeInSandboxInternal(parentCtx context.Context, sandboxPath, homeDir, userDir, command string, timeoutSec int, extraEnv map[string]string) (string, error) {
	// Enforce timeout ceiling
	if timeoutSec <= 0 {
		timeoutSec = 60
	}
	if timeoutSec > MaxTimeout {
		timeoutSec = MaxTimeout
	}

	// Fix unbalanced quotes (common LLM generation artifact)
	command = fixUnbalancedQuotes(command)

	// Audit log
	logCommandAudit(sandboxPath, command)

	ctx, cancel := context.WithTimeout(parentCtx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	pkgDir := filepath.Join(homeDir, "packages")
	if userDir == "" {
		userDir = sandboxPath
	}

	// Layer 3: OS sandbox when available
	var cmd *exec.Cmd
	if seatbeltAvailable {
		profile := buildSeatbeltProfile(defaultSeatbeltConfig())
		cmd = exec.CommandContext(ctx, "sandbox-exec", "-p", profile, "sh", "-c", command)
		log.Printf("🔒 [sandbox] Executing with seatbelt: %.80s...", command)
	} else if runtime.GOOS == "linux" {
		if bwrapPath, err := exec.LookPath("bwrap"); err == nil {
			args := buildBwrapArgs(sandboxPath, userDir, pkgDir, command)
			cmd = exec.CommandContext(ctx, bwrapPath, args...)
			log.Printf("🔒 [sandbox] Executing with bwrap: %.80s...", command)
		} else {
			cmd = exec.CommandContext(ctx, "sh", "-c", command)
		}
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", command)
	}
	cmd.Dir = sandboxPath

	// Layer 2: Build safe PATH (whitelist instead of full host PATH)
	if err := ensureUserRuntimeDirs(userDir); err != nil {
		return "", fmt.Errorf("prepare user runtime directories: %v", err)
	}
	shimDir, shimEnv, err := createRuntimeShims(sandboxPath, userDir, pkgDir)
	if err != nil {
		return "", fmt.Errorf("prepare runtime shims: %v", err)
	}
	sandboxBinPath := buildSafePATH(pkgDir)
	venvDir := filepath.Join(userDir, "python", "venvs", "default")
	userBinDir := filepath.Join(userDir, "bin")
	runtimeBinDir := filepath.Join(pkgDir, "runtime", "bin")
	sandboxBinPath = strings.Join([]string{
		shimDir,
		userBinDir,
		filepath.Join(venvDir, "bin"),
		runtimeBinDir,
		sandboxBinPath,
	}, ":")
	userConfigDir := filepath.Join(userDir, "config")
	userCacheDir := filepath.Join(userDir, "cache")
	userStateDir := filepath.Join(userDir, "state")
	userNPMDir := filepath.Join(userDir, "npm")
	userUVToolsDir := filepath.Join(userDir, "uv", "tools")
	pipCacheDir := filepath.Join(pkgDir, "cache", "pip")
	uvCacheDir := filepath.Join(pkgDir, "cache", "uv")

	// Build environment: inherit host capabilities, isolate file writes
	env := []string{
		"HOME=" + userDir,
		"PATH=" + sandboxBinPath,
		"TMPDIR=" + filepath.Join(sandboxPath, "tmp"),
		"LANG=en_US.UTF-8",
		"TERM=dumb",
		"XDG_CONFIG_HOME=" + userConfigDir,
		"XDG_CACHE_HOME=" + userCacheDir,
		"NODE_PATH=" + filepath.Join(pkgDir, "node_modules"),
		"PIP_CACHE_DIR=" + pipCacheDir,
		"PIP_REQUIRE_VIRTUALENV=true",
		"PYTHONPATH=" + filepath.Join(venvDir, "lib"),
		"UV_CACHE_DIR=" + uvCacheDir,
		"UV_TOOL_DIR=" + userUVToolsDir,
		"UV_TOOL_BIN_DIR=" + userBinDir,
		"NPM_CONFIG_PREFIX=" + userNPMDir,
		"NPM_CONFIG_CACHE=" + filepath.Join(userCacheDir, "npm"),
		"VIRTUAL_ENV=" + venvDir,
		"TOFI_USER_ROOT=" + userDir,
		"TOFI_USER_BIN=" + userBinDir,
		"TOFI_USER_STATE_DIR=" + userStateDir,
		"TOFI_USER_VENV=" + venvDir,
		"TOFI_RUNTIME_BIN=" + runtimeBinDir,
	}
	for k, v := range shimEnv {
		env = append(env, k+"="+v)
	}

	// Inject extra environment variables (e.g., skill secrets)
	for k, v := range extraEnv {
		env = append(env, k+"="+v)
	}

	cmd.Env = env

	// Limit output size
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &limitedWriter{w: &stdout, limit: MaxOutputBytes}
	cmd.Stderr = &limitedWriter{w: &stderr, limit: MaxOutputBytes}

	err = cmd.Run()

	// Combine output
	output := stdout.String()
	if errOut := stderr.String(); errOut != "" {
		if output != "" {
			output += "\n"
		}
		output += errOut
	}

	// Check for timeout
	if ctx.Err() == context.DeadlineExceeded {
		return output, fmt.Errorf("command timed out after %d seconds", timeoutSec)
	}

	if err != nil {
		return output, fmt.Errorf("command failed: %v\n%s", err, output)
	}

	return strings.TrimRight(output, "\n"), nil
}

func ensureUserRuntimeDirs(userDir string) error {
	for _, dir := range []string{
		userDir,
		filepath.Join(userDir, "bin"),
		filepath.Join(userDir, "config"),
		filepath.Join(userDir, "config", "npm"),
		filepath.Join(userDir, "cache"),
		filepath.Join(userDir, "cache", "npm"),
		filepath.Join(userDir, "state"),
		filepath.Join(userDir, "state", "tool-runs"),
		filepath.Join(userDir, "python"),
		filepath.Join(userDir, "python", "venvs"),
		filepath.Join(userDir, "npm"),
		filepath.Join(userDir, "uv"),
		filepath.Join(userDir, "uv", "tools"),
	} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	return nil
}

func createRuntimeShims(sandboxPath, userDir, pkgDir string) (string, map[string]string, error) {
	shimDir := filepath.Join(sandboxPath, ".tofi-shims")
	if err := os.MkdirAll(shimDir, 0755); err != nil {
		return "", nil, err
	}

	pythonPath, err := resolveHostTool("python3")
	if err != nil {
		return "", nil, fmt.Errorf("python3 not found: %w", err)
	}
	nodePath, _ := resolveHostTool("node")
	npmPath, _ := resolveHostTool("npm")
	npxPath, _ := resolveHostTool("npx")
	uvPath, _ := resolveHostTool("uv")

	shimEnv := map[string]string{
		"TOFI_RUNTIME_PYTHON": pythonPath,
	}
	if nodePath != "" {
		shimEnv["TOFI_RUNTIME_NODE"] = nodePath
	}
	if npmPath != "" {
		shimEnv["TOFI_RUNTIME_NPM"] = npmPath
	}
	if npxPath != "" {
		shimEnv["TOFI_RUNTIME_NPX"] = npxPath
	}
	if uvPath != "" {
		shimEnv["TOFI_RUNTIME_UV"] = uvPath
	}

	shims := map[string]string{
		"python": runtimePythonShim(),
		"python3": runtimePythonShim(),
		"pip":    runtimePipShim(),
		"pip3":   runtimePipShim(),
	}
	if nodePath != "" {
		shims["node"] = passthroughShim("runtime-node", "$TOFI_RUNTIME_NODE")
	}
	if npmPath != "" {
		shims["npm"] = passthroughShim("runtime-npm", "$TOFI_RUNTIME_NPM")
	}
	if npxPath != "" {
		shims["npx"] = passthroughShim("runtime-npx", "$TOFI_RUNTIME_NPX")
	}
	if uvPath != "" {
		shims["uv"] = passthroughShim("runtime-uv", "$TOFI_RUNTIME_UV")
	}

	for name, content := range shims {
		if err := os.WriteFile(filepath.Join(shimDir, name), []byte(content), 0755); err != nil {
			return "", nil, err
		}
	}

	return shimDir, shimEnv, nil
}

func resolveHostTool(name string) (string, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return "", err
	}
	return path, nil
}

func runtimePythonShim() string {
	return `#!/bin/sh
set -eu
STAMP="${TOFI_USER_STATE_DIR}/tool-runs/python"
mkdir -p "$(dirname "$STAMP")"
touch "$STAMP"
if [ ! -x "${TOFI_USER_VENV}/bin/python" ]; then
  mkdir -p "$(dirname "$TOFI_USER_VENV")"
  "$TOFI_RUNTIME_PYTHON" -m venv "$TOFI_USER_VENV"
fi
exec "${TOFI_USER_VENV}/bin/python" "$@"
`
}

func runtimePipShim() string {
	return `#!/bin/sh
set -eu
STAMP="${TOFI_USER_STATE_DIR}/tool-runs/pip"
mkdir -p "$(dirname "$STAMP")"
touch "$STAMP"
if [ ! -x "${TOFI_USER_VENV}/bin/python" ]; then
  mkdir -p "$(dirname "$TOFI_USER_VENV")"
  "$TOFI_RUNTIME_PYTHON" -m venv "$TOFI_USER_VENV"
fi
exec "${TOFI_USER_VENV}/bin/python" -m pip "$@"
`
}

func passthroughShim(toolName, targetVar string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
STAMP="${TOFI_USER_STATE_DIR}/tool-runs/%s"
mkdir -p "$(dirname "$STAMP")"
touch "$STAMP"
exec %s "$@"
`, toolName, targetVar)
}

// fixUnbalancedQuotes removes trailing unmatched double quotes from commands.
// LLMs sometimes generate commands with an extra trailing " (e.g. `--flag value"`),
// which causes sh to fail with "Unterminated quoted string".
func fixUnbalancedQuotes(command string) string {
	n := strings.Count(command, `"`)
	if n%2 != 0 && strings.HasSuffix(strings.TrimSpace(command), `"`) {
		command = strings.TrimSpace(command)
		command = command[:len(command)-1]
	}
	return command
}

// ─── ValidateCommand ────────────────────────────────────────

// ValidateCommand performs security checks on a command before execution.
// Layer 1: blocks dangerous prefixes and regex patterns.
// This is best-effort — OS-level sandbox-exec (Layer 3) is the true defense.
func ValidateCommand(command, sandboxPath string) error {
	if strings.TrimSpace(command) == "" {
		return fmt.Errorf("empty command")
	}

	lower := strings.ToLower(command)

	if inlineEnvAssignPattern.MatchString(command) {
		return fmt.Errorf("inline environment overrides are not allowed")
	}

	// Check blocked command prefixes
	for _, prefix := range blockedPrefixes {
		if strings.HasPrefix(lower, prefix) {
			return fmt.Errorf("blocked command: '%s' is not allowed", prefix)
		}
		// Also check after shell operators: ; && || |
		for _, sep := range []string{"; ", "&& ", "|| ", "| "} {
			if strings.Contains(lower, sep+prefix) {
				return fmt.Errorf("blocked command: '%s' is not allowed (after '%s')", prefix, strings.TrimSpace(sep))
			}
		}
	}

	// Check blocked patterns (regex-based detection)
	for _, pattern := range blockedPatterns {
		if pattern.MatchString(command) {
			return fmt.Errorf("blocked pattern: potentially dangerous command detected")
		}
	}

	if absoluteRuntimeExecPattern.MatchString(command) {
		return fmt.Errorf("absolute runtime paths are not allowed")
	}

	if err := validateAbsolutePaths(command, sandboxPath); err != nil {
		return err
	}

	return nil
}

func validateAbsolutePaths(command, sandboxPath string) error {
	allowedPrefixes := []string{
		sandboxPath,
		"/usr/",
		"/bin/",
		"/opt/",
		"/tmp/",
		"/dev/null",
		"/dev/urandom",
		"/dev/zero",
	}
	if realSandbox, err := filepath.EvalSymlinks(sandboxPath); err == nil && realSandbox != "" {
		allowedPrefixes = append(allowedPrefixes, realSandbox)
	}

	matches := absolutePathPattern.FindAllStringSubmatch(command, -1)
	for _, match := range matches {
		if len(match) < 3 {
			continue
		}
		path := strings.TrimRight(match[2], ",)")
		if path == "" || strings.HasPrefix(path, "//") {
			continue
		}
		if isAllowedAbsolutePath(path, allowedPrefixes) {
			continue
		}
		return fmt.Errorf("absolute path %q is not allowed", path)
	}
	return nil
}

func isAllowedAbsolutePath(path string, allowedPrefixes []string) bool {
	for _, prefix := range allowedPrefixes {
		if path == prefix || strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

func buildBwrapArgs(sandboxPath, userDir, pkgDir, command string) []string {
	args := []string{
		"--die-with-parent",
		"--new-session",
		"--proc", "/proc",
		"--dev", "/dev",
	}

	for _, pair := range [][2]string{
		{"/usr", "/usr"},
		{"/bin", "/bin"},
		{"/lib", "/lib"},
		{"/lib64", "/lib64"},
		{"/usr/local", "/usr/local"},
		{"/opt", "/opt"},
		{"/etc/resolv.conf", "/etc/resolv.conf"},
		{"/etc/hosts", "/etc/hosts"},
		{"/etc/nsswitch.conf", "/etc/nsswitch.conf"},
		{"/etc/passwd", "/etc/passwd"},
		{"/etc/group", "/etc/group"},
		{"/etc/localtime", "/etc/localtime"},
		{"/etc/ssl", "/etc/ssl"},
		{"/etc/pki", "/etc/pki"},
		{"/etc/ca-certificates", "/etc/ca-certificates"},
	} {
		if _, err := os.Stat(pair[0]); err == nil {
			args = append(args, "--ro-bind", pair[0], pair[1])
		}
	}

	for _, pair := range [][2]string{
		{pkgDir, pkgDir},
		{userDir, userDir},
		{sandboxPath, sandboxPath},
		{filepath.Join(sandboxPath, "tmp"), filepath.Join(sandboxPath, "tmp")},
	} {
		if pair[0] == "" {
			continue
		}
		if _, err := os.Stat(pair[0]); err == nil {
			args = append(args, "--bind", pair[0], pair[1])
		}
	}

	args = append(args,
		"--chdir", sandboxPath,
		"sh", "-c", command,
	)
	return args
}

// ─── limitedWriter ──────────────────────────────────────────

// limitedWriter wraps a writer and stops writing after limit bytes.
type limitedWriter struct {
	w       io.Writer
	limit   int
	written int
}

func (lw *limitedWriter) Write(p []byte) (n int, err error) {
	if lw.written >= lw.limit {
		return len(p), nil // silently discard
	}
	remaining := lw.limit - lw.written
	if len(p) > remaining {
		p = p[:remaining]
	}
	n, err = lw.w.Write(p)
	lw.written += n
	return len(p), err // report full write to avoid broken pipe
}
