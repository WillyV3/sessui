package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// useTempConfigDir points os.UserConfigDir at a throwaway directory for one
// test, so these never read or write the real ~/.config/sessui.
func useTempConfigDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	return dir
}

// TestConfigPath_IsDotConfigOnEveryOS pins the Mac fix: with XDG unset the
// file lives under $HOME/.config regardless of GOOS -- never under
// ~/Library/Application Support, where os.UserConfigDir would put it on
// Darwin and where no dotfile tooling would ever look.
func TestConfigPath_IsDotConfigOnEveryOS(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")

	got, err := configPath()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, ".config", "sessui", "config.json"); got != want {
		t.Errorf("configPath() = %q, want %q", got, want)
	}

	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg"))
	if got, _ := configPath(); got != filepath.Join(home, "xdg", "sessui", "config.json") {
		t.Errorf("configPath() with XDG set = %q, want it honoured", got)
	}
}

// TestConfig_FirstRunIsTheShippedTable pins the upgrade contract: no file on
// disk means the exact default column set, with no error -- a user who has
// never opened the editor must see the table they always had.
func TestConfig_FirstRunIsTheShippedTable(t *testing.T) {
	useTempConfigDir(t)

	c, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() on first run: %v, want nil (a missing file is normal)", err)
	}
	if len(c.Columns) != len(defaultColumnOrder) {
		t.Fatalf("got %d columns, want the %d shipped defaults", len(c.Columns), len(defaultColumnOrder))
	}
	for i, id := range defaultColumnOrder {
		if c.Columns[i].ID != id {
			t.Errorf("column %d = %s, want %s", i, c.Columns[i].ID, id)
		}
	}
}

// TestConfig_RoundTrip is the behaviour a user actually relies on: what they
// arrange in the editor is what they get back next launch, including a
// hidden column (absent from the slice) and a width override.
func TestConfig_RoundTrip(t *testing.T) {
	dir := useTempConfigDir(t)
	want := Config{Columns: []columnSetting{
		{ID: colStatus},
		{ID: colSession, Width: 30},
		{ID: colAge},
	}}

	if err := SaveConfig(want); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	got, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if len(got.Columns) != 3 {
		t.Fatalf("got %d columns back, want 3", len(got.Columns))
	}
	for i := range want.Columns {
		if got.Columns[i] != want.Columns[i] {
			t.Errorf("column %d = %+v, want %+v", i, got.Columns[i], want.Columns[i])
		}
	}

	// The file is where dotfile tooling expects it, and readable in a diff.
	raw, err := os.ReadFile(filepath.Join(dir, "sessui", "config.json"))
	if err != nil {
		t.Fatalf("config not at the XDG path: %v", err)
	}
	if !strings.Contains(string(raw), "\n  ") {
		t.Errorf("config is not pretty-printed:\n%s", raw)
	}
	if leftovers, _ := filepath.Glob(filepath.Join(dir, "sessui", ".config-*")); len(leftovers) != 0 {
		t.Errorf("atomic write left temp files behind: %v", leftovers)
	}
}

// TestConfig_CorruptFileIsSurfaced: a hand-edit that breaks the JSON must
// not be silently replaced by defaults -- the user would lose their layout
// and never learn why. Load still returns a usable config so the app runs.
func TestConfig_CorruptFileIsSurfaced(t *testing.T) {
	dir := useTempConfigDir(t)
	path := filepath.Join(dir, "sessui", "config.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"columns": [`), 0o644); err != nil {
		t.Fatal(err)
	}

	c, err := LoadConfig()
	if err == nil {
		t.Fatal("LoadConfig() on corrupt JSON returned nil error, want the parse failure surfaced")
	}
	if len(c.Columns) == 0 {
		t.Error("LoadConfig() on corrupt JSON returned no columns; the app must still be usable")
	}
}
