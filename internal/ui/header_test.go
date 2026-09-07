package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func stripStyle(s string) string { return ansi.Strip(s) }

func countNewlines(s string) int { return strings.Count(s, "\n") }

func TestCountText(t *testing.T) {
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
		{"not filtering ignores shown", 7, 3, false, "7 sessions"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := countText(tc.total, tc.shown, tc.filtering); got != tc.want {
				t.Errorf("countText(%d,%d,%v) = %q, want %q", tc.total, tc.shown, tc.filtering, got, tc.want)
			}
		})
	}
}

// The brand line sits above the table, so a second line would push the list
// down on every render. One line of at most width cells, at any width.
func TestBrandLine_OneLineWithinWidth(t *testing.T) {
	s := testStyles()
	for _, w := range []int{0, 1, 5, 12, 20, 40, 80, 112, 200} {
		got := brandLine(s, "willy@omarchy", 7, 7, false, w)
		if countNewlines(got) != 0 {
			t.Errorf("width %d: %d newline(s), want 0", w, countNewlines(got))
		}
		if n := lipgloss.Width(got); n > w {
			t.Errorf("width %d: rendered %d cells, overflows", w, n)
		}
	}
}

// The identity is centred on the FULL width, not merely placed between the two
// ends -- so it holds still while the tally changes width under a filter. A
// centre that drifted as you typed would be worse than no centre.
func TestBrandLine_IdentityStaysCentredAsTallyChanges(t *testing.T) {
	s := testStyles()
	const w = 112
	// Measured in CELLS, not bytes: the rule is U+2500, three bytes each, so a
	// byte offset from strings.Index is roughly triple the column it sits at.
	pos := func(total, shown int, filtering bool) int {
		line := stripStyle(brandLine(s, "willy@omarchy", total, shown, filtering, w))
		i := strings.Index(line, "willy@omarchy")
		if i < 0 {
			t.Fatalf("identity absent from %q", line)
		}
		return lipgloss.Width(line[:i])
	}
	wide := pos(7, 7, false)   // "7 sessions"
	narrow := pos(13, 3, true) // "3 of 13 sessions" -- six cells longer
	if wide != narrow {
		t.Errorf("identity moved when the tally grew: %d vs %d", wide, narrow)
	}
	// And it really is centred, not left-parked.
	// +1 for the space that pads the centred block; the NAME sits on the
	// midpoint, the padding straddles it.
	mid := (w-(lipgloss.Width("willy@omarchy")+2))/2 + 1
	if wide != mid {
		t.Errorf("identity at %d, want centred at %d", wide, mid)
	}
}

// Both halves of the useful content survive at the real popup width.
func TestBrandLine_CarriesWordmarkAndTally(t *testing.T) {
	got := stripStyle(brandLine(testStyles(), "willy@omarchy", 7, 7, false, 112))
	for _, want := range []string{brandName, "willy@omarchy", "7 sessions"} {
		if !strings.Contains(got, want) {
			t.Errorf("brand line missing %q: %q", want, got)
		}
	}
}

// Narrow sheds the wordmark first and the identity last: which machine you are
// on is the useful half.
func TestBrandLine_NarrowKeepsIdentity(t *testing.T) {
	got := stripStyle(brandLine(testStyles(), "willy@omarchy", 7, 7, false, 34))
	if !strings.Contains(got, "willy@omarchy") {
		t.Errorf("narrow brand line dropped the identity: %q", got)
	}
	if strings.Contains(got, brandName) {
		t.Errorf("narrow brand line kept the wordmark with no room: %q", got)
	}
}

// A terminal with no Nerd Font gets an ASCII rule; box drawing is not assumed.
func TestBrandLine_ASCIIRule(t *testing.T) {
	defer func(prev glyphSet) { activeGlyphs = prev }(activeGlyphs)
	activeGlyphs = glyphsASCII
	got := stripStyle(brandLine(testStyles(), "willy@omarchy", 7, 7, false, 112))
	if strings.Contains(got, "─") {
		t.Errorf("ASCII mode still used box drawing: %q", got)
	}
	if !strings.Contains(got, "--") {
		t.Errorf("ASCII mode has no rule: %q", got)
	}
}
