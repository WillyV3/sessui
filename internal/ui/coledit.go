package ui

// The column editor: the header row IS the editing surface. There is no
// separate settings screen with checkboxes and a form -- you arrow across
// the same header the table already draws, and the exact table the app
// already renders (renderHeader/renderRow, handed in as `preview`) redraws
// live underneath every edit. Nothing here is a mockup of the table; it is
// the table, mid-edit.
//
// Five verbs, same meaning in every state:
//   ←/→   move the selection, or (armed) swap the selection with a neighbour
//   enter arm the selection for moving / drop it back down
//   space hide or show the selected column
//   +/-   resize the selected column (no-op on the flex column, status)
//   esc   apply and close -- or, while armed, just disarm
// ctrl+r is the one non-verb: reset to the shipped defaults, "the way back."
//
// Arm-to-move (rather than, say, a drag or a persistent "reorder mode") was
// chosen because left/right already means "move the cursor" -- arming is
// the one bit of state needed to make the SAME two keys also mean "move the
// column," without stealing up/down for a job they don't need (see
// GOTCHAS #5: this app doesn't do second meanings on an axis lightly).
//
// There is no cancel gesture on purpose: this editor is opened ~120x/hour by
// a tool built to be fast, and a confirm step would be friction on every
// single open. ctrl+r is the undo for a bad session.

import (
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// glyphArmed marks the column currently armed for moving -- distinct from
// every column's own glyph, so "this one is grabbed" reads without first
// learning a colour. Verified present in JetBrainsMono Nerd Font:
// otfinfo -u .../JetBrainsMonoNerdFont-Regular.ttf | grep -i 25C6 -> uni25C6.
const glyphArmed = 0x25C6 // ◆

// allColumnIDs is every column the catalog knows, in the order columns.go
// declares them. columnCatalog is a map (unordered), so this is the
// editor's own stable "everything" list -- the strip's starting shelf of
// catalog columns not yet in the user's settings (age, windows, attached,
// machine) comes from walking this after the user's configured ones.
var allColumnIDs = []columnID{
	colSession, colApps, colActive, colCWD, colPeer, colStatus,
	colAge, colWindows, colAttached, colMachine,
}

// editorKeys is the editor's whole vocabulary, held as one key.Binding set
// so bubbles/help renders straight off it. ShortHelp/FullHelp (below) only
// ever adjust an existing binding's label/presence for the current state
// (armed, flex column selected) -- they never introduce a sixth verb.
type editorKeys struct {
	move   key.Binding
	arm    key.Binding
	hide   key.Binding
	resize key.Binding
	close  key.Binding
	reset  key.Binding
}

func newEditorKeys() editorKeys {
	return editorKeys{
		move:   key.NewBinding(key.WithKeys("left", "right"), key.WithHelp("←/→", "select")),
		arm:    key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "arm to move")),
		hide:   key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "hide/show")),
		resize: key.NewBinding(key.WithKeys("+", "=", "-", "_"), key.WithHelp("+/-", "resize")),
		close:  key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "apply & close")),
		reset:  key.NewBinding(key.WithKeys("ctrl+r"), key.WithHelp("ctrl+r", "reset")),
	}
}

// columnEditor is the WYSIWYG column editor overlay (see the package doc
// above). It owns the keyboard while it's up and reports itself done via
// result() (the pull-based overlayResult contract in overlay.go) -- there is
// deliberately no path that discards the edit; see esc's handling below.
type columnEditor struct {
	styles   Styles
	showPeer bool
	width    int
	preview  func(tableLayout) string
	keys     editorKeys
	help     help.Model

	// order is EVERY catalog column, visible and hidden, in strip order --
	// the unit arm+swap moves. reset() seeds it as the configured columns
	// followed by whatever the catalog has left, hidden; nothing thereafter
	// keeps visible columns contiguous automatically -- if the user swaps a
	// hidden column into the middle of the visible run, that's them
	// deliberately inserting it, and the strip and preview will show
	// exactly that (see stripColumns/visibleColumns).
	order  []columnID
	hidden map[columnID]bool
	// widths is a per-column resize override, 0 = catalog default -- the
	// exact columnSetting.Width contract, so Result() is a direct read.
	widths map[columnID]int

	cursor   int  // index into order
	armed    bool // enter arms the selected column for a swap-move
	finished bool // an unarmed esc was pressed -- see result()
}

// newColumnEditor builds the editor over `current`'s configuration. preview
// is Model's real renderer (renderHeader + renderRow over live sessions) --
// see the package doc for why that's the one idea worth stealing from
// ccstatusline: the preview is production code, not a mockup.
func newColumnEditor(styles Styles, current []columnSetting, showPeer bool, width int, preview func(tableLayout) string) *columnEditor {
	hp := help.New()
	// Tint bubbles/help onto the app's own palette instead of its baked-in
	// greys -- Header and Help are already the right colours for "key" and
	// "de-emphasised description," so this is a straight reuse, not a new
	// style (see GOTCHAS #3 on not inventing text colours).
	hp.Styles.ShortKey = styles.Header
	hp.Styles.ShortDesc = styles.Help
	hp.Styles.ShortSeparator = styles.Help

	e := &columnEditor{styles: styles, showPeer: showPeer, width: width, preview: preview, keys: newEditorKeys(), help: hp}
	e.reset(current)
	return e
}

