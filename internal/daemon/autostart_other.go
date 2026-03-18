//go:build !darwin && !linux

package daemon

import "fmt"

// EnableAutoStart is not supported on this platform.
func EnableAutoStart(homeDir string) error {
	return fmt.Errorf("auto-start is not supported on this platform yet")
}

// DisableAutoStart is not supported on this platform.
func DisableAutoStart(homeDir string) error {
	return fmt.Errorf("auto-start is not supported on this platform yet")
}

// IsAutoStartEnabled always returns false on unsupported platforms.
func IsAutoStartEnabled(homeDir string) bool {
	return false
}
