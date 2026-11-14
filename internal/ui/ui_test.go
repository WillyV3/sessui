package ui

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/WillyV3/sessui/internal/session"
)

// New reads the config and the editor's apply path writes it and sets a tmux
// option. Nothing here may reach the real ones: the config home is a temp
// dir, and tmux is pointed at a socket directory with no server in it.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("/tmp", "sessui-ui")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Setenv("HOME", dir) // and no ~/.ssh/config to discover hosts from
	os.Setenv("XDG_CONFIG_HOME", dir)
	os.Setenv("TMUX_TMPDIR", dir)
	os.Unsetenv("TMUX")
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// testEditor is the settings editor over the default columns, in the ASCII
// glyph set so a row's text is the same on every machine.
func testEditor(width int) *columnEditor {
	_, styles := restyle(ThemeConfig{Palette: paletteDark}, glyphsASCII)
	return newColumnEditor(styles, defaultColumnSettings(), true, width, popupWidthDefault,
		func(Styles, tableLayout) string { return "" })
}

func TestLayoutFillsTheWidthItIsGiven(t *testing.T) {
	cols := resolveColumns(defaultColumnSettings(), true)
	for _, width := range []int{108, 160, 240} {
		if got := layoutColumns(cols, width).width(); got != width {
			t.Errorf("layout at %d is %d wide", width, got)
		}
	}
	// Too narrow for the fixed columns: the flex column stops at its minimum
	// instead of going negative, which lipgloss would panic on.
	for i, w := range layoutColumns(cols, 10).widths {
		if w < cols[i].minWidth || w < 0 {
			t.Errorf("column %s is %d wide in a 10-cell window", cols[i].id, w)
		}
	}
}

func TestMarqueeHoldsAtBothEndsAndNeverJumps(t *testing.T) {
	const overflow = 7
	seen := map[int]bool{}
	prev := 0
	for frame := range 200 {
		off := marqueeOffset(frame, overflow)
		if off < 0 || off > overflow {
			t.Fatalf("frame %d: offset %d outside 0..%d", frame, off, overflow)
		}
		if d := off - prev; d < -1 || d > 1 {
			t.Fatalf("frame %d: jumped %d cells", frame, d)
		}
		seen[off], prev = true, off
	}
	if len(seen) != overflow+1 {
		t.Errorf("reached %d of %d offsets", len(seen), overflow+1)
	}
	for frame := range 5 {
		if off := marqueeOffset(frame, overflow); off != 0 {
			t.Errorf("frame %d: left the start early (offset %d)", frame, off)
		}
	}
	if off := marqueeOffset(3, 0); off != 0 {
		t.Errorf("nothing hidden, yet offset %d", off)
	}
}

func TestCountText(t *testing.T) {
	for _, tc := range []struct {
		total, shown int
		filtering    bool
		want         string
	}{
		{0, 0, false, "0 sessions"},
		{1, 1, false, "1 session"},
		{12, 12, false, "12 sessions"},
		{12, 3, true, "3 of 12 sessions"},
		{12, 12, true, "12 sessions"}, // a filter that hides nothing is not worth a word
	} {
		if got := countText(tc.total, tc.shown, tc.filtering); got != tc.want {
			t.Errorf("countText(%d, %d, %v) = %q, want %q", tc.total, tc.shown, tc.filtering, got, tc.want)
		}
	}
}

// Hiding a column and showing it again puts it back where it was.
func TestEditorHideThenShowRestoresTheColumns(t *testing.T) {
	e := testEditor(112)
	want := e.Result()

	hiddenID := e.order[e.cursor]
	e.toggle()
	for _, s := range e.Result() {
		if s.ID == hiddenID {
			t.Fatalf("%s is still in the result after hiding it", hiddenID)
		}
	}

	e.row = rowShelf
	e.toggle()
	if got := e.Result(); !reflect.DeepEqual(got, want) {
		t.Errorf("after hide then show:\n got %v\nwant %v", got, want)
	}
}

