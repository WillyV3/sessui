// The count line above the column headers: "N sessions", left-aligned, and
// nothing else.
//
// It briefly carried a widget section -- a row of collapsed widgets on the
// right (audio, host, agents, shell) that expanded under ^w. That is gone by
// Willy's call. What is left is what was here before it: one tally.
package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// renderHeaderLine is the tally: "N sessions", or "S of N sessions" while a
// filter is narrowing the list.
func renderHeaderLine(s Styles, total, shown int, filtering bool) string {
	noun := "sessions"
	if total == 1 {
		noun = "session"
	}
	if filtering && shown != total {
		return s.Count.Render(fmt.Sprintf("%d of %d %s", shown, total, noun))
	}
	return s.Count.Render(fmt.Sprintf("%d %s", total, noun))
}

// brandName is spaced out because at one row it is chrome, not a logo: letter
// spacing reads as deliberate typography where a bare "SESSUI" reads as a
// stray word in the rule.
const brandName = "S E S S U I"

// brandLine is the one decorative row: a rule carrying who and where on the
// left and the app name on the right.
//
//	── willy@omarchy ─────────────────────── S E S S U I ──
//
// One row, and only one: the popup is 26 rows and the list wants every one of
// them (14 sessions is a normal day here), so this is the whole budget the
// decoration gets. It degrades instead of wrapping -- a second line would push
// the table down on every render.
func brandLine(s Styles, who string, width int) string {
	if width <= 0 {
		return ""
	}
	rule := "─"
	if activeGlyphs == glyphsASCII {
		rule = "-"
	}
	cap := strings.Repeat(rule, 2)
	left := cap + " " + who + " "
	right := " " + brandName + " " + cap
	// Too narrow for both: the identity is the useful half, so the name goes
	// first and the rule follows it rather than the line being dropped.
	if len(left)+len(right)+4 > width {
		if lipgloss.Width(left) > width {
			return s.Muted.Render(ansi.Truncate(left, width, "…"))
		}
		return s.Muted.Render(left + strings.Repeat(rule, width-lipgloss.Width(left)))
	}
	fill := width - lipgloss.Width(left) - lipgloss.Width(right)
	return s.Muted.Render(left + strings.Repeat(rule, fill) + right)
}
