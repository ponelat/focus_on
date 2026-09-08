//go:build linux

package cronsetup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServiceContentsRunsSync(t *testing.T) {
	out := serviceContents("/usr/local/bin/focuson")
	for _, want := range []string{
		"Type=oneshot",
		"ExecStart=/usr/local/bin/focuson sync",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("service unit missing %q:\n%s", want, out)
		}
	}
}

func TestTimerContentsSchedulesTheGivenTime(t *testing.T) {
	out := timerContents(9, 5)
	for _, want := range []string{
		"OnCalendar=*-*-* 09:05:00", // zero-padded: systemd rejects "9:5"
		"Persistent=true",           // catch up after a suspended/off machine
		"Unit=" + serviceUnit,
		"WantedBy=timers.target",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("timer unit missing %q:\n%s", want, out)
		}
	}
}

func TestPreferNixProfilePathPrefersAProfileOverTheStore(t *testing.T) {
	// A store path pins one build; the next garbage collection deletes it
	// and the timer silently stops working. The profile symlink Nix
	// repoints on upgrade is what the unit has to name instead.
	home := t.TempDir()
	t.Setenv("HOME", home)

	profileBin := filepath.Join(home, ".nix-profile", "bin")
	if err := os.MkdirAll(profileBin, 0o755); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(profileBin, "focuson")
	if err := os.WriteFile(want, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	got := preferNixProfilePath("/nix/store/abc123-focuson-1.0/bin/focuson")
	if got != want {
		t.Errorf("preferNixProfilePath(store path) = %q, want %q", got, want)
	}
}

func TestPreferNixProfilePathFallsBackToTheStorePathWhenNoProfileExists(t *testing.T) {
	// Better a path that works today than one that never worked: a
	// dangling profile entry would break the timer immediately.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USER", "nobody-with-no-profile")

	// A name nothing on the host could plausibly have installed, so the
	// system-wide candidate (/run/current-system/sw/bin) can't accidentally
	// satisfy the lookup when this test runs on a real NixOS machine.
	const store = "/nix/store/abc123-focuson-1.0/bin/focuson-not-a-real-binary"
	if got := preferNixProfilePath(store); got != store {
		t.Errorf("preferNixProfilePath(%q) = %q, want it unchanged", store, got)
	}
}
