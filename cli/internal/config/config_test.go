package config

import (
	"path/filepath"
	"testing"
)

// isolateConfigDir points UserConfigDir/UserHomeDir at a temp tree so tests
// never read or write the real machine config. Unix uses HOME (and
// XDG_CONFIG_HOME); Windows uses USERPROFILE + APPDATA.
func isolateConfigDir(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))
	t.Setenv("APPDATA", filepath.Join(dir, "AppData", "Roaming"))
	t.Setenv("LOCALAPPDATA", filepath.Join(dir, "AppData", "Local"))
}

func TestLoadNoConfigIsNotAnError(t *testing.T) {
	isolateConfigDir(t)
	cfg, ok, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if ok {
		t.Fatalf("expected no config on first run, got %+v", cfg)
	}
}

func TestSaveThenLoadRoundTrips(t *testing.T) {
	isolateConfigDir(t)
	want := Config{DataDir: "/tmp/whatever/focuson-data"}
	if err := Save(want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, ok, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !ok || got.DataDir != want.DataDir {
		t.Fatalf("round trip mismatch: got %+v ok=%v, want %+v", got, ok, want)
	}
}
