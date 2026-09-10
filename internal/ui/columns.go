package ui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"

	"github.com/WillyV3/sessui/internal/session"
)

// columnID names one kind of table column. It is the unit of user
// configuration and what persists to disk, so the string values are a stable
// contract.
type columnID string

const (
	colSession  columnID = "session"
	colApps     columnID = "apps"
	colActive   columnID = "active"
	colCWD      columnID = "cwd"
	colPeer     columnID = "peer"
	colStatus   columnID = "status"
	colAge      columnID = "age"      // how long the session has existed
	colWindows  columnID = "windows"  // tmux window count
	colAttached columnID = "attached" // a client is on it right now
	colHost     columnID = "host"     // the machine this session lives on
)

// column is one cell of the table: its label, its width, and how it draws a
// session. Widths are fixed cells so rows form a rectangle; exactly one
// column per layout is flex (width 0) and takes what the fixed ones leave.
type column struct {
	id    columnID
	label string
	glyph rune // header glyph codepoint; 0 = label only
	// asciiLabel stands in for a glyph-only header when the glyph set is
	// ASCII, so "active" and "attached" do not become blank headers.
	asciiLabel string
	// width is the fixed cell width, or 0 for the single flex column.
	width int
	// minWidth is the narrowest a user may resize this column to.
	minWidth int
	render   func(cell) string
}

// cell is everything a renderer needs to draw one session. A value, not a
// pointer, because renderers are pure: the same cell must render identically
// in the live list and in --dump.
type cell struct {
	styles  Styles
	home    string
	now     time.Time
	session session.Session
	spinner spinner.Model
}

// tableLayout is the resolved shape of one render: the visible columns and
// the width each gets. Computed once per resize or reload, never per cell.
type tableLayout struct {
	columns []column
	widths  []int
}

// columnGap is the one-cell separator between columns; there are len-1 of
// them. The cursor's own trailing space is its gap.
const columnGap = 1

// gapsFor is the total separator width a column count needs.
func gapsFor(columnCount int) int { return columnGap * max(columnCount-1, 0) }

// cursorWidth is the "> " prefix before the first column. It is not a column
// -- it cannot be moved, hidden or resized -- so it lives outside the spec.
const cursorWidth = 2

// layoutColumns splits width across cols. Fixed columns get their width; the
// flex column gets the remainder, floored at its minWidth so a too-narrow
// popup degrades to a clipped column rather than a negative width.
func layoutColumns(cols []column, width int) tableLayout {
	widths := make([]int, len(cols))
	remaining := width - cursorWidth - gapsFor(len(cols))
	flex := -1
	for i, c := range cols {
		if c.width == 0 {
			flex = i
			continue
		}
		widths[i] = c.width
		remaining -= c.width
	}
	if flex >= 0 {
		widths[flex] = max(remaining, cols[flex].minWidth)
	}
	return tableLayout{columns: cols, widths: widths}
}

// width reports the total the layout occupies, cursor and gaps included.
func (l tableLayout) width() int {
	w := cursorWidth + gapsFor(len(l.columns))
	for _, cw := range l.widths {
		w += cw
	}
	return w
}