// reset rebuilds the strip from settings: the configured columns in their
// order first, then every catalog column the settings don't mention,
// appended hidden -- which is how a user discovers age/windows/attached/
// machine (see the deliverable). ctrl+r calls this with
// defaultColumnSettings() to restore the shipped table. The peer column is
// dropped outright (not even offered hidden) when showPeer is false, same
// as the live table -- toggling a column that resolveColumns would strip
// back out at render time would be a lie the editor tells the user.
func (e *columnEditor) reset(settings []columnSetting) {
	seen := make(map[columnID]bool, len(allColumnIDs))
	order := make([]columnID, 0, len(allColumnIDs))
	hidden := make(map[columnID]bool, len(allColumnIDs))
	widths := make(map[columnID]int, len(allColumnIDs))

	eligible := func(id columnID) bool {
		_, known := columnCatalog[id]
		return known && !seen[id] && (id != colPeer || e.showPeer)
	}
	for _, s := range settings {
		if !eligible(s.ID) {
			continue
		}
		seen[s.ID] = true
		order = append(order, s.ID)
		widths[s.ID] = s.Width
	}
	for _, id := range allColumnIDs {
		if !eligible(id) {
			continue
		}
		seen[id] = true
		order = append(order, id)
		hidden[id] = true
	}

	e.order, e.hidden, e.widths = order, hidden, widths
	e.cursor, e.armed = 0, false
}

// resolvedColumn is id's catalog column at its current effective width: the
// resize override if the user set one, floored at minWidth, else the
// catalog default -- exactly resolveColumns' own rule, so a column reads
// identically here and in the real table the instant Result() is saved.
func (e *columnEditor) resolvedColumn(id columnID) column {
	c := columnCatalog[id]
	if w := e.widths[id]; w > 0 && c.width > 0 { // never override the flex column
		c.width = max(w, c.minWidth)
	}
	return c
}

// stripColumns is every column in the strip, in order, at its resolved
// width -- what the editable header band lays out against.
func (e *columnEditor) stripColumns() []column {
	cols := make([]column, len(e.order))
	for i, id := range e.order {
		cols[i] = e.resolvedColumn(id)
	}
	return cols
}

// visibleColumns is the subset of the strip that isn't hidden, in order --
// what the live preview lays out against. Because it's built by filtering
// the SAME order the strip walks, a visible column's position among other
// visible columns always matches between the two views (a hidden column
// contributes no width to either), which is what lets the strip band read
// as a header directly above the preview it's the header of.
func (e *columnEditor) visibleColumns() []column {
	cols := make([]column, 0, len(e.order))
	for _, id := range e.order {
		if !e.hidden[id] {
			cols = append(cols, e.resolvedColumn(id))
		}
	}
	return cols
}

// Result is the edited settings: visible columns only, in strip order, each
// carrying its width override (0 = catalog default). What result()'s apply
// Cmd hands Model once the editor is finished.
func (e *columnEditor) Result() []columnSetting {
	out := make([]columnSetting, 0, len(e.order))
	for _, id := range e.order {
		if e.hidden[id] {
			continue
		}
		out = append(out, columnSetting{ID: id, Width: e.widths[id]})
	}
	return out
}

// move slides the cursor across strip cells, or -- while armed -- swaps the
// armed column with its neighbour and follows it, so the arm stays on the
// piece being moved rather than the slot it used to occupy. Same two keys,
// different meaning, entirely driven by armed: the vocabulary never grows.
func (e *columnEditor) move(delta int) {
	next := e.cursor + delta
	if next < 0 || next >= len(e.order) {
		return
	}
	if e.armed {
		e.order[e.cursor], e.order[next] = e.order[next], e.order[e.cursor]
	}
	e.cursor = next
}

// resize grows or shrinks the selected column by one cell, floored at its
// minWidth. The flex column (status) has nothing fixed to resize -- it
// already takes whatever the fixed columns leave -- so this is a no-op for
// it, and selectionIsFlex/ShortHelp drop the hint while it's selected.
func (e *columnEditor) resize(delta int) {
	id := e.order[e.cursor]
	col := columnCatalog[id]
	if col.width == 0 {
		return
	}
	cur := e.widths[id]
	if cur == 0 {
		cur = col.width
	}
	next := max(cur+delta, col.minWidth)
	if next == col.width {
		next = 0 // back at the catalog default -- keep Width's omitempty clean
	}
	e.widths[id] = next
}

func (e *columnEditor) selectionIsFlex() bool {
	if e.cursor < 0 || e.cursor >= len(e.order) {
		return false
	}
	return columnCatalog[e.order[e.cursor]].width == 0
}

func (e *columnEditor) Init() tea.Cmd { return nil }

