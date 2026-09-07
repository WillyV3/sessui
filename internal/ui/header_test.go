package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// The header used to carry a widget section under ^w. It no longer does, so
// what is left to test is the tally itself: the noun agrees with the count,
// and the filter form only appears when a filter is actually narrowing.
func TestRenderHeaderLine_Tally(t *testing.T) {
	s := testStyles()
	for _, tc := range []struct {
		name         string
		total, shown int
		filtering    bool
		want         string
	}{
		{"plural", 7, 7, false, "7 sessions"},
		{"singular takes the singular noun", 1, 1, false, "1 session"},
		{"zero is plural", 0, 0, false, "0 sessions"},
		{"filtering shows the match count", 13, 3, true, "3 of 13 sessions"},
		// Filtering with everything still visible is not narrowing anything,
		// so the "N of N" form would be noise.
		{"filtering that excludes nothing stays plain", 7, 7, true, "7 sessions"},
		// The filter form is only reachable while filtering; a stale shown
		// count must not leak into the unfiltered line.
		{"not filtering ignores shown", 7, 3, false, "7 sessions"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ansi.Strip(renderHeaderLine(s, tc.total, tc.shown, tc.filtering)); got != tc.want {
				t.Errorf("renderHeaderLine(%d, %d, %v) = %q, want %q", tc.total, tc.shown, tc.filtering, got, tc.want)
			}
		})
	}
}

// The header is one line and must stay one line: it sits above the table, so a
// second line would push the list down on every render.
func TestRenderHeaderLine_IsAlwaysOneLine(t *testing.T) {
	s := testStyles()
	for _, tc := range []struct{ total, shown int }{{0, 0}, {1, 1}, {999, 12}} {
		got := renderHeaderLine(s, tc.total, tc.shown, true)
		if n := countNewlines(got); n != 0 {
			t.Errorf("renderHeaderLine(%d,%d) has %d newline(s), want 0: %q", tc.total, tc.shown, n, got)
		}
	}
}

func countNewlines(s string) int {
	n := 0
	for _, r := range s {
		if r == '\n' {
			n++
		}
	}
	return n
}

// The brand line is decoration on a tool whose scarce resource is rows, so the
// invariant that matters is that it is always exactly one line of at most the
// width it was given -- at any width, including absurd ones.
func TestBrandLine_OneLineWithinWidth(t *testing.T) {
	s := testStyles()
	for _, w := range []int{0, 1, 5, 12, 20, 40, 80, 112, 200} {
		got := brandLine(s, "willy@omarchy", w)
		if countNewlines(got) != 0 {
			t.Errorf("width %d: %d newline(s), want 0", w, countNewlines(got))
		}
		if n := lipgloss.Width(got); n > w {
			t.Errorf("width %d: rendered %d cells, overflows", w, n)
		}
	}
}

// Narrow terminals keep the identity and drop the name: which machine you are
// on is the useful half, the wordmark is not.
func TestBrandLine_NarrowKeepsIdentity(t *testing.T) {
	got := stripStyle(brandLine(testStyles(), "willy@omarchy", 24))
	if !strings.Contains(got, "willy@omarchy") {
		t.Errorf("narrow brand line dropped the identity: %q", got)
	}
	if strings.Contains(got, brandName) {
		t.Errorf("narrow brand line kept the wordmark, no room for it: %q", got)
	}
}

// A terminal with no Nerd Font gets the ASCII rule; box-drawing is not assumed.
func TestBrandLine_ASCIIRule(t *testing.T) {
	defer func(prev glyphSet) { activeGlyphs = prev }(activeGlyphs)
	activeGlyphs = glyphsASCII
	got := stripStyle(brandLine(testStyles(), "willy@omarchy", 80))
	if strings.Contains(got, "─") {
		t.Errorf("ASCII mode still used box drawing: %q", got)
	}
	if !strings.Contains(got, "--") {
		t.Errorf("ASCII mode has no rule: %q", got)
	}
}

func stripStyle(s string) string { return ansi.Strip(s) }