// A fleet is wider than a popup. Wherever the cursor is, its chip is drawn
// and the row still fits: a cursor on an undrawn chip toggles the unseen.
func TestHostRowKeepsTheCursorInView(t *testing.T) {
	const width = 60
	var hosts []string
	for i := range 12 {
		hosts = append(hosts, fmt.Sprintf("host-%02d", i))
	}
	e := testEditor(width).withHosts(hosts, nil, nil)
	e.row = rowHosts

	check := func() {
		t.Helper()
		row := ansi.Strip(e.renderHostsRow(width))
		if focused := e.hostOrder()[e.hostCursor]; !strings.Contains(row, focused) {
			t.Errorf("cursor on %s, row is %q", focused, row)
		}
		if w := lipgloss.Width(row); w > width {
			t.Errorf("row is %d wide in a %d window: %q", w, width, row)
		}
	}
	for range hosts {
		check()
		e.hostMove(1)
	}
	for range hosts {
		check()
		e.hostMove(-1)
	}
}

// An unreachable host says why on the chip that has the cursor, and only
// there: the other chips keep the row's ordinary hint.
func TestFocusedDeadHostSaysWhy(t *testing.T) {
	const width = 112
	e := testEditor(width).withHosts([]string{"up", "down"}, []string{"up", "down"}, nil)
	e.row = rowHosts
	e.hostState = map[string]session.RemoteHost{
		"down": {Alias: "down", Err: errors.New("connect to host 10.0.0.9 port 22: No route to host")},
	}

	e.hostCursor = 1
	if row := ansi.Strip(e.renderHostsRow(width)); !strings.Contains(row, "No route to host") {
		t.Errorf("cursor on the dead host, row is %q", row)
	}
	e.hostCursor = 0
	if row := ansi.Strip(e.renderHostsRow(width)); !strings.Contains(row, "2 watched") || strings.Contains(row, "No route") {
		t.Errorf("cursor on the live host, row is %q", row)
	}

	// An error longer than the row must not push the chips out of it.
	e.hostState["down"] = session.RemoteHost{Alias: "down", Err: errors.New(strings.Repeat("no route ", 40))}
	e.hostCursor = 1
	row := ansi.Strip(e.renderHostsRow(width))
	if !strings.Contains(row, "down") || lipgloss.Width(row) > width {
		t.Errorf("long error: row is %d wide: %q", lipgloss.Width(row), row)
	}
}

// The fast tmux-only pass knows nothing of peers or agents. One that arrives
// after the full load must not replace it.
func TestLatePartialReloadIsDropped(t *testing.T) {
	var model any = New()
	update := func(msg reloadMsg) Model {
		next, _ := model.(Model).Update(msg)
		model = next
		return next.(Model)
	}

	full := []session.Session{{Name: "alpha", PeerName: "keeper", PeerUp: true}, {Name: "beta"}}
	if m := update(reloadMsg{sessions: full}); len(m.list.Items()) != 2 || !m.showPeer {
		t.Fatalf("full load: %d rows, showPeer %v", len(m.list.Items()), m.showPeer)
	}
	if m := update(reloadMsg{sessions: []session.Session{{Name: "alpha"}}, partial: true}); len(m.list.Items()) != 2 || !m.showPeer {
		t.Errorf("late partial replaced the full load: %d rows, showPeer %v", len(m.list.Items()), m.showPeer)
	}
	// A failed reload keeps the rows it had and carries the error.
	m := update(reloadMsg{err: errors.New("tmux went away")})
	if len(m.list.Items()) != 2 || m.err == nil {
		t.Errorf("failed reload: %d rows, err %v", len(m.list.Items()), m.err)
	}
}

func TestDumpRowsIsOneLinePerSession(t *testing.T) {
	out := ansi.Strip(DumpRows([]session.Session{{Name: "alpha"}, {Name: "beta"}}))
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 2 || !strings.Contains(out, "alpha") || !strings.Contains(out, "beta") {
		t.Errorf("dump is %q", out)
	}
}

