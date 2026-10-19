package session

import (
	"strconv"
	"strings"
	"testing"
)

// screen is one pane's worth of captured lines: "<id>-1" .. "<id>-<height>".
func screen(id string, height int) string {
	lines := make([]string, height)
	for i := range lines {
		lines[i] = id + "-" + strconv.Itoa(i+1)
	}
	return strings.Join(lines, "\n")
}

func TestSplitCaptures(t *testing.T) {
	refs := []agentPaneRef{{ID: "%1", Height: 5}, {ID: "%2", Height: 3}, {ID: "%3", Height: 4}}
	out := screen("a", 5) + "\n" + screen("b", 3) + "\n" + screen("c", 4) + "\n"

	captures, ok := splitCaptures(out, refs, 2)
	if !ok {
		t.Fatal("split rejected a well-formed batch")
	}
	// Each pane gets its OWN tail, never its neighbour's lines.
	for id, want := range map[string]string{
		"%1": "a-4\na-5",
		"%2": "b-2\nb-3",
		"%3": "c-3\nc-4",
	} {
		if got := captures[id]; got != want {
			t.Errorf("%s: got %q, want %q", id, got, want)
		}
	}
}

func TestSplitCapturesKeepsBlankLines(t *testing.T) {
	// A pane whose screen is mostly empty still accounts for its full height;
	// dropping its blanks would shift every later pane's content.
	refs := []agentPaneRef{{ID: "%1", Height: 4}, {ID: "%2", Height: 2}}
	out := "top\n\n\n\n" + "x\ny\n"

	captures, ok := splitCaptures(out, refs, 20)
	if !ok {
		t.Fatal("split rejected blank lines")
	}
	if got := captures["%1"]; got != "top\n\n\n" {
		t.Errorf("%%1: got %q", got)
	}
	if got := captures["%2"]; got != "x\ny" {
		t.Errorf("%%2: got %q, want %q", got, "x\ny")
	}
}

func TestSplitCapturesRejectsMismatch(t *testing.T) {
	refs := []agentPaneRef{{ID: "%1", Height: 5}, {ID: "%2", Height: 5}}
	tests := map[string]string{
		"short: a pane closed mid-call": screen("a", 5) + "\n" + screen("b", 2) + "\n",
		"long: unexpected padding":      screen("a", 5) + "\n" + screen("b", 7) + "\n",
		"empty":                         "",
	}
	for name, out := range tests {
		t.Run(name, func(t *testing.T) {
			// Rejection is what sends the caller to a per-pane read. Returning a
			// misaligned map would attribute one session's state to another.
			if _, ok := splitCaptures(out, refs, 20); ok {
				t.Error("accepted a batch that did not add up")
			}
		})
	}
}

func TestSplitCapturesTailShorterThanLimit(t *testing.T) {
	refs := []agentPaneRef{{ID: "%1", Height: 2}}
	captures, ok := splitCaptures("one\ntwo\n", refs, 20)
	if !ok {
		t.Fatal("split rejected a short pane")
	}
	if got := captures["%1"]; got != "one\ntwo" {
		t.Errorf("got %q, want %q", got, "one\ntwo")
	}
}
