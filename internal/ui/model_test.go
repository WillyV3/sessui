package ui

import (
	"testing"
	"time"

	"github.com/willyv3/sessui/internal/session"
)

// TestTrimmedFilter pins the real bug: typing "jim" then a stray trailing
// space used to match nothing (bubbles/list's DefaultFilter passes the raw
// term straight to sahilm/fuzzy), which then let Enter create a new
// session literally named "jim ". trimmedFilter must still match "jim".
func TestTrimmedFilter(t *testing.T) {
	targets := []string{"Jim", "doorboard", "sontara-sales"}

	ranks := trimmedFilter("jim ", targets)
	if len(ranks) != 1 {
		t.Fatalf(`trimmedFilter("jim ", ...) matched %d targets, want 1: %+v`, len(ranks), ranks)
	}
	if got := targets[ranks[0].Index]; got != "Jim" {
		t.Errorf(`trimmedFilter("jim ", ...) matched %q, want %q`, got, "Jim")
	}

	if ranks := trimmedFilter("  jim", targets); len(ranks) != 1 {
		t.Errorf(`trimmedFilter("  jim", ...) matched %d targets, want 1 (leading space too)`, len(ranks))
	}

	if ranks := trimmedFilter("jim", targets); len(ranks) != 1 {
		t.Errorf(`trimmedFilter("jim", ...) matched %d targets, want 1 (unchanged behavior with no stray space)`, len(ranks))
	}
}

// TestMarquee_ResetsOnSelectionChange drives the real Update path: the scroll
// frame advances while the selection holds, but snaps back to 0 the moment a
// different row is selected -- so a long name always reveals its head first
// instead of dropping the user into the middle of a scroll. This exercises the
// behaviour the UI actually establishes (a global counter never being 0 at
// selection time was the bug the advisor caught).
func TestMarquee_ResetsOnSelectionChange(t *testing.T) {
	m := New()
	m.list.SetItems(toItems([]session.Session{{Name: "alpha"}, {Name: "bravo"}}))
	m.list.Select(0)
	m.marqueeFrame, m.marqueeFor = 42, "alpha" // pretend we've been scrolling alpha

	// A tick with the selection unchanged advances the frame.
	next, _ := m.Update(marqueeTickMsg(time.Now()))
	m = next.(Model)
	if m.marqueeFrame != 43 {
		t.Fatalf("same selection: marqueeFrame = %d, want 43 (advance)", m.marqueeFrame)
	}

	// Move to a different row; the next tick resets the frame to that row's start.
	m.list.Select(1)
	next, _ = m.Update(marqueeTickMsg(time.Now()))
	m = next.(Model)
	if m.marqueeFrame != 0 || m.marqueeFor != "bravo" {
		t.Fatalf("selection changed: frame=%d for=%q, want frame=0 for=\"bravo\"", m.marqueeFrame, m.marqueeFor)
	}
}
