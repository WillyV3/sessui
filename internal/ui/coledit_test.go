package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// idsPreview is a stand-in for Model's real preview closure: instead of
// rendering session rows (which coledit.go never touches -- that's the
// point, see its package doc), it records the visible column ids it was
// handed, comma-joined, so tests can assert on exactly what the editor
// decided was visible without parsing rendered table cells.
func idsPreview(l tableLayout) string {
	ids := make([]string, len(l.columns))
	for i, c := range l.columns {
		ids[i] = string(c.id)
	}
	return strings.Join(ids, ",")
}

func newTestEditor() *columnEditor {
	return newColumnEditor(testStyles(), defaultColumnSettings(), true, defaultUsableWidth, idsPreview)
}

// keyMsg builds the tea.KeyMsg a real terminal would send for one of the
// editor's keys, by name -- so tests read as the actual keystrokes a user
// presses, not internal enum values.
func keyMsg(k string) tea.KeyMsg {
	switch k {
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "space":
		return tea.KeyMsg{Type: tea.KeySpace}
	case "ctrl+r":
		return tea.KeyMsg{Type: tea.KeyCtrlR}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
	}
}

// press drives Update with one key by name and returns the (possibly
// replaced) *columnEditor, the way Model's forwarding loop would.
func press(t *testing.T, e *columnEditor, keys ...string) *columnEditor {
	t.Helper()
	for _, k := range keys {
		next, _ := e.Update(keyMsg(k))
		var ok bool
		e, ok = next.(*columnEditor)
		if !ok {
			t.Fatalf("Update(%q) returned a %T, not *columnEditor", k, next)
		}
	}
	return e
}

// idOf is the columnID at strip position i, for readable assertions.
func idOf(e *columnEditor, i int) columnID { return e.order[i] }

func resultIDs(e *columnEditor) []columnID {
	r := e.Result()
	ids := make([]columnID, len(r))
	for i, s := range r {
		ids[i] = s.ID
	}
	return ids
}

// TestColumnEditor_MoveThenArmThenSwap pins the core gesture: left/right
// alone only moves the cursor (the strip's order, and therefore Result(),
// stays the shipped default); enter arms the selection (View shows the ◆
// marker); armed, the same left/right swaps two neighbours instead, and the
// swap is visible immediately in Result() -- which is what "live" means
// here, there's no separate apply step.
func TestColumnEditor_MoveThenArmThenSwap(t *testing.T) {
	e := newTestEditor()

	if idOf(e, 0) != colSession {
		t.Fatalf("fresh editor cursor col = %s, want session (index 0)", idOf(e, 0))
	}

	e = press(t, e, "right") // cursor -> apps (index 1), nothing armed yet
	if got := resultIDs(e); got[1] != colApps || got[2] != colActive {
		t.Fatalf("plain move must not reorder: Result() = %v", got)
	}

	e = press(t, e, "enter") // arm apps
	if !e.armed {
		t.Fatal("enter did not arm the selection")
	}
	if !strings.Contains(e.View(), "◆") {
		t.Error("armed View() must show the ◆ marker somewhere in the strip")
	}

	e = press(t, e, "right") // armed: swap apps and active, cursor follows to index 2
	if e.cursor != 2 {
		t.Fatalf("armed move: cursor = %d, want 2 (follows the swapped column)", e.cursor)
	}
	if idOf(e, 1) != colActive || idOf(e, 2) != colApps {
		t.Fatalf("swap: order[1..2] = %s,%s, want active,apps", idOf(e, 1), idOf(e, 2))
	}
	got := resultIDs(e)
	if got[1] != colActive || got[2] != colApps {
		t.Fatalf("Result() after swap = %v, want active before apps", got)
	}
}

