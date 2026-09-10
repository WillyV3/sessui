package ui

// The settings editor. The header row is the editing surface: you arrow
// across the same header the table draws, and the real table redraws live
// underneath every edit. Nothing here is a mockup of the table.
//
// Six rows, one cursor axis each:
//
//	strip   the visible columns, laid out exactly as the preview below them
//	shelf   the hidden columns as chips -- what you can add
//	width   the popup's width, applied on the next open
//	theme   the palette source (auto / dark / light)
//	icons   the glyph set (nerd / ascii)
//	hosts   the machines whose sessions the list shows
//
// The same verbs mean the same thing on every row: ←/→ selects (or, armed on
// the strip, swaps with a neighbour), enter arms a strip column for moving,
// space hides or shows, +/- resizes. esc applies and closes; ctrl+z abandons;
// ctrl+r resets in place.
//
// The strip lays out only the visible columns, at the same width as the
// preview, so alignment holds by construction and the strip can never be
// wider than the popup. Arm-to-move rather than a reorder mode because
// left/right already means "move the cursor"; arming is the one bit of state
// that lets the same two keys also mean "move the column". There is no
// confirm step: the editor is opened constantly, and esc applying keeps it
// fast.

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/WillyV3/sessui/internal/session"
	"github.com/charmbracelet/lipgloss"
)

// glyphArmed marks the column armed for moving: distinct from every column's
// own glyph, so "grabbed" reads without first learning a colour.
const glyphArmed = 0x25C6 // ◆

// allColumnIDs is every catalog column in declaration order. columnCatalog is
// a map, so this is the editor's stable "everything" list.
var allColumnIDs = []columnID{
	colSession, colApps, colActive, colCWD, colPeer, colStatus,
	colAge, colWindows, colAttached, colHost,
}

// Popup width bounds. The floor is what the shipped columns need before
// status hits its minimum; the ceiling is a sanity stop.
const (
	popupWidthDefault = 112
	popupWidthMin     = 80
	popupWidthMax     = 240
	popupWidthStep    = 4
)

// editorRow is which row the cursor is on.
type editorRow int

const (
	rowStrip editorRow = iota
	rowShelf
	rowWidth
	rowTheme
	rowIcons
	rowHosts
	rowLast = rowHosts
)

// palettes is the ←→ cycle order on the theme row.
var palettes = []paletteSource{paletteAuto, paletteDark, paletteLight}

func cyclePalette(cur paletteSource, delta int) paletteSource {
	i := slices.Index(palettes, cur)
	if i < 0 {
		i = 0
	}
	return palettes[(i+delta+len(palettes))%len(palettes)]
}

func cycleGlyphs(cur glyphSet) glyphSet {
	if cur == glyphsASCII {
		return glyphsNerd
	}
	return glyphsASCII
}

// editorKeys is the editor's whole vocabulary, held as one key.Binding set so
// bubbles/help renders straight off it. ShortHelp only adjusts a binding's
// label or presence for the current row; it never introduces a new verb.
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

// Labels are short because the whole legend shares one line with the title;
// a truncated legend is a hidden key.
func newEditorKeys() editorKeys {
	return editorKeys{
		rows:   key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑↓", "row")),
		move:   key.NewBinding(key.WithKeys("left", "right"), key.WithHelp("←→", "select")),
		arm:    key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "arm")),
		hide:   key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "hide")),
		resize: key.NewBinding(key.WithKeys("+", "=", "-", "_"), key.WithHelp("+/-", "resize")),
		close:  key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "apply")),
		// ctrl+z reads as "undo". ctrl+q/ctrl+s are terminal flow control on
		// some setups, and ctrl+x already means kill in the list.
		abandon: key.NewBinding(key.WithKeys("ctrl+z"), key.WithHelp("^z", "abandon")),
		reset:   key.NewBinding(key.WithKeys("ctrl+r"), key.WithHelp("^r", "reset")),
	}
}

