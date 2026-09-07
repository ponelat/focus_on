package cronsetup

import (
	"os"
	"testing"
)

// Install/Uninstall aren't unit-tested here — they shell out to the real
// scheduler (launchctl / systemctl) and, worse, os.Executable() during
// `go test` itself resolves to a temp build (which isTempBuildPath is
// specifically designed to reject), making the full path untestable without
// mutating real launchd/systemd state. The pieces that matter — content
// generation, the temp-path guard, time parsing — are fully testable in
// isolation, here for what's shared and in the per-OS test files for the
// rest.

func TestIsTempBuildPathRejectsGoBuildAndTempDir(t *testing.T) {
	cases := map[string]bool{
		"/private/var/folders/xy/abc/T/go-build12345/b001/exe/focuson": true,
		os.TempDir() + "/focuson-test-binary":                          true,
		"/usr/local/bin/focuson":                                       false,
		"/Users/carl/bin/focuson":                                      false,
		"/home/carl/bin/focuson":                                       false,
	}
	for path, want := range cases {
		if got := isTempBuildPath(path); got != want {
			t.Errorf("isTempBuildPath(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestParseTime(t *testing.T) {
	h, m, err := ParseTime("18:30")
	if err != nil || h != 18 || m != 30 {
		t.Fatalf("ParseTime(18:30) = %d, %d, %v", h, m, err)
	}
	if _, _, err := ParseTime("not-a-time"); err == nil {
		t.Fatalf("expected an error for a malformed time")
	}
	if _, _, err := ParseTime("18"); err == nil {
		t.Fatalf("expected an error for a time missing minutes")
	}
}

func TestValidTime(t *testing.T) {
	for _, ok := range [][2]int{{0, 0}, {23, 59}, {9, 5}} {
		if err := validTime(ok[0], ok[1]); err != nil {
			t.Errorf("validTime(%d, %d) = %v, want nil", ok[0], ok[1], err)
		}
	}
	for _, bad := range [][2]int{{24, 0}, {-1, 0}, {0, 60}, {0, -1}} {
		if err := validTime(bad[0], bad[1]); err == nil {
			t.Errorf("validTime(%d, %d) = nil, want an error", bad[0], bad[1])
		}
	}
}

func TestInstallRefusesATempBuildPath(t *testing.T) {
	// go test's own binary is itself a temp build, so this exercises the
	// real guard end-to-end without touching actual launchd/systemd state.
	if _, err := Install(18, 0); err == nil {
		t.Fatalf("expected Install to refuse running from the go test binary")
	}
}

func TestStableBinaryPathLeavesAnOrdinaryPathAlone(t *testing.T) {
	// True on both platforms, for different reasons — see each OS's
	// stableBinaryPath. Only a Nix store path is ever rewritten.
	const ordinary = "/usr/local/bin/focuson"
	if got := stableBinaryPath(ordinary); got != ordinary {
		t.Errorf("stableBinaryPath(%q) = %q, want it unchanged", ordinary, got)
	}
}