// TestColumnEditor_HideThenShow proves space is the one gesture for both
// directions: a hidden column drops out of Result() and out of the preview
// closure's visible columns, and pressing space again on the same (still
// selected) cell restores it to its original position -- toggling never
// moves it.
func TestColumnEditor_HideThenShow(t *testing.T) {
	e := newTestEditor()
	e = press(t, e, "right") // select apps

	e = press(t, e, "space")
	if !e.hidden[colApps] {
		t.Fatal("space did not hide the selected column")
	}
	for _, id := range resultIDs(e) {
		if id == colApps {
			t.Fatal("Result() still contains a hidden column")
		}
	}
	if preview := e.View(); strings.Contains(preview, "apps,") || strings.HasSuffix(preview, "apps") {
		// idsPreview joins ids with ",", so "apps" bordered by "," or a line
		// edge would mean it leaked into the preview's visible set.
		t.Errorf("hidden column leaked into the preview: %q", preview)
	}

	// Hiding put apps on the shelf with the shelf cursor already on it, so
	// showing it again is ↓ then the same space -- and it returns to the
	// slot it came from, never the end.
	e = press(t, e, "down")
	if e.row != rowShelf || idOf(e, e.shelfCursor) != colApps {
		t.Fatalf("↓ after hiding: row %d, shelf cursor on %s; want the shelf with apps selected", e.row, idOf(e, e.shelfCursor))
	}
	e = press(t, e, "space")
	if e.hidden[colApps] {
		t.Fatal("space on the shelf did not show the column again")
	}
	if idOf(e, 1) != colApps {
		t.Fatalf("un-hiding moved the column: order[1] = %s, want apps (same slot it left)", idOf(e, 1))
	}
}

// TestColumnEditor_Resize covers +/- on a fixed column (grows, floors at
// minWidth) and confirms the flex column (status) refuses to resize at all
// and drops the hint from its own contextual help.
func TestColumnEditor_Resize(t *testing.T) {
	t.Run("fixed column grows and floors at minWidth", func(t *testing.T) {
		e := newTestEditor()
		e = press(t, e, "right", "right", "right") // session, apps, active, cwd
		if idOf(e, e.cursor) != colCWD {
			t.Fatalf("test setup: selected %s, want cwd", idOf(e, e.cursor))
		}

		e = press(t, e, "+")
		want := columnCatalog[colCWD].width + 1
		if e.widths[colCWD] != want {
			t.Fatalf("cwd width after one +: %d, want %d", e.widths[colCWD], want)
		}

		e = press(t, e, "-", "-") // back to default, then below it
		floor := columnCatalog[colCWD].minWidth
		for i := 0; i < 30; i++ { // hammer well past the floor
			e = press(t, e, "-")
		}
		if e.widths[colCWD] != floor {
			t.Fatalf("cwd width hammered below minWidth: %d, want floored at %d", e.widths[colCWD], floor)
		}
	})

	t.Run("flex column (status) does not resize", func(t *testing.T) {
		e := newTestEditor()
		for idOf(e, e.cursor) != colStatus {
			e = press(t, e, "right")
		}
		if !e.selectionIsFlex() {
			t.Fatal("test setup: status must read as the flex column")
		}

		e = press(t, e, "+", "+", "-")
		if w, ok := e.widths[colStatus]; ok && w != 0 {
			t.Errorf("status got a width override from +/-: %d, want 0 (untouched)", w)
		}

		for _, b := range e.ShortHelp() {
			if b.Help().Desc == "resize" {
				t.Error("ShortHelp still offers resize while the flex column is selected")
			}
		}
	})
}

