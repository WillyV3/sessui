// The one chrome row above the column headers: a rule carrying the wordmark,
// who and where (centred, bold), and the session tally.
//
//	── S E S S U I ──────────  willy@omarchy  ────────── 7 sessions ──
//
// It briefly carried a widget section that expanded under ^w; that is gone.
// The tally used to be its own row underneath and has been folded in here,
// which gives the list that row back -- the popup is 26 rows and 14 sessions
// is a normal day, so a row is worth more to the table than to chrome.
package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// brandName is spaced out because at one row it is chrome, not a logo: letter
// spacing reads as deliberate typography where a bare "SESSUI" reads as a
// stray word in the rule.
const brandName = "S E S S U I"

// countText is the tally: "N sessions", or "S of N sessions" while a filter is
// narrowing the list -- the one moment the number earns its place.
func countText(total, shown int, filtering bool) string {
	noun := "sessions"
	if total == 1 {
		noun = "session"
	}
	if filtering && shown != total {
		return fmt.Sprintf("%d of %d %s", shown, total, noun)
	}
	return fmt.Sprintf("%d %s", total, noun)
}

// brandLine composes the row. who is centred on the FULL width -- not merely
// placed between the two ends -- so it stays put as the tally changes width
// under a filter; a centre that drifted while typing would be worse than no
// centre at all.
//
// Always exactly one line of at most width cells. It sheds parts rather than
// wrapping, in this order: the wordmark, then the tally, then the rule itself.
// A second line would push the table down on every render.
func brandLine(s Styles, who string, total, shown int, filtering bool, width int) string {
	if width <= 0 {
		return ""
	}
	rule := "─"
	if activeGlyphs == glyphsASCII {
		rule = "-"
	}
	cap := strings.Repeat(rule, 2)

	// A space either side: the rule running flush against the name reads as a
	// collision rather than a centrepiece. Padding is part of the centred
	// block, so the name itself stays on the midpoint.
	// A space either side: the rule running flush against the name reads as a
	// collision rather than a centrepiece. Padding is part of the centred
	// block, so the name itself stays on the midpoint.
	centre := " " + s.Name.Bold(true).Render(who) + " "
	centreW := lipgloss.Width(who) + 2
	start := (width - centreW) / 2

	// Shed in order of usefulness as the terminal narrows: the wordmark is
	// decoration, the tally is information, the identity is the reason the
	// line exists. Each rung is tried whole -- an earlier version shrank both
	// ends at once and produced "── ──── willy@omarchy ──── ──", a rule with
	// a hole in it where the tally had been.
	for _, rung := range []struct{ tally, mark bool }{
		{true, true},
		{true, false},
		{false, false},
	} {
		left, right := cap, cap
		if rung.tally {
			left = cap + " " + countText(total, shown, filtering) + " "
		}
		if rung.mark {
			right = " " + brandName + " " + cap
		}
		leftFill := start - lipgloss.Width(left)
		rightFill := width - start - centreW - lipgloss.Width(right)
		if leftFill < 1 || rightFill < 1 {
			continue
		}
		return s.Muted.Render(left+strings.Repeat(rule, leftFill)) +
			centre +
			s.Muted.Render(strings.Repeat(rule, rightFill)+right)
	}

	// Narrower than the identity plus a rule either side: the identity is the
	// only part left worth showing.
	return s.Name.Bold(true).Render(ansi.Truncate(who, width, "…"))
}
