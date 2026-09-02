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

	"github.com/willyv3/sessui/internal/session"
)

// sessionItem adapts a session.Session to list.Item so it can live in a
// bubbles/list.Model. FilterValue drives the list's built-in fuzzy filter.
type sessionItem struct{ session.Session }

func (i sessionItem) FilterValue() string { return i.Name }

func toItems(sessions []session.Session) []list.Item {
	items := make([]list.Item, len(sessions))
	for i, s := range sessions {
		items[i] = sessionItem{s}
	}
	return items
}

// rowDelegate is the list.ItemDelegate that renders each session as one row:
// session name, app icons, last-active, cwd, peer, and status. The spinner
// and marqueeFrame are the only mutable state (kept in sync with Model's,
// which owns their ticks); everything else -- the live last-active counter
// included -- reads fresh at render time, so nothing else needs per-frame
// updating.
type rowDelegate struct {
	styles  Styles
	home    string
	spinner spinner.Model
	// marqueeFrame is a monotonic counter (advanced by the marquee tick,
	// kept in sync with Model's) that drives the selected row's scroll so
	// its long cells reveal fully instead of truncating -- see marqueeCell.
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
	fmt.Fprint(w, renderRow(d.styles, d.home, time.Now(), it.Session, d.spinner, d.marqueeFrame, index == m.Index()))
}

// Column widths. lipgloss's ANSI-aware Width() padding gives the row a
// table's alignment without a separate table widget; fixedCol additionally
// sets Inline (skip wrapping) and MaxWidth (truncate) so a cell's content
// running long -- a 200-character cp3 summary, a 19-character session name
// -- clips to one line instead of wrapping the whole row. maxIcons caps how
// many app icons a row shows; iconSlotWidth reserves 2 cells per icon
// slot -- Nerd Font glyphs measure as width 1 under lipgloss.Width() but
// often render double-width in the terminal, so reserving 2 (rather than
// the 1 lipgloss thinks it needs) keeps the column's counted width
// deterministic regardless of which slots are actually double-wide.
//
// Widths split usableWidth across the fixed cells plus five 1-wide
// separators. Column order is cursor, session, apps, active, cwd, peer,
// status -- session (the name) leads, status (the native summary, the star)
// sits last and takes whatever the others leave so its marquee scrolls along
// the right edge. Every cell is fixed width, so rows form a balanced
// rectangle rather than a left-aligned sawtooth.
const (
	// usableWidth is the row width inside the popup: the tmux popup is
	// 112 wide (see tmux.conf) minus appStyle's Padding(1,2) = 4.
	usableWidth    = 108
	cursorColWidth = 2
	maxIcons       = 3
	iconSlotWidth  = 2
	iconColWidth   = maxIcons * iconSlotWidth
	nameColWidth   = 22
	activeColWidth = 5 // last-active elapsed: "now", "3m", "2h", "234d"
	cwdColWidth    = 20
	peerColWidth   = 14 // ●/○ liveness dot + the peer name when it differs + ✉
	statusColWidth = usableWidth - cursorColWidth - iconColWidth - nameColWidth - activeColWidth - cwdColWidth - peerColWidth - 5
)

// fixedCol is one row cell: a fixed width that never wraps (Inline) and
// never overruns (MaxWidth) -- see the width-budget comment above for why
// both matter once real content (a long cp3 summary, a long session name)
// is in play.
func fixedCol(w int) lipgloss.Style {
	return lipgloss.NewStyle().Inline(true).Width(w).MaxWidth(w)
}

