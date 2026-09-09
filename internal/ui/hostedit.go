// The hosts row of the settings editor.
//
// Same surface, same verbs. Columns are edited by arrowing across the header
// the table already draws; hosts are edited by arrowing across the machines
// whose sessions the list already shows, and the preview underneath gains and
// loses rows as you toggle. Nothing here is a settings form.
//
// One row, one cursor axis -- watched machines first, then the rest as chips,
// exactly the strip/shelf split the column rows use. `space` toggles, which is
// what it means on the shelf too. `enter` is deliberately unbound: it means
// arm-to-move on the strip, hosts have no order to change, and a row-specific
// meaning would break "the same verbs always mean the same thing".
package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/WillyV3/sessui/internal/session"
)

// Host reachability marks. Plain Unicode, not Nerd Font PUA, with ASCII
// stand-ins for a terminal without one -- the same rule every other glyph here
// follows.
func hostMark(h session.RemoteHost) string {
	switch {
	case h.Seen.IsZero() && h.Err == nil:
		return glyphOr('◌', "?") // never answered yet -- still asking
	case h.Err != nil:
		return glyphOr('○', "x")
	default:
		return glyphOr('●', "*")
	}
}

// hostOrder is the row's left-to-right sequence: watched machines first in the
// order the user configured them, then everything else discovered.
//
// Unwatched hosts sort reachable-first. Discovery's job is to OFFER, so a
// machine that is asleep is still listed -- hiding it would be the same mistake
// as polling it automatically -- but the ones that can actually be added right
// now come first.
func (e *columnEditor) hostOrder() []string {
	watched := make([]string, 0, len(e.hostWatched))
	for _, h := range e.hosts {
		if e.hostWatched[h] {
			watched = append(watched, h)
		}
	}
	rest := make([]string, 0, len(e.hosts))
	for _, h := range e.hosts {
		if !e.hostWatched[h] {
			rest = append(rest, h)
		}
	}
	sort.SliceStable(rest, func(i, j int) bool {
		ri := e.hostState[rest[i]].Err == nil && !e.hostState[rest[i]].Seen.IsZero()
		rj := e.hostState[rest[j]].Err == nil && !e.hostState[rest[j]].Seen.IsZero()
		return ri && !rj
	})
	return append(watched, rest...)
}

// hostMove walks the cursor along the row.
func (e *columnEditor) hostMove(delta int) {
	n := len(e.hostOrder())
	if n == 0 {
		return
	}
	e.hostCursor = min(max(e.hostCursor+delta, 0), n-1)
}

// hostToggle watches or unwatches the host under the cursor. Toggling is the
// whole interaction: there is no add/remove distinction to learn, and the row
// re-orders under the cursor so the effect is visible immediately.
func (e *columnEditor) hostToggle() {
	order := e.hostOrder()
	if e.hostCursor < 0 || e.hostCursor >= len(order) {
		return
	}
	alias := order[e.hostCursor]
	if e.hostWatched == nil {
		e.hostWatched = map[string]bool{}
	}
	e.hostWatched[alias] = !e.hostWatched[alias]
}

// hostResult is the watched set in the user's configured order, for Config.
func (e *columnEditor) hostResult() []string {
	var out []string
	for _, h := range e.hosts {
		if e.hostWatched[h] {
			out = append(out, h)
		}
	}
	return out
}

func (e *columnEditor) renderHostsRow(width int) string {
	selected := e.row == rowHosts
	order := e.hostOrder()
	if len(order) == 0 {
		return e.controlRow("hosts", e.styles.Muted.Render("no hosts in ~/.ssh/config"), "", width)
	}

	var chips []string
	for i, alias := range order {
		st := e.hostState[alias]
		watched := e.hostWatched[alias]

		// The mark is on every chip, watched or not: "which of these can I
		// add right now" is the question this row exists to answer, and an
		// unwatched host with no state is exactly the one you cannot judge.
		// Watched-ness is carried by colour and the session count instead.
		text := hostMark(st) + " " + alias
		if watched && st.Err == nil && len(st.Sessions) > 0 {
			text += fmt.Sprintf(" %d", len(st.Sessions))
		}

		switch {
		case selected && i == e.hostCursor:
			chips = append(chips, highlightCell(e.styles.selectedBG, e.styles.Header.Render(text)))
		case watched:
			chips = append(chips, e.styles.PeerUp.Render(text))
		default:
			chips = append(chips, e.styles.Muted.Render(text))
		}
	}

	hint := fmt.Sprintf("%d watched", len(e.hostResult()))
	// controlRow spends cursorWidth + captionWidth on the left and anchors the
	// hint right; what is left is the viewport.
	budget := width - cursorWidth - captionWidth - lipgloss.Width(hint) - 1
	return e.controlRow("hosts", e.windowChips(chips, budget), hint, width)
}

// chipGap separates chips. Declared once because the viewport has to measure
// with the same value the join uses, or the window is wrong by a space a chip.
const chipGap = "   "

