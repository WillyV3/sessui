package ui

import (
	"errors"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

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

// TestHandleKey_RenameOpensAndEscClosesOverlay pins Model's wiring to the
// overlay slot: ctrl+r on a selected row opens one (Model.overlay != nil,
// replacing the old mode enum), and esc clears it -- with no action taken,
// since the injected rename here would fail the test if it ran.
func TestHandleKey_RenameOpensAndEscClosesOverlay(t *testing.T) {
	m := New()
	m.width, m.height = 80, 24
	m.list.SetItems(toItems([]session.Session{{Name: "alpha"}}))
	m.list.Select(0)

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	m = next.(Model)
	if m.overlay == nil {
		t.Fatalf("ctrl+r did not open an overlay")
	}
	if cmd == nil {
		t.Fatalf("ctrl+r returned a nil cmd, want the overlay's Init()")
	}

	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)
	if m.overlay != nil {
		t.Fatalf("esc did not clear the overlay")
	}
}

// drainCmd feeds cmd's result back into ov.Update, unwrapping tea.Batch
// results, until no command remains -- replicating just enough of what a
// real tea.Program's event loop does automatically, since huh submits a
// group by chaining commands (field -> nextField -> nextGroup -> complete)
// rather than returning a finished state synchronously. It is deliberately
// NOT used for ordinary typing: bubbles/textinput also returns a real
// cursor-blink command on every position-changing keystroke (see
// bubbles/textinput's Update), which blocks for a real clock tick if
// executed synchronously here -- draining that would make every typed
// character pause the test. Typing only needs the single Update call (the
// keystroke is fully applied within it); submit() below is the one place
// commands must be chased.
func drainCmd(t *testing.T, ov tea.Model, cmd tea.Cmd) tea.Model {
	t.Helper()
	for cmd != nil {
		msg := cmd()
		if msg == nil {
			return ov
		}
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, c := range batch {
				ov = drainCmd(t, ov, c)
			}
			return ov
		}
		ov, cmd = ov.Update(msg)
	}
	return ov
}

// typeKey sends one keystroke to the overlay. The keystroke is fully
// applied within this single call (see drainCmd's doc comment for why its
// returned command -- a cursor-blink reset -- is discarded, not chased).
func typeKey(ov tea.Model, msg tea.KeyMsg) tea.Model {
	next, _ := ov.Update(msg)
	return next
}

// submit sends the key that completes a single-field form (enter for
// rename's Input, the given key for kill's Confirm) and drains the
// resulting field->nextField->nextGroup command chain so the overlay ends
// the call already reporting its final state.
func submit(t *testing.T, ov tea.Model, msg tea.KeyMsg) tea.Model {
	t.Helper()
	next, cmd := ov.Update(msg)
	return drainCmd(t, next, cmd)
}

// TestRenameOverlay_CompletesAndInvokesAction drives the huh form the same
// way a real keyboard would: clear the prefilled name, type a new one,
// press enter. It asserts the overlay reports finished with a non-nil
// apply command, and that running that command calls the injected rename
// action with the right (old, new) names -- never session.Rename itself,
// so this never shells out to tmux. The fake returns an error, which makes
// doAndReload return before it would call the real session.List() reload.
func TestRenameOverlay_CompletesAndInvokesAction(t *testing.T) {
	var gotOld, gotNew string
	calls := 0
	fakeErr := errors.New("fake rename")
	rename := func(old, newName string) error {
		calls++
		gotOld, gotNew = old, newName
		return fakeErr
	}

	ov := renameOverlay(newHuhTheme(loadPalette(paletteAuto)), 60, "alpha", rename)
	ov.Init() // huh focuses the field synchronously inside Init; the returned
	// cmd only carries a window-size query and dynamic-title refresh, both
	// irrelevant here (see drainCmd, which is for the submit chain below).

	for range "alpha" {
		ov = typeKey(ov, tea.KeyMsg{Type: tea.KeyBackspace})
	}
	for _, r := range "bravo" {
		ov = typeKey(ov, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	ov = submit(t, ov, tea.KeyMsg{Type: tea.KeyEnter})

	result, ok := ov.(overlayResult)
	if !ok {
		t.Fatalf("overlay %T does not implement overlayResult", ov)
	}
	finished, apply := result.result()
	if !finished {
		t.Fatalf("result() finished = false after enter, want true")
	}
	if apply == nil {
		t.Fatalf("result() apply = nil, want the rename+reload command")
	}

	apply() // runs doAndReload's func; fakeErr short-circuits before session.List()

	if calls != 1 {
		t.Fatalf("rename called %d times, want 1", calls)
	}
	if gotOld != "alpha" || gotNew != "bravo" {
		t.Fatalf("rename(%q, %q), want (\"alpha\", \"bravo\")", gotOld, gotNew)
	}
}

// TestRenameOverlay_EscAbortsWithNoAction pins the completion contract's
// other branch: esc must finish the overlay without ever invoking the
// action.
func TestRenameOverlay_EscAbortsWithNoAction(t *testing.T) {
	calls := 0
	rename := func(string, string) error { calls++; return nil }

	ov := renameOverlay(newHuhTheme(loadPalette(paletteAuto)), 60, "alpha", rename)
	ov.Init() // huh focuses the field synchronously inside Init; the returned
	// cmd only carries a window-size query and dynamic-title refresh, both
	// irrelevant here (see drainCmd, which is for the submit chain below).
	ov = typeKey(ov, tea.KeyMsg{Type: tea.KeyEsc})

	result, ok := ov.(overlayResult)
	if !ok {
		t.Fatalf("overlay %T does not implement overlayResult", ov)
	}
	finished, apply := result.result()
	if !finished {
		t.Fatalf("result() finished = false after esc, want true")
	}
	if apply != nil {
		t.Fatalf("result() apply != nil after esc, want nil (no side effects)")
	}
	if calls != 0 {
		t.Fatalf("rename called %d times after esc, want 0", calls)
	}
}

// TestKillOverlay_ConfirmInvokesAction mirrors the rename case for the
// confirm form: pressing "y" must complete the overlay and invoke the
// injected kill action.
func TestKillOverlay_ConfirmInvokesAction(t *testing.T) {
	var got string
	calls := 0
	fakeErr := errors.New("fake kill")
	kill := func(name string) error {
		calls++
		got = name
		return fakeErr
	}

	ov := killOverlay(newHuhTheme(loadPalette(paletteAuto)), 60, "alpha", kill)
	ov.Init() // huh focuses the field synchronously inside Init; the returned
	// cmd only carries a window-size query and dynamic-title refresh, both
	// irrelevant here (see drainCmd, which is for the submit chain below).
	ov = submit(t, ov, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})

	result, ok := ov.(overlayResult)
	if !ok {
		t.Fatalf("overlay %T does not implement overlayResult", ov)
	}
	finished, apply := result.result()
	if !finished || apply == nil {
		t.Fatalf("result() = (%v, %v) after y, want (true, non-nil)", finished, apply)
	}

	apply()

	if calls != 1 || got != "alpha" {
		t.Fatalf("kill called %d times with %q, want 1 call with \"alpha\"", calls, got)
	}
}
