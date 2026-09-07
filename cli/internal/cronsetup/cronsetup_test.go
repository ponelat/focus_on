package cronsetup

import (
	"os"
	"testing"
)

// Install/Uninstall aren't unit-tested here — they shell out to the real
// OS scheduler (launchctl / schtasks) and, worse, os.Executable() during
// `go test` itself resolves to a temp build (which isTempBuildPath is
// specifically designed to reject), making the full path untestable without
// mutating real scheduler state. The pieces that matter — content
// generation, the temp-path guard, time parsing — are fully testable in
// isolation below.

func TestIsTempBuildPathRejectsGoBuildAndTempDir(t *testing.T) {
	cases := map[string]bool{
		"/private/var/folders/xy/abc/T/go-build12345/b001/exe/focuson": true,
		os.TempDir() + "/focuson-test-binary":                          true,
		"/usr/local/bin/focuson":                                       false,
		"/Users/carl/bin/focuson":                                      false,
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

func TestInstallRefusesATempBuildPath(t *testing.T) {
	// go test's own binary is itself a temp build, so this exercises the
	// real guard end-to-end without touching actual launchd state.
	if _, err := Install(18, 0); err == nil {
		t.Fatalf("expected Install to refuse running from the go test binary")
	}
}