// renderRow renders one session as: cursor, session name, app icons,
// last-active, cwd, peer, and status (see the column-order note above).
// Shared by the interactive delegate, --dump, and renderHeader (same column
// widths, so labels line up with the data). Every column is a fixed-width
// lipgloss cell joined with JoinHorizontal, so columns start at the same
// offset on every row regardless of how long the name, summary, or cwd is;
// the selected row's over-long cells scroll (marqueeCell) and it gets a
// full-width highlight (selectRow).
func renderRow(styles Styles, home string, now time.Time, s session.Session, sp spinner.Model, frame int, selected bool) string {
	cursor := "  "
	if selected {
		cursor = styles.Cursor.Render("> ")
	}

	cells := []string{
		fixedCol(cursorColWidth).Render(cursor),
		marqueeCell(nameColWidth, frame, selected, renderName(styles, s)),
		" ",
		renderIcons(s, sp),
		" ",
		fixedCol(activeColWidth).Render(styles.activeHeat(now.Sub(s.Activity)).Render(session.LastActive(s.Activity, now))),
		" ",
		marqueeCell(cwdColWidth, frame, selected, renderCWD(styles, session.AbbreviatePath(s.CWD, home))),
		" ",
		marqueeCell(peerColWidth, frame, selected, renderPeer(styles, s)),
		" ",
		marqueeCell(statusColWidth, frame, selected, renderStatus(styles, now, s)),
	}
	row := lipgloss.JoinHorizontal(lipgloss.Top, cells...)
	if selected {
		row = styles.selectRow(row)
	}
	return row
}

// marqueeCell renders one fixed-width cell. On the SELECTED row, a cell whose
// content is wider than the column scrolls -- ansi.Cut windows the already
// styled string (colour/attrs preserved, grapheme- and wide-char-aware) at an
// offset from marqueeOffset -- so long names/summaries/paths reveal fully
// instead of being permanently cut off. Every other cell (and any that fits)
// truncates the usual way. This is the Charm ecosystem's own horizontal-window
// primitive (the same ansi.Cut bubbles/viewport scrolls with), not hand-rolled
// slicing.
func marqueeCell(width, frame int, selected bool, content string) string {
	if selected {
		if overflow := ansi.StringWidth(content) - width; overflow > 0 {
			off := marqueeOffset(frame, overflow)
			return fixedCol(width).Render(ansi.Cut(content, off, off+width))
		}
	}
	return fixedCol(width).Render(content)
}

// marqueeOffset returns the left scroll offset that reveals `overflow` hidden
// columns, ping-ponging on a monotonic frame counter with a brief hold at each
// end so both the start and the end stay readable rather than whipping past.
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

// Header-label glyphs (verified present in JetBrainsMono Nerd Font via
// otfinfo -u): a terminal for the session, a broadcast wave for status, a
// folder for cwd, and a pulse for the last-active column -- which is labelled
// by the icon alone (no word), since its cell is only wide enough for the
// short elapsed values beneath it.
const (
	glyphSession = 0xF489  // nf-oct-terminal
	glyphActive  = 0xF0430 // nf-md-pulse
	glyphStatus  = 0xF43C  // nf-oct-broadcast
	glyphCWD     = 0xEA83  // nf-cod-folder
	glyphPeer    = 0xF0318 // nf-md-lan-connect
)

// renderHeader renders the column-label band above the list (when not
// filtering): "apps" over the icon column, a pulse glyph over last-active,
// then icon+label for session, cwd, peer and status. In the accent colour (see
// styles.Header) so it reads as a header, not another data row. Mirrors
// renderRow's cell order and widths exactly so labels sit over their data.
// There's no "peer" label -- the peer is folded into the session cell.
func renderHeader(styles Styles) string {
	h := styles.Header
	cells := []string{
		fixedCol(cursorColWidth).Render(""),
		fixedCol(nameColWidth).Render(h.Render(glyphU(glyphSession) + " session")),
		" ",
		fixedCol(iconColWidth).Render(h.Render("apps")),
		" ",
		fixedCol(activeColWidth).Render(h.Render(glyphU(glyphActive))),
		" ",
		fixedCol(cwdColWidth).Render(h.Render(glyphU(glyphCWD) + " cwd")),
		" ",
		fixedCol(peerColWidth).Render(h.Render(glyphU(glyphPeer) + " peer")),
		" ",
		fixedCol(statusColWidth).Render(h.Render(glyphU(glyphStatus) + " status")),
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, cells...)
}

// idleThreshold is how long an agent must sit without activity before its
// status reads "idle" -- long enough that a between-turns pause doesn't flap.
const idleThreshold = 15 * time.Minute

