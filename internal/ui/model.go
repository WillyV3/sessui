// Package ui is the bubbletea layer for sessui. bubbles/list.Model does
// cursor movement, filtering, pagination, help and the status bar; this
// package only adds the live "active" tick, background reload, and the
// three tmux actions (switch, rename, kill) list doesn't know about.
package ui

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/willyv3/sessui/internal/session"
)

type mode int

const (
	modeList mode = iota
	modeRename
	modeConfirmKill
)

type tickMsg time.Time
type reloadTickMsg time.Time
type marqueeTickMsg time.Time

type reloadMsg struct {
	sessions []session.Session
	err      error
}

// Custom actions the list doesn't provide, surfaced in its own help view
// via AdditionalShortHelpKeys/AdditionalFullHelpKeys and handled below.
var (
	enterKey  = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "switch/create"))
	renameKey = key.NewBinding(key.WithKeys("ctrl+r"), key.WithHelp("ctrl+r", "rename"))
	killKey   = key.NewBinding(key.WithKeys("ctrl+x"), key.WithHelp("ctrl+x", "kill"))
)

var appStyle = lipgloss.NewStyle().Padding(1, 2)

type Model struct {
	list     list.Model
	spinner  spinner.Model
	delegate *rowDelegate // same pointer handed to list.New, so its spinner
	// field can be kept in sync with Model's on every spinner.TickMsg.

	mode         mode
	renameInput  textinput.Model
	renameTarget string
	killTarget   string

	err           error
	home          string
	width, height int
	styles        Styles
	// columns is the user's table configuration; showPeer is the runtime
	// fact (any cp3 peer in use) that can hide the peer column regardless of
	// it. Both feed relayout, which is the only writer of delegate.layout.
	columns  []columnSetting
	showPeer bool
	// marqueeFrame advances the selected row's scroll; marqueeFor is the
	// session name it's counting for, so the frame resets to 0 (back to the
	// row's start) the instant the selection moves -- keyed by name, not
	// index, because a reload re-sorts and shifts indexes every 2s.
	marqueeFrame int
	marqueeFor   string
}

// New builds the initial model: reads the theme palette and the user's
// home directory once, up front.
func New() Model {
	home, _ := os.UserHomeDir()
	palette := loadPalette()
	applyTheme(palette)
	styles := newStyles(palette)
	sp := spinner.New(spinner.WithSpinner(spinner.MiniDot))
	delegate := &rowDelegate{styles: styles, home: home, spinner: sp}

	l := list.New(nil, delegate, 0, 0)
	l.Title = "tmux sessions"
	l.Styles = themedListStyles(palette)
	// The title bar and the default filter bar are both replaced by
	// Model's own header line (renderHeader when unfiltered, the same
	// FilterInput when filtering -- see headerLine), so hide both of
	// list's built-in ones to avoid rendering that line twice.
	l.SetShowTitle(false)
	l.SetShowFilter(false)
	// The count moves to Model's own line ABOVE the column header (see
	// countLine/View), so hide list's built-in status bar too.
	l.SetShowStatusBar(false)
	// Tolerate a trailing/leading space in the typed filter -- "jim "
	// should still match "jim" instead of matching nothing and then, on
	// enter, creating a new session literally named "jim ".
	l.Filter = trimmedFilter
	// The built-in help footer's hints don't match this app: type-to-filter
	// overrides "/", esc quits (not "cancel filter"), and letters can't be
	// vim nav because they feed the filter -- and in filter state list
	// hardcodes its own filter help. All misleading, so hide it and render
	// one honest line ourselves (see helpLine / footerLine).
	l.SetShowHelp(false)
	// The list opens Unfiltered so the arrows nav natively; type-to-filter
	// (any letter, no "/") is armed in handleKey, and the arrows keep working
	// even mid-filter -- see handleKey.

	ti := textinput.New()
	ti.CharLimit = 128
	ti.Width = 40

	// A corrupt config is surfaced in the footer rather than fatal: the
	// user still gets a working table (LoadConfig returns defaults on any
	// failure) and can see why their layout came back as the shipped one.
	cfg, cfgErr := LoadConfig()

	m := Model{list: l, spinner: sp, delegate: delegate, renameInput: ti, home: home, styles: styles, columns: cfg.Columns, err: cfgErr}
	m.relayout()
	return m
}

