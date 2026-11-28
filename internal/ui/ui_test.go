package ui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

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

// The fill row starts solid, ←→ flips it, ctrl+r puts it back, and esc
// carries the choice out with the rest of the settings.
func TestEditorFillRowTogglesResetsAndApplies(t *testing.T) {
	e := testEditor(112)
	fill := func() string { return ansi.Strip(e.renderFillRow(112)) }
	if e.transparent || !strings.Contains(fill(), "solid") {
		t.Fatalf("a new editor is not solid: %q", fill())
	}
	e.row = rowFill
	press(e, keyOf(tea.KeyRight))
	if !e.transparent || !strings.Contains(fill(), "transparent") {
		t.Fatalf("→ did not make it transparent: %q", fill())
	}
	press(e, keyOf(tea.KeyCtrlR))
	if e.transparent {
		t.Error("ctrl+r left it transparent")
	}

	e = testEditor(112).withPopupLook(true, true)
	press(e, keyOf(tea.KeyEsc))
	_, apply := e.result()
	if apply == nil {
		t.Fatal("esc applied nothing")
	}
	if msg := apply().(editorAppliedMsg); !msg.transparent || !msg.borderless {
		t.Errorf("esc carried out transparent %v, borderless %v", msg.transparent, msg.borderless)
	}
}

// The border row starts on (tmux's own border), ←→ flips it, ctrl+r puts it
// back. Its hint, the longest of the popup rows, still fits the narrowest
// editor the default popup gives: 112 less tmux's border.
func TestEditorBorderRowTogglesAndResets(t *testing.T) {
	e := testEditor(110)
	border := func() string { return ansi.Strip(e.renderBorderRow(110)) }
	if e.borderless || !strings.Contains(border(), "on") {
		t.Fatalf("a new editor has no border: %q", border())
	}
	e.row = rowBorder
	press(e, keyOf(tea.KeyRight))
	if !e.borderless || !strings.Contains(border(), "off") {
		t.Fatalf("→ did not turn the border off: %q", border())
	}
	e.transparent = true // the widest value on the fill row
	for name, row := range map[string]string{"border": border(), "fill": ansi.Strip(e.renderFillRow(110)), "width": ansi.Strip(e.renderWidthRow(110))} {
		if w := lipgloss.Width(row); w > 110 {
			t.Errorf("the %s row is %d wide in a 110 editor: %q", name, w, row)
		}
		if !strings.Contains(row, nextOpenHint) {
			t.Errorf("the %s row does not say when it applies: %q", name, row)
		}
	}
	press(e, keyOf(tea.KeyCtrlR))
	if e.borderless {
		t.Error("ctrl+r left the border off")
	}
}

// Transparent survives an apply and a restart, and the editor reopens on it.
// A config written before it existed loads solid and saves back without it.
func TestTransparentPersistsAndOldConfigsAreUntouched(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path, err := configPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"icons":"ascii","hosts":["box"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	old, err := LoadConfig()
	if err != nil || old.Transparent {
		t.Fatalf("old config: transparent %v, err %v", old.Transparent, err)
	}
	if err := SaveConfig(old); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); strings.Contains(string(data), "transparent") || strings.Contains(string(data), "borderless") {
		t.Errorf("save added a key to an old config:\n%s", data)
	}

	m := New()
	m, _ = send(m, editorAppliedMsg{columns: m.columns, popupWidth: popupWidthDefault, transparent: true, borderless: true, theme: m.cfg.Theme, icons: m.cfg.Icons, hosts: m.cfg.Hosts})
	if !m.cfg.Transparent || !m.cfg.Borderless {
		t.Fatal("apply did not set them")
	}
	restarted := New()
	if !restarted.cfg.Transparent || !restarted.cfg.Borderless || restarted.cfg.Icons != glyphsASCII || !slices.Equal(restarted.cfg.Hosts, []string{"box"}) {
		t.Fatalf("after restart: %+v", restarted.cfg)
	}
	restarted, _ = send(restarted, keyOf(tea.KeyCtrlE))
	if e, ok := restarted.overlay.(*columnEditor); !ok || !e.transparent || !e.borderless {
		t.Error("the editor did not reopen on transparent and borderless")
	}
}

// A save that fails keeps what the file still has: fill and border do nothing
// in this session, so ctrl+e must reopen on the truth, not on the unsaved
// choice.
func TestFailedSaveKeepsTheSavedPopupLook(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := New()
	apply := func(on bool) {
		m, _ = send(m, editorAppliedMsg{columns: m.columns, popupWidth: popupWidthDefault, transparent: on, borderless: on, theme: m.cfg.Theme, icons: m.cfg.Icons, hosts: m.cfg.Hosts})
	}
	apply(true)

	// A file where the config directory should be: the save fails.
	blocked := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocked, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", blocked)
	apply(false)
	if m.err == nil {
		t.Fatal("the save did not fail")
	}
	if !m.cfg.Transparent || !m.cfg.Borderless {
		t.Error("a failed save left the unsaved choice for ctrl+e to reopen on")
	}
}