func (e *columnEditor) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return e, nil
	}
	switch km.String() {
	case "left":
		e.move(-1)
	case "right":
		e.move(1)
	case "enter":
		e.armed = !e.armed
	case " ":
		id := e.order[e.cursor]
		e.hidden[id] = !e.hidden[id]
	case "+", "=":
		e.resize(1)
	case "-", "_":
		e.resize(-1)
	case "ctrl+r":
		e.reset(defaultColumnSettings())
	case "esc":
		if e.armed { // esc while armed only disarms -- it does not close
			e.armed = false
			return e, nil
		}
		e.finished = true // see result(): there is no cancel gesture, on purpose
	}
	return e, nil
}

// result satisfies overlay.go's pull-based overlayResult contract: the
// editor is finished only after an unarmed esc (never by ctrl+r, which
// resets in place without closing), and its apply Cmd captures the current
// Result() into a columnsAppliedMsg for Model to persist and relayout from.
func (e *columnEditor) result() (finished bool, apply tea.Cmd) {
	if !e.finished {
		return false, nil
	}
	settings := e.Result()
	return true, func() tea.Msg { return columnsAppliedMsg{columns: settings} }
}

// ShortHelp/FullHelp make columnEditor itself a help.KeyMap: the SAME five
// bindings every time, their labels (and, for resize, their presence)
// adjusted for the live state -- never a sixth verb, never a hidden one.
func (e *columnEditor) ShortHelp() []key.Binding {
	move, arm, close := e.keys.move, e.keys.arm, e.keys.close
	if e.armed {
		move.SetHelp("←/→", "swap")
		arm.SetHelp("enter", "drop")
		close.SetHelp("esc", "disarm")
	}
	bindings := []key.Binding{move, arm, e.keys.hide}
	if !e.selectionIsFlex() {
		bindings = append(bindings, e.keys.resize)
	}
	return append(bindings, close, e.keys.reset)
}

func (e *columnEditor) FullHelp() [][]key.Binding {
	return [][]key.Binding{e.ShortHelp()}
}

// stripLabel is a strip cell's text: the real header's glyph+label (see
// column.headerCell), with the glyph swapped for ◆ while armed so "this
// column is being moved" reads without requiring the highlight colour to
// have been learned first.
func stripLabel(col column, armed bool) string {
	glyph := col.glyph
	if armed {
		glyph = glyphArmed
	}
	switch {
	case glyph == 0:
		return col.label
	case col.label == "":
		return glyphU(glyph)
	default:
		return glyphU(glyph) + " " + col.label
	}
}

// highlightCell re-applies the selection background after the cell's own
// reset -- the exact trick Styles.selectRow uses for a whole row (GOTCHAS
// #9: a plain Background() dies at the content's first \x1b[0m), scoped to
// one header cell. Reuses styles.selectedBG itself (the same background a
// selected ROW gets) rather than inventing a second highlight colour, so the
// selected header cell and the selected row read as the same gesture.
func highlightCell(bg, content string) string {
	if bg == "" {
		return content
	}
	return bg + strings.ReplaceAll(content, "\x1b[0m", "\x1b[0m"+bg) + "\x1b[0m"
}

// stripCell renders one strip header cell. Hidden columns drop to
// Muted+Strikethrough (Faint text, never the theme's muted slot -- GOTCHAS
// #3); armed recolours to Mail's warning yellow so "grabbed" doesn't rely on
// the reader having learned the selection background first; the selected
// cell (armed or not) gets highlightCell's background.
func stripCell(styles Styles, col column, width int, hidden, selected, armed bool) string {
	style := styles.Header
	switch {
	case armed:
		style = styles.Mail.Bold(true)
	case hidden:
		style = styles.Muted.Strikethrough(true)
	}
	rendered := fixedCol(width).Render(style.Render(stripLabel(col, armed)))
	if selected {
		return highlightCell(styles.selectedBG, rendered)
	}
	return rendered
}

func (e *columnEditor) renderStrip() string {
	layout := layoutColumns(e.stripColumns(), e.width)
	cells := make([]string, 0, 2*len(layout.columns))
	cells = append(cells, fixedCol(cursorWidth).Render(""))
	for i, col := range layout.columns {
		if i > 0 {
			cells = append(cells, " ")
		}
		id := e.order[i]
		cells = append(cells, stripCell(e.styles, col, layout.widths[i], e.hidden[id], i == e.cursor, e.armed && i == e.cursor))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, cells...)
}

// padRight lays left flush-left and right flush-right on one line at width,
// dropping right if there's no room for it -- narrower than the title alone
// is not a case this app's popup width ever hits, but it must degrade
// instead of panicking (a negative strings.Repeat count panics).
func padRight(left, right string, width int) string {
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return left
	}
	return left + strings.Repeat(" ", gap) + right
}

// View renders the editor's whole screen: a title band (the contextual help
// line right-aligned to width), the editable header strip, a blank line,
// then the live preview -- see the package doc and newColumnEditor.
func (e *columnEditor) View() string {
	title := e.styles.Header.Render("edit columns")
	e.help.Width = max(e.width-lipgloss.Width(title)-1, 0)
	band := padRight(title, e.help.View(e), e.width)

	preview := e.preview(layoutColumns(e.visibleColumns(), e.width))

	return strings.Join([]string{band, e.renderStrip(), "", preview}, "\n")
}