// usableWidth is the row width the list has to work with: the window minus
// appStyle's frame, or the shipped default before the first WindowSizeMsg
// arrives (and for --dump, which never gets one).
func (m Model) usableWidth() int {
	if m.width == 0 {
		return defaultUsableWidth
	}
	w, _ := m.listSize(0)
	return w
}

// relayout recomputes the delegate's column layout from configuration,
// runtime peer visibility and the current width. It is the single writer of
// delegate.layout so a resize, a reload and a settings change all go through
// one path.
func (m *Model) relayout() {
	m.delegate.layout = layoutColumns(resolveColumns(m.columns, m.showPeer), m.usableWidth())
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(reloadCmd(), reloadTickCmd(), tickCmd(), marqueeTickCmd(), m.spinner.Tick)
}

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func reloadTickCmd() tea.Cmd {
	return tea.Tick(2*time.Second, func(t time.Time) tea.Msg { return reloadTickMsg(t) })
}

// marqueeTickCmd drives the selected row's horizontal scroll. 200ms ≈ 5
// columns/sec -- fast enough to reveal a long name/path in a second or two,
// slow enough to actually read as it moves.
func marqueeTickCmd() tea.Cmd {
	return tea.Tick(200*time.Millisecond, func(t time.Time) tea.Msg { return marqueeTickMsg(t) })
}

