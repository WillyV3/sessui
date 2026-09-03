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

func loadPalette() Palette {
	return Palette{
		Accent:     themeColor("accent", "#00AFFF"),
		Background: themeColor("background", "#1E1E2E"),
		Foreground: themeColor("foreground", "#CDD6F4"),
		Muted:      themeColor("muted", "#6C7086"),
		Red:        themeColor("red", "#F38BA8"),
		Green:      themeColor("green", "#A6E3A1"),
		Yellow:     themeColor("yellow", "#F9E2AF"),
		Blue:       themeColor("blue", "#89B4FA"),
		Magenta:    themeColor("magenta", "#F5C2E7"),
		Cyan:       themeColor("cyan", "#94E2D5"),
		Orange:     themeColor("orange", "#FAB387"),
	}
}

func themeColor(name, fallback string) lipgloss.Color {
	out, err := exec.Command("omarchy-theme-color", name).Output()
	hex := strings.TrimSpace(string(out))
	if err != nil || hex == "" {
		return lipgloss.Color(fallback)
	}
	return lipgloss.Color(hex)
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

func newStyles(p Palette) Styles {
	return Styles{
		Name: lipgloss.NewStyle().Bold(true).Foreground(p.Foreground),
		// "Muted" = de-emphasised TEXT (age, idle status, the last-active
		// icon). The theme's muted slot is a border colour (#414868 on Tokyo
		// Night) -- unreadable as text -- so dim the foreground with the
		// terminal's own Faint attribute instead: readable-dim, theme-agnostic,
		// and it falls back to full foreground (never invisible) if unsupported.
		Muted:   lipgloss.NewStyle().Foreground(p.Foreground).Faint(true),
		Cursor:  lipgloss.NewStyle().Foreground(p.Accent).Bold(true),
		Footer:  lipgloss.NewStyle().Foreground(p.Foreground),
		Help:    lipgloss.NewStyle().Foreground(p.Foreground).Faint(true).Italic(true),
		Error:   lipgloss.NewStyle().Foreground(p.Red).Bold(true),
		Working: lipgloss.NewStyle().Foreground(p.Accent),
		Summary: lipgloss.NewStyle().Foreground(p.Foreground),
		Count:   lipgloss.NewStyle().Foreground(p.Foreground),
		// Column labels: the accent colour + bold, distinct from the
		// foreground row text (and well clear of the theme's near-invisible
		// "muted" slot). The header carries its own icons, so it reads as a
		// header band rather than another data row.
		Header: lipgloss.NewStyle().Foreground(p.Accent).Bold(true),
		// The theme's "muted" slot is a surface/border colour -- wrong for
		// text, right as a subtle full-row selection background that leaves
		// every cell's own foreground readable on top (see selectRow).
		selectedBG:   bgSGR(p.Muted),
		PeerUp:       lipgloss.NewStyle().Foreground(p.Green),
		PeerDown:     lipgloss.NewStyle().Foreground(p.Red),
		Mail:         lipgloss.NewStyle().Foreground(p.Yellow),
		NeedsYouPill: lipgloss.NewStyle().Bold(true).Background(p.Red).Foreground(p.Background).Padding(0, 1),
		MailPill:     lipgloss.NewStyle().Bold(true).Background(p.Accent).Foreground(p.Background).Padding(0, 1),
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
