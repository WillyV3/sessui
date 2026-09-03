package ui

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/lipgloss"
)

// keyMap is sessui's complete control legend -- the bindings handleKey
// matches against (Enter/Rename/Kill), plus the ones bubbles/list already
// implements but whose own help text would be wrong here (Move stays
// enabled while filtering, Filter has no literal key -- see handleKey and
// GOTCHAS #5, "no vim keys"). One type is the single source of truth for
// both dispatch and the legend, so they can't drift apart.
type keyMap struct {
	Move    key.Binding
	Filter  key.Binding
	Enter   key.Binding
	Rename  key.Binding
	Kill    key.Binding
	Columns key.Binding // opens the column editor (coledit.go)
	Widgets key.Binding // focuses the header widgets (headerfocus.go)
	Quit    key.Binding
}

// appKeys is the app's one keymap instance -- handleKey matches Enter,
// Rename and Kill against it directly; helpLine renders all of it via
// bubbles/help.
var appKeys = keyMap{
	Move: key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑↓", "move")),
	// Keys carries a placeholder, not a real trigger (never matched against --
	// any printable rune arms the filter, see handleKey): key.Binding.Enabled
	// requires a non-nil key list, so an empty WithKeys would make bubbles/help
	// silently drop this from the legend.
	Filter:  key.NewBinding(key.WithKeys("a-z"), key.WithHelp("a-z", "filter")),
	Enter:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "switch/create")),
	Rename:  key.NewBinding(key.WithKeys("ctrl+r"), key.WithHelp("^r", "rename")),
	Kill:    key.NewBinding(key.WithKeys("ctrl+x"), key.WithHelp("^x", "kill")),
	Columns: key.NewBinding(key.WithKeys("ctrl+e"), key.WithHelp("^e", "columns")),
	Widgets: key.NewBinding(key.WithKeys("ctrl+w"), key.WithHelp("^w", "widgets")),
	Quit:    key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "quit")),
}

// ShortHelp satisfies bubbles/help's help.KeyMap: the one line footerLine
// renders when no overlay or error is showing.
func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Move, k.Filter, k.Enter, k.Quit, k.Rename, k.Kill, k.Columns, k.Widgets}
}

// FullHelp satisfies help.KeyMap. sessui's footer is a single line -- it
// never shows the multi-column full help -- but the interface requires it.
func (k keyMap) FullHelp() [][]key.Binding { return [][]key.Binding{k.ShortHelp()} }

// newHelp builds the bubbles/help renderer helpLine uses, themed off the
// app's palette instead of charm's default grey (see style.go's Palette).
func newHelp(p Palette) help.Model {
	h := help.New()
	muted := lipgloss.NewStyle().Foreground(p.Foreground).Faint(true)
	h.Styles.ShortKey = lipgloss.NewStyle().Foreground(p.Accent)
	h.Styles.ShortDesc = muted
	h.Styles.ShortSeparator = muted
	h.Styles.Ellipsis = muted
	return h
}
