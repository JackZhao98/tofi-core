package executor

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// DockerExecutor runs commands inside ephemeral Docker containers.
// Each task gets a fresh container that is destroyed after execution.
type DockerExecutor struct {
	imageName string // Docker image name (e.g. "tofi-sandbox:latest")
	dataDir   string // Host data directory for volume mounts
}

// NewDockerExecutor creates a DockerExecutor after verifying Docker is available.
func NewDockerExecutor(dataDir, imageName string) (*DockerExecutor, error) {
	if err := exec.Command("docker", "info").Run(); err != nil {
		return nil, fmt.Errorf("docker is not available: %v (is Docker running?)", err)
	}
	return &DockerExecutor{
		imageName: imageName,
		dataDir:   dataDir,
	}, nil
}

// containerName returns a unique container name for a task.
func containerName(cardID string) string {
	if cardID == "" {
		cardID = "unknown"
	}
	return "tofi-task-" + cardID
}

// CreateSandbox is a no-op for Docker executor — containers are created at execution time.
// Returns the in-container workspace path.
func (d *DockerExecutor) CreateSandbox(cfg SandboxConfig) (string, error) {
	return "/workspace", nil
}

// Execute runs a command inside an ephemeral Docker container.
// The container is created, runs the command, and is automatically removed (--rm).
func (d *DockerExecutor) Execute(ctx context.Context, sandboxPath, userDir, command string, timeoutSec int, env map[string]string) (string, error) {
	if timeoutSec <= 0 {
		timeoutSec = 60
	}
	if timeoutSec > MaxTimeout {
		timeoutSec = MaxTimeout
	}

	execCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	// Extract cardID from context or generate a short ID
	cardID := "task"
	if userDir != "" {
		cardID = filepath.Base(userDir)
	}
	name := containerName(cardID + "-" + fmt.Sprintf("%d", time.Now().UnixNano()%100000))

	// Build docker run args with security hardening
	args := []string{
		"run", "--rm",
		"--name", name,

		// Resource limits
		"--memory", "512m",
		"--cpus", "1",
		"--pids-limit", "256", // Prevent fork bombs

		// Security hardening
		"--read-only",              // Read-only root filesystem
		"--no-new-privileges",      // Prevent privilege escalation
		"--cap-drop", "ALL",        // Drop all Linux capabilities

		// Writable temporary directories
		"--tmpfs", "/tmp:rw,noexec,nosuid,size=100m",

		// Working directory
		"-w", "/workspace",
	}

	// Volume mounts — user data directory
	if userDir != "" {
		args = append(args, "-v", userDir+":/home/user")
	}

	// Network policy — default allow for pip install / curl / API calls
	// Can be restricted later with --network=none
	// (AllowNetwork is checked via SandboxConfig at CreateSandbox time,
	//  but since we don't store it, we default to allowing network)

	// Environment variables
	defaultEnv := map[string]string{
		"HOME":   "/workspace",
		"TMPDIR": "/tmp",
		"LANG":   "en_US.UTF-8",
		"TERM":   "dumb",
	}
	for k, v := range defaultEnv {
		args = append(args, "-e", k+"="+v)
	}
	for k, v := range env {
		args = append(args, "-e", k+"="+v)
	}

	// Image + command
	args = append(args, d.imageName, "sh", "-c", command)

	cmd := exec.CommandContext(execCtx, "docker", args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &limitedWriter{w: &stdout, limit: MaxOutputBytes}
	cmd.Stderr = &limitedWriter{w: &stderr, limit: MaxOutputBytes}

	log.Printf("[docker] running: %s (container: %s)", truncateCmd(command, 80), name)
	err := cmd.Run()

	output := stdout.String()
	if errOut := stderr.String(); errOut != "" {
		if output != "" {
			output += "\n"
		}
		output += errOut
	}

	if execCtx.Err() == context.DeadlineExceeded {
		// Force kill container on timeout
		_ = exec.Command("docker", "kill", name).Run()
		return output, fmt.Errorf("command timed out after %d seconds", timeoutSec)
	}

	if err != nil {
		return output, fmt.Errorf("command failed: %v\n%s", err, output)
	}

	return strings.TrimRight(output, "\n"), nil
}

// Cleanup is a no-op — ephemeral containers are automatically removed via --rm.
func (d *DockerExecutor) Cleanup(sandboxPath string) {
	// Nothing to do — container was created with --rm
}

// truncateCmd truncates a command string for logging.
func truncateCmd(cmd string, maxLen int) string {
	if len(cmd) <= maxLen {
		return cmd
	}
	return cmd[:maxLen] + "..."
}
