package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/lipgloss"
)

// Palette is the set of theme colours sessui draws with, read once at startup.
type Palette struct {
	Accent     lipgloss.Color
	Background lipgloss.Color
	Foreground lipgloss.Color
	Muted      lipgloss.Color
	Red        lipgloss.Color
	Green      lipgloss.Color
	Yellow     lipgloss.Color
	Blue       lipgloss.Color
	Magenta    lipgloss.Color
	Cyan       lipgloss.Color
	Orange     lipgloss.Color
}

// slotName names one colour in a Palette. It is what a user picks when
// recolouring a role -- never raw hex, so a theme switch re-resolves it.
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

// slotNames lists every slot in a stable display order.
func (p Palette) slotNames() []slotName {
	return []slotName{
		slotAccent, slotBackground, slotForeground, slotMuted,
		slotRed, slotGreen, slotYellow, slotBlue, slotMagenta, slotCyan, slotOrange,
	}
}

// slot resolves a slotName against this Palette. ok is false for an unknown
// name, so a caller can fall back instead of rendering garbage.
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

// paletteSource is the user-facing theme setting: "auto" follows the Omarchy
// theme when the box has one and falls back to the built-in dark palette;
// "dark"/"light" pin a built-in regardless.
type paletteSource string

const (
	paletteAuto  paletteSource = "auto"
	paletteDark  paletteSource = "dark"
	paletteLight paletteSource = "light"
)

// builtinPalettes need no theme tooling: Catppuccin Mocha and Latte.
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

// themeColorQuery asks the environment for one named theme colour. A package
// var so tests can stand in without shelling out.
var themeColorQuery = omarchyThemeColor

// omarchyThemeColor shells out to omarchy-theme-color for one slot. ok is
// false on any failure.
func omarchyThemeColor(name string) (hex string, ok bool) {
	out, err := exec.Command("omarchy-theme-color", name).Output()
	hex = strings.TrimSpace(string(out))
	return hex, err == nil && hex != ""
}

// omarchyThemeAvailable is answered once per process, not once per colour:
// eleven failed execs at every launch on a box without Omarchy is the cost
// this avoids, and what makes "auto" cheap enough to be the default.
func omarchyThemeAvailable() bool {
	_, err := exec.LookPath("omarchy-theme-color")
	return err == nil
}

// loadPalette resolves the palette for a source. "auto" follows Omarchy when
// available; a slot Omarchy fails to answer falls back individually, so a
// partial theme degrades per colour. A pinned source never shells out.
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

// Styles bundles the lipgloss styles used across the list, footer and
// prompts, built once from the Palette.
type Styles struct {
	Name     lipgloss.Style
	Muted    lipgloss.Style
	Cursor   lipgloss.Style
	Footer   lipgloss.Style
	Help     lipgloss.Style
	Error    lipgloss.Style
	Working  lipgloss.Style // a Working agent's live verb and elapsed
	Summary  lipgloss.Style // a session's EffectiveSummary in the status cell
	Count    lipgloss.Style // the session tally
	Header   lipgloss.Style // the column-label row
	PeerUp   lipgloss.Style // an up peer
	PeerDown lipgloss.Style // a down peer
	Mail     lipgloss.Style // the "✉" owed-mail marker

	// Pills are solid badges. Foreground is the palette's Background: at
	// whichever end of the light/dark scale the theme sits, Background is
	// opposite Red and Accent, so the text stays legible without a
	// light/dark branch.
	NeedsYouPill lipgloss.Style
	MailPill     lipgloss.Style

	// activeHeat buckets: last-active coloured by recency.
	activeFresh  lipgloss.Style // < 1m
	activeRecent lipgloss.Style // < 1h
	activeToday  lipgloss.Style // < 1d
	activeStale  lipgloss.Style // >= 1d

	// selectedBG is the raw SGR background for the highlighted row: a raw
	// sequence, not a Style, because it is re-applied after every cell's
	// reset. See selectRow.
	selectedBG string
}

// bgSGR builds a truecolor background escape from a "#rrggbb" colour, or "".
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

// selectRow paints the selection background across an already multi-coloured
// row. A plain lipgloss Background cannot: every cell closes with a full
// reset that also clears the background, leaving holes past the first cell.
// So re-apply it after each reset and close once at the end.
func (s Styles) selectRow(row string) string {
	if s.selectedBG == "" {
		return row
	}
	return s.selectedBG + strings.ReplaceAll(row, "\x1b[0m", "\x1b[0m"+s.selectedBG) + "\x1b[0m"
}

// activeHeat picks the last-active colour for an elapsed duration.
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

// role names one visible thing a user may recolour: only the elements that
// carry meaning on their own, so recolouring "needs you" cannot also
// recolour unrelated errors.
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

// roleNames lists every role in a stable display order.
func roleNames() []role {
	return []role{
		roleHeader, roleCursor, roleNeedsYou, roleMail,
		roleWorking, rolePeerUp, rolePeerDown, roleSelection,
	}
}

// defaultRoleSlots is the slot each role resolves to absent a user mapping,
// so an empty mapping renders the shipped look. roleMail drives the pill,
// not the inline "✉", which keeps its literal yellow as a secondary glyph.
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

// resolveRole is the one place a role becomes a colour. A missing role, or
// one mapped to an unknown slot, falls back to its default, so a bad
// config.json degrades one role rather than failing the whole form.
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
		// A theme's muted slot is a surface colour, unreadable as text, so
		// de-emphasised text dims the foreground with the terminal's Faint
		// attribute instead: theme-agnostic, and never invisible.
		Muted:   lipgloss.NewStyle().Foreground(p.Foreground).Faint(true),
		Cursor:  lipgloss.NewStyle().Foreground(c(roleCursor)).Bold(true),
		Footer:  lipgloss.NewStyle().Foreground(p.Foreground),
		Help:    lipgloss.NewStyle().Foreground(p.Foreground).Faint(true).Italic(true),
		Error:   lipgloss.NewStyle().Foreground(p.Red).Bold(true),
		Working: lipgloss.NewStyle().Foreground(c(roleWorking)),
		Summary: lipgloss.NewStyle().Foreground(p.Foreground),
		Count:   lipgloss.NewStyle().Foreground(p.Foreground),
		Header:  lipgloss.NewStyle().Foreground(c(roleHeader)).Bold(true),
		// The same muted slot is right as a subtle selection background that
		// leaves every cell's own foreground readable.
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

// themedListStyles retunes bubbles/list's default styling onto the palette.
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

// themeStampPath is the file Omarchy writes the active theme's name into.
const themeStampPath = ".local/state/omarchy/current/theme.name"

// ThemeStamp identifies the active Omarchy theme, or "" when there is nothing
// to identify. A file read, deliberately: no exec, no watcher. On a machine
// without Omarchy the path is absent and this returns "" forever.
func ThemeStamp() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(home, themeStampPath))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
