package ui

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/lipgloss"
)

// Palette is the small set of theme colors sessui needs, read once at
// startup from `omarchy-theme-color` (falling back to sensible defaults
// when that command isn't available, e.g. off Omarchy).
type Palette struct {
	Accent     lipgloss.Color
	Background lipgloss.Color
	Foreground lipgloss.Color
	Muted      lipgloss.Color
	// Named ANSI-ish slots, pulled from the Omarchy theme so icons and
	// accents follow the active theme instead of hardcoded hex.
	Red     lipgloss.Color
	Green   lipgloss.Color
	Yellow  lipgloss.Color
	Blue    lipgloss.Color
	Magenta lipgloss.Color
	Cyan    lipgloss.Color
	Orange  lipgloss.Color
}

// slotName names one colour in a Palette. It is the unit the user picks
// from when recolouring a role -- never a raw hex, so a theme switch (a new
// Palette) always re-resolves it instead of leaving a stale colour behind.
type slotName string

const (
	slotAccent     slotName = "accent"
	slotBackground slotName = "background"
	slotForeground slotName = "foreground"
	slotMuted      slotName = "muted"
	slotRed        slotName = "red"
	slotGreen      slotName = "green"
	slotYellow     slotName = "yellow"
	slotBlue       slotName = "blue"
	slotMagenta    slotName = "magenta"
	slotCyan       slotName = "cyan"
	slotOrange     slotName = "orange"
)

// slotNames lists every Palette slot in a stable display order -- what a
// settings UI iterates to offer the picker, so it never has to reach into
// Palette's fields directly.
func (p Palette) slotNames() []slotName {
	return []slotName{
		slotAccent, slotBackground, slotForeground, slotMuted,
		slotRed, slotGreen, slotYellow, slotBlue, slotMagenta, slotCyan, slotOrange,
	}
}

// slot resolves a slotName against this Palette. ok is false for anything
// that isn't one of the constants above -- e.g. a typo hand-edited into
// config.json -- so a caller can fall back instead of rendering garbage.
func (p Palette) slot(name slotName) (lipgloss.Color, bool) {
	switch name {
	case slotAccent:
		return p.Accent, true
	case slotBackground:
		return p.Background, true
	case slotForeground:
		return p.Foreground, true
	case slotMuted:
		return p.Muted, true
	case slotRed:
		return p.Red, true
	case slotGreen:
		return p.Green, true
	case slotYellow:
		return p.Yellow, true
	case slotBlue:
		return p.Blue, true
	case slotMagenta:
		return p.Magenta, true
	case slotCyan:
		return p.Cyan, true
	case slotOrange:
		return p.Orange, true
	default:
		return "", false
	}
}

// paletteSource names where colours come from. It is the user-facing
// setting: "auto" follows the Omarchy theme when the box has one and falls
// back to the built-in dark palette when it does not (a Mac, an SSH box);
// "dark"/"light" pin a built-in palette regardless, for a terminal whose
// look Omarchy does not control.
type paletteSource string

const (
	paletteAuto  paletteSource = "auto"
	paletteDark  paletteSource = "dark"
	paletteLight paletteSource = "light"
)

// builtinPalettes are complete palettes that need no theme tooling. Dark is
// Catppuccin Mocha, the set the app has always fallen back to; light is
// Catppuccin Latte, the same hues on a light ground, so a Mac with a light
// terminal is not stuck with dim text on white.
var builtinPalettes = map[paletteSource]Palette{
	paletteDark: {
		Accent: "#00AFFF", Background: "#1E1E2E", Foreground: "#CDD6F4", Muted: "#6C7086",
		Red: "#F38BA8", Green: "#A6E3A1", Yellow: "#F9E2AF", Blue: "#89B4FA",
		Magenta: "#F5C2E7", Cyan: "#94E2D5", Orange: "#FAB387",
	},
	paletteLight: {
		Accent: "#1E66F5", Background: "#EFF1F5", Foreground: "#4C4F69", Muted: "#9CA0B0",
		Red: "#D20F39", Green: "#40A02B", Yellow: "#DF8E1D", Blue: "#1E66F5",
		Magenta: "#EA76CB", Cyan: "#179299", Orange: "#FE640B",
	},
}

