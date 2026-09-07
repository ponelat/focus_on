//go:build darwin

package cronsetup

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Unchanged from the pre-port single-platform version: an existing install
// out in the world already has a LaunchAgent under this label, and renaming
// it would orphan that job rather than replace it.
const label = "com.focuson.dailysync"

func plistPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", label+".plist"), nil
}

func logPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "Logs", "focuson-sync.log"), nil
}

// stableBinaryPath has nothing to add on macOS — a path under /Applications
// or ~/bin stays valid across upgrades on its own. (Linux has the Nix store
// problem; see cronsetup_linux.go.)
func stableBinaryPath(resolved string) string { return resolved }

func plistContents(binaryPath, logFile string, hour, minute int) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s</string>
	<key>ProgramArguments</key>
	<array>
		<string>%s</string>
		<string>sync</string>
	</array>
	<key>StartCalendarInterval</key>
	<dict>
		<key>Hour</key>
		<integer>%d</integer>
		<key>Minute</key>
		<integer>%d</integer>
	</dict>
	<key>StandardOutPath</key>
	<string>%s</string>
	<key>StandardErrorPath</key>
	<string>%s</string>
	<key>RunAtLoad</key>
	<false/>
</dict>
</plist>
`, label, binaryPath, hour, minute, logFile, logFile)
}

// Install writes and loads a LaunchAgent that runs `focuson sync` daily at
// hour:minute (local time, 24h). Safe to call again to change the time —
// unloads any existing job for this label first.
func Install(hour, minute int) (jobFile string, err error) {
	if err := validTime(hour, minute); err != nil {
		return "", err
	}
	binaryPath, err := resolveStableBinaryPath()
	if err != nil {
		return "", err
	}
	path, err := plistPath()
	if err != nil {
		return "", err
	}
	logFile, err := logPath()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("creating %s: %w", filepath.Dir(path), err)
	}

	// Idempotent: unload whatever's there under this label before writing
	// the new plist, so re-running install (e.g. with a different --time)
	// doesn't fight with an already-loaded job. Ignore the error — it's
	// expected to fail with "no such job" on a fresh install.
	exec.Command("launchctl", "unload", path).Run()

	if err := os.WriteFile(path, []byte(plistContents(binaryPath, logFile, hour, minute)), 0o644); err != nil {
		return "", fmt.Errorf("writing %s: %w", path, err)
	}

	load := exec.Command("launchctl", "load", "-w", path)
	if out, err := load.CombinedOutput(); err != nil {
		return "", fmt.Errorf("launchctl load: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return path, nil
}

// Uninstall unloads and removes the LaunchAgent, if one exists.
func Uninstall() error {
	path, err := plistPath()
	if err != nil {
		return err
	}
	if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
		return nil
	}
	exec.Command("launchctl", "unload", path).Run() // best-effort; job may already be gone
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("removing %s: %w", path, err)
	}
	return nil
}
