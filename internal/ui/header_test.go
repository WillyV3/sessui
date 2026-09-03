package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/willyv3/sessui/internal/session"
)

// attentionFixture builds a session list with the given attention counts --
// the widget derives its pills from sessions, so tests speak in sessions.
func attentionFixture(total, needsYou, mail int) []session.Session {
	sessions := make([]session.Session, total)
	for i := range sessions {
		sessions[i].Name = "s" + string(rune('a'+i))
		if i < needsYou {
			sessions[i].State = session.StateNotify
		}
		if i < mail {
			sessions[i].OwedMail = true
		}
	}
	return sessions
}

func defaultWidgetsForTest(t *testing.T) []namedWidget {
	t.Helper()
	ws, err := resolveWidgets(defaultHeaderWidgets())
	if err != nil {
		t.Fatalf("resolveWidgets(defaults): %v", err)
	}
	return ws
}

func TestRenderHeaderLine_Default(t *testing.T) {
	styles := testStyles()
	widgets := defaultWidgetsForTest(t)

	cases := []struct {
		name       string
		st         widgetState
		wantExact  string   // set when the line has no pills: exact match
		wantSubstr []string // set when it has pills: must all appear
		noSubstr   []string // must NOT appear
	}{
		{
			name:      "zero counts render no pill text",
			st:        widgetState{sessions: attentionFixture(14, 0, 0), total: 14, shown: 14},
			wantExact: "14 sessions",
			noSubstr:  []string{glyphMail, glyphU(glyphNeedsYou)},
		},
		{
			name:       "both pills present",
			st:         widgetState{sessions: attentionFixture(14, 2, 1), total: 14, shown: 14},
			wantSubstr: []string{"14 sessions", glyphU(glyphNeedsYou) + " 2", glyphMail + " 1"},
		},
		{
			name:       "only needs-you pending",
			st:         widgetState{sessions: attentionFixture(5, 1, 0), total: 5, shown: 5},
			wantSubstr: []string{"5 sessions", glyphU(glyphNeedsYou) + " 1"},
			noSubstr:   []string{glyphMail},
		},
		{
			name:       "only mail pending",
			st:         widgetState{sessions: attentionFixture(5, 0, 3), total: 5, shown: 5},
			wantSubstr: []string{"5 sessions", glyphMail + " 3"},
			noSubstr:   []string{glyphU(glyphNeedsYou)},
		},
		{
			name:      "filtering shows N of M",
			st:        widgetState{sessions: attentionFixture(14, 0, 0), total: 14, shown: 3, filtering: true},
			wantExact: "3 of 14 sessions",
		},
		{
			name:      "filtering with the full set shown collapses back to the plain tally",
			st:        widgetState{sessions: attentionFixture(14, 0, 0), total: 14, shown: 14, filtering: true},
			wantExact: "14 sessions",
		},
		{
			name:      "singular session",
			st:        widgetState{sessions: attentionFixture(1, 0, 0), total: 1, shown: 1},
			wantExact: "1 session",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.st.styles = styles
			got := renderHeaderLine(c.st, defaultUsableWidth, widgets, -1)

			if c.wantExact != "" && got != c.wantExact {
				t.Errorf("renderHeaderLine() = %q, want exactly %q", got, c.wantExact)
			}
			for _, want := range c.wantSubstr {
				if !strings.Contains(got, want) {
					t.Errorf("renderHeaderLine() = %q, want substring %q", got, want)
				}
			}
			for _, bad := range c.noSubstr {
				if strings.Contains(got, bad) {
					t.Errorf("renderHeaderLine() = %q, must not contain %q", got, bad)
				}
			}
		})
	}
}

