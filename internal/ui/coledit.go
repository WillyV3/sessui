package ui

// The column editor: the header row IS the editing surface. There is no
// separate settings screen with checkboxes and a form -- you arrow across
// the same header the table already draws, and the exact table the app
// already renders (renderHeader/renderRow, handed in as `preview`) redraws
// live underneath every edit. Nothing here is a mockup of the table; it is
// the table, mid-edit.
//
// Three rows, one cursor axis each:
//
//	strip   the visible columns, laid out EXACTLY as the preview below them
//	shelf   the hidden columns as compact chips -- what you can add
//	width   the popup's width, applied on the next open
//
// ↑/↓ picks the row. Within a row the same verbs always mean the same thing:
//
//	←/→   move the selection, or (armed, strip only) swap with a neighbour
//	enter arm the selected column for moving / drop it back (strip only)
//	space hide the selected column (strip) / show it (shelf)
//	+/-   resize the selected column (strip), or adjust the width (width row)
//	esc   apply and close -- or, while armed, just disarm
//
// Two non-verbs: ctrl+z abandons (close, apply nothing), ctrl+r resets to
// the shipped defaults in place.
//
// Why the strip never contains a hidden column: an earlier version laid out
// all ten catalog columns in the strip at their real widths. That squeezed
// the flex column to a stub, truncated "windows" to "wi", and -- because the
// preview laid out only the visible six -- nothing in the strip sat over the
// data it labelled. The strip and the preview now lay out the same column
// slice at the same width, so alignment holds by construction and the strip
// can never be wider than the popup. Hidden columns live on the shelf as
// chips at their natural width, which is how a user discovers and adds
// age/windows/attached/machine.
//
// Arm-to-move (rather than a drag or a persistent "reorder mode") was chosen
// because left/right already means "move the cursor" -- arming is the one
// bit of state needed to make the SAME two keys also mean "move the column."
//
// There is no confirm step on purpose: this editor is opened ~120x/hour by a
// tool built to be fast, and esc applying immediately keeps it that way.
// Abandon exists because the owner asked for it after using it.

import (
	"fmt"
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
// editor's own stable "everything" list -- the shelf's starting contents
// (age, windows, attached, machine) come from walking this after the
// user's configured ones.
var allColumnIDs = []columnID{
	colSession, colApps, colActive, colCWD, colPeer, colStatus,
	colAge, colWindows, colAttached, colMachine,
}

// Popup width bounds and step. The floor is what the shipped six columns
// need before status hits its minWidth; the ceiling is a sanity stop, not a
// real limit. Steps of four keep the count of keypresses sane.
const (
	popupWidthDefault = 112
	popupWidthMin     = 80
	popupWidthMax     = 240
	popupWidthStep    = 4
)

// editorRow is which of the three rows the cursor is on.
type editorRow int

const (
	rowStrip editorRow = iota
	rowShelf
	rowWidth
)

// editorKeys is the editor's whole vocabulary, held as one key.Binding set
// so bubbles/help renders straight off it. ShortHelp (below) only ever
// adjusts an existing binding's label/presence for the current row and
// state -- it never introduces a new verb.
type editorKeys struct {
	rows    key.Binding
	move    key.Binding
	arm     key.Binding
	hide    key.Binding
	resize  key.Binding
	close   key.Binding
	abandon key.Binding
	reset   key.Binding
}

// Labels are short on purpose: the whole legend has to share one line with
// the title at the shipped 108 columns, with no "…" -- a truncated legend
// is a hidden key.
func newEditorKeys() editorKeys {
	return editorKeys{
		rows:   key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑↓", "row")),
		move:   key.NewBinding(key.WithKeys("left", "right"), key.WithHelp("←→", "select")),
		arm:    key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "arm")),
		hide:   key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "hide")),
		resize: key.NewBinding(key.WithKeys("+", "=", "-", "_"), key.WithHelp("+/-", "resize")),
		close:  key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "apply")),
		// abandon closes WITHOUT applying. ctrl+z reads as "undo", which is
		// what the user means by it; ctrl+q/ctrl+s are terminal flow-control
		// on some setups and ctrl+x already means kill in the list -- the
		// same letter must not mean two things.
		abandon: key.NewBinding(key.WithKeys("ctrl+z"), key.WithHelp("^z", "abandon")),
		reset:   key.NewBinding(key.WithKeys("ctrl+r"), key.WithHelp("^r", "reset")),
	}
}