// TestColumnEditor_Reset proves ctrl+r is a full restore -- order, hidden
// state and any resize all snap back to defaultColumnSettings(), including
// after a swap, a hide and a resize all landed first.
func TestColumnEditor_Reset(t *testing.T) {
	e := newTestEditor()

	// Three different kinds of mutation, so reset has something real to
	// undo. Navigation loops (rather than a fixed count of arrow presses)
	// keep this immune to exactly where the earlier swap left the cursor.
	e = press(t, e, "right", "enter", "right", "esc") // arm apps, swap right with active, disarm
	if idOf(e, 1) != colActive || idOf(e, 2) != colApps {
		t.Fatalf("test setup: order = %v, want active before apps", []columnID{idOf(e, 1), idOf(e, 2)})
	}

	for idOf(e, e.cursor) != colPeer {
		e = press(t, e, "right")
	}
	e = press(t, e, "space") // hide peer
	if !e.hidden[colPeer] {
		t.Fatal("test setup: peer not hidden")
	}

	for idOf(e, e.cursor) != colCWD {
		e = press(t, e, "left")
	}
	e = press(t, e, "+", "+") // resize cwd
	if e.widths[colCWD] == 0 {
		t.Fatal("test setup: cwd width override didn't take")
	}

	e = press(t, e, "ctrl+r")

	want := defaultColumnSettings()
	got := e.Result()
	if len(got) != len(want) {
		t.Fatalf("after reset: %d columns, want %d", len(got), len(want))
	}
	for i, s := range want {
		if got[i].ID != s.ID || got[i].Width != 0 {
			t.Errorf("after reset: column %d = %+v, want %+v", i, got[i], columnSetting{ID: s.ID})
		}
	}
	if e.armed {
		t.Error("reset must leave the editor unarmed")
	}
}

// TestColumnEditor_Esc covers both esc behaviours in one place, against the
// pull-based overlayResult contract (result()): while armed esc only
// disarms (the editor stays unfinished), and otherwise it finishes, and
// result()'s apply Cmd resolves to a columnsAppliedMsg carrying Result().
func TestColumnEditor_Esc(t *testing.T) {
	t.Run("armed: only disarms", func(t *testing.T) {
		e := newTestEditor()
		e = press(t, e, "right", "enter")
		e = press(t, e, "esc")
		if e.armed {
			t.Error("esc while armed left armed=true")
		}
		if finished, apply := e.result(); finished || apply != nil {
			t.Errorf("esc while armed: result() = (%v, %v), want (false, nil) -- it must not close the editor", finished, apply)
		}
	})

	t.Run("unarmed: finishes and applies", func(t *testing.T) {
		e := newTestEditor()
		e = press(t, e, "esc")

		finished, apply := e.result()
		if !finished {
			t.Fatal("esc unarmed did not finish the editor")
		}
		if apply == nil {
			t.Fatal("result() returned a nil apply cmd, want one that resolves to columnsAppliedMsg")
		}
		msg, ok := apply().(columnsAppliedMsg)
		if !ok {
			t.Fatalf("apply() = %#v, want columnsAppliedMsg", msg)
		}
		if want := e.Result(); !equalColumnSettings(msg.columns, want) {
			t.Errorf("columnsAppliedMsg.columns = %v, want %v", msg.columns, want)
		}
	})
}

func equalColumnSettings(a, b []columnSetting) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestColumnEditor_PreviewSeesOnlyVisibleColumns is the explicit contract
// item: the closure Model hands in must never be invoked with a hidden
// column in its layout, so the live preview can never render session data
// under a header the user just hid.
func TestColumnEditor_PreviewSeesOnlyVisibleColumns(t *testing.T) {
	var captured tableLayout
	capture := func(l tableLayout) string { captured = l; return idsPreview(l) }
	e := newColumnEditor(testStyles(), defaultColumnSettings(), true, defaultUsableWidth, capture)

	e = press(t, e, "right", "space") // hide apps
	e.View()

	for _, c := range captured.columns {
		if c.id == colApps {
			t.Fatal("preview was invoked with a hidden column in its layout")
		}
	}
	wantOrder := []columnID{colSession, colActive, colCWD, colPeer, colStatus}
	if len(captured.columns) != len(wantOrder) {
		t.Fatalf("preview layout has %d columns, want %d: %v", len(captured.columns), len(wantOrder), captured.columns)
	}
	for i, want := range wantOrder {
		if captured.columns[i].id != want {
			t.Errorf("preview layout[%d] = %s, want %s", i, captured.columns[i].id, want)
		}
	}
}