// themeColorQuery asks the environment for one named theme colour. A
// package var so tests can stand in for omarchy-theme-color without
// shelling out; the real one is omarchyThemeColor.
var themeColorQuery = omarchyThemeColor

// omarchyThemeColor shells out to omarchy-theme-color for one slot. ok is
// false on any failure -- missing binary, non-zero exit, empty output.
func omarchyThemeColor(name string) (hex string, ok bool) {
	out, err := exec.Command("omarchy-theme-color", name).Output()
	hex = strings.TrimSpace(string(out))
	return hex, err == nil && hex != ""
}

// omarchyThemeAvailable is answered ONCE per process, not once per colour:
// eleven failed execs at startup on every box without Omarchy is the cost
// this avoids, and it is what makes "auto" cheap enough to be the default.
func omarchyThemeAvailable() bool {
	_, err := exec.LookPath("omarchy-theme-color")
	return err == nil
}

// loadPalette resolves the palette for a source. "auto" and any unknown
// value follow Omarchy when it is available; slots Omarchy fails to answer
// fall back to the dark palette individually, so a partial theme degrades
// per-colour rather than all-or-nothing. A pinned source never shells out.
func loadPalette(source paletteSource) Palette {
	if p, pinned := builtinPalettes[source]; pinned {
		return p
	}
	base := builtinPalettes[paletteDark]
	if !omarchyThemeAvailable() {
		return base
	}
	slot := func(name string, fallback lipgloss.Color) lipgloss.Color {
		if hex, ok := themeColorQuery(name); ok {
			return lipgloss.Color(hex)
		}
		return fallback
	}
	return Palette{
		Accent:     slot("accent", base.Accent),
		Background: slot("background", base.Background),
		Foreground: slot("foreground", base.Foreground),
		Muted:      slot("muted", base.Muted),
		Red:        slot("red", base.Red),
		Green:      slot("green", base.Green),
		Yellow:     slot("yellow", base.Yellow),
		Blue:       slot("blue", base.Blue),
		Magenta:    slot("magenta", base.Magenta),
		Cyan:       slot("cyan", base.Cyan),
		Orange:     slot("orange", base.Orange),
	}
}

// Styles bundles the lipgloss styles used across the list, footer, and
// prompts. Built once from the Palette at startup.
type Styles struct {
	Name     lipgloss.Style
	Muted    lipgloss.Style
	Cursor   lipgloss.Style
	Footer   lipgloss.Style
	Help     lipgloss.Style
	Error    lipgloss.Style
	Working  lipgloss.Style // a Working agent's live verb+elapsed status
	Summary  lipgloss.Style // a session's EffectiveSummary in the status cell
	Count    lipgloss.Style // the "N sessions" line above the header
	Header   lipgloss.Style // the column-label row above the list
	PeerUp   lipgloss.Style // an up peer's name colour (green) in the session cell
	PeerDown lipgloss.Style // a down peer's name colour (red) in the session cell
	Mail     lipgloss.Style // the "✉" owed-mail marker trailing the name

	// Count-line attention pills (see header.go) -- a solid badge, not just
	// coloured text, so they read as status lights above the header rather
	// than more prose next to the tally. Foreground is the palette's
	// Background colour: whichever end of the light/dark scale the active
	// theme sits at, Background sits at the opposite end from Red/Accent, so
	// the pill text stays legible on both the light Omarchy default and the
	// dark fallback palette without a hardcoded light/dark branch.
	NeedsYouPill lipgloss.Style // bell + count, on the theme red
	MailPill     lipgloss.Style // envelope + count, on the accent

	// activeHeat buckets: the last-active elapsed, coloured by recency --
	// hottest (green) when fresh, cooling to dim once it's a day-plus stale.
	activeFresh  lipgloss.Style // < 1m
	activeRecent lipgloss.Style // < 1h
	activeToday  lipgloss.Style // < 1d
	activeStale  lipgloss.Style // >= 1d

	// selectedBG is the raw SGR background sequence for the highlighted row --
	// a raw sequence, not a lipgloss.Style, because it has to be re-applied
	// after every cell's reset; see selectRow.
	selectedBG string
}

