//go:build !darwin && !windows

package cronsetup

import "fmt"

// Install is not implemented on this OS — launchd and Task Scheduler are
// the two schedulers this package knows how to drive.
func Install(hour, minute int) (string, error) {
	if err := validateTime(hour, minute); err != nil {
		return "", err
	}
	return "", fmt.Errorf("daily sync scheduling is only supported on macOS and Windows")
}

// Uninstall is a no-op on unsupported platforms.
func Uninstall() error {
	return nil
}
