// Package cronsetup installs/removes a per-user scheduled job that runs
// `focuson sync` once a day — the "don't lose data" safety net.
//
// Real cron isn't a reliable target on either supported platform: on macOS
// it isn't guaranteed to fire if the machine was asleep and newer versions
// gate it behind Full Disk Access; on a systemd Linux desktop it's often not
// installed at all. Each OS's own scheduler is what actually runs things, so
// that's what this drives — a launchd LaunchAgent on macOS
// (cronsetup_darwin.go), a systemd user timer on Linux
// (cronsetup_linux.go) — even though the user-facing framing stays "a daily
// cron job". This file holds only what's identical on both.
package cronsetup

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// resolveStableBinaryPath finds a path to the currently-running binary that
// will still work weeks from now, and refuses anything that looks like a
// `go run` temp build — a scheduled job pointing at a build-cache path that
// gets swept the moment the temp dir is cleaned would silently stop working
// forever, which is exactly the kind of failure this command exists to
// prevent. stableBinaryPath (per-OS) gets first refusal on the resolved
// path, which is how the Nix store case is handled on Linux.
func resolveStableBinaryPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("finding current executable: %w", err)
	}
	real, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return "", fmt.Errorf("resolving %s: %w", exe, err)
	}
	if isTempBuildPath(real) {
		return "", fmt.Errorf(
			"refusing to install: %s looks like a temporary `go run`/`go test` build, not a stable binary.\n"+
				"Build one first — e.g. `go build -o ~/bin/focuson ./cli` — then run `cron install` from that binary",
			real)
	}
	return stableBinaryPath(real), nil
}

func isTempBuildPath(path string) bool {
	return strings.Contains(path, "go-build") || strings.HasPrefix(path, os.TempDir())
}

// validTime is the one shared precondition — both schedulers take a local
// 24h wall-clock time and neither can do anything sensible with a bad one.
func validTime(hour, minute int) error {
	if hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return fmt.Errorf("invalid time %02d:%02d", hour, minute)
	}
	return nil
}

// ParseTime parses "HH:MM" into (hour, minute).
func ParseTime(s string) (hour, minute int, err error) {
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("expected HH:MM, got %q", s)
	}
	hour, err = strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, fmt.Errorf("invalid hour in %q: %w", s, err)
	}
	minute, err = strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, fmt.Errorf("invalid minute in %q: %w", s, err)
	}
	return hour, minute, nil
}
