package ui

// headerFocus is the overlay that puts the keyboard on the header's widget
// section: ^w opens it with the first widget expanded, tab / shift+tab move
// focus (the focused widget IS the expanded one -- focus and expansion are
// one state, not two), esc collapses everything and returns to the list.
// While a controllable widget is focused, every other key is its own.
//
// It rides the same overlay slot as rename, kill and the column editor, so
// Model gained no new mode for it -- only countLine asks whether a
// headerFocus is up to know which widget to expand.

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

type headerFocus struct {
	widgets  []namedWidget
	help     help.Model
	index    int
	finished bool
}

var headerFocusKeys = struct{ next, prev, close key.Binding }{
	next:  key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next")),
	prev:  key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("⇧tab", "prev")),
	close: key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
}

func newHeaderFocus(widgets []namedWidget, h help.Model) *headerFocus {
	return &headerFocus{widgets: widgets, help: h}
}

func (h *headerFocus) Init() tea.Cmd { return nil }

func (h *headerFocus) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return h, nil
	}
	switch {
	case key.Matches(km, headerFocusKeys.close):
		h.finished = true
	case key.Matches(km, headerFocusKeys.next):
		h.index = (h.index + 1) % len(h.widgets)
	case key.Matches(km, headerFocusKeys.prev):
		h.index = (h.index + len(h.widgets) - 1) % len(h.widgets)
	default:
		if c, ok := h.focused().widget.(controllable); ok {
			return h, c.handle(km)
		}
	}
	return h, nil
}

// View is the footer while the header has focus: the header itself renders
// the focused widget (see countLine), so all this overlay draws is its
// legend, in the footer slot every overlay owns.
func (h *headerFocus) View() string { return h.help.View(h) }

func (h *headerFocus) result() (finished bool, apply tea.Cmd) {
	return h.finished, nil
}

func (h *headerFocus) focused() namedWidget { return h.widgets[h.index] }

// ShortHelp is the footer legend while the header has focus: the three
// focus keys, then whatever the focused widget offers.
func (h *headerFocus) ShortHelp() []key.Binding {
	bindings := []key.Binding{headerFocusKeys.next, headerFocusKeys.prev, headerFocusKeys.close}
	if c, ok := h.focused().widget.(controllable); ok {
		bindings = append(bindings, c.keys()...)
	}
	return bindings
}

func (h *headerFocus) FullHelp() [][]key.Binding { return [][]key.Binding{h.ShortHelp()} }
