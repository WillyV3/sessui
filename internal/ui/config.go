package ui

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Config is everything sessui persists between runs. Popup geometry is not
// here: tmux needs the width before this binary starts, so it lives in the
// @sessui-width tmux option.
//
// The zero value is a valid configuration, and Load never fails the program
// over a missing or unreadable file.
type Config struct {
	Columns []columnSetting `json:"columns,omitempty"`
	// Icons is the glyph set: "nerd" (default, needs a Nerd Font) or "ascii".
	Icons glyphSet    `json:"icons,omitempty"`
	Theme ThemeConfig `json:"theme,omitempty"`
	// Hosts are the ssh aliases whose sessions are listed alongside local
	// ones. Empty is the shipped state: no network until the user picks one.
	Hosts []string `json:"hosts,omitempty"`
}

// ThemeConfig is how the palette is chosen and, optionally, recoloured.
type ThemeConfig struct {
	// Palette is "auto" (Omarchy's theme when present, else dark), or a
	// pinned built-in: "dark" | "light".
	Palette paletteSource `json:"palette,omitempty"`
	// Roles remaps a UI role ("needs-you") to a Palette slot ("blue"). Never
	// raw hex: a slot is resolved against whatever Palette is live, so the
	// mapping survives a theme switch. Unknown roles or slots fall back to
	// their defaults -- see resolveRole.
	Roles map[role]slotName `json:"roles,omitempty"`
}

// withDefaults fills anything the file left unset, so callers never branch
// on "was this configured".
func (c Config) withDefaults() Config {
	if len(c.Columns) == 0 {
		c.Columns = defaultColumnSettings()
	}
	if c.Icons == "" {
		c.Icons = glyphsNerd
	}
	if c.Theme.Palette == "" {
		c.Theme.Palette = paletteAuto
	}
	return c
}

// configPath is $XDG_CONFIG_HOME/sessui/config.json, defaulting to
// ~/.config/sessui/config.json on every OS. Not os.UserConfigDir: on Darwin
// that returns ~/Library/Application Support and ignores XDG_CONFIG_HOME,
// which is the wrong place for a dotfile-managed tool.
func configPath() (string, error) {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("home dir: %w", err)
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "sessui", "config.json"), nil
}

// LoadConfig reads the persisted configuration, or the defaults when there
// is none. A missing file is the normal first run and is silent; a file that
// exists but cannot be parsed is surfaced, because a silent fallback would
// hide a hand-edit gone wrong.
func LoadConfig() (Config, error) {
	path, err := configPath()
	if err != nil {
		return Config{}.withDefaults(), err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Config{}.withDefaults(), nil
	}
	if err != nil {
		return Config{}.withDefaults(), fmt.Errorf("read %s: %w", path, err)
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return Config{}.withDefaults(), fmt.Errorf("parse %s: %w", path, err)
	}
	return c.withDefaults(), nil
}

// SaveConfig writes the configuration atomically -- a sibling temp file
// renamed over the target -- so a crash mid-write leaves the previous config
// intact. Pretty-printed because the file is meant to be edited by hand.
func SaveConfig(c Config) error {
	path, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*.json")
	if err != nil {
		return fmt.Errorf("temp file: %w", err)
	}
	tmpName := tmp.Name()
	// Success renames the temp file away first, so the deferred remove
	// becomes a no-op.
	defer os.Remove(tmpName)
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return fmt.Errorf("write %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("install %s: %w", path, err)
	}
	return nil
}