// bgSGR builds a truecolor background escape sequence from a "#rrggbb" theme
// colour, or "" if it isn't a hex colour.
func bgSGR(c lipgloss.Color) string {
	hex := strings.TrimPrefix(string(c), "#")
	if len(hex) != 6 {
		return ""
	}
	v, err := strconv.ParseUint(hex, 16, 32)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("\x1b[48;2;%d;%d;%dm", v>>16&0xff, v>>8&0xff, v&0xff)
}

// selectRow paints the selection background across a fully rendered, already
// multi-coloured row. A plain lipgloss Background can't do this: every cell
// closes with a full reset (\x1b[0m) that also clears the background, leaving
// holes past the first cell (measured, not assumed). So re-apply the
// background immediately after each reset, and close once at the end.
func (s Styles) selectRow(row string) string {
	if s.selectedBG == "" {
		return row
	}
	return s.selectedBG + strings.ReplaceAll(row, "\x1b[0m", "\x1b[0m"+s.selectedBG) + "\x1b[0m"
}

// activeHeat picks the last-active colour for an elapsed duration -- a small
// recency gradient off the theme palette (see the bucket fields).
func (s Styles) activeHeat(since time.Duration) lipgloss.Style {
	switch {
	case since < time.Minute:
		return s.activeFresh
	case since < time.Hour:
		return s.activeRecent
	case since < 24*time.Hour:
		return s.activeToday
	default:
		return s.activeStale
	}
}

// role names one visible thing a user may recolour -- deliberately a small
// set: only the elements that carry meaning on their own (a status colour, a
// pill, the row highlight), not every lipgloss.Style newStyles happens to
// build. Everything else (Name, Footer, Help, Summary, Count, the active*
// heat buckets, Error) stays on its literal Palette field -- recolouring
// "needs you" must not also recolour unrelated errors or the count line.
type role string

const (
	roleHeader    role = "header"
	roleCursor    role = "cursor"
	roleNeedsYou  role = "needs-you"
	roleMail      role = "mail"
	roleWorking   role = "working"
	rolePeerUp    role = "peer-up"
	rolePeerDown  role = "peer-down"
	roleSelection role = "selection"
)

// roleNames lists every role in a stable display order -- what a settings
// UI iterates to offer the picker, paired with slotNames.
func roleNames() []role {
	return []role{
		roleHeader, roleCursor, roleNeedsYou, roleMail,
		roleWorking, rolePeerUp, rolePeerDown, roleSelection,
	}
}

// defaultRoleSlots is the slot each role resolves to absent a user mapping
// -- exactly what newStyles hardcoded before roles existed, so an empty
// mapping renders byte-identical to the shipped look.
//
// roleMail is the one slot that took a real decision: today the header's
// MailPill badge is Accent while the inline "✉" marker next to a peer dot
// (Styles.Mail, delegate.go's renderPeer) is Yellow -- the same concept, two
// colours. A role maps to exactly one slot, so only one of them can move
// with it. MailPill is the one exposed: it mirrors NeedsYouPill, the other
// header-band badge that IS role-driven, so the two pills stay a consistent
// pair a user can retheme together. The inline "✉" keeps its literal
// p.Yellow -- a small secondary glyph beside the peer dot, not a role worth
// a picker entry of its own.
var defaultRoleSlots = map[role]slotName{
	roleHeader:    slotAccent,
	roleCursor:    slotAccent,
	roleNeedsYou:  slotRed,
	roleMail:      slotAccent,
	roleWorking:   slotAccent,
	rolePeerUp:    slotGreen,
	rolePeerDown:  slotRed,
	roleSelection: slotMuted,
}

// resolveRole is the one place a role becomes a colour: the user's mapping
// wins when it names a slot that exists on p; a missing role, or one mapped
// to an unknown slot name (a typo hand-edited into config.json), falls back
// to defaultRoleSlots -- so a bad config.json degrades to the shipped look
// for that one role instead of failing the whole form.
func resolveRole(p Palette, roles map[role]slotName, r role) lipgloss.Color {
	name, ok := roles[r]
	if !ok {
		name = defaultRoleSlots[r]
	}
	if c, ok := p.slot(name); ok {
		return c
	}
	c, _ := p.slot(defaultRoleSlots[r])
	return c
}

