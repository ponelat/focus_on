//go:build darwin

package cronsetup

import (
	"strings"
	"testing"
)

func TestPlistContentsIncludesEverythingNeeded(t *testing.T) {
	out := plistContents("/usr/local/bin/focuson", "/tmp/focuson-sync.log", 18, 30)
	for _, want := range []string{
		label,
		"/usr/local/bin/focuson",
		"<string>sync</string>",
		"<integer>18</integer>",
		"<integer>30</integer>",
		"/tmp/focuson-sync.log",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("plist missing %q:\n%s", want, out)
		}
	}
}

// The label is load-bearing for anyone who installed before the Linux port
// split this file: launchctl identifies an already-loaded job by it, so a
// rename would leave the old job running forever alongside the new one
// rather than replacing it.
func TestLabelIsUnchangedFromTheOriginalInstall(t *testing.T) {
	if label != "com.focuson.dailysync" {
		t.Fatalf("label = %q; renaming it orphans existing installs' LaunchAgents", label)
	}
}
