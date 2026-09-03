package ui

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Config is everything sessui persists between runs. Popup geometry is NOT
// here on purpose: tmux needs the width before this binary starts, so it
// lives in the @sessui-width tmux option and sessui.tmux reads it at open
// time. Everything the binary itself owns goes in this file.
//
// The zero value is a valid configuration -- a fresh install has no file --
// and Load never fails the program over a missing or unreadable one.
type Config struct {
	Columns []columnSetting `json:"columns,omitempty"`
}

// withDefaults fills anything the file left unset, so callers never branch
// on "was this configured". A config that names no columns gets the shipped
// table, which is exactly what a first run should see.
func (c Config) withDefaults() Config {
	if len(c.Columns) == 0 {
		c.Columns = defaultColumnSettings()
	}
	return c
}

// configPath honours XDG so the file lands where chezmoi and every other
// dotfile tool expects: ~/.config/sessui/config.json.
func configPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("config dir: %w", err)
	}
	return filepath.Join(dir, "sessui", "config.json"), nil
}

// LoadConfig reads the persisted configuration, or the defaults when there
// is none yet. A missing file is the normal first-run case and is silent; a
// file that exists but cannot be parsed is surfaced, because silently
// falling back would hide a hand-edit gone wrong.
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

// SaveConfig writes the configuration atomically: to a sibling temp file,
// then renamed over the target, so a crash mid-write leaves the previous
// config intact rather than a truncated one. Pretty-printed because this
// file is meant to be readable in a diff and editable by hand.
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
	// Any failure from here on removes the temp file; success renames it
	// away first, so the deferred remove becomes a no-op.
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
