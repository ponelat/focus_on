//go:build linux

package cronsetup

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// systemd unit names rather than a launchd label. A .timer needs a .service
// to trigger, so the "daily cron job" is two files on this platform.
const (
	serviceUnit = "focuson-sync.service"
	timerUnit   = "focuson-sync.timer"
)

// unitDir is where a user's own systemd units live — $XDG_CONFIG_HOME/systemd/user,
// which os.UserConfigDir already resolves (including the ~/.config default).
// Same base directory the shared focuson config.toml uses.
func unitDir() (string, error) {
	cfg, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cfg, "systemd", "user"), nil
}

// nixStorePrefix is deliberately the literal path rather than anything read
// from the environment: /nix/store is fixed by definition on every NixOS
// system, and a store path is exactly what must not end up baked into a
// unit file. See stableBinaryPath.
const nixStorePrefix = "/nix/store/"

// stableBinaryPath undoes too much symlink resolution on Nix systems.
//
// os.Executable() reads /proc/self/exe, which is already fully resolved, so
// a focuson installed by Nix reports a path like
// /nix/store/<hash>-focuson-1.0/bin/focuson. That path is pinned to one
// build: the next `nix profile upgrade` or `nixos-rebuild` writes a *new*
// store path, and the old one is deleted by the next garbage collection —
// at which point the timer starts failing with ENOENT and the user's daily
// backup silently stops, which is the precise failure resolveStableBinaryPath
// exists to prevent.
//
// The fix is to point the unit at one of the stable indirections Nix
// maintains for exactly this reason, preferring the most specific. Each is
// a symlink that Nix repoints on upgrade, so the unit keeps working. If the
// binary isn't under the store at all (a plain `go build -o ~/bin/focuson`),
// there's nothing to undo and the resolved path is already stable.
func stableBinaryPath(resolved string) string {
	if !strings.HasPrefix(resolved, nixStorePrefix) {
		return resolved
	}
	name := filepath.Base(resolved)
	var candidates []string
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, ".nix-profile", "bin", name))
	}
	if user := os.Getenv("USER"); user != "" {
		candidates = append(candidates, filepath.Join("/etc/profiles/per-user", user, "bin", name))
	}
	candidates = append(candidates, filepath.Join("/run/current-system/sw/bin", name))

	for _, c := range candidates {
		// Stat, not Lstat: a profile entry that dangles is worse than the
		// store path, since it never worked in the first place.
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return resolved
}

func serviceContents(binaryPath string) string {
	return fmt.Sprintf(`[Unit]
Description=FocusOn daily sync
Documentation=https://github.com/ckritzinger/focus_on

[Service]
Type=oneshot
ExecStart=%s sync
`, binaryPath)
}

// Persistent=true is the whole reason this is a systemd timer and not a
// crontab line: it makes systemd run a missed occurrence at the next boot
// or resume, so a laptop that was closed at the scheduled time still syncs.
// That matches what the macOS LaunchAgent gives us and preserves the
// promise the feature is sold on — you can't forget to back up.
func timerContents(hour, minute int) string {
	return fmt.Sprintf(`[Unit]
Description=FocusOn daily sync
Documentation=https://github.com/ckritzinger/focus_on

[Timer]
OnCalendar=*-*-* %02d:%02d:00
Persistent=true
Unit=%s

[Install]
WantedBy=timers.target
`, hour, minute, serviceUnit)
}

// systemctl runs a `systemctl --user` subcommand, turning the "no user bus"
// case into an explanation rather than systemd's own opaque message. That
// happens over plain ssh and in containers, where the units are written
// correctly but nothing can load them.
func systemctl(args ...string) error {
	cmd := exec.Command("systemctl", append([]string{"--user"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	text := strings.TrimSpace(string(out))
	if strings.Contains(text, "Failed to connect to bus") || strings.Contains(text, "DBUS_SESSION_BUS_ADDRESS") {
		return fmt.Errorf("no systemd user session to talk to (%s).\n"+
			"The unit files are written; log in to a desktop session and run "+
			"`systemctl --user enable --now %s` to start the timer", text, timerUnit)
	}
	return fmt.Errorf("systemctl --user %s: %w: %s", strings.Join(args, " "), err, text)
}

// Install writes and enables a systemd user timer that runs `focuson sync`
// daily at hour:minute (local time, 24h). Safe to call again to change the
// time — the units are overwritten and the timer restarted.
//
// There's no log file argument as there is on macOS: a systemd service's
// output goes to the journal automatically, so `journalctl --user -u
// focuson-sync.service` is the Linux equivalent of ~/Library/Logs.
func Install(hour, minute int) (jobFile string, err error) {
	if err := validTime(hour, minute); err != nil {
		return "", err
	}
	binaryPath, err := resolveStableBinaryPath()
	if err != nil {
		return "", err
	}
	dir, err := unitDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating %s: %w", dir, err)
	}

	servicePath := filepath.Join(dir, serviceUnit)
	timerPath := filepath.Join(dir, timerUnit)
	if err := os.WriteFile(servicePath, []byte(serviceContents(binaryPath)), 0o644); err != nil {
		return "", fmt.Errorf("writing %s: %w", servicePath, err)
	}
	if err := os.WriteFile(timerPath, []byte(timerContents(hour, minute)), 0o644); err != nil {
		return "", fmt.Errorf("writing %s: %w", timerPath, err)
	}

	if err := systemctl("daemon-reload"); err != nil {
		return "", err
	}
	// --now covers both a fresh install and a re-install with a new time:
	// enable is idempotent, and restart picks up the rewritten OnCalendar
	// that a plain start would leave alone on an already-running timer.
	if err := systemctl("enable", timerUnit); err != nil {
		return "", err
	}
	if err := systemctl("restart", timerUnit); err != nil {
		return "", err
	}
	return timerPath, nil
}

// Uninstall stops, disables and removes the timer and its service, if they
// exist.
func Uninstall() error {
	dir, err := unitDir()
	if err != nil {
		return err
	}
	timerPath := filepath.Join(dir, timerUnit)
	servicePath := filepath.Join(dir, serviceUnit)

	if _, statErr := os.Stat(timerPath); os.IsNotExist(statErr) {
		if _, statErr := os.Stat(servicePath); os.IsNotExist(statErr) {
			return nil
		}
	}

	// Best-effort, and deliberately not fatal: the units may already be
	// stopped, or there may be no user bus at all. Removing the files is
	// what actually has to succeed.
	systemctl("disable", "--now", timerUnit)

	for _, p := range []string{timerPath, servicePath} {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("removing %s: %w", p, err)
		}
	}
	systemctl("daemon-reload")
	return nil
}
