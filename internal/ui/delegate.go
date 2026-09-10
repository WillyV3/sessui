package ui

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/WillyV3/sessui/internal/session"
)

// sessionItem adapts a session.Session to list.Item. FilterValue drives the
// list's built-in fuzzy filter.
type sessionItem struct{ session.Session }

func (i sessionItem) FilterValue() string { return i.Name }

func toItems(sessions []session.Session) []list.Item {
	items := make([]list.Item, len(sessions))
	for i, s := range sessions {
		items[i] = sessionItem{s}
	}
	return items
}

// rowDelegate renders each session as one row. The spinner and marqueeFrame
// are the only mutable state, kept in sync with Model's; everything else
// reads fresh at render time.
type rowDelegate struct {
	styles  Styles
	home    string
	spinner spinner.Model
	// layout is the resolved column shape for the current width and
	// configuration; Model.relayout is its only writer.
	layout tableLayout
	// marqueeFrame drives the selected row's scroll -- see marqueeCell.
	marqueeFrame int
}

func (d *rowDelegate) Height() int                         { return 1 }
func (d *rowDelegate) Spacing() int                        { return 0 }
func (d *rowDelegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }

func (d *rowDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	it, ok := item.(sessionItem)
	if !ok {
		return
	}
	c := cell{styles: d.styles, home: d.home, now: time.Now(), session: it.Session, spinner: d.spinner}
	fmt.Fprint(w, renderRow(d.layout, c, d.marqueeFrame, index == m.Index()))
}

const (
	// defaultUsableWidth is the row width at the shipped geometry: a 112-wide
	// popup minus appStyle's horizontal padding. The live list uses the real
	// width; --dump lays out against this.
	defaultUsableWidth = 108
	// iconSlotWidth reserves two cells per app icon: Nerd Font glyphs measure
	// as width 1 under lipgloss but often render double-width, so reserving
	// two keeps the column's counted width deterministic.
	maxIcons      = 3
	iconSlotWidth = 2
	iconColWidth  = maxIcons * iconSlotWidth
)

// anyPeer reports whether any session joined a cp3 peer. When false the peer
// column disappears.
func anyPeer(sessions []session.Session) bool {
	for _, s := range sessions {
		if s.PeerName != "" {
			return true
		}
	}
	return false
}

// fixedCol is one row cell: a fixed width that never wraps (Inline) and never
// overruns (MaxWidth), so rows form a rectangle and a long summary clips to
// one line instead of wrapping the row.
func fixedCol(w int) lipgloss.Style {
	return lipgloss.NewStyle().Inline(true).Width(w).MaxWidth(w)
}

// renderRow draws one session as the layout's columns at their resolved
// widths. Shared by the delegate, --dump and renderHeader, so labels sit over
// their data. The selected row's over-long cells scroll and it gets a
// full-width highlight.
func renderRow(l tableLayout, c cell, frame int, selected bool) string {
	cursor := "  "
	if selected {
		cursor = c.styles.Cursor.Render("> ")
	}

	cells := make([]string, 0, 2*len(l.columns))
	cells = append(cells, fixedCol(cursorWidth).Render(cursor))
	for i, col := range l.columns {
		if i > 0 {
			cells = append(cells, " ")
		}
		cells = append(cells, marqueeCell(l.widths[i], frame, selected, col.render(c)))
	}
	row := lipgloss.JoinHorizontal(lipgloss.Top, cells...)
	if selected {
		row = c.styles.selectRow(row)
	}
	return row
}

// marqueeCell renders one fixed-width cell. On the selected row a cell wider
// than its column scrolls: ansi.Cut windows the styled string, preserving
// colour and wide characters, so long content reveals fully instead of being
// cut off. Every other cell truncates.
func marqueeCell(width, frame int, selected bool, content string) string {
	if selected {
		if overflow := ansi.StringWidth(content) - width; overflow > 0 {
			off := marqueeOffset(frame, overflow)
			return fixedCol(width).Render(ansi.Cut(content, off, off+width))
		}
	}
	return fixedCol(width).Render(content)
}

// marqueeOffset ping-pongs across overflow hidden columns on a monotonic
// frame counter, holding at each end so both stay readable.
func marqueeOffset(frame, overflow int) int {
	if overflow <= 0 {
		return 0
	}
	const hold = 5 // frames paused at each extreme
	leg := 2*hold + overflow
	p := frame % (2 * leg) // one forward leg, then one backward leg
	if p >= leg {
		p = 2*leg - 1 - p // reflect the backward leg onto the forward one
	}
	return min(max(p-hold, 0), overflow) // hold..advance..hold
}

// Header glyphs. The last-active column is labelled by its icon alone: its
// cell is only wide enough for the short values beneath it.
const (
	glyphSession = 0xF489  // nf-oct-terminal
	glyphActive  = 0xF0430 // nf-md-pulse
	glyphStatus  = 0xF43C  // nf-oct-broadcast
	glyphCWD     = 0xEA83  // nf-cod-folder
	glyphPeer    = 0xF0318 // nf-md-lan-connect
)