// columnEditor is the settings overlay. It owns the keyboard while up and
// reports itself done through result() (see overlayResult).
type columnEditor struct {
	styles   Styles
	showPeer bool
	preview  func(Styles, tableLayout) string
	keys     editorKeys
	help     help.Model

	// theme and icons are the choice being edited; restyle realises a choice
	// as Styles, the same path New takes, so the preview IS the choice. orig
	// is what ctrl+z puts back.
	theme     ThemeConfig
	icons     glyphSet
	origTheme ThemeConfig
	origIcons glyphSet
	restyle   func(ThemeConfig, glyphSet) Styles

	// hosts is every machine discovery found; hostWatched the subset whose
	// sessions the list shows. hostState is the watcher's view, re-read as
	// each probe lands so chips fill in as answers arrive.
	hosts       []string
	hostWatched map[string]bool
	hostState   map[string]session.RemoteHost
	hostCursor  int
	hostScroll  int                  // leftmost visible chip: the row is a viewport
	hostArrived map[string]time.Time // when each machine answered, for the fill animation
	hostProbed  bool
	watcher     *session.Watcher

	// order is every catalog column, visible and hidden, in one stable
	// sequence. Hiding flags a column rather than moving it, so hide-then-show
	// puts it back where it was.
	order  []columnID
	hidden map[columnID]bool
	// widths is the per-column resize override, 0 = catalog default: the
	// columnSetting.Width contract, so Result() is a direct read.
	widths map[columnID]int

	// width is the row width of the window the editor is in; every line
	// renders at it. popupWidth is the tmux width being set for the NEXT open
	// and is only ever a number on the width row, never a layout width.
	width      int
	popupWidth int

	row         editorRow
	cursor      int  // index into order; on a visible entry while row == rowStrip
	shelfCursor int  // index into order; on a hidden entry while row == rowShelf
	armed       bool // enter arms the selected strip column for a swap-move
	finished    bool // an unarmed esc or ctrl+z was pressed -- see result()
	abandoned   bool // it was ctrl+z: finish with nothing to apply
}

// newColumnEditor builds the editor over current. width is the row width of
// the window; popupWidth the configured tmux width the user may change for
// next time; preview is Model's real renderer over live sessions.
func newColumnEditor(styles Styles, current []columnSetting, showPeer bool, width, popupWidth int, preview func(Styles, tableLayout) string) *columnEditor {
	e := &columnEditor{styles: styles, showPeer: showPeer, preview: preview, keys: newEditorKeys(), help: help.New(), width: width, popupWidth: clampPopupWidth(popupWidth)}
	// Until withTheme is called the theme rows edit the defaults and restyle
	// is a no-op.
	e.withTheme(ThemeConfig{Palette: paletteAuto}, glyphsNerd, func(ThemeConfig, glyphSet) Styles { return styles })
	e.reset(current)
	return e
}

// withTheme seats the editor on the current theme choice and the function
// that realises a choice as Styles.
func (e *columnEditor) withTheme(t ThemeConfig, g glyphSet, restyle func(ThemeConfig, glyphSet) Styles) *columnEditor {
	e.theme, e.icons, e.origTheme, e.origIcons, e.restyle = t, g, t, g, restyle
	e.tint()
	return e
}

// withHosts hands the editor the discovered machines, the watched ones, and
// the cache to read reachability from.
func (e *columnEditor) withHosts(hosts, watched []string, w *session.Watcher) *columnEditor {
	e.hosts, e.watcher = hosts, w
	e.hostWatched = make(map[string]bool, len(watched))
	for _, h := range watched {
		e.hostWatched[h] = true
		// A configured host discovery no longer sees still belongs in the
		// row; otherwise unwatching it would be impossible.
		if !slices.Contains(e.hosts, h) {
			e.hosts = append(e.hosts, h)
		}
	}
	e.syncHostState()
	e.noteArrivals(true)
	return e
}

// tint re-derives the editor's styles from the current choice and re-tints
// bubbles/help onto them.
func (e *columnEditor) tint() {
	e.styles = e.restyle(e.theme, e.icons)
	e.help.Styles.ShortKey = e.styles.Header
	e.help.Styles.ShortDesc = e.styles.Help
	e.help.Styles.ShortSeparator = e.styles.Help
}

func clampPopupWidth(w int) int {
	if w <= 0 {
		return popupWidthDefault
	}
	return min(max(w, popupWidthMin), popupWidthMax)
}

// reset rebuilds the editor from settings: the configured columns in order,
// then every catalog column they do not mention, hidden. The peer column is
// dropped outright when showPeer is false, as in the live table; offering a
// column that render would strip back out would be a lie.
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

// resolvedColumn is id's catalog column at its effective width -- exactly
// resolveColumns' rule, so a column reads the same here and in the table.
func (e *columnEditor) resolvedColumn(id columnID) column {
	c := columnCatalog[id]
	if w := e.widths[id]; w > 0 && c.width > 0 { // never override the flex column
		c.width = max(w, c.minWidth)
	}
	return c
}