// columnEditor is the WYSIWYG column editor overlay (see the package doc
// above). It owns the keyboard while it's up and reports itself done via
// result() (the pull-based overlayResult contract in overlay.go).
type columnEditor struct {
	styles   Styles
	showPeer bool
	preview  func(tableLayout) string
	keys     editorKeys
	help     help.Model

	// order is EVERY catalog column, visible and hidden, in one stable
	// sequence. Hiding does not move a column; it flags it, so hide-then-show
	// puts it back exactly where it was. The strip is order's visible
	// entries, the shelf its hidden ones.
	order  []columnID
	hidden map[columnID]bool
	// widths is a per-column resize override, 0 = catalog default -- the
	// exact columnSetting.Width contract, so Result() is a direct read.
	widths map[columnID]int

	// width is the row width of the window the editor is IN right now --
	// every line renders at this, so nothing can clip. popupWidth is the tmux
	// popup width the user is setting for the NEXT open; it is only ever a
	// number on the width row, never a layout width. An earlier build laid
	// the page out at popupWidth and every line ran off the current popup.
	width      int
	popupWidth int

	row         editorRow
	cursor      int  // index into order; on a visible entry while row == rowStrip
	shelfCursor int  // index into order; on a hidden entry while row == rowShelf
	armed       bool // enter arms the selected strip column for a swap-move
	finished    bool // an unarmed esc or ctrl+z was pressed -- see result()
	abandoned   bool // it was ctrl+z: finish with nothing to apply
}

// newColumnEditor builds the editor over `current`'s configuration. width is
// the row width of the current window (Model.usableWidth); popupWidth is the
// configured tmux popup width (session.PopupWidth), which the editor lets
// the user change for next time. preview is Model's real renderer
// (renderHeader + renderRow over live sessions) -- production code, not a
// mockup.
func newColumnEditor(styles Styles, current []columnSetting, showPeer bool, width, popupWidth int, preview func(tableLayout) string) *columnEditor {
	hp := help.New()
	// Tint bubbles/help onto the app's own palette instead of its baked-in
	// greys -- Header and Help are already the right colours for "key" and
	// "de-emphasised description," so this is a straight reuse.
	hp.Styles.ShortKey = styles.Header
	hp.Styles.ShortDesc = styles.Help
	hp.Styles.ShortSeparator = styles.Help

	e := &columnEditor{styles: styles, showPeer: showPeer, preview: preview, keys: newEditorKeys(), help: hp, width: width, popupWidth: clampPopupWidth(popupWidth)}
	e.reset(current)
	return e
}

func clampPopupWidth(w int) int {
	if w <= 0 {
		return popupWidthDefault
	}
	return min(max(w, popupWidthMin), popupWidthMax)
}

// reset rebuilds the editor from settings: the configured columns in their
// order first, then every catalog column the settings don't mention,
// appended hidden (the shelf). ctrl+r calls this with defaultColumnSettings()
// to restore the shipped table. The peer column is dropped outright (not even
// shelved) when showPeer is false, same as the live table -- offering a
// column that resolveColumns would strip back out at render time would be a
// lie the editor tells the user.
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
	e.row, e.armed = rowStrip, false
	e.cursor = e.nearest(0, false)
	e.shelfCursor = e.nearest(0, true)
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

// visibleColumns is the strip AND the preview's column slice: order's
// visible entries, resolved. Both views lay out this one slice at usable(),
// which is what makes the strip a true header for the preview.
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