func reloadCmd() tea.Cmd {
	return func() tea.Msg {
		sessions, err := session.List()
		return reloadMsg{sessions: sessions, err: err}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tickMsg:
		return m, tickCmd()

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		m.delegate.spinner = m.spinner
		return m, cmd

	case marqueeTickMsg:
		if name := m.selectedName(); name != m.marqueeFor {
			m.marqueeFor, m.marqueeFrame = name, 0 // selection moved: scroll from its start
		} else {
			m.marqueeFrame++
		}
		m.delegate.marqueeFrame = m.marqueeFrame
		return m, marqueeTickCmd()

	case reloadTickMsg:
		return m, tea.Batch(reloadCmd(), reloadTickCmd())

	case reloadMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.err = nil
		return m, m.applyReload(msg.sessions)

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.list.SetSize(m.listSize(0))
		m.relayout()
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	// Everything else -- FilterMatchesMsg, the list's spinner/status
	// timers -- belongs to the list itself.
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

// listSize is the list's content box: the full window minus appStyle's
// frame, one row for Model's own header line (always shown -- see
// headerLine), and, when a footer line is also showing, one more row for
// that.
func (m Model) listSize(footerLines int) (int, int) {
	h, v := appStyle.GetFrameSize()
	// -2: Model always shows the count line and the column-header line above
	// the list (see View); footerLines is the optional rename/kill/error row.
	return max(0, m.width-h), max(0, m.height-v-2-footerLines)
}

// trimmedFilter wraps list.DefaultFilter to tolerate surrounding
// whitespace in the typed term: "jim " still matches "jim" instead of
// matching nothing (see selectOrCreate's belt-and-suspenders trim too).
func trimmedFilter(term string, targets []string) []list.Rank {
	return list.DefaultFilter(strings.TrimSpace(term), targets)
}

// sortSessions orders sessions for display: any that need the user (tmux
// bell rang) first, then by last-active, most recent first. Agent-exited
// zombie workspaces are NOT promoted -- they're informational, not urgent
// (there are often many), and their red "agent exited" styling already
// makes them stand out; promoting them would bury the user's live work.
// Shared by applyReload and DumpRows so --dump matches the interactive order.
func sortSessions(sessions []session.Session) {
	slices.SortStableFunc(sessions, func(a, b session.Session) int {
		an, bn := a.State == session.StateNotify, b.State == session.StateNotify
		if an != bn {
			if an {
				return -1
			}
			return 1
		}
		return b.Activity.Compare(a.Activity)
	})
}

// applyReload re-selects by name -- sorting reorders the list on every
// reload, so a raw index would point at the wrong row.
func (m *Model) applyReload(sessions []session.Session) tea.Cmd {
	prevName := ""
	if it, ok := m.list.SelectedItem().(sessionItem); ok {
		prevName = it.Name
	}

	sortSessions(sessions)
	m.showPeer = anyPeer(sessions)
	m.relayout()

	cmd := m.list.SetItems(toItems(sessions))

	if prevName != "" && m.list.FilterState() == list.Unfiltered {
		if i := slices.IndexFunc(sessions, func(s session.Session) bool { return s.Name == prevName }); i >= 0 {
			m.list.Select(i)
		}
	}
	return cmd
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}

	switch m.mode {
	case modeRename:
		return m.handleRenameKey(msg)
	case modeConfirmKill:
		return m.handleConfirmKey(msg)
	}

	// Arrows scroll in EVERY state. bubbles/list disables its own CursorUp/
	// CursorDown while the filter is being typed (list.go updateKeybindings),
	// which would strand the user mid-filter, so drive the list's public nav
	// API directly instead. Not hand-rolled navigation -- it's the library's
	// own methods, just not gated behind the keymap it turns off.
	switch msg.Type {
	case tea.KeyUp:
		m.list.CursorUp()
		m.resetMarquee()
		return m, nil
	case tea.KeyDown:
		m.list.CursorDown()
		m.resetMarquee()
		return m, nil
	}

	switch {
	case msg.Type == tea.KeyEsc:
		// esc closes the switcher outright -- a reflex tool opened dozens of
		// times an hour; one key out beats a clear-then-quit two-step.
		// Backspace clears typed filter text.
		return m, tea.Quit

	case key.Matches(msg, enterKey):
		return m.selectOrCreate()

	case key.Matches(msg, renameKey):
		if s, ok := m.selected(); ok {
			m.mode = modeRename
			m.renameTarget = s.Name
			m.renameInput.SetValue(s.Name)
			m.renameInput.CursorEnd()
			m.renameInput.Focus()
			return m, textinput.Blink
		}
		return m, nil

	case key.Matches(msg, killKey):
		if s, ok := m.selected(); ok {
			m.mode = modeConfirmKill
			m.killTarget = s.Name
		}
		return m, nil
	}

	// Type-to-filter without "/": the first printable key arms the list's own
	// filter; from then on the list handles the typing. Arrows still scroll
	// (intercepted above), so this narrows the list without locking it.
	if msg.Type == tea.KeyRunes && m.list.FilterState() == list.Unfiltered {
		m.list.SetFilterState(list.Filtering)
		m.list.FilterInput.Focus()
	}

	// Everything else -- typed filter chars, pagination, help -- is the list's.
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m Model) selected() (session.Session, bool) {
	if it, ok := m.list.SelectedItem().(sessionItem); ok {
		return it.Session, true
	}
	return session.Session{}, false
}

// selectedName is the highlighted session's name, or "" when nothing is
// selected (empty list / filtered to nothing).
func (m Model) selectedName() string {
	if s, ok := m.selected(); ok {
		return s.Name
	}
	return ""
}

// resetMarquee snaps the selected row's scroll back to its start -- called the
// instant the cursor moves so a long name/summary shows its identifying head
// first, not wherever the global counter happened to be.
func (m *Model) resetMarquee() {
	m.marqueeFrame, m.marqueeFor = 0, m.selectedName()
	m.delegate.marqueeFrame = 0
}

// selectOrCreate: switch to the highlighted session, or, if the filter
// matched nothing, create one named by the current filter text -- unless
// that trimmed text exactly names an existing session (belt-and-suspenders
// against trimmedFilter missing a case), in which case switch to it
// instead of spawning a duplicate.
func (m Model) selectOrCreate() (tea.Model, tea.Cmd) {
	if s, ok := m.selected(); ok {
		return m, doAndQuit(func() error { return session.Switch(s.Name) })
	}
	name := strings.TrimSpace(m.list.FilterInput.Value())
	if name == "" {
		return m, nil
	}
	for _, item := range m.list.Items() {
		if it, ok := item.(sessionItem); ok && it.Name == name {
			return m, doAndQuit(func() error { return session.Switch(name) })
		}
	}
	return m, doAndQuit(func() error { return session.New(name) })
}

func doAndQuit(action func() error) tea.Cmd {
	return func() tea.Msg {
		if err := action(); err != nil {
			return reloadMsg{err: err}
		}
		return tea.Quit()
	}
}

// doAndReload runs a tmux action and only reloads on success, so a
// failure's error isn't immediately stomped by a clean reload.
func doAndReload(action func() error) tea.Cmd {
	return func() tea.Msg {
		if err := action(); err != nil {
			return reloadMsg{err: err}
		}
		sessions, err := session.List()
		return reloadMsg{sessions: sessions, err: err}
	}
}

func (m Model) handleRenameKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = modeList
		m.renameInput.Blur()
		return m, nil
	case "enter":
		newName := strings.TrimSpace(m.renameInput.Value())
		old := m.renameTarget
		m.mode = modeList
		m.renameInput.Blur()
		if newName == "" || newName == old {
			return m, nil
		}
		return m, doAndReload(func() error { return session.Rename(old, newName) })
	}

	var cmd tea.Cmd
	m.renameInput, cmd = m.renameInput.Update(msg)
	return m, cmd
}