// windowChips scrolls the row like a table rather than letting it run off the
// edge. A fleet is not a handful: with 16 machines the row is far wider than a
// 112-column popup, and before this the cursor could sit on host 12 while the
// row still showed 1-8 -- space would then toggle something invisible.
//
// The window only moves when the cursor would leave it, so chips stay put while
// you arrow within view instead of re-centring on every keypress. ‹ and › mark
// that there is more in that direction; they are inside the budget, never
// added on top of it.
func (e *columnEditor) windowChips(chips []string, budget int) string {
	if len(chips) == 0 || budget <= 0 {
		return ""
	}
	if e.hostCursor < e.hostScroll {
		e.hostScroll = e.hostCursor
	}

	for {
		start := e.hostScroll
		end, used := start, 0
		for end < len(chips) {
			w := lipgloss.Width(chips[end])
			if end > start {
				w += len(chipGap)
			}
			// Reserve a cell for the marker whenever chips remain beyond here.
			reserve := 0
			if end < len(chips)-1 {
				reserve = 2
			}
			if used+w+reserve > budget {
				break
			}
			used += w
			end++
		}
		if end == start { // one chip wider than the whole row: show it anyway
			end = start + 1
		}
		// Cursor still off the right edge -- give up a chip on the left and
		// measure again rather than guessing how much that frees.
		if e.hostCursor >= end && start < len(chips)-1 {
			e.hostScroll++
			continue
		}
		out := strings.Join(chips[start:end], chipGap)
		if start > 0 {
			out = e.styles.Muted.Render("‹ ") + out
		}
		if end < len(chips) {
			out += e.styles.Muted.Render(" ›")
		}
		return out
	}
}

// syncHostState re-reads the watcher's cache. Called when a probe lands, so a
// chip goes from "still asking" to reachable or not without the panel having
// blocked on it.
func (e *columnEditor) syncHostState() {
	if e.watcher == nil {
		return
	}
	if e.hostState == nil {
		e.hostState = make(map[string]session.RemoteHost, len(e.hosts))
	}
	for _, h := range e.watcher.Snapshot(e.hosts) {
		e.hostState[h.Alias] = h
	}
}

// probeOnEnter starts one fan-out the first time the cursor reaches the hosts
// row -- across EVERY discovered machine, not just the watched ones, because
// the question the shelf has to answer is "which of these can I add right now".
// Once per editor: arrowing up and down the rows must not re-poll the fleet.
func (e *columnEditor) probeOnEnter() tea.Cmd {
	if e.row != rowHosts || e.hostProbed || e.watcher == nil || len(e.hosts) == 0 {
		return nil
	}
	e.hostProbed = true
	return refreshHostsCmd(e.watcher, e.hosts)
}

// --- creating a session somewhere else -------------------------------------

// creating reports the one state in which enter CREATES rather than switches:
// text typed, nothing matched. Only then does a target matter, and only then
// are ←→ free -- there is no selection for them to move.
func (m Model) creating() bool {
	if _, ok := m.selected(); ok {
		return false
	}
	return strings.TrimSpace(m.list.FilterInput.Value()) != ""
}

// createTargets is local first, then every watched host. Local leads because
// it is the overwhelmingly common answer and the default must cost nothing.
func (m Model) createTargets() []string {
	return append([]string{""}, m.cfg.Hosts...)
}

func (m *Model) moveCreateTarget(delta int) {
	n := len(m.createTargets())
	m.createTarget = min(max(m.effectiveTarget()+delta, 0), n-1)
	m.createTargetFor = strings.TrimSpace(m.list.FilterInput.Value())
}

// effectiveTarget is the chosen index, or local if the name has changed since
// it was chosen -- the choice belongs to a name, not to the session.
func (m Model) effectiveTarget() int {
	if strings.TrimSpace(m.list.FilterInput.Value()) != m.createTargetFor {
		return 0
	}
	return m.createTarget
}

// createHost is the chosen machine, "" for local.
func (m Model) createHost() string {
	t := m.createTargets()
	i := m.effectiveTarget()
	if i <= 0 || i >= len(t) {
		return ""
	}
	return t[i]
}

// renderCreateBar replaces the help line while a name is being typed that
// matches nothing. It is only drawn when there is a choice to make: with no
// watched hosts there is nothing to pick, so the line stays as it was.
func (m Model) renderCreateBar() string {
	targets := m.createTargets()
	if len(targets) < 2 {
		return ""
	}
	name := strings.TrimSpace(m.list.FilterInput.Value())

	var chips []string
	for i, host := range targets {
		label := host
		if host == "" {
			label = "local"
		} else {
			label = hostMark(m.watcher.Snapshot([]string{host})[0]) + " " + host
		}
		if i == m.effectiveTarget() {
			chips = append(chips, highlightCell(m.styles.selectedBG, m.styles.Header.Render(label)))
		} else {
			chips = append(chips, m.styles.Muted.Render(label))
		}
	}
	return m.styles.Muted.Render("create "+quoteName(name)+" on ") +
		strings.Join(chips, "  ") +
		m.styles.Muted.Render("   ←→ machine")
}

func quoteName(s string) string { return `"` + s + `"` }
