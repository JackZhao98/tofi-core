//go:build linux

package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"text/template"
)

const serviceTemplate = `[Unit]
Description=Tofi AI App Engine
After=network.target

[Service]
Type=simple
ExecStart={{.ExePath}} start --foreground --home {{.HomeDir}}
Restart=on-failure
RestartSec=5

[Install]
WantedBy=default.target
`

type serviceData struct {
	ExePath string
	HomeDir string
}

func systemdServicePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "systemd", "user", systemdServiceName+".service")
}

// EnableAutoStart installs a systemd user service for tofi engine.
func EnableAutoStart(homeDir string) error {
	exe, err := autostartExePath()
	if err != nil {
		return fmt.Errorf("cannot find executable: %w", err)
	}

	data := serviceData{
		ExePath: exe,
		HomeDir: homeDir,
	}

	servicePath := systemdServicePath()

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(servicePath), 0755); err != nil {
		return fmt.Errorf("cannot create systemd user directory: %w", err)
	}

	f, err := os.Create(servicePath)
	if err != nil {
		return fmt.Errorf("cannot create service file: %w", err)
	}
	defer f.Close()

	tmpl := template.Must(template.New("service").Parse(serviceTemplate))
	if err := tmpl.Execute(f, data); err != nil {
		return fmt.Errorf("cannot write service file: %w", err)
	}

	// Reload and enable
	exec.Command("systemctl", "--user", "daemon-reload").Run()
	cmd := exec.Command("systemctl", "--user", "enable", systemdServiceName)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl enable failed: %s (%w)", string(out), err)
	}

	return nil
}

// DisableAutoStart removes the systemd user service.
func DisableAutoStart(homeDir string) error {
	// Disable first (ignore errors)
	exec.Command("systemctl", "--user", "disable", systemdServiceName).Run()
	exec.Command("systemctl", "--user", "daemon-reload").Run()

	servicePath := systemdServicePath()
	if err := os.Remove(servicePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("cannot remove service file: %w", err)
	}

	return nil
}

// IsAutoStartEnabled checks if the systemd user service exists.
func IsAutoStartEnabled(homeDir string) bool {
	_, err := os.Stat(systemdServicePath())
	return err == nil
}