// nearest returns the index of the entry at or after from (searching
// forward, then backward) whose hidden flag matches want; -1 if none. It
// is how each row's cursor stays on an entry that belongs to that row.
func (e *columnEditor) nearest(from int, want bool) int {
	for i := from; i < len(e.order); i++ {
		if e.hidden[e.order[i]] == want {
			return i
		}
	}
	for i := min(from, len(e.order)-1); i >= 0; i-- {
		if e.hidden[e.order[i]] == want {
			return i
		}
	}
	return -1
}

// step returns the next entry in the row's direction that belongs to the
// row (visible for the strip, hidden for the shelf), or -1 at the edge.
func (e *columnEditor) step(from, delta int, want bool) int {
	for i := from + delta; i >= 0 && i < len(e.order); i += delta {
		if e.hidden[e.order[i]] == want {
			return i
		}
	}
	return -1
}

// move slides the row's cursor, or -- armed, on the strip -- swaps the armed
// column with its nearest visible neighbour and follows it, so the arm stays
// on the piece being moved rather than the slot it used to occupy. Hidden
// entries between them are skipped over, not disturbed. On the width row
// ←/→ is the same as +/-: one control, both natural keys.
func (e *columnEditor) move(delta int) {
	switch e.row {
	case rowStrip:
		next := e.step(e.cursor, delta, false)
		if next < 0 {
			return
		}
		if e.armed {
			e.order[e.cursor], e.order[next] = e.order[next], e.order[e.cursor]
		}
		e.cursor = next
	case rowShelf:
		if next := e.step(e.shelfCursor, delta, true); next >= 0 {
			e.shelfCursor = next
		}
	case rowWidth:
		e.adjustWidth(delta)
	}
}

// changeRow moves ↑/↓ between strip, shelf and width. Arming is a strip
// state, so leaving the strip drops it. The shelf is skipped when it is
// empty -- there is nothing to select there -- so ↓ from a full strip goes
// straight to the width row.
func (e *columnEditor) changeRow(delta int) {
	next := e.row + editorRow(delta)
	if next == rowShelf && e.shelfCursor < 0 {
		next += editorRow(delta)
	}
	if next < rowStrip || next > rowWidth {
		return
	}
	e.row, e.armed = next, false
}

// toggle is space: on the strip it hides the selected column (which then
// appears on the shelf, in place); on the shelf it shows the selected chip
// (which reappears in the strip at its original position). Each row's cursor
// is then re-seated on an entry that still belongs to it.
func (e *columnEditor) toggle() {
	switch e.row {
	case rowStrip:
		if e.cursor < 0 {
			return
		}
		id := e.order[e.cursor]
		e.hidden[id] = true
		e.armed = false
		e.shelfCursor = e.cursor
		e.cursor = e.nearest(e.cursor, false)
	case rowShelf:
		if e.shelfCursor < 0 {
			return
		}
		id := e.order[e.shelfCursor]
		e.hidden[id] = false
		e.cursor = e.shelfCursor
		e.shelfCursor = e.nearest(e.shelfCursor, true)
	}
}