// columnCatalog is every column sessui knows how to draw. The user's
// configuration selects and orders a subset; a column not in the catalog
// cannot be configured into existence.
var columnCatalog = map[columnID]column{
	colSession: {
		id: colSession, label: "session", glyph: glyphSession, width: 22, minWidth: 8,
		render: func(c cell) string { return c.styles.Name.Render(c.session.Name) },
	},
	colApps: {
		id: colApps, label: "apps", width: iconColWidth, minWidth: iconSlotWidth,
		render: func(c cell) string { return renderIcons(c.session, c.spinner) },
	},
	colActive: {
		id: colActive, label: "", glyph: glyphActive, asciiLabel: "ago", width: 5, minWidth: 4,
		render: func(c cell) string {
			since := c.now.Sub(c.session.Activity)
			return c.styles.activeHeat(since).Render(session.LastActive(c.session.Activity, c.now))
		},
	},
	colCWD: {
		id: colCWD, label: "cwd", glyph: glyphCWD, width: 20, minWidth: 6,
		render: func(c cell) string {
			return c.styles.Muted.Render(session.AbbreviatePath(c.session.CWD, c.home))
		},
	},
	colPeer: {
		id: colPeer, label: "peer", glyph: glyphPeer, width: 14, minWidth: 3,
		render: func(c cell) string { return renderPeer(c.styles, c.session) },
	},
	colStatus: {
		id: colStatus, label: "status", glyph: glyphStatus, width: 0, minWidth: 10,
		render: func(c cell) string { return renderStatus(c.styles, c.now, c.session) },
	},
	colAge: {
		id: colAge, label: "age", glyph: glyphAge, width: 5, minWidth: 4,
		render: func(c cell) string {
			return c.styles.Muted.Render(session.LastActive(c.session.Created, c.now))
		},
	},
	colWindows: {
		// 5, not 4: the header is glyph + space + "win" and must not truncate
		// to "wi"; the data fits in far less, hence the lower floor.
		id: colWindows, label: "win", glyph: glyphWindows, width: 5, minWidth: 3,
		render: func(c cell) string {
			return c.styles.Muted.Render(strconv.Itoa(c.session.Windows))
		},
	},
	colAttached: {
		id: colAttached, label: "", glyph: glyphAttached, asciiLabel: "on", width: 2, minWidth: 2,
		render: func(c cell) string {
			if c.session.Attached {
				return c.styles.PeerUp.Render(glyphU(glyphAttached))
			}
			return ""
		},
	},
	colHost: {
		id: colHost, label: "host", glyph: glyphMachine, width: 12, minWidth: 4,
		// Blank for a local session: local is the default and needs no label.
		render: func(c cell) string { return c.styles.Muted.Render(sessionMachine(c.session)) },
	},
}

// defaultColumnOrder is the table a fresh install shows.
var defaultColumnOrder = []columnID{colSession, colApps, colActive, colCWD, colPeer, colStatus}

// columnSetting is one column as the user configured it. Slice order is
// display order.
type columnSetting struct {
	ID    columnID `json:"id"`
	Width int      `json:"width,omitempty"` // 0 = catalog default
}

// defaultColumnSettings is defaultColumnOrder with no overrides.
func defaultColumnSettings() []columnSetting {
	out := make([]columnSetting, len(defaultColumnOrder))
	for i, id := range defaultColumnOrder {
		out[i] = columnSetting{ID: id}
	}
	return out
}

// resolveColumns turns settings into renderable columns. Unknown ids are
// dropped rather than failing, so a config written by a newer build never
// bricks an older one. showPeer hides the peer column when no cp3 peers are
// in use, regardless of configuration.
func resolveColumns(settings []columnSetting, showPeer bool) []column {
	cols := make([]column, 0, len(settings))
	for _, s := range settings {
		c, ok := columnCatalog[s.ID]
		if !ok || (s.ID == colPeer && !showPeer) {
			continue
		}
		if s.Width > 0 && c.width > 0 { // never override the flex column
			c.width = max(s.Width, c.minWidth)
		}
		cols = append(cols, c)
	}
	return cols
}

// Header glyphs for the optional columns; the rest live in delegate.go.
const (
	glyphAge      = 0xF0150 // nf-md-calendar_clock
	glyphWindows  = 0xF0E1E // nf-md-dock_window
	glyphAttached = 0xF0E7E // nf-md-monitor_eye
	glyphMachine  = 0xF0EA1 // nf-md-server_network_outline
)

// headerCell renders one column's label for the header band.
func (c column) headerCell(s Styles) string {
	return s.Header.Render(c.headerText(c.glyph))
}

// headerText is the header's content for a glyph codepoint (the column's
// own, or the editor's armed marker): glyph and label with one space between,
// whichever exist. In ASCII mode a glyph-only column shows its asciiLabel.
func (c column) headerText(glyph rune) string {
	g := ""
	if glyph != 0 {
		g = glyphU(glyph)
	}
	label := c.label
	if g == "" && label == "" {
		label = c.asciiLabel
	}
	return joinGlyph(g, label)
}

// String makes a column read well in logs.
func (c column) String() string {
	return fmt.Sprintf("%s(w=%d)", c.id, c.width)
}

// Widths are plain ints and exactly one column flexes. A second flex column
// would need weight-based distribution of the remainder.

// sessionMachine is which computer a session is on. A remote session carries
// its Host; a proxied one is a genuinely local session whose origin lives in
// its name. A local session literally named "a/b" therefore reads as machine
// "a", which is rare.
func sessionMachine(s session.Session) string {
	if s.Host != "" {
		return s.Host
	}
	if host, _, ok := strings.Cut(s.Name, session.ProxySep); ok {
		return host
	}
	return ""
}
