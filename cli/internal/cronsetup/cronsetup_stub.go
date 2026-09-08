//go:build !darwin && !windows && !linux

package cronsetup

import "fmt"

// Install is not implemented on this OS — launchd, Task Scheduler and
// systemd user timers are the three schedulers this package knows how to
// drive.
func Install(hour, minute int) (string, error) {
	if err := validateTime(hour, minute); err != nil {
		return "", err
	}
	return "", fmt.Errorf("daily sync scheduling is only supported on macOS, Windows and Linux")
}

// Uninstall is a no-op on unsupported platforms.
func Uninstall() error {
	return nil
}