// TestColumnEditor_DiscoveryColumnsStartHidden confirms the catalog columns
// the shipped default doesn't show (age, windows, attached, machine) are
// present in the strip from the start, but hidden -- how a user finds them
// at all.
func TestColumnEditor_DiscoveryColumnsStartHidden(t *testing.T) {
	e := newTestEditor()
	for _, id := range []columnID{colAge, colWindows, colAttached, colMachine} {
		if !e.hidden[id] {
			t.Errorf("%s must start hidden (a discoverable catalog column)", id)
		}
		found := false
		for _, o := range e.order {
			found = found || o == id
		}
		if !found {
			t.Errorf("%s missing from the strip entirely -- it must be reachable to un-hide", id)
		}
	}
}

// TestColumnEditor_View_ContextualHelp proves the help line changes meaning
// (not vocabulary) with state: armed relabels move/arm/close, and the
// resize hint disappears while status is selected.
func TestColumnEditor_View_ContextualHelp(t *testing.T) {
	e := newTestEditor()
	if !strings.Contains(e.View(), "select") {
		t.Error("unarmed help should offer \"select\"")
	}

	e = press(t, e, "enter")
	view := e.View()
	if !strings.Contains(view, "swap") {
		t.Error("armed help should offer \"swap\", not \"select\"")
	}
	if strings.Contains(view, "arm to move") {
		t.Error("armed help should not still say \"arm to move\"")
	}
}

// TestColumnEditor_AbandonDiscards pins the way out that isn't esc: ctrl+z
// finishes the editor with NOTHING to apply, so a swap made before it never
// reaches Model, the list keeps the order it had, and no config is written.
// The esc subtest is the control -- same swap, applied -- so this cannot
// pass by the editor simply never finishing.
func TestColumnEditor_AbandonDiscards(t *testing.T) {
	press := func(e *columnEditor, keys ...string) {
		for _, k := range keys {
			var msg tea.KeyMsg
			switch k {
			case "right":
				msg = tea.KeyMsg{Type: tea.KeyRight}
			case "enter":
				msg = tea.KeyMsg{Type: tea.KeyEnter}
			case "esc":
				msg = tea.KeyMsg{Type: tea.KeyEsc}
			case "ctrl+z":
				msg = tea.KeyMsg{Type: tea.KeyCtrlZ}
			default:
				t.Fatalf("unknown key %q", k)
			}
			e.Update(msg)
		}
	}
	open := func() *columnEditor {
		return newColumnEditor(testStyles(), defaultColumnSettings(), true, defaultUsableWidth, func(tableLayout) string { return "" })
	}

	t.Run("ctrl+z after a swap finishes with nil apply", func(t *testing.T) {
		e := open()
		press(e, "right", "enter", "right", "ctrl+z") // arm apps, swap it right, abandon
		finished, apply := e.result()
		if !finished {
			t.Fatal("result() finished = false after ctrl+z, want true (the editor must close)")
		}
		if apply != nil {
			t.Fatal("result() apply != nil after ctrl+z, want nil (nothing must be applied)")
		}
	})

	t.Run("control: esc after the same swap applies it", func(t *testing.T) {
		e := open()
		press(e, "right", "enter", "right", "enter", "esc")
		finished, apply := e.result()
		if !finished || apply == nil {
			t.Fatalf("result() = (%v, %v), want (true, non-nil) -- esc must apply", finished, apply != nil)
		}
		got := apply().(columnsAppliedMsg).columns
		if got[1].ID != colActive || got[2].ID != colApps {
			t.Errorf("applied order = %v, want apps and active swapped", got)
		}
	})
}