func keyOf(t tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: t} }
func typed(s string) tea.KeyMsg      { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func press(e *columnEditor, keys ...tea.KeyMsg) {
	for _, k := range keys {
		e.Update(k)
	}
}

// enter arms a column, arrows carry it, and esc means two things: disarm
// while armed, apply otherwise.
func TestEditorMovesAColumnAndAppliesOnEsc(t *testing.T) {
	e := testEditor(112)
	before := e.Result()

	press(e, keyOf(tea.KeyEnter), keyOf(tea.KeyRight))
	moved := e.Result()
	if moved[0].ID != before[1].ID || moved[1].ID != before[0].ID {
		t.Fatalf("armed move: got %v from %v", moved, before)
	}

	press(e, keyOf(tea.KeyEsc))
	if done, _ := e.result(); done || e.armed {
		t.Fatalf("first esc: done %v, armed %v; want it to disarm only", done, e.armed)
	}
	press(e, keyOf(tea.KeyRight)) // disarmed: the cursor moves, the column stays
	if got := e.Result(); !reflect.DeepEqual(got, moved) {
		t.Errorf("unarmed arrow moved a column: %v", got)
	}

	press(e, keyOf(tea.KeyEsc))
	done, apply := e.result()
	if !done || apply == nil {
		t.Fatalf("second esc: done %v, apply nil %v", done, apply == nil)
	}
	if msg := apply().(editorAppliedMsg); !reflect.DeepEqual(msg.columns, moved) {
		t.Errorf("applied %v, want %v", msg.columns, moved)
	}
}

func TestEditorResizeStopsAtTheMinimumAndSkipsFlex(t *testing.T) {
	e := testEditor(112) // cursor on session: 22 wide, 8 at least
	width := func() int { return e.Result()[0].Width }

	press(e, typed("+"))
	if width() != 23 {
		t.Errorf("after +: width %d, want 23", width())
	}
	press(e, typed("-"))
	if width() != 0 {
		t.Errorf("back at the default: width %d, want 0 so nothing is persisted", width())
	}
	for range 40 {
		press(e, typed("-"))
	}
	if width() != 8 {
		t.Errorf("shrunk past the minimum: width %d, want 8", width())
	}

	for range len(e.order) {
		if e.selectionIsFlex() {
			break
		}
		press(e, keyOf(tea.KeyRight))
	}
	if !e.selectionIsFlex() {
		t.Fatal("no flex column on the strip")
	}
	flex := e.order[e.cursor]
	press(e, typed("+"), typed("+"))
	if e.widths[flex] != 0 {
		t.Errorf("flex column %s took a width of %d", flex, e.widths[flex])
	}
}

func TestEditorRowsPopupWidthAndReset(t *testing.T) {
	e := testEditor(112)
	press(e, keyOf(tea.KeyUp))
	if e.row != rowStrip {
		t.Errorf("up from the first row went to row %d", e.row)
	}

	press(e, keyOf(tea.KeyDown), keyOf(tea.KeyDown))
	if e.row != rowWidth {
		t.Fatalf("two rows down is row %d, want the popup width row", e.row)
	}
	press(e, keyOf(tea.KeyRight))
	if e.popupWidth != popupWidthDefault+popupWidthStep {
		t.Errorf("popup width %d after one step", e.popupWidth)
	}
	for range 100 {
		press(e, keyOf(tea.KeyRight))
	}
	if e.popupWidth != popupWidthMax {
		t.Errorf("popup width %d, want it to stop at %d", e.popupWidth, popupWidthMax)
	}

	for range 20 {
		press(e, keyOf(tea.KeyDown))
	}
	if e.row != rowLast {
		t.Errorf("down from the last row went to row %d", e.row)
	}

	press(e, keyOf(tea.KeyCtrlR))
	if e.popupWidth != popupWidthDefault || e.row != rowStrip || !reflect.DeepEqual(e.Result(), defaultColumnSettings()) {
		t.Errorf("reset: width %d, row %d, columns %v", e.popupWidth, e.row, e.Result())
	}
}

func TestEditorAbandonAppliesNothing(t *testing.T) {
	e := testEditor(112)
	press(e, keyOf(tea.KeySpace), keyOf(tea.KeyCtrlZ))
	if done, apply := e.result(); !done || apply != nil {
		t.Errorf("ctrl+z: done %v, apply nil %v", done, apply == nil)
	}
}

// loaded is the model as it stands once the window is sized and tmux has
// answered: names[0] is the most recently active, and so the first row.
func loaded(names ...string) Model {
	sessions := make([]session.Session, len(names))
	for i, n := range names {
		sessions[i] = session.Session{Name: n, Activity: time.Unix(int64(1000-i), 0)}
	}
	var m tea.Model = New()
	m, _ = m.Update(tea.WindowSizeMsg{Width: 112, Height: 26})
	m, _ = m.Update(reloadMsg{sessions: sessions})
	return m.(Model)
}

func send(m Model, msg tea.Msg) (Model, tea.Cmd) {
	next, cmd := m.Update(msg)
	return next.(Model), cmd
}

func TestArrowsMoveTheSelection(t *testing.T) {
	m := loaded("alpha", "beta", "gamma")
	if got := m.selectedName(); got != "alpha" {
		t.Fatalf("first row is %q", got)
	}
	m, _ = send(m, keyOf(tea.KeyDown))
	if got := m.selectedName(); got != "beta" {
		t.Errorf("after down: %q", got)
	}
	// A reload reorders the list; the selection follows the name, not the row.
	m, _ = send(m, reloadMsg{sessions: []session.Session{
		{Name: "gamma", Activity: time.Unix(3000, 0)}, {Name: "alpha", Activity: time.Unix(2000, 0)}, {Name: "beta"},
	}})
	if got := m.selectedName(); got != "beta" {
		t.Errorf("after a reload that moved it: %q", got)
	}
	m, _ = send(m, keyOf(tea.KeyUp))
	if got := m.selectedName(); got != "alpha" {
		t.Errorf("after up: %q", got)
	}
}

func isQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

func TestEscQuitsAndCtrlCQuitsThroughAnOverlay(t *testing.T) {
	m := loaded("alpha")
	if _, cmd := send(m, keyOf(tea.KeyEsc)); !isQuit(cmd) {
		t.Error("esc on the list did not quit")
	}

	m, _ = send(m, keyOf(tea.KeyCtrlE))
	if m.overlay == nil {
		t.Fatal("ctrl+e did not open settings")
	}
	if _, cmd := send(m, keyOf(tea.KeyCtrlC)); !isQuit(cmd) {
		t.Error("ctrl+c did not quit with settings open")
	}
}

func TestSettingsArePersisted(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // this one writes a config
	m := loaded("alpha")
	m, _ = send(m, keyOf(tea.KeyCtrlE))
	m, _ = send(m, keyOf(tea.KeySpace)) // hide the first column: session
	editor := m.overlay.(*columnEditor)
	m, _ = send(m, keyOf(tea.KeyEsc))
	_, apply := editor.result()
	m, _ = send(m, apply())

	hasSession := func(cols []columnSetting) bool {
		return slices.ContainsFunc(cols, func(c columnSetting) bool { return c.ID == colSession })
	}
	if hasSession(m.columns) {
		t.Errorf("hidden column still in the table: %v", m.columns)
	}
	saved, err := LoadConfig()
	if err != nil || hasSession(saved.Columns) {
		t.Errorf("config on disk: %v, err %v", saved.Columns, err)
	}
}

// The peer column is not shown, or offered, while no cp3 peer is up. Opening
// settings in that state and applying must not delete it from the config:
// it has to come back when a peer does.
func TestApplyingSettingsWithNoPeerUpKeepsThePeerColumn(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := loaded("alpha")
	m, _ = send(m, keyOf(tea.KeyCtrlE))
	editor := m.overlay.(*columnEditor)
	m, _ = send(m, keyOf(tea.KeyEsc))
	_, apply := editor.result()
	m, _ = send(m, apply())

	if !reflect.DeepEqual(m.columns, defaultColumnSettings()) {
		t.Errorf("columns after an untouched apply: %v", m.columns)
	}
}
