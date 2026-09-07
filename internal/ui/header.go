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
	centre := " " + s.Name.Bold(true).Render(who) + " "
	centreW := lipgloss.Width(who) + 2
	left := cap + " " + brandName + " "
	right := " " + countText(total, shown, filtering) + " " + cap

	// Centre first, ends second: the identity is the thing being centred, so
	// it keeps its position and the ends give way around it.
	start := (width - centreW) / 2
	leftFill := start - lipgloss.Width(left)
	rightFill := width - start - centreW - lipgloss.Width(right)

	// No room for the wordmark: drop it, keep the tally.
	if leftFill < 1 {
		left = cap + " "
		leftFill = start - lipgloss.Width(left)
	}
	// Still no room, or none on the right: bare rule around the identity.
	if leftFill < 1 || rightFill < 1 {
		left, right = cap+" ", " "+cap
		leftFill = start - lipgloss.Width(left)
		rightFill = width - start - centreW - lipgloss.Width(right)
	}
	// Narrower than the identity itself: it is the only useful part left.
	if leftFill < 0 || rightFill < 0 {
		return s.Name.Bold(true).Render(ansi.Truncate(who, width, "…"))
	}

	return s.Muted.Render(left+strings.Repeat(rule, leftFill)) +
		centre +
		s.Muted.Render(strings.Repeat(rule, rightFill)+right)
}