// TestColumnEditor_StripNeverWiderThanPopup pins the bug the shelf exists
// for: whatever is hidden, the strip lays out the visible columns at the
// preview's width and is exactly that wide -- never squeezed, never past the
// popup. Before this, all ten catalog columns shared the strip and status
// collapsed to a stub while "windows" truncated to "wi".
func TestColumnEditor_StripNeverWiderThanPopup(t *testing.T) {
	e := newColumnEditor(testStyles(), defaultColumnSettings(), true, 112, func(tableLayout) string { return "" })
	for _, hide := range [][]string{{}, {"right", "space"}, {"right", "space", "space"}, {"down", "space", "space", "space"}} {
		e.reset(defaultColumnSettings())
		e = press(t, e, hide...)
		layout := layoutColumns(e.visibleColumns(), e.usable())
		if w := lipgloss.Width(e.renderStrip(layout)); w != e.usable() {
			t.Errorf("after %v: strip width = %d, want exactly usable %d", hide, w, e.usable())
		}
		for _, line := range strings.Split(e.View(), "\n") {
			if w := lipgloss.Width(line); w > e.usable() {
				t.Errorf("after %v: a View line is %d wide, past usable %d: %q", hide, w, e.usable(), line)
			}
		}
	}
}

// TestColumnEditor_ShelfAddsAColumn is the discovery path: the four catalog
// columns not in the shipped table start on the shelf; ↓ reaches it, ←/→
// selects a chip, space shows it -- it appears in the strip and in Result().
func TestColumnEditor_ShelfAddsAColumn(t *testing.T) {
	e := newColumnEditor(testStyles(), defaultColumnSettings(), true, 112, func(tableLayout) string { return "" })
	if e.row != rowStrip {
		t.Fatalf("opens on row %d, want the strip", e.row)
	}
	e = press(t, e, "down") // shelf
	if e.row != rowShelf || idOf(e, e.shelfCursor) != colAge {
		t.Fatalf("↓ landed on row %d cursor %s, want shelf on age (first hidden)", e.row, idOf(e, e.shelfCursor))
	}
	e = press(t, e, "right", "space") // windows -> shown
	if e.hidden[colWindows] {
		t.Fatal("space on the shelf did not show the chip")
	}
	if e.row != rowShelf {
		t.Errorf("row after adding = %d, want to stay on the shelf to add more", e.row)
	}
	var ids []columnID
	for _, s := range e.Result() {
		ids = append(ids, s.ID)
	}
	if ids[len(ids)-1] != colWindows {
		t.Errorf("Result() = %v, want windows appended after the shipped six", ids)
	}
	if !strings.Contains(e.View(), "win") {
		t.Error("View() does not show the added column in the strip")
	}
}

// TestColumnEditor_PopupWidth: the width row adjusts in steps, clamps, and
// rides out on the apply msg; ctrl+r puts it back to the default.
func TestColumnEditor_PopupWidth(t *testing.T) {
	e := newColumnEditor(testStyles(), defaultColumnSettings(), true, 112, func(tableLayout) string { return "" })
	e = press(t, e, "down", "down") // strip -> shelf -> width
	if e.row != rowWidth {
		t.Fatalf("row = %d, want the width row", e.row)
	}
	e = press(t, e, "+", "+", "-", "right")
	if e.popupWidth != 112+2*popupWidthStep {
		t.Errorf("popupWidth = %d, want %d (+,+,-,right = +2 steps)", e.popupWidth, 112+2*popupWidthStep)
	}
	for i := 0; i < 100; i++ {
		e = press(t, e, "+")
	}
	if e.popupWidth != popupWidthMax {
		t.Errorf("popupWidth after 100 '+' = %d, want clamped to %d", e.popupWidth, popupWidthMax)
	}
	e = press(t, e, "esc")
	_, apply := e.result()
	if got := apply().(columnsAppliedMsg).popupWidth; got != popupWidthMax {
		t.Errorf("applied popupWidth = %d, want %d", got, popupWidthMax)
	}

	e = newColumnEditor(testStyles(), defaultColumnSettings(), true, 140, func(tableLayout) string { return "" })
	e = press(t, e, "ctrl+r")
	if e.popupWidth != popupWidthDefault {
		t.Errorf("ctrl+r left popupWidth at %d, want %d", e.popupWidth, popupWidthDefault)
	}
}
