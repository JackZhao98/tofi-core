//go:build darwin

package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"text/template"
)

const plistTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>{{.Label}}</string>
    <key>ProgramArguments</key>
    <array>
        <string>{{.ExePath}}</string>
        <string>start</string>
        <string>--foreground</string>
        <string>--home</string>
        <string>{{.HomeDir}}</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <false/>
    <key>StandardOutPath</key>
    <string>{{.LogPath}}</string>
    <key>StandardErrorPath</key>
    <string>{{.LogPath}}</string>
</dict>
</plist>
`

type plistData struct {
	Label   string
	ExePath string
	HomeDir string
	LogPath string
}

func launchAgentPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", launchAgentLabel+".plist")
}

// EnableAutoStart installs a macOS LaunchAgent for tofi engine.
func EnableAutoStart(homeDir string) error {
	exe, err := autostartExePath()
	if err != nil {
		return fmt.Errorf("cannot find executable: %w", err)
	}

	logPath := filepath.Join(homeDir, "logs", "engine.log")

	data := plistData{
		Label:   launchAgentLabel,
		ExePath: exe,
		HomeDir: homeDir,
		LogPath: logPath,
	}

	plistPath := launchAgentPath()

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(plistPath), 0755); err != nil {
		return fmt.Errorf("cannot create LaunchAgents directory: %w", err)
	}

	f, err := os.Create(plistPath)
	if err != nil {
		return fmt.Errorf("cannot create plist: %w", err)
	}
	defer f.Close()

	tmpl := template.Must(template.New("plist").Parse(plistTemplate))
	if err := tmpl.Execute(f, data); err != nil {
		return fmt.Errorf("cannot write plist: %w", err)
	}

	// Load the agent
	cmd := exec.Command("launchctl", "load", plistPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("launchctl load failed: %s (%w)", string(out), err)
	}

	return nil
}

// DisableAutoStart removes the macOS LaunchAgent.
func DisableAutoStart(homeDir string) error {
	plistPath := launchAgentPath()

	// Unload first (ignore errors if not loaded)
	cmd := exec.Command("launchctl", "unload", plistPath)
	cmd.Run() // ignore error

	if err := os.Remove(plistPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("cannot remove plist: %w", err)
	}

	return nil
}

// IsAutoStartEnabled checks if the LaunchAgent plist exists.
func IsAutoStartEnabled(homeDir string) bool {
	_, err := os.Stat(launchAgentPath())
	return err == nil
}