// renderStatus renders the row's status cell -- what the session is doing
// right now, in priority order: a bell-rung Notify needs attention and wins
// outright; otherwise a live summary (the peer's own authored cp3 summary,
// or else claude's own pane-title summary -- see Session.EffectiveSummary)
// is the most informative thing to show; a Working agent with no summary
// shows its live verb + elapsed instead; an agent parked past idleThreshold
// reads "idle". Everything else is blank -- a plain shell has nothing to say
// here, and its last-active time is its own column now.
func renderStatus(styles Styles, now time.Time, s session.Session) string {
	switch {
	case s.State == session.StateNotify:
		return styles.Error.Render("needs you")
	case s.EffectiveSummary() != "":
		return styles.Summary.Render(s.EffectiveSummary())
	case s.State == session.StateWorking && s.WorkingVerb != "":
		return styles.Working.Render(strings.TrimSpace(s.WorkingVerb + "… " + s.WorkingElapsed))
	default:
		// A parked agent -- present but untouched for a good while, with no
		// summary to show -- reads "idle" so a blank cell doesn't look like
		// missing data. A freshly-idle agent (may still resume) and a plain
		// shell (not an agent at all) stay blank; its last-active time is its
		// own column anyway.
		if s.Agent && now.Sub(s.Activity) > idleThreshold {
			return styles.Muted.Render("idle")
		}
		return ""
	}
}

// renderName renders the session-name cell -- just the name, bold. Peer
// liveness lives in its own column now (see renderPeer).
func renderName(styles Styles, s session.Session) string {
	return styles.Name.Render(s.Name)
}

// renderPeer renders the peer cell: a filled green dot when the workspace's
// bound peer is live (up), a hollow red one when it's down, a "✉" alongside
// when that peer has unread cp3 mail waiting, and the peer's own name when it
// differs from the session's (case/hyphenation-insensitively, via
// session.NormalizeName -- the common case is they match, so most peer
// workspaces show just a dot). "" -- no cell -- when the session isn't a peer
// workspace at all.
func renderPeer(styles Styles, s session.Session) string {
	if s.PeerName == "" {
		return ""
	}

	dot := styles.PeerDown.Render("○")
	if s.Machine != "" {
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

// renderIcons renders exactly maxIcons fixed-width slots -- one per app,
// blank-padded past len(s.Apps) -- so the icon column's counted width
// never depends on how many apps a session runs. The app that is the
// session's AI agent (there's at most one that matters -- see
// session.IsAgentApp) reflects the session's live AgentState: a ticking
// spinner while Working, a blinking icon on Notify, the static icon
// otherwise. Every other app in the session always renders its static
// icon, unaffected by agent state.
func renderIcons(s session.Session, sp spinner.Model) string {
	slot := lipgloss.NewStyle().Width(iconSlotWidth)
	slots := make([]string, maxIcons)
	for i := range slots {
		if i < len(s.Apps) {
			app := s.Apps[i]
			slots[i] = slot.Render(renderIcon(iconFor(app), s.State, session.IsAgentApp(app), sp))
		} else {
			slots[i] = slot.Render("")
		}
	}
	return lipgloss.NewStyle().Width(iconColWidth).Render(lipgloss.JoinHorizontal(lipgloss.Top, slots...))
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
	default: // StateIdle (StateNone can't reach here: isAgent implies Agent)
		return style.Render(icon.Glyph)
	}
}

// renderCWD returns the raw muted-styled path -- uniform, de-emphasised, no
// per-path colour. Returning it unpadded (not a fixed cell) lets marqueeCell
// own the width, so a long path scrolls on the selected row like the others.
func renderCWD(styles Styles, abbrev string) string {
	return styles.Muted.Render(abbrev)
}

// DumpRows renders the same per-session rows the interactive list would
// show, sorted the same way, as plain text with no selection, for headless
// verification (`sessui --dump`). The spinner is a fresh, untouched model --
// only its first frame renders; --dump is a snapshot, so there's nothing to
// animate.
func DumpRows(sessions []session.Session) string {
	m := New()
	sortSessions(sessions)
	sp := spinner.New(spinner.WithSpinner(spinner.MiniDot))
	var b strings.Builder
	for _, s := range sessions {
		b.WriteString(renderRow(m.styles, m.home, time.Now(), s, sp, 0, false))
		b.WriteString("\n")
	}
	return b.String()
}
