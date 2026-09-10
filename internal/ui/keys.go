package ui

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/lipgloss"
)

// keyMap is the control legend: the bindings handleKey matches, plus the ones
// bubbles/list implements but whose own help text would be wrong here. One
// type drives both dispatch and the legend, so they cannot drift apart.
type keyMap struct {
	Move    key.Binding
	Filter  key.Binding
	Enter   key.Binding
	Rename  key.Binding
	Kill    key.Binding
	Columns key.Binding // opens the settings editor (coledit.go)
	Quit    key.Binding
}

var appKeys = keyMap{
	Move: key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑↓", "move")),
	// Filter's key list is a placeholder, never matched: any printable rune
	// arms the filter (see handleKey). It is non-empty because bubbles/help
	// silently drops a binding with no keys from the legend.
	Filter:  key.NewBinding(key.WithKeys("a-z"), key.WithHelp("a-z", "filter")),
	Enter:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "switch/create")),
	Rename:  key.NewBinding(key.WithKeys("ctrl+r"), key.WithHelp("^r", "rename")),
	Kill:    key.NewBinding(key.WithKeys("ctrl+x"), key.WithHelp("^x", "kill")),
	Columns: key.NewBinding(key.WithKeys("ctrl+e"), key.WithHelp("^e", "columns")),
	Quit:    key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "quit")),
}

// ShortHelp satisfies help.KeyMap: the one line the footer renders.
func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Move, k.Filter, k.Enter, k.Quit, k.Rename, k.Kill, k.Columns}
}

// FullHelp satisfies help.KeyMap; the footer is a single line and never
// shows it.
func (k keyMap) FullHelp() [][]key.Binding { return [][]key.Binding{k.ShortHelp()} }

// newHelp builds the help renderer themed off the palette instead of charm's
// default grey.
func newHelp(p Palette) help.Model {
	h := help.New()
	muted := lipgloss.NewStyle().Foreground(p.Foreground).Faint(true)
	h.Styles.ShortKey = lipgloss.NewStyle().Foreground(p.Accent)
	h.Styles.ShortDesc = muted
	h.Styles.ShortSeparator = muted
	h.Styles.Ellipsis = muted
	return h
}
