package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// The chrome row above the column headers: a rule carrying the tally on the
// left, user@host centred and bold, and the wordmark on the right.

// brandName is letter-spaced because at one row it is chrome, not a logo: a
// bare "SESSUI" reads as a stray word in the rule.
const brandName = "S E S S U I"

// countText is the tally: "N sessions", or "S of N sessions" while a filter
// narrows the list.
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
// segment, with rule characters filling the gaps.
type ruleRow struct {
	styles Styles
	rule   string
	left   string
	right  string
	// centre is already styled, so its width is carried separately.
	centre      string
	centreWidth int
}

// render lays the row out at width. The centre sits on the midpoint of the
// full width, not between the two segments, so it holds still as the tally
// changes width under a filter.
//
// ok is false when the segments do not fit; the caller sheds one and asks
// again rather than this guessing which to sacrifice.
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

// identity is the centred block and its width: the name in bold with a space
// either side, so the rule does not run flush against it. The padding is part
// of the block, so the name itself lands on the midpoint.
func identity(s Styles, who string) (string, int) {
	return " " + s.Name.Bold(true).Render(who) + " ", lipgloss.Width(who) + 2
}

// identityOnly is what is left when the row is narrower than the identity
// plus a rule either side.
func identityOnly(s Styles, who string, width int) string {
	return s.Name.Bold(true).Render(ansi.Truncate(who, width, "…"))
}

// brandLine composes the row, shedding parts rather than wrapping -- the
// wordmark, then the tally, then everything but the identity. Each rung is
// tried whole: shrinking both ends at once leaves a rule with a hole in it.
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

// brandLineLoading is the same row before the first list has arrived: a
// spinner where the tally will go, rather than a "0 sessions" that claims
// something about the machine the program has not yet asked.
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