// visibleColumns is the slice both the strip and the preview lay out, which
// is what makes the strip a true header for the preview.
func (e *columnEditor) visibleColumns() []column {
	cols := make([]column, 0, len(e.order))
	for _, id := range e.order {
		if !e.hidden[id] {
			cols = append(cols, e.resolvedColumn(id))
		}
	}
	return cols
}

// Result is the edited settings: visible columns in strip order, each with
// its width override.
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

// nearest returns the index of the entry at or after from (forward, then
// backward) whose hidden flag matches want; -1 if none. It keeps each row's
// cursor on an entry that belongs to that row.
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

// step returns the next entry in the row's direction that belongs to the row,
// or -1 at the edge.
func (e *columnEditor) step(from, delta int, want bool) int {
	for i := from + delta; i >= 0 && i < len(e.order); i += delta {
		if e.hidden[e.order[i]] == want {
			return i
		}
	}
	return -1
}

// move slides the row's cursor or, armed on the strip, swaps the armed column
// with its nearest visible neighbour and follows it. Hidden entries between
// them are skipped, not disturbed.
func (e *columnEditor) move(delta int) {
	if e.row == rowHosts {
		e.hostMove(delta)
		return
	}
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
	case rowTheme:
		e.theme.Palette = cyclePalette(e.theme.Palette, delta)
		e.tint()
	case rowIcons:
		e.icons = cycleGlyphs(e.icons)
		e.tint()
	}
}

// changeRow moves between rows. Arming is a strip state, so leaving the strip
// drops it. An empty shelf is skipped.
func (e *columnEditor) changeRow(delta int) {
	next := e.row + editorRow(delta)
	if next == rowShelf && e.shelfCursor < 0 {
		next += editorRow(delta)
	}
	if next < rowStrip || next > rowLast {
		return
	}
	e.row, e.armed = next, false
}

// toggle is space: hide the selected strip column, or show the selected
// shelf chip in its original position. Each row's cursor is then re-seated
// on an entry that still belongs to it.
func (e *columnEditor) toggle() {
	if e.row == rowHosts {
		e.hostToggle()
		return
	}
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
// its minimum. The flex column takes what the others leave, so it is a no-op
// there. On the width row, +/- adjusts the popup.
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
		next = 0 // back at the catalog default: keep Width's omitempty clean
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

func (e *columnEditor) Init() tea.Cmd { return e.probeHosts() }

func (e *columnEditor) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// A probe landing is not a keystroke: re-read the watcher so the chips
	// update in place.
	if _, ok := msg.(hostsRefreshedMsg); ok {
		e.syncHostState()
		if e.noteArrivals(false) {
			return e, hostAnimCmd()
		}
		return e, nil
	}
	// The fill animation ticks only while a chip is mid-animation and stops
	// on its own, so an idle editor costs nothing.
	if _, ok := msg.(hostAnimMsg); ok {
		if e.noteArrivals(false) {
			return e, hostAnimCmd()
		}
		return e, nil
	}
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
		e.theme.Palette, e.icons = paletteAuto, glyphsNerd
		e.tint()
	case "ctrl+z":
		// The preview was restyled live; put the process back the way the
		// list expects before handing the keyboard back.
		if e.theme.Palette != e.origTheme.Palette || e.icons != e.origIcons {
			e.restyle(e.origTheme, e.origIcons)
		}
		e.finished, e.abandoned = true, true
	case "esc":
		if e.armed { // esc while armed only disarms
			e.armed = false
			return e, nil
		}
		e.finished = true // apply: see result()
	}
	return e, nil
}

// result satisfies overlayResult. esc's apply Cmd captures Result() and the
// popup width into an editorAppliedMsg; ctrl+z finishes with nil, and since
// the list was never re-laid-out while the editor was open, abandoning needs
// no undo.
func (e *columnEditor) result() (finished bool, apply tea.Cmd) {
	if !e.finished {
		return false, nil
	}
	if e.abandoned {
		return true, nil
	}
	msg := editorAppliedMsg{columns: e.Result(), popupWidth: e.popupWidth, theme: e.theme, icons: e.icons, hosts: e.hostResult()}
	return true, func() tea.Msg { return msg }
}