func newStyles(p Palette, roles map[role]slotName) Styles {
	c := func(r role) lipgloss.Color { return resolveRole(p, roles, r) }
	return Styles{
		Name: lipgloss.NewStyle().Bold(true).Foreground(p.Foreground),
		// "Muted" = de-emphasised TEXT (age, idle status, the last-active
		// icon). The theme's muted slot is a border colour (#414868 on Tokyo
		// Night) -- unreadable as text -- so dim the foreground with the
		// terminal's own Faint attribute instead: readable-dim, theme-agnostic,
		// and it falls back to full foreground (never invisible) if unsupported.
		Muted:   lipgloss.NewStyle().Foreground(p.Foreground).Faint(true),
		Cursor:  lipgloss.NewStyle().Foreground(c(roleCursor)).Bold(true),
		Footer:  lipgloss.NewStyle().Foreground(p.Foreground),
		Help:    lipgloss.NewStyle().Foreground(p.Foreground).Faint(true).Italic(true),
		Error:   lipgloss.NewStyle().Foreground(p.Red).Bold(true),
		Working: lipgloss.NewStyle().Foreground(c(roleWorking)),
		Summary: lipgloss.NewStyle().Foreground(p.Foreground),
		Count:   lipgloss.NewStyle().Foreground(p.Foreground),
		// Column labels: the accent colour + bold, distinct from the
		// foreground row text (and well clear of the theme's near-invisible
		// "muted" slot). The header carries its own icons, so it reads as a
		// header band rather than another data row.
		Header: lipgloss.NewStyle().Foreground(c(roleHeader)).Bold(true),
		// The theme's "muted" slot is a surface/border colour -- wrong for
		// text, right as a subtle full-row selection background that leaves
		// every cell's own foreground readable on top (see selectRow).
		selectedBG:   bgSGR(c(roleSelection)),
		PeerUp:       lipgloss.NewStyle().Foreground(c(rolePeerUp)),
		PeerDown:     lipgloss.NewStyle().Foreground(c(rolePeerDown)),
		Mail:         lipgloss.NewStyle().Foreground(p.Yellow),
		NeedsYouPill: lipgloss.NewStyle().Bold(true).Background(c(roleNeedsYou)).Foreground(p.Background).Padding(0, 1),
		MailPill:     lipgloss.NewStyle().Bold(true).Background(c(roleMail)).Foreground(p.Background).Padding(0, 1),
		activeFresh:  lipgloss.NewStyle().Foreground(p.Green),
		activeRecent: lipgloss.NewStyle().Foreground(p.Cyan),
		activeToday:  lipgloss.NewStyle().Foreground(p.Yellow),
		activeStale:  lipgloss.NewStyle().Foreground(p.Foreground).Faint(true),
	}
}

// themedListStyles retunes bubbles/list's default styling (title bar,
// filter prompt, status bar, help, empty state) onto the loaded theme
// palette instead of charm's baked-in brand colors.
func themedListStyles(p Palette) list.Styles {
	s := list.DefaultStyles()
	s.Title = lipgloss.NewStyle().Bold(true).Foreground(p.Background).Background(p.Accent).Padding(0, 1)
	s.FilterPrompt = lipgloss.NewStyle().Foreground(p.Accent)
	s.FilterCursor = lipgloss.NewStyle().Foreground(p.Accent)
	s.StatusBar = lipgloss.NewStyle().Foreground(p.Muted).Padding(0, 0, 1, 2)
	s.StatusBarActiveFilter = lipgloss.NewStyle().Foreground(p.Foreground)
	s.NoItems = lipgloss.NewStyle().Foreground(p.Foreground).Faint(true)
	s.HelpStyle = lipgloss.NewStyle().Foreground(p.Foreground).Faint(true).Padding(1, 0, 0, 2)
	return s
}
