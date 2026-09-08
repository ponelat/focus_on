//go:build windows

package cronsetup

import (
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"unicode/utf16"
)

// Task Scheduler name. Kept short and ASCII — schtasks is picky about
// punctuation, and this is what `cron uninstall` looks up later.
const taskName = "FocusOnDailySync"

func xmlEscape(s string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&apos;",
	)
	return replacer.Replace(s)
}

func taskXML(binaryPath string, hour, minute int) string {
	// StartBoundary's date is arbitrary; ScheduleByDay makes it recur
	// every day at that local time. Battery + StartWhenAvailable matter
	// on a laptop — schtasks' /Create defaults skip both.
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo>
    <Description>Daily focuson sync — commit and push the data directory</Description>
    <URI>\%s</URI>
  </RegistrationInfo>
  <Triggers>
    <CalendarTrigger>
      <StartBoundary>2020-01-01T%02d:%02d:00</StartBoundary>
      <Enabled>true</Enabled>
      <ScheduleByDay>
        <DaysInterval>1</DaysInterval>
      </ScheduleByDay>
    </CalendarTrigger>
  </Triggers>
  <Principals>
    <Principal id="Author">
      <LogonType>InteractiveToken</LogonType>
      <RunLevel>LeastPrivilege</RunLevel>
    </Principal>
  </Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <AllowHardTerminate>true</AllowHardTerminate>
    <StartWhenAvailable>true</StartWhenAvailable>
    <RunOnlyIfNetworkAvailable>false</RunOnlyIfNetworkAvailable>
    <IdleSettings>
      <StopOnIdleEnd>false</StopOnIdleEnd>
      <RestartOnIdle>false</RestartOnIdle>
    </IdleSettings>
    <AllowStartOnDemand>true</AllowStartOnDemand>
    <Enabled>true</Enabled>
    <Hidden>false</Hidden>
    <RunOnlyIfIdle>false</RunOnlyIfIdle>
    <WakeToRun>false</WakeToRun>
    <ExecutionTimeLimit>PT1H</ExecutionTimeLimit>
    <Priority>7</Priority>
  </Settings>
  <Actions Context="Author">
    <Exec>
      <Command>%s</Command>
      <Arguments>sync</Arguments>
    </Exec>
  </Actions>
</Task>
`, taskName, hour, minute, xmlEscape(binaryPath))
}

func writeUTF16LE(path, s string) error {
	u := utf16.Encode([]rune(s))
	buf := make([]byte, 2+len(u)*2)
	buf[0], buf[1] = 0xFF, 0xFE
	for i, r := range u {
		binary.LittleEndian.PutUint16(buf[2+i*2:], r)
	}
	return os.WriteFile(path, buf, 0o644)
}

// Install registers a daily Task Scheduler job that runs `focuson sync` at
// hour:minute (local time, 24h). Safe to call again to change the time —
// `/F` overwrites any existing task with this name.
func Install(hour, minute int) (string, error) {
	if err := validateTime(hour, minute); err != nil {
		return "", err
	}
	binaryPath, err := resolveStableBinaryPath()
	if err != nil {
		return "", err
	}

	tmp, err := os.CreateTemp("", "focuson-task-*.xml")
	if err != nil {
		return "", fmt.Errorf("creating task xml: %w", err)
	}
	xmlPath := tmp.Name()
	tmp.Close()
	defer os.Remove(xmlPath)

	if err := writeUTF16LE(xmlPath, taskXML(binaryPath, hour, minute)); err != nil {
		return "", fmt.Errorf("writing task xml: %w", err)
	}

	cmd := exec.Command("schtasks", "/Create", "/TN", taskName, "/XML", xmlPath, "/F")
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("schtasks create: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return taskName, nil
}

// Uninstall removes the Task Scheduler job, if one exists.
func Uninstall() error {
	cmd := exec.Command("schtasks", "/Delete", "/TN", taskName, "/F")
	out, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	msg := strings.ToLower(string(out))
	if strings.Contains(msg, "cannot find") || strings.Contains(msg, "does not exist") {
		return nil
	}
	return fmt.Errorf("schtasks delete: %w: %s", err, strings.TrimSpace(string(out)))
}