// resize grows or shrinks the selected strip column by one cell, floored at
// its minWidth. The flex column (status) has nothing fixed to resize -- it
// already takes whatever the fixed columns leave -- so it is a no-op there
// and ShortHelp drops the hint. On the width row, +/- adjust the popup.
func (e *columnEditor) resize(delta int) {
	if e.row == rowWidth {
		e.adjustWidth(delta)
		return
	}
	if e.row != rowStrip || e.cursor < 0 {
		return
	}
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

func (e *columnEditor) adjustWidth(delta int) {
	e.popupWidth = clampPopupWidth(e.popupWidth + delta*popupWidthStep)
}

func (e *columnEditor) selectionIsFlex() bool {
	if e.row != rowStrip || e.cursor < 0 {
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
	case "up":
		e.changeRow(-1)
	case "down":
		e.changeRow(1)
	case "left":
		e.move(-1)
	case "right":
		e.move(1)
	case "enter":
		if e.row == rowStrip && e.cursor >= 0 {
			e.armed = !e.armed
		}
	case " ":
		e.toggle()
	case "+", "=":
		e.resize(1)
	case "-", "_":
		e.resize(-1)
	case "ctrl+r":
		e.reset(defaultColumnSettings())
		e.popupWidth = popupWidthDefault
	case "ctrl+z":
		e.finished, e.abandoned = true, true
	case "esc":
		if e.armed { // esc while armed only disarms -- it does not close
			e.armed = false
			return e, nil
		}
		e.finished = true // apply: see result()
	}
	return e, nil
}

// result satisfies overlay.go's pull-based overlayResult contract. The
// editor is finished after an unarmed esc or a ctrl+z (never by ctrl+r,
// which resets in place without closing). esc's apply Cmd captures the
// current Result() and popup width into a columnsAppliedMsg for Model to
// persist and relayout from; ctrl+z finishes with a nil apply, so Model
// drops the overlay and nothing else happens -- the list was never
// re-laid-out while the editor was open, so "abandon" needs no undo.
func (e *columnEditor) result() (finished bool, apply tea.Cmd) {
	if !e.finished {
		return false, nil
	}
	if e.abandoned {
		return true, nil
	}
	settings, width := e.Result(), e.popupWidth
	return true, func() tea.Msg { return columnsAppliedMsg{columns: settings, popupWidth: width} }
}

// ShortHelp/FullHelp make columnEditor itself a help.KeyMap: the same
// bindings, their labels (and presence) adjusted for the current row and
// state -- never a new verb, never a hidden one.
func (e *columnEditor) ShortHelp() []key.Binding {
	rows, move, arm, hide, resize, close := e.keys.rows, e.keys.move, e.keys.arm, e.keys.hide, e.keys.resize, e.keys.close
	var bindings []key.Binding
	switch e.row {
	case rowStrip:
		if e.armed {
			move.SetHelp("←→", "swap")
			arm.SetHelp("enter", "drop")
			close.SetHelp("esc", "disarm")
		}
		bindings = []key.Binding{rows, move, arm, hide}
		if !e.selectionIsFlex() {
			bindings = append(bindings, resize)
		}
	case rowShelf:
		hide.SetHelp("space", "add")
		bindings = []key.Binding{rows, move, hide}
	case rowWidth:
		resize.SetHelp("+/-", "adjust")
		bindings = []key.Binding{rows, resize}
	}
	return append(bindings, close, e.keys.abandon, e.keys.reset)
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
// one cell. Reuses styles.selectedBG itself (the same background a selected
// ROW gets) rather than inventing a second highlight colour, so the selected
// cell and the selected row read as the same gesture.
func highlightCell(bg, content string) string {
	if bg == "" {
		return content
	}
	return bg + strings.ReplaceAll(content, "\x1b[0m", "\x1b[0m"+bg) + "\x1b[0m"
}

// stripCell renders one strip header cell. Armed recolours to Mail's yellow
// so "grabbed" doesn't rely on the reader having learned the selection
// background first; the selected cell (armed or not) gets highlightCell's
// background.
func stripCell(styles Styles, col column, width int, selected, armed bool) string {
	style := styles.Header
	if armed {
		style = styles.Mail.Bold(true)
	}
	rendered := fixedCol(width).Render(style.Render(stripLabel(col, armed)))
	if selected {
		return highlightCell(styles.selectedBG, rendered)
	}
	return rendered
}

// renderStrip lays the visible columns out at the preview's own layout, so
// every cell sits over the data it labels and the strip is exactly as wide
// as the table -- never wider than the popup.
func (e *columnEditor) renderStrip(layout tableLayout) string {
	selected := e.row == rowStrip
	cells := make([]string, 0, 2*len(layout.columns))
	cells = append(cells, fixedCol(cursorWidth).Render(""))
	vi := 0
	for i, id := range e.order {
		if e.hidden[id] {
			continue
		}
		if vi > 0 {
			cells = append(cells, " ")
		}
		isSel := selected && i == e.cursor
		cells = append(cells, stripCell(e.styles, layout.columns[vi], layout.widths[vi], isSel, isSel && e.armed))
		vi++
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, cells...)
}

// captionWidth is the label gutter the two control rows share, so "add" and
// "width" form a left edge under the strip and the eye reads three rows as
// one instrument. The strip itself has no caption: its cells must sit
// exactly over the preview's columns, so nothing may shift it.
const captionWidth = 8

// controlRow composes one full-width control row: a faint caption in the
// gutter, the control itself, and a hint anchored to the right edge -- so
// the row uses the width it is given rather than trailing off after the
// control (the page is 108+ wide; a short left-aligned line reads as
// abandoned).
func (e *columnEditor) controlRow(caption, control, hint string, width int) string {
	left := fixedCol(cursorWidth).Render("") + fixedCol(captionWidth).Render(e.styles.Muted.Render(caption)) + control
	return padRight(left, e.styles.Muted.Render(hint), width)
}

// renderShelf is the hidden columns as chips: "+ glyph label", each at its
// natural width, faint, with the selected one highlighted when the shelf has
// the cursor. Compact by design -- a chip is an offer, not a column.
func (e *columnEditor) renderShelf(width int) string {
	selected := e.row == rowShelf
	var chips []string
	for i, id := range e.order {
		if !e.hidden[id] {
			continue
		}
		text := "+ " + stripLabel(columnCatalog[id], false)
		if selected && i == e.shelfCursor {
			chips = append(chips, highlightCell(e.styles.selectedBG, e.styles.Header.Render(text)))
		} else {
			chips = append(chips, e.styles.Muted.Render(text))
		}
	}
	if len(chips) == 0 {
		return e.controlRow("add", e.styles.Muted.Render("every column is shown"), "", width)
	}
	hint := fmt.Sprintf("%d hidden", len(chips))
	return e.controlRow("add", strings.Join(chips, "   "), hint, width)
}

// renderWidthRow is the tmux popup width control -- captioned "popup" and
// spelled out in the hint, because a user reading "width" next to a column
// editor assumes a column. The value is stored in the @sessui-width tmux
// option and takes effect on the next prefix+s; the popup you are looking
// at cannot resize itself, and the hint says so.
func (e *columnEditor) renderWidthRow(width int) string {
	value := fmt.Sprintf("%d cols", e.popupWidth)
	if e.row == rowWidth {
		value = highlightCell(e.styles.selectedBG, e.styles.Header.Render(" "+value+" "))
	} else {
		value = e.styles.Muted.Render(value)
	}
	hint := "tmux popup width · +/- to change · takes effect next open"
	if e.popupWidth != popupWidthDefault {
		hint = fmt.Sprintf("tmux popup width · default %d · takes effect next open", popupWidthDefault)
	}
	return e.controlRow("popup", value, hint, width)
}

// padRight lays left flush-left and right flush-right on one line at width,
// dropping right if there's no room for it -- it must degrade instead of
// panicking (a negative strings.Repeat count panics).
func padRight(left, right string, width int) string {
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return left
	}
	return left + strings.Repeat(" ", gap) + right
}

// View renders the editor's whole screen at the CURRENT window width: a
// title band (the contextual help line right-aligned), the strip, the shelf,
// the popup-width row, a blank line, then the live preview.
func (e *columnEditor) View() string {
	width := e.width
	title := e.styles.Header.Render("edit columns")
	e.help.Width = max(width-lipgloss.Width(title)-1, 0)
	band := padRight(title, e.help.View(e), width)

	layout := layoutColumns(e.visibleColumns(), width)
	return lipgloss.JoinVertical(lipgloss.Left,
		band,
		e.renderStrip(layout),
		e.renderShelf(width),
		e.renderWidthRow(width),
		"",
		e.preview(layout),
	)
}
