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

// stylePalette is a fixed, fully populated Palette for the role tests below
// -- distinct slot values throughout, so a wrong-slot bug can't hide behind
// two slots that happen to share a colour.
var stylePalette = Palette{
	Accent: "#00AFFF", Background: "#1E1E2E", Foreground: "#CDD6F4", Muted: "#6C7086",
	Red: "#F38BA8", Green: "#A6E3A1", Yellow: "#F9E2AF", Blue: "#89B4FA",
	Magenta: "#F5C2E7", Cyan: "#94E2D5", Orange: "#FAB387",
}

// TestNewStyles_EmptyMappingMatchesShipped pins that an empty (or nil) role
// mapping resolves every role-driven field to exactly what newStyles
// hardcoded before roles existed. lipgloss strips colour from Render() in a
// non-TTY `go test` (verified: Render() returns the plain text, no SGR at
// all here), so this compares each Style's own GetForeground/GetBackground
// -- introspection on the style value, not a rendered escape sequence --
// against the literal Palette field newStyles used to write directly.
func TestNewStyles_EmptyMappingMatchesShipped(t *testing.T) {
	p := stylePalette
	for _, roles := range []map[role]slotName{nil, {}} {
		got := newStyles(p, roles)
		want := map[string]lipgloss.Color{
			"Cursor.fg":       p.Accent,
			"Header.fg":       p.Accent,
			"Working.fg":      p.Accent,
			"PeerUp.fg":       p.Green,
			"PeerDown.fg":     p.Red,
			"NeedsYouPill.bg": p.Red,
			"MailPill.bg":     p.Accent,
		}
		fg := map[string]lipgloss.Style{"Cursor.fg": got.Cursor, "Header.fg": got.Header, "Working.fg": got.Working, "PeerUp.fg": got.PeerUp, "PeerDown.fg": got.PeerDown}
		bg := map[string]lipgloss.Style{"NeedsYouPill.bg": got.NeedsYouPill, "MailPill.bg": got.MailPill}
		for name, s := range fg {
			if s.GetForeground() != want[name] {
				t.Errorf("%s = %v, want %v (roles=%#v)", name, s.GetForeground(), want[name], roles)
			}
		}
		for name, s := range bg {
			if s.GetBackground() != want[name] {
				t.Errorf("%s = %v, want %v (roles=%#v)", name, s.GetBackground(), want[name], roles)
			}
		}
		if got.selectedBG != bgSGR(p.Muted) {
			t.Errorf("selectedBG = %q, want bgSGR(Muted) = %q", got.selectedBG, bgSGR(p.Muted))
		}
	}
}

// TestNewStyles_RoleMappingRecolours pins that a user's role→slot mapping
// actually reaches the style it governs, and only that style.
func TestNewStyles_RoleMappingRecolours(t *testing.T) {
	p := stylePalette
	got := newStyles(p, map[role]slotName{roleNeedsYou: slotBlue})
	if bg := got.NeedsYouPill.GetBackground(); bg != p.Blue {
		t.Errorf("NeedsYouPill background = %v, want Blue (%v)", bg, p.Blue)
	}
	if fg := got.Header.GetForeground(); fg != p.Accent {
		t.Errorf("Header foreground = %v, want unaffected Accent (%v)", fg, p.Accent)
	}
}

// TestNewStyles_UnknownSlotFallsBack pins that a slot name that doesn't
// exist on Palette (a hand-edit typo in config.json) degrades to the
// role's default instead of rendering an empty/garbage colour.
func TestNewStyles_UnknownSlotFallsBack(t *testing.T) {
	p := stylePalette
	got := newStyles(p, map[role]slotName{roleHeader: slotName("chartreuse")})
	if fg := got.Header.GetForeground(); fg != p.Accent {
		t.Errorf("Header foreground = %v, want the default Accent (%v) for an unknown slot", fg, p.Accent)
	}
}

// TestDefaultRoleSlots_NamesRealSlots guards against a rename orphaning an
// entry: every role in defaultRoleSlots (and returned by roleNames) must
// name a slot that Palette.slot recognises, and roleNames/slotNames must
// list every role/slot exactly once with no stragglers.
func TestDefaultRoleSlots_NamesRealSlots(t *testing.T) {
	roles := roleNames()
	if len(roles) != len(defaultRoleSlots) {
		t.Fatalf("roleNames has %d entries, defaultRoleSlots has %d", len(roles), len(defaultRoleSlots))
	}
	seen := map[role]bool{}
	for _, r := range roles {
		if seen[r] {
			t.Errorf("roleNames lists %q more than once", r)
		}
		seen[r] = true
		name, ok := defaultRoleSlots[r]
		if !ok {
			t.Errorf("role %q has no entry in defaultRoleSlots", r)
			continue
		}
		if _, ok := (Palette{}).slot(name); !ok {
			t.Errorf("defaultRoleSlots[%q] = %q, which Palette.slot does not recognise", r, name)
		}
	}

	names := (Palette{}).slotNames()
	seenSlot := map[slotName]bool{}
	for _, name := range names {
		if seenSlot[name] {
			t.Errorf("slotNames lists %q more than once", name)
		}
		seenSlot[name] = true
		if _, ok := (Palette{}).slot(name); !ok {
			t.Errorf("slotNames lists %q, which Palette.slot does not recognise", name)
		}
	}
}
