package daemon

import "os"

const (
	// LaunchAgent plist label (macOS)
	launchAgentLabel = "com.tofi.engine"
	// Systemd service name (Linux)
	systemdServiceName = "tofi-engine"
)

// autostartExePath returns the path to the current executable.
func autostartExePath() (string, error) {
	return os.Executable()
}
