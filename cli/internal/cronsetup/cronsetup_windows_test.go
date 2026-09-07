//go:build windows

package cronsetup

import (
	"strings"
	"testing"
)

func TestTaskXMLIncludesBinaryAndTime(t *testing.T) {
	out := taskXML(`C:\Users\naama\bin\focuson.exe`, 18, 30)
	for _, want := range []string{
		taskName,
		`C:\Users\naama\bin\focuson.exe`,
		"<Arguments>sync</Arguments>",
		"2020-01-01T18:30:00",
		"<DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>",
		"<StartWhenAvailable>true</StartWhenAvailable>",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("task XML missing %q:\n%s", want, out)
		}
	}
}

func TestXMLEscape(t *testing.T) {
	got := xmlEscape(`C:\A&B\focuson.exe`)
	if !strings.Contains(got, "&amp;") {
		t.Fatalf("expected ampersand escaped, got %q", got)
	}
}
