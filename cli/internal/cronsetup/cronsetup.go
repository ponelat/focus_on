// Package cronsetup installs/removes a daily job that runs `focuson sync` —
// the "don't lose data" safety net. Real cron isn't reliable on modern
// macOS (it's not guaranteed to fire if the machine was asleep, and newer
// macOS versions gate it behind Full Disk Access); launchd is what the OS
// actually uses for anything scheduled. On Windows the equivalent is a
// Task Scheduler daily task. The user-facing framing is still "a daily
// cron job" on both.
package cronsetup

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const label = "com.focuson.dailysync"

// resolveStableBinaryPath finds the currently-running binary's real,
// symlink-resolved path, and refuses anything that looks like a `go run`
// temp build — a scheduled job that points at a build-cache path that gets
// swept the moment the temp dir is cleaned would silently stop working
// forever, which is exactly the kind of failure this command exists to
// prevent.
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
	return real, nil
}

func isTempBuildPath(path string) bool {
	return strings.Contains(path, "go-build") || strings.HasPrefix(path, os.TempDir())
}

func validateTime(hour, minute int) error {
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
