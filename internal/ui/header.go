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

// ruleRow is one chrome row: a left segment, a centred block and a right
// segment, with rule characters filling the gaps between them.
type ruleRow struct {
	styles Styles
	rule   string
	left   string
	right  string
	// centre is already styled, so its display width cannot be measured from
	// it reliably; centreWidth carries that separately.
	centre      string
	centreWidth int
}

// render lays the row out at width.
//
// The centre sits on the midpoint of the FULL width, not between the two
// segments, so it holds still as they change size -- the tally grows by six
// cells the moment a filter narrows the list, and a centre that slid sideways
// while typing would be worse than no centre at all.
//
// ok is false when the segments do not fit. That is the caller's cue to drop
// one and ask again, rather than this guessing which to sacrifice.
func (r ruleRow) render(width int) (string, bool) {
	start := (width - r.centreWidth) / 2
	leftFill := start - lipgloss.Width(r.left)
	rightFill := width - start - r.centreWidth - lipgloss.Width(r.right)
	if leftFill < 1 || rightFill < 1 {
		return "", false
	}
	return r.styles.Muted.Render(r.left+strings.Repeat(r.rule, leftFill)) +
		r.centre +
		r.styles.Muted.Render(strings.Repeat(r.rule, rightFill)+r.right), true
}

// ruleChar is the fill, degrading for a terminal with no box drawing.
func ruleChar() string {
	if activeGlyphs == glyphsASCII {
		return "-"
	}
	return "─"
}

// ruleEnd caps a rule at the edge of the row.
func ruleEnd() string { return strings.Repeat(ruleChar(), 2) }

// identity is the centred block and its display width: the name in bold with a
// space either side, because a rule running flush against it reads as a
// collision rather than a centrepiece. The padding is part of the centred
// block, so the name itself still lands on the midpoint.
func identity(s Styles, who string) (string, int) {
	return " " + s.Name.Bold(true).Render(who) + " ", lipgloss.Width(who) + 2
}

// identityOnly is what is left when the row is narrower than the identity plus
// a rule either side. Which machine you are on is the half worth keeping.
func identityOnly(s Styles, who string, width int) string {
	return s.Name.Bold(true).Render(ansi.Truncate(who, width, "…"))
}

// brandLine composes the row, shedding parts rather than wrapping: the wordmark
// is decoration, the tally is information, the identity is why the line exists.
//
// Each rung is tried whole. An earlier version shrank both ends at once and
// produced "── ──── willy@omarchy ──── ──" -- a rule with a hole in it where
// the tally had been.
func brandLine(s Styles, who string, total, shown int, filtering bool, width int) string {
	if width <= 0 {
		return ""
	}
	centre, centreWidth := identity(s, who)
	end := ruleEnd()

	for _, rung := range []struct{ tally, wordmark bool }{
		{tally: true, wordmark: true},
		{tally: true, wordmark: false},
		{tally: false, wordmark: false},
	} {
		row := ruleRow{
			styles: s, rule: ruleChar(),
			left: end, right: end,
			centre: centre, centreWidth: centreWidth,
		}
		if rung.tally {
			row.left = end + " " + countText(total, shown, filtering) + " "
		}
		if rung.wordmark {
			row.right = " " + brandName + " " + end
		}
		if out, ok := row.render(width); ok {
			return out
		}
	}
	return identityOnly(s, who, width)
}

// brandLineLoading is the same row before the first list has arrived: a spinner
// and a word where the tally will go.
//
// It exists because the alternative was rendering "0 sessions" -- a claim about
// the machine, when the truth was only that this program had not finished
// asking. On a 17-session box that read as an empty fleet on every open.
func brandLineLoading(s Styles, who, spinner string, width int) string {
	if width <= 0 {
		return ""
	}
	centre, centreWidth := identity(s, who)
	end := ruleEnd()

	row := ruleRow{
		styles: s, rule: ruleChar(),
		left:   end + " " + spinner + " " + s.Muted.Render("loading") + " ",
		right:  " " + brandName + " " + end,
		centre: centre, centreWidth: centreWidth,
	}
	if out, ok := row.render(width); ok {
		return out
	}
	return identityOnly(s, who, width)
}