// TestRenderHeaderLine_DefaultIsByteIdenticalToLegacy pins the promise made
// when widgets were introduced: with the shipped config (now-playing silent,
// nothing focused) the count line is exactly what it was before -- the
// tally, a run of spaces, the pills flush right at width.
func TestRenderHeaderLine_DefaultIsByteIdenticalToLegacy(t *testing.T) {
	styles := testStyles()
	st := widgetState{sessions: attentionFixture(14, 2, 1), total: 14, shown: 14, styles: styles}

	left := countText(styles, 14, 14, false)
	right := attentionPills(styles, 2, 1)
	legacy := left + strings.Repeat(" ", defaultUsableWidth-lipgloss.Width(left)-lipgloss.Width(right)) + right

	got := renderHeaderLine(st, defaultUsableWidth, defaultWidgetsForTest(t), -1)
	if got != legacy {
		t.Errorf("default header drifted from legacy:\n got %q\nwant %q", got, legacy)
	}
	if w := lipgloss.Width(got); w != defaultUsableWidth {
		t.Errorf("lipgloss.Width() = %d, want exactly %d", w, defaultUsableWidth)
	}
}

// TestRenderHeaderLine_OneLineAtEveryFocus is the invariant the whole design
// rests on: whatever is configured, whichever widget is expanded, at any
// width, the header is ONE line no wider than the row. Expansion is
// horizontal; the table below never moves.
func TestRenderHeaderLine_OneLineAtEveryFocus(t *testing.T) {
	widgets, err := resolveWidgets([]widgetSetting{
		{Name: "attention"}, {Name: "host"}, {Name: "agents"},
		{Name: "shell", Args: map[string]string{"cmd": "true", "icon": "$"}},
	})
	if err != nil {
		t.Fatalf("resolveWidgets: %v", err)
	}
	// Give the shell widget something long to expand so truncation is exercised.
	widgets[3].widget.(*shellWidget).absorb(widgetPollMsg{output: strings.Repeat("x", 300)})
	widgets = append(widgets, namedWidget{name: "rude", widget: rudeWidget{}})

	sessions := attentionFixture(14, 2, 1)
	sessions[3].State = session.StateWorking
	sessions[4].State = session.StateIdle
	st := widgetState{sessions: sessions, total: 14, shown: 14, styles: testStyles()}

	for _, width := range []int{defaultUsableWidth, 130, 60} {
		for focus := -1; focus < len(widgets); focus++ {
			got := renderHeaderLine(st, width, widgets, focus)
			if strings.Contains(got, "\n") {
				t.Errorf("width %d focus %d: header grew a second line: %q", width, focus, got)
			}
			if w := lipgloss.Width(got); w > width {
				t.Errorf("width %d focus %d: lipgloss.Width() = %d, exceeds the row", width, focus, w)
			}
			if focus >= 0 {
				if w := lipgloss.Width(got); w != width {
					t.Errorf("width %d focus %d: expanded header measures %d, want flush at %d", width, focus, w, width)
				}
			}
		}
	}
}

// TestRenderHeaderLine_FocusedWidgetExpands: focus swaps the widget's icon
// for its expanded form; the others stay collapsed beside it.
func TestRenderHeaderLine_FocusedWidgetExpands(t *testing.T) {
	widgets, err := resolveWidgets([]widgetSetting{{Name: "attention"}, {Name: "host"}})
	if err != nil {
		t.Fatalf("resolveWidgets: %v", err)
	}
	st := widgetState{sessions: attentionFixture(3, 1, 0), total: 3, shown: 3, styles: testStyles()}

	collapsed := renderHeaderLine(st, defaultUsableWidth, widgets, -1)
	if !strings.Contains(collapsed, glyphU(glyphNeedsYou)+" 1") || strings.Contains(collapsed, "sa") {
		t.Errorf("collapsed: want the pill, not the session name: %q", collapsed)
	}

	expanded := renderHeaderLine(st, defaultUsableWidth, widgets, 0)
	if !strings.Contains(expanded, "sa") {
		t.Errorf("focus 0: want the attention widget to name session sa: %q", expanded)
	}
	if !strings.Contains(expanded, hostname()) {
		t.Errorf("focus 0: the unfocused host widget must still show its icon: %q", expanded)
	}
}

// rudeWidget ignores the width it is given -- the third-party widget the
// framework must contain rather than trust.
type rudeWidget struct{}

func (rudeWidget) icon(widgetState) string        { return "!" }
func (rudeWidget) expand(widgetState, int) string { return strings.Repeat("x", 300) }
