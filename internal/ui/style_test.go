package ui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// stubThemeColor replaces the omarchy-theme-color query for one test with a
// fixed answer table, and counts calls -- so a test can assert not only what
// palette came back but whether the environment was consulted at all.
func stubThemeColor(t *testing.T, answers map[string]string) *int {
	t.Helper()
	calls := 0
	prev := themeColorQuery
	themeColorQuery = func(name string) (string, bool) {
		calls++
		hex, ok := answers[name]
		return hex, ok
	}
	t.Cleanup(func() { themeColorQuery = prev })
	return &calls
}

// TestLoadPalette pins the three behaviours the setting exists for.
func TestLoadPalette(t *testing.T) {
	t.Run("a pinned built-in never consults the environment", func(t *testing.T) {
		calls := stubThemeColor(t, map[string]string{"accent": "#123456"})
		for _, src := range []paletteSource{paletteDark, paletteLight} {
			got := loadPalette(src)
			if got != builtinPalettes[src] {
				t.Errorf("%s: got %+v, want the built-in", src, got)
			}
		}
		if *calls != 0 {
			t.Errorf("pinned palettes made %d theme queries, want 0", *calls)
		}
	})

	t.Run("light and dark are complete and different", func(t *testing.T) {
		d, l := builtinPalettes[paletteDark], builtinPalettes[paletteLight]
		for _, p := range []Palette{d, l} {
			for _, c := range []lipgloss.Color{p.Accent, p.Background, p.Foreground, p.Muted, p.Red, p.Green, p.Yellow, p.Blue, p.Magenta, p.Cyan, p.Orange} {
				if len(c) != 7 || c[0] != '#' {
					t.Errorf("built-in slot %q is not a #rrggbb colour", c)
				}
			}
		}
		if d.Background == l.Background || d.Foreground == l.Foreground {
			t.Error("light and dark share a background or foreground; they must invert")
		}
	})

	// The auto path shells out through omarchyThemeAvailable (a real
	// LookPath) before consulting themeColorQuery; on a box with the
	// binary, a partially-answering theme must degrade per slot.
	t.Run("auto degrades per slot to the dark palette", func(t *testing.T) {
		if !omarchyThemeAvailable() {
			t.Skip("omarchy-theme-color not on PATH; the auto path is exercised on Omarchy CI only")
		}
		stubThemeColor(t, map[string]string{"accent": "#ABCDEF"}) // everything else unanswered
		got := loadPalette(paletteAuto)
		if got.Accent != "#ABCDEF" {
			t.Errorf("Accent = %q, want the theme's answer", got.Accent)
		}
		if got.Red != builtinPalettes[paletteDark].Red {
			t.Errorf("Red = %q, want the dark fallback for an unanswered slot", got.Red)
		}
	})
}