// renderHeader renders the column-label band, walking the same layout as
// renderRow so labels sit over their data by construction.
func renderHeader(styles Styles, l tableLayout) string {
	cells := make([]string, 0, 2*len(l.columns))
	cells = append(cells, fixedCol(cursorWidth).Render(""))
	for i, col := range l.columns {
		if i > 0 {
			cells = append(cells, " ")
		}
		cells = append(cells, fixedCol(l.widths[i]).Render(col.headerCell(styles)))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, cells...)
}

// idleThreshold is how long an agent sits without activity before its status
// reads "idle": long enough that a between-turns pause does not flap.
const idleThreshold = 15 * time.Minute

// renderStatus is the status cell, in priority order: a bell-rung Notify wins
// outright; then a live summary; then a Working agent's verb and elapsed;
// then "idle" for an agent parked past idleThreshold. A plain shell has
// nothing to say here.
func renderStatus(styles Styles, now time.Time, s session.Session) string {
	switch {
	case s.State == session.StateNotify:
		return styles.Error.Render("needs you")
	case s.EffectiveSummary() != "":
		return styles.Summary.Render(s.EffectiveSummary())
	case s.State == session.StateWorking && s.WorkingVerb != "":
		return styles.Working.Render(strings.TrimSpace(s.WorkingVerb + "… " + s.WorkingElapsed))
	default:
		if s.Agent && now.Sub(s.Activity) > idleThreshold {
			return styles.Muted.Render("idle")
		}
		return ""
	}
}

// renderName renders the session-name cell.
func renderName(styles Styles, s session.Session) string {
	return styles.Name.Render(s.Name)
}

// renderPeer renders the peer cell: a filled dot when the bound peer is up, a
// hollow one when down, "✉" when it has unread mail, and the peer's name only
// when it differs from the session's. "" when the session is not a peer
// workspace.
func renderPeer(styles Styles, s session.Session) string {
	if s.PeerName == "" {
		return ""
	}

	dot := styles.PeerDown.Render("○")
	if s.PeerUp {
		dot = styles.PeerUp.Render("●")
	}

	b := dot
	if s.OwedMail {
		b += " " + styles.Mail.Render("✉")
	}
	if session.NormalizeName(s.PeerName) != session.NormalizeName(s.Name) {
		b += " " + s.PeerName
	}
	return b
}

// renderIcons renders exactly maxIcons fixed-width slots, blank-padded, so the
// column's counted width never depends on how many apps a session runs. The
// app that is the session's agent reflects its live state: a spinner while
// Working, blinking on Notify, static otherwise.
func renderIcons(s session.Session, sp spinner.Model) string {
	// An ASCII tag is two cells and needs its own gap, so its slot is one
	// wider; the column is then cut to its layout width like any other cell.
	width := iconSlotWidth
	if activeGlyphs == glyphsASCII {
		width++
	}
	slot := lipgloss.NewStyle().Width(width)
	slots := make([]string, maxIcons)
	for i := range slots {
		if i < len(s.Apps) {
			app := s.Apps[i]
			slots[i] = slot.Render(renderIcon(iconFor(app), s.State, session.IsAgentApp(app), sp))
		} else {
			slots[i] = slot.Render("")
		}
	}
	return lipgloss.NewStyle().Width(maxIcons * width).Render(lipgloss.JoinHorizontal(lipgloss.Top, slots...))
}

func renderIcon(icon Icon, state session.AgentState, isAgent bool, sp spinner.Model) string {
	style := lipgloss.NewStyle().Foreground(icon.Color).Bold(true)
	if !isAgent {
		return style.Render(icon.Glyph)
	}
	switch state {
	case session.StateWorking:
		return style.Render(sp.View())
	case session.StateNotify:
		return style.Blink(true).Render(icon.Glyph)
	default: // StateIdle; StateNone cannot reach here since isAgent implies Agent
		return style.Render(icon.Glyph)
	}
}

// renderCWD returns the muted path, unpadded so marqueeCell owns the width.
func renderCWD(styles Styles, abbrev string) string {
	return styles.Muted.Render(abbrev)
}

// DumpRows renders the rows the interactive list would show, sorted the same
// way, as plain text with no selection, for `sessui -dump`.
func DumpRows(sessions []session.Session) string {
	m := New()
	sortSessions(sessions)
	layout := layoutColumns(resolveColumns(m.columns, anyPeer(sessions)), defaultUsableWidth)
	sp := spinner.New(spinner.WithSpinner(spinner.MiniDot))
	now := time.Now()
	var b strings.Builder
	for _, s := range sessions {
		c := cell{styles: m.styles, home: m.home, now: now, session: s, spinner: sp}
		b.WriteString(renderRow(layout, c, 0, false))
		b.WriteString("\n")
	}
	return b.String()
}