// What display-popup is told for each fill and border. Solid is the palette's
// own background, which is what makes a light palette readable on a dark
// terminal; a palette with no background of its own leaves the popup to tmux.
func TestPopupFlags(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // auto without Omarchy: the terminal palette
	light, dark := builtinPalettes[paletteLight], builtinPalettes[paletteDark]
	cfg := func(p paletteSource, transparent, borderless bool) Config {
		return Config{Theme: ThemeConfig{Palette: p}, Transparent: transparent, Borderless: borderless}
	}
	for _, tc := range []struct {
		name   string
		cfg    Config
		border string
		want   string
	}{
		{"solid light keeps the border", cfg(paletteLight, false, false), "fg=#8bcbb1",
			fmt.Sprintf("-s bg=%s,fg=%s -S fg=#8bcbb1,bg=%s", light.Background, light.Foreground, light.Background)},
		{"solid dark, no border style", cfg(paletteDark, false, false), "",
			fmt.Sprintf("-s bg=%s,fg=%s -S bg=%s", dark.Background, dark.Foreground, dark.Background)},
		{"solid light, flush edge", cfg(paletteLight, false, true), "fg=#8bcbb1",
			fmt.Sprintf("-b padded -s bg=%s,fg=%s -S bg=%s", light.Background, light.Foreground, light.Background)},
		{"transparent keeps the border", cfg(paletteLight, true, false), "fg=red bg=blue",
			"-s bg=terminal -S fg=red,bg=blue,bg=terminal"},
		{"transparent, flush edge", cfg(paletteDark, true, true), "fg=red",
			"-b padded -s bg=terminal -S bg=terminal"},
		{"terminal palette is tmux's own popup", cfg(paletteTerminal, false, false), "fg=red", ""},
		{"auto without Omarchy is tmux's own popup", cfg(paletteAuto, false, false), "fg=red", ""},
		{"terminal palette, flush edge in popup-style", cfg(paletteTerminal, false, true), "fg=red",
			"-b padded -S bg=colour236,fg=white"},
	} {
		got := strings.Join(PopupFlags(tc.cfg, "bg=colour236 fg=white", tc.border), " ")
		if got != tc.want {
			t.Errorf("%s:\n got  %q\n want %q", tc.name, got, tc.want)
		}
		if strings.Contains(strings.Join(PopupFlags(tc.cfg, "bg=colour236 fg=white", tc.border), "\x00"), " ") {
			t.Errorf("%s: a flag carries a space, and the shell splits on those: %q", tc.name, got)
		}
	}
	if got := PopupFlags(cfg(paletteTerminal, false, true), "", ""); !slices.Equal(got, []string{"-b", "padded", "-S", "default"}) {
		t.Errorf("flush edge with no popup-style: %q", got)
	}
}

