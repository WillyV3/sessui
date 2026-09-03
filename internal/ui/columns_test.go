package ui

import (
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/lipgloss"

	"github.com/willyv3/sessui/internal/session"
)

// testLayout is the shipped column layout at the shipped width, with or
// without the peer column -- the shape every rendering test lays out against.
func testLayout(showPeer bool) tableLayout {
	return layoutColumns(resolveColumns(defaultColumnSettings(), showPeer), defaultUsableWidth)
}

// cellFor wraps a session in everything a column renderer needs, with the
// test styles and a fresh spinner.
func cellFor(now time.Time, s session.Session) cell {
	return cell{
		styles:  testStyles(),
		home:    "/home/willy",
		now:     now,
		session: s,
		spinner: spinner.New(spinner.WithSpinner(spinner.MiniDot)),
	}
}

// sampleCell is a fully populated session -- every column has something to
// draw -- for tests that only care that rendering holds its shape.
func sampleCell() cell {
	now := time.Now()
	return cellFor(now, session.Session{
		Name: "peer-mcp-maintainer", Created: now.Add(-23 * time.Hour), Activity: now,
		Attached: true, Windows: 3, CWD: "/home/willy/projects/peer-mcp-maintainer",
		Apps: []string{"claude", "nvim"}, Agent: true, Machine: "omarchy",
		PeerName: "peer-mcp-maintainer", PeerSummary: "a summary long enough to matter",
	})
}

// TestLayoutColumns_FillsExactlyTheWidth pins the one invariant every other
// layout property depends on: at any width, fixed columns keep their width
// and the single flex column absorbs the remainder, so the row is exactly as
// wide as the popup -- never a sawtooth, never an overrun -- until the popup
// is too narrow to hold the fixed columns at all, at which point the flex
// column stops at its floor and the row is allowed to overrun rather than
// go negative.
func TestLayoutColumns_FillsExactlyTheWidth(t *testing.T) {
	cols := resolveColumns(defaultColumnSettings(), true)

	for _, tc := range []struct {
		name  string
		width int
	}{
		{"shipped geometry", defaultUsableWidth},
		{"narrower popup", 96},
		{"wider popup", 140},
		{"very wide", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := layoutColumns(cols, tc.width)
			if got := l.width(); got != tc.width {
				t.Errorf("layout width = %d, want %d (flex column must absorb the difference)", got, tc.width)
			}
			for i, c := range l.columns {
				if c.width > 0 && l.widths[i] != c.width {
					t.Errorf("%s: width = %d, want fixed %d", c.id, l.widths[i], c.width)
				}
			}
		})
	}

	t.Run("too narrow floors the flex column", func(t *testing.T) {
		l := layoutColumns(cols, 40)
		status := l.widths[len(l.widths)-1]
		if want := columnCatalog[colStatus].minWidth; status != want {
			t.Errorf("status width at 40 cols = %d, want floor %d (never negative)", status, want)
		}
	})
}

// TestLayoutColumns_ShippedWidthsUnchanged guards the upgrade: a user who
// never opens the column editor must get byte-identical column widths to the
// pre-configurable build (session 22, apps 6, active 5, cwd 20, peer 14,
// status = the remainder), so nothing moves under them.
func TestLayoutColumns_ShippedWidthsUnchanged(t *testing.T) {
	l := layoutColumns(resolveColumns(defaultColumnSettings(), true), defaultUsableWidth)
	want := map[columnID]int{colSession: 22, colApps: 6, colActive: 5, colCWD: 20, colPeer: 14, colStatus: 34}
	for i, c := range l.columns {
		if l.widths[i] != want[c.id] {
			t.Errorf("%s: width = %d, want %d", c.id, l.widths[i], want[c.id])
		}
	}
}

// TestResolveColumns covers the configuration edge cases: the peer column
// hides at runtime regardless of config, an id from a future build is
// ignored rather than fatal, a width override applies to fixed columns and is
// floored at minWidth, and the flex column cannot be given a fixed width.
func TestResolveColumns(t *testing.T) {
	t.Run("peer hidden when no cp3 peers in use", func(t *testing.T) {
		cols := resolveColumns(defaultColumnSettings(), false)
		for _, c := range cols {
			if c.id == colPeer {
				t.Fatal("peer column present with showPeer=false")
			}
		}
		if got, want := layoutColumns(cols, defaultUsableWidth).width(), defaultUsableWidth; got != want {
			t.Errorf("width without peer = %d, want %d (status must reclaim the space)", got, want)
		}
	})

	t.Run("unknown id from a newer build is dropped, not fatal", func(t *testing.T) {
		cols := resolveColumns([]columnSetting{{ID: colSession}, {ID: "holograms"}, {ID: colStatus}}, true)
		if len(cols) != 2 {
			t.Fatalf("got %d columns, want 2 (unknown id silently dropped)", len(cols))
		}
	})

	t.Run("width override applies and floors at minWidth", func(t *testing.T) {
		cols := resolveColumns([]columnSetting{{ID: colSession, Width: 30}, {ID: colCWD, Width: 1}, {ID: colStatus, Width: 99}}, true)
		byID := map[columnID]column{}
		for _, c := range cols {
			byID[c.id] = c
		}
		if byID[colSession].width != 30 {
			t.Errorf("session width = %d, want 30 (override)", byID[colSession].width)
		}
		if want := columnCatalog[colCWD].minWidth; byID[colCWD].width != want {
			t.Errorf("cwd width = %d, want %d (floored at minWidth)", byID[colCWD].width, want)
		}
		if byID[colStatus].width != 0 {
			t.Errorf("status width = %d, want 0 (flex column ignores overrides)", byID[colStatus].width)
		}
	})

	t.Run("every catalog column renders one line at its width", func(t *testing.T) {
		all := make([]columnSetting, 0, len(columnCatalog))
		for id := range columnCatalog {
			all = append(all, columnSetting{ID: id})
		}
		l := layoutColumns(resolveColumns(all, true), 160)
		row := renderRow(l, sampleCell(), 0, false)
		if h := lipgloss.Height(row); h != 1 {
			t.Errorf("row with every column: height = %d, want 1", h)
		}
		if w := lipgloss.Width(row); w != 160 {
			t.Errorf("row with every column: width = %d, want 160", w)
		}
	})
}
