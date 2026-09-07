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
