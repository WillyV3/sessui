package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestRenderCountLine(t *testing.T) {
	styles := testStyles()

	cases := []struct {
		name       string
		data       countLineData
		wantExact  string   // set when the line has no pills: exact match
		wantSubstr []string // set when it has pills: must all appear
		noSubstr   []string // must NOT appear
	}{
		{
			name:      "zero counts render no pill text",
			data:      countLineData{Total: 14, NeedsYou: 0, Mail: 0},
			wantExact: "14 sessions",
			noSubstr:  []string{glyphMail, glyphU(glyphNeedsYou)},
		},
		{
			name:       "both pills present",
			data:       countLineData{Total: 14, NeedsYou: 2, Mail: 1},
			wantSubstr: []string{"14 sessions", glyphU(glyphNeedsYou) + " 2", glyphMail + " 1"},
		},
		{
			name:       "only needs-you pending",
			data:       countLineData{Total: 5, NeedsYou: 1, Mail: 0},
			wantSubstr: []string{"5 sessions", glyphU(glyphNeedsYou) + " 1"},
			noSubstr:   []string{glyphMail},
		},
		{
			name:       "only mail pending",
			data:       countLineData{Total: 5, NeedsYou: 0, Mail: 3},
			wantSubstr: []string{"5 sessions", glyphMail + " 3"},
			noSubstr:   []string{glyphU(glyphNeedsYou)},
		},
		{
			name:      "filtering shows N of M",
			data:      countLineData{Total: 14, Shown: 3, Filtering: true},
			wantExact: "3 of 14 sessions",
		},
		{
			name:      "filtering with the full set shown collapses back to the plain tally",
			data:      countLineData{Total: 14, Shown: 14, Filtering: true},
			wantExact: "14 sessions",
		},
		{
			name:      "singular session",
			data:      countLineData{Total: 1, Shown: 1},
			wantExact: "1 session",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := renderCountLine(styles, defaultUsableWidth, c.data)

			if c.wantExact != "" && got != c.wantExact {
				t.Errorf("renderCountLine() = %q, want exactly %q", got, c.wantExact)
			}
			for _, want := range c.wantSubstr {
				if !strings.Contains(got, want) {
					t.Errorf("renderCountLine() = %q, want substring %q", got, want)
				}
			}
			for _, bad := range c.noSubstr {
				if strings.Contains(got, bad) {
					t.Errorf("renderCountLine() = %q, must not contain %q", got, bad)
				}
			}
		})
	}
}

// TestRenderCountLine_Width pins the width invariant the header spec calls
// out explicitly: with pills present at width 108, the rendered line must
// measure exactly 108 -- the pills are flush to the row's right edge, not
// merely somewhere on it.
func TestRenderCountLine_Width(t *testing.T) {
	styles := testStyles()
	got := renderCountLine(styles, defaultUsableWidth, countLineData{Total: 14, NeedsYou: 2, Mail: 1})
	if w := lipgloss.Width(got); w != defaultUsableWidth {
		t.Errorf("lipgloss.Width(renderCountLine()) = %d, want exactly %d", w, defaultUsableWidth)
	}
}