func (m Model) handleConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y":
		target := m.killTarget
		m.mode, m.killTarget = modeList, ""
		return m, doAndReload(func() error { return session.Kill(target) })
	case "n", "esc":
		m.mode, m.killTarget = modeList, ""
	}
	return m, nil
}

func (m Model) View() string {
	lm := m.list
	lm.SetSize(m.listSize(1)) // count + header (in listSize) + 1 footer line

	body := m.countLine() + "\n" + m.headerLine() + "\n" + lm.View() + "\n" + m.footerLine()
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, appStyle.Render(body))
}

// countLine is the quiet session tally at the very top (list's own status bar
// is hidden, see New). Just a count -- per-session status (working, down,
// needs-you) already reads off the rows, so a stats bar here would be noise on
// a tool opened to switch, not to monitor. While filtering it doubles as
// match feedback ("3 of 13"), the one moment the number earns its place.
func (m Model) countLine() string {
	total := len(m.list.Items())
	noun := "sessions"
	if total == 1 {
		noun = "session"
	}
	if m.filtering() {
		if shown := len(m.list.VisibleItems()); shown != total {
			return m.styles.Count.Render(fmt.Sprintf("%d of %d %s", shown, total, noun))
		}
	}
	return m.styles.Count.Render(fmt.Sprintf("%d %s", total, noun))
}

// filtering reports whether the user is actively filtering with text typed.
// bubbles/list stays in Filtering state even after the text is backspaced to
// empty, so an empty filter counts as NOT filtering -- which is what brings
// the column headers back (see headerLine).
func (m Model) filtering() bool {
	return m.list.FilterState() == list.Filtering && m.list.FilterInput.Value() != ""
}

// headerLine is the one line Model always shows above the list: the filter
// textinput while the user is actively typing a filter (list's own filter bar
// is hidden, see New), otherwise the column-label header -- so backspacing the
// filter empty restores the labels.
func (m Model) headerLine() string {
	if m.filtering() {
		return m.list.FilterInput.View()
	}
	return renderHeader(m.styles, m.delegate.layout)
}

// footerLine is the one line sessui always shows below the list: the rename
// textinput, the kill confirmation, a surfaced error, or -- the normal case
// -- the honest key hints.
func (m Model) footerLine() string {
	switch m.mode {
	case modeRename:
		return m.styles.Footer.Render("rename: ") + m.renameInput.View() +
			m.styles.Help.Render("  (enter apply, esc cancel)")
	case modeConfirmKill:
		return m.styles.Error.Render(fmt.Sprintf("kill %s? ", m.killTarget)) +
			m.styles.Footer.Render("(y/n)")
	}
	if m.err != nil {
		return m.styles.Error.Render("error: " + m.err.Error())
	}
	return m.helpLine()
}

// helpLine is the app's real controls, shown because the list's built-in help
// is hidden (its hints don't match -- see New). No vim keys: letters feed the
// filter, so the arrows are the nav.
func (m Model) helpLine() string {
	return m.styles.Help.Render("↑↓ move · type to filter · ⏎ switch · esc quit · ^r rename · ^x kill")
}