// ShortHelp makes columnEditor a help.KeyMap: the same bindings, labels
// adjusted for the current row.
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
	case rowTheme, rowIcons:
		move.SetHelp("←→", "choose")
		bindings = []key.Binding{rows, move}
	}
	return append(bindings, close, e.keys.abandon, e.keys.reset)
}

func (e *columnEditor) FullHelp() [][]key.Binding {
	return [][]key.Binding{e.ShortHelp()}
}

// stripLabel is a strip cell's text: the header's glyph and label, with the
// glyph swapped for ◆ while armed.
func stripLabel(col column, armed bool) string {
	glyph := col.glyph
	if armed {
		glyph = glyphArmed
	}
	return col.headerText(glyph)
}

// highlightCell re-applies the selection background after the cell's own
// reset -- the trick Styles.selectRow uses for a whole row, scoped to one
// cell -- with the same background a selected row gets, so the two read as
// one gesture.
func highlightCell(bg, content string) string {
	if bg == "" {
		return content
	}
	return bg + strings.ReplaceAll(content, "\x1b[0m", "\x1b[0m"+bg) + "\x1b[0m"
}

// stripCell renders one strip header cell. Armed recolours so "grabbed" does
// not rely on having learned the selection background.
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
// every cell sits over the data it labels.
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

// captionWidth is the label gutter the control rows share, so their captions
// form one left edge. The strip has no caption: its cells must sit exactly
// over the preview's columns.
const captionWidth = 8

// controlRow composes one full-width control row: a faint caption in the
// gutter, the control, and a hint anchored right, so the row uses the width
// it is given rather than trailing off.
func (e *columnEditor) controlRow(caption, control, hint string, width int) string {
	left := fixedCol(cursorWidth).Render("") + fixedCol(captionWidth).Render(e.styles.Muted.Render(caption)) + control
	return padRight(left, e.styles.Muted.Render(hint), width)
}

// renderShelf is the hidden columns as chips at their natural width. Compact
// by design: a chip is an offer, not a column.
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

// renderWidthRow is the tmux popup width, captioned "popup" because "width"
// next to a column editor reads as a column. The popup you are looking at
// cannot resize itself, and the hint says so.
func (e *columnEditor) renderWidthRow(width int) string {
	hint := "tmux popup width · +/- to change · takes effect next open"
	if e.popupWidth != popupWidthDefault {
		hint = fmt.Sprintf("tmux popup width · default %d · takes effect next open", popupWidthDefault)
	}
	return e.controlRow("popup", e.valueCell(fmt.Sprintf("%d cols", e.popupWidth), e.row == rowWidth), hint, width)
}

func (e *columnEditor) renderThemeRow(width int) string {
	hint := "palette · auto follows the Omarchy theme, else dark · ←→ to change"
	return e.controlRow("theme", e.valueCell(string(e.theme.Palette), e.row == rowTheme), hint, width)
}

func (e *columnEditor) renderIconsRow(width int) string {
	hint := "glyph set · ascii for a terminal without a Nerd Font · ←→ to change"
	return e.controlRow("icons", e.valueCell(string(e.icons), e.row == rowIcons), hint, width)
}

// valueCell is a control row's single value: highlighted while its row has
// the cursor, faint otherwise.
func (e *columnEditor) valueCell(value string, selected bool) string {
	if selected {
		return highlightCell(e.styles.selectedBG, e.styles.Header.Render(" "+value+" "))
	}
	return e.styles.Muted.Render(value)
}

// padRight lays left flush-left and right flush-right on one line, dropping
// right if there is no room: a negative strings.Repeat count panics.
func padRight(left, right string, width int) string {
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return left
	}
	return left + strings.Repeat(" ", gap) + right
}

// View renders the editor at the current window width: a title band with
// the help line, the six rows, a blank line, then the live preview in the
// editor's current styles, so a theme choice is seen before it is applied.
func (e *columnEditor) View() string {
	width := e.width
	title := e.styles.Header.Render("settings")
	e.help.Width = max(width-lipgloss.Width(title)-1, 0)
	band := padRight(title, e.help.View(e), width)

	layout := layoutColumns(e.visibleColumns(), width)
	return lipgloss.JoinVertical(lipgloss.Left,
		band,
		e.renderStrip(layout),
		e.renderShelf(width),
		e.renderWidthRow(width),
		e.renderThemeRow(width),
		e.renderIconsRow(width),
		e.renderHostsRow(width),
		"",
		e.preview(e.styles, layout),
	)
}
