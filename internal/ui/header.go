// The count line above the column headers: "N sessions", left-aligned, and
// nothing else.
//
// It briefly carried a widget section -- a row of collapsed widgets on the
// right (audio, host, agents, shell) that expanded under ^w. That is gone by
// Willy's call. What is left is what was here before it: one tally.
package ui

import "fmt"

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
