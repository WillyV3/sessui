package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// recorderWidget is a controllable widget that remembers the keys it was
// handed, so a test can prove the overlay routes them.
type recorderWidget struct {
	got []string
}

func (recorderWidget) icon(widgetState) string        { return "r" }
func (recorderWidget) expand(widgetState, int) string { return "recorder" }
func (r *recorderWidget) handle(k tea.KeyMsg) tea.Cmd { r.got = append(r.got, k.String()); return nil }
func (recorderWidget) keys() []key.Binding {
	return []key.Binding{key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "play/pause"))}
}

func pressFocus(h *headerFocus, k tea.KeyMsg) *headerFocus {
	m, _ := h.Update(k)
	return m.(*headerFocus)
}

func TestHeaderFocus_TabCyclesAndWraps(t *testing.T) {
	h := newHeaderFocus([]namedWidget{
		{name: "a", widget: hostWidget{name: "h"}}, {name: "b", widget: hostWidget{name: "h"}}, {name: "c", widget: hostWidget{name: "h"}},
	}, testHelp())

	tab := tea.KeyMsg{Type: tea.KeyTab}
	shiftTab := tea.KeyMsg{Type: tea.KeyShiftTab}

	for i, want := range []int{1, 2, 0} {
		if h = pressFocus(h, tab); h.index != want {
			t.Fatalf("tab #%d: index = %d, want %d", i+1, h.index, want)
		}
	}
	if h = pressFocus(h, shiftTab); h.index != 2 {
		t.Errorf("shift+tab from 0: index = %d, want 2 (wrap)", h.index)
	}
}

func TestHeaderFocus_EscFinishesWithNothingToApply(t *testing.T) {
	h := newHeaderFocus([]namedWidget{{name: "a", widget: hostWidget{name: "h"}}}, testHelp())
	if done, _ := h.result(); done {
		t.Fatal("finished before any key")
	}
	h = pressFocus(h, tea.KeyMsg{Type: tea.KeyEsc})
	done, apply := h.result()
	if !done || apply != nil {
		t.Errorf("after esc: finished=%v apply=%v, want true,nil", done, apply)
	}
}

func TestHeaderFocus_RoutesKeysToTheFocusedControllable(t *testing.T) {
	rec := &recorderWidget{}
	h := newHeaderFocus([]namedWidget{
		{name: "host", widget: hostWidget{name: "h"}}, {name: "rec", widget: rec},
	}, testHelp())

	space := tea.KeyMsg{Type: tea.KeySpace}
	h = pressFocus(h, space) // host is focused and not controllable: dropped
	if len(rec.got) != 0 {
		t.Fatalf("key reached an unfocused widget: %v", rec.got)
	}
	h = pressFocus(h, tea.KeyMsg{Type: tea.KeyTab})
	h = pressFocus(h, space)
	if len(rec.got) != 1 {
		t.Errorf("focused controllable got %v, want one key", rec.got)
	}

	// The legend carries the widget's keys only while it is focused.
	if n := len(h.ShortHelp()); n != 4 {
		t.Errorf("ShortHelp with recorder focused has %d bindings, want 3 focus keys + 1", n)
	}
	h = pressFocus(h, tea.KeyMsg{Type: tea.KeyTab})
	if n := len(h.ShortHelp()); n != 3 {
		t.Errorf("ShortHelp with host focused has %d bindings, want just the 3 focus keys", n)
	}
}

func testHelp() help.Model { return help.New() }

// TestHeaderFocus_LegendFitsTheFooter: the widest shipped legend -- the
// transport plus the three focus keys -- fits the default row untruncated.
func TestHeaderFocus_LegendFitsTheFooter(t *testing.T) {
	np := &nowPlayingWidget{client: &fakeMedia{}, available: true}
	h := newHeaderFocus([]namedWidget{{name: "now-playing", widget: np}}, testHelp())
	legend := h.View()
	if w := lipgloss.Width(legend); w > defaultUsableWidth {
		t.Errorf("legend is %d wide, exceeds %d: %q", w, defaultUsableWidth, legend)
	}
	if strings.Contains(legend, "…") {
		t.Errorf("legend truncated: %q", legend)
	}
}