// The host column names this machine for a local session, so it is never a
// column of blanks, and a remote session's own host still wins.
func TestHostColumnNamesTheLocalMachine(t *testing.T) {
	_, styles := restyle(ThemeConfig{Palette: paletteDark}, glyphsASCII)
	render := func(s session.Session) string {
		return ansi.Strip(columnCatalog[colHost].render(cell{session: s, styles: styles}))
	}
	if got := render(session.Session{Name: "work"}); got == "" || got != localMachine {
		t.Errorf("a local session's host cell is %q, want %q", got, localMachine)
	}
	if got := render(session.Session{Name: "work", Host: "box"}); got != "box" {
		t.Errorf("a remote session's host cell is %q", got)
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

// A session that needs you says what for, and blinks without moving or going
// blank: a popup that cannot redraw freezes on one phase, and either one must
// still read.
func TestNeedsYouSaysWhatForAndBlinksInPlace(t *testing.T) {
	was := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256) // a test has no terminal, and no styling to compare without one
	t.Cleanup(func() { lipgloss.SetColorProfile(was) })

	styles := New().styles
	sp := spinner.New()
	s := session.Session{Name: "api", Agent: true, Apps: []string{"claude"}, State: session.StateNotify, Notice: "approve Bash: make install"}
	on, off := time.UnixMilli(0), time.UnixMilli(500)

	for name, render := range map[string]func(time.Time) string{
		"status": func(now time.Time) string { return renderStatus(styles, now, s) },
		"icon":   func(now time.Time) string { return renderIcons(s, sp, now) },
	} {
		a, b := render(on), render(off)
		if a == b {
			t.Errorf("%s: both phases are drawn the same, so nothing blinks", name)
		}
		if ansi.Strip(a) != ansi.Strip(b) || strings.TrimSpace(ansi.Strip(a)) == "" {
			t.Errorf("%s: phases must differ in style only: %q and %q", name, ansi.Strip(a), ansi.Strip(b))
		}
	}

	if got := ansi.Strip(renderStatus(styles, on, s)); got != "needs you · approve Bash: make install" {
		t.Errorf("status with a notice: %q", got)
	}
	s.Notice = "" // it only rang the bell
	if got := ansi.Strip(renderStatus(styles, on, s)); got != "needs you" {
		t.Errorf("status without a notice: %q", got)
	}
}

// Why an action failed stays on screen through the reloads that follow it,
// which come every second, and goes with the next key press.
func TestActionErrorOutlivesAReload(t *testing.T) {
	m, _ := send(New(), tea.WindowSizeMsg{Width: 112, Height: 26})
	m, _ = send(m, reloadMsg{sessions: []session.Session{{Name: "alpha"}}})
	m, _ = send(m, doAndQuit(func() error { return errors.New("no route to host") })())
	m, _ = send(m, reloadMsg{sessions: []session.Session{{Name: "alpha"}}})
	if !strings.Contains(m.View(), "no route to host") {
		t.Errorf("the reason was gone after one reload:\n%s", m.View())
	}
	m, _ = send(m, keyOf(tea.KeyDown))
	if strings.Contains(m.View(), "no route to host") {
		t.Error("the reason outlived the next key press")
	}
}

// Without Omarchy, auto is the terminal's own colours: colour numbers the
// terminal resolves, never hex. A chip there has no background colour to name
// for its text, so it is drawn in reverse video, and the selection background
// is a colour number too.
func TestAutoIsTheTerminalPaletteWithoutOmarchy(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // no omarchy-theme-color to find
	p := loadPalette(paletteAuto)
	if p != builtinPalettes[paletteTerminal] {
		t.Fatalf("auto without Omarchy is %+v", p)
	}
	for _, name := range p.slotNames() {
		if c, _ := p.slot(name); strings.HasPrefix(string(c), "#") {
			t.Errorf("slot %s is the fixed colour %s", name, c)
		}
	}
	if !p.chip(p.Red).GetReverse() {
		t.Error("a chip on the terminal palette is not reverse video")
	}
	if builtinPalettes[paletteDark].chip("#F38BA8").GetReverse() {
		t.Error("a chip on a hex palette became reverse video")
	}
	if got := bgSGR(p.Muted); got != "\x1b[48;5;8m" {
		t.Errorf("selection background is %q", got)
	}
	if !slices.Contains(palettes, paletteTerminal) {
		t.Error("terminal is not a choice on the theme row")
	}
}

// The frame carries its own background, so a palette change shows on the next
// frame: tmux fills the popup only when it opens. Every line starts on the
// ground and returns to it after each reset. A palette that leaves its
// background to the terminal, and a popup opened transparent, paint nothing.
func TestTheFramePaintsItsOwnGround(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Cleanup(func() { restyle(ThemeConfig{Palette: paletteDark}, glyphsASCII) })

	restyle(ThemeConfig{Palette: paletteLight}, glyphsASCII)
	light := "\x1b[48;2;239;241;245m\x1b[38;2;76;79;105m"
	if ground != light {
		t.Fatalf("the light palette's ground is %q", ground)
	}
	got := paintGround("a\x1b[0mb\nc")
	if want := light + "a\x1b[0m" + light + "b\x1b[0m\n" + light + "c\x1b[0m"; got != want {
		t.Errorf("painted frame:\n got  %q\n want %q", got, want)
	}

	m, _ := send(New(), tea.WindowSizeMsg{Width: 112, Height: 26})
	restyle(ThemeConfig{Palette: paletteLight}, glyphsASCII) // as the editor's preview does
	for i, line := range strings.Split(m.View(), "\n") {
		if !strings.HasPrefix(line, light) {
			t.Fatalf("line %d of the view is not on the ground: %q", i, line)
		}
	}
	m.transparentAtOpen = true
	if strings.Contains(m.View(), light) {
		t.Error("a popup opened transparent painted a background")
	}

	restyle(ThemeConfig{Palette: paletteTerminal}, glyphsASCII)
	if ground != "" || paintGround("a") != "a" {
		t.Errorf("the terminal palette painted a ground: %q", ground)
	}
}
