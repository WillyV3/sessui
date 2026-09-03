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

// countLineData is everything renderCountLine needs: the plain tally
// (Total/Shown/Filtering, the line's original content, wording unchanged) plus
// the two attention counts. NeedsYou/Mail are counts, not booleans, so a pill
// reads "3" rather than just lighting up -- worth knowing before you scan for
// which row.
type countLineData struct {
	Total, Shown int
	Filtering    bool
	NeedsYou     int
	Mail         int
}

// renderCountLine draws the tally on the left (identical wording to the old
// countLine) and, flush right at width, a pill per nonzero attention signal --
// needs-you then mail. A pill is silent at zero: no glyph, no background, no
// reserved space, so the line stays exactly as quiet as it used to be when
// nothing is pending, and only grows a designed status band when something
// is.
func renderCountLine(s Styles, width int, c countLineData) string {
	noun := "sessions"
	if c.Total == 1 {
		noun = "session"
	}
	left := fmt.Sprintf("%d %s", c.Total, noun)
	if c.Filtering && c.Shown != c.Total {
		left = fmt.Sprintf("%d of %d %s", c.Shown, c.Total, noun)
	}
	leftRendered := s.Count.Render(left)

	var pills []string
	if c.NeedsYou > 0 {
		pills = append(pills, s.NeedsYouPill.Render(fmt.Sprintf("%s %d", glyphU(glyphNeedsYou), c.NeedsYou)))
	}
	if c.Mail > 0 {
		pills = append(pills, s.MailPill.Render(fmt.Sprintf("%s %d", glyphMail, c.Mail)))
	}
	if len(pills) == 0 {
		return leftRendered
	}

	right := strings.Join(pills, " ")
	// ponytail: real content (a tally + two short pills) never comes close to
	// filling 108 columns -- this floor only stops strings.Repeat from seeing
	// a negative count on a pathologically narrow width, it doesn't fire in
	// practice.
	gap := max(width-lipgloss.Width(leftRendered)-lipgloss.Width(right), 1)
	return leftRendered + strings.Repeat(" ", gap) + right
}
