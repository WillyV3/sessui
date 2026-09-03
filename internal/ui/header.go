// The count line above the column headers used to be plain text: "N
// sessions" and nothing else, left-aligned, the right half of a 108-column
// row sitting empty. This file gives that dead space a job -- a right-aligned
// attention band carrying the two signals that exist nowhere else on screen
// (a row's "needs you" and "✉" are per-session and only visible once you're
// already looking at that row; here they're a fleet-wide count, always in
// view at 120 opens/hour).
package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// glyphNeedsYou echoes the bell that actually drives Notify (tmux's
// window_bell_flag -- see renderStatus's "needs you" and renderIcon's
// blinking treatment of session.StateNotify): verified present via
// otfinfo -u JetBrainsMonoNerdFont-Regular.ttf | grep -i F0F3.
const glyphNeedsYou = 0xF0F3 // nf-fa-bell

// glyphMail is the exact "✉" renderPeer already trails an owed-mail session
// with (a plain literal there, not a Nerd Font PUA codepoint via glyphU) --
// reused verbatim so the two count-line signals never invent new iconography.
const glyphMail = "✉"

// attentionPills is the collapsed attention widget: a pill per nonzero
// signal -- needs-you then mail -- and nothing at all at zero, so the line
// stays exactly as quiet as it used to be when nothing is pending.
func attentionPills(s Styles, needsYou, mail int) string {
	var pills []string
	if needsYou > 0 {
		pills = append(pills, s.NeedsYouPill.Render(fmt.Sprintf("%s %d", glyphU(glyphNeedsYou), needsYou)))
	}
	if mail > 0 {
		pills = append(pills, s.MailPill.Render(fmt.Sprintf("%s %d", glyphMail, mail)))
	}
	return strings.Join(pills, " ")
}

// countText is the tally on the left: "N sessions", or "S of N sessions"
// while a filter is narrowing the list.
func countText(s Styles, total, shown int, filtering bool) string {
	noun := "sessions"
	if total == 1 {
		noun = "session"
	}
	if filtering && shown != total {
		return s.Count.Render(fmt.Sprintf("%d of %d %s", shown, total, noun))
	}
	return s.Count.Render(fmt.Sprintf("%d %s", total, noun))
}

// widgetGap separates collapsed widgets in the header's right section.
const widgetGap = "  "

// renderHeaderLine composes the count line: the tally flush left, the widget
// section flush right. focus is the index of the expanded widget, or -1 when
// the list has the keyboard -- then every widget is its icon. An expanded
// widget gets whatever the tally and the other icons leave. The result is
// always ONE line of at most width cells: widgets contract to fit (x/ansi
// truncation, the primitive bubbles scrolls with) rather than the header
// ever growing a second line and pushing the table down.
func renderHeaderLine(st widgetState, width int, widgets []namedWidget, focus int) string {
	left := countText(st.styles, st.total, st.shown, st.filtering)

	var icons []string
	iconsWidth := 0
	for i, w := range widgets {
		if i == focus {
			continue
		}
		if ic := w.icon(st); ic != "" {
			icons = append(icons, ic)
			iconsWidth += lipgloss.Width(ic) + len(widgetGap)
		}
	}

	var right []string
	if focus >= 0 && focus < len(widgets) {
		// Everything not taken by the tally, the icons and their gaps is the
		// expanded widget's; it truncates itself to that.
		avail := width - lipgloss.Width(left) - len(widgetGap) - iconsWidth
		right = append(right, widgets[focus].expand(st, max(avail, 0)))
	}
	right = append(right, icons...)
	rightLine := strings.Join(right, widgetGap)
	if rightLine == "" {
		return left
	}
	return padRight(left, ansi.Truncate(rightLine, max(width-lipgloss.Width(left)-1, 0), "…"), width)
}
