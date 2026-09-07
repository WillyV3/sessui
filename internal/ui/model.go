// Package ui is the bubbletea layer for sessui. bubbles/list.Model does
// cursor movement, filtering, pagination, help and the status bar; this
// package only adds the live "active" tick, background reload, and the
// three tmux actions (switch, rename, kill) list doesn't know about.
package ui

import (
	"os"
	"os/user"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"

	"github.com/willyv3/sessui/internal/session"
)

type tickMsg time.Time
type reloadTickMsg time.Time
type marqueeTickMsg time.Time

type reloadMsg struct {
	sessions []session.Session
	err      error
}

// editorAppliedMsg is the column editor's apply Cmd result (see coledit.go's
// result()): the edited settings, ready for Model to persist and relayout
// from. Kept overlay-agnostic on purpose -- the editor never touches Model
// internals directly, the same shape rename/kill already use via
// doAndReload's Cmd-returns-a-msg pattern.
type editorAppliedMsg struct {
	columns    []columnSetting
	popupWidth int // for @sessui-width; takes effect on the next open
	theme      ThemeConfig
	icons      glyphSet
}

// restyle applies a theme choice process-wide -- the glyph set, the icon
// colours -- and returns the palette and Styles for it. New, the editor's
// live preview and the apply path all go through here, so a choice looks
// the same in all three.
func restyle(t ThemeConfig, g glyphSet) (Palette, Styles) {
	useGlyphs(g)
	p := loadPalette(t.Palette)
	applyTheme(p)
	return p, newStyles(p, t.Roles)
}

// editorPreviewRows caps how many real session rows the column editor's live
// preview draws -- enough to read as a table, not a full scroll of the list.
const editorPreviewRows = 6

// editorPreview is the column editor's preview closure (see coledit.go): the
// SAME renderers the live list uses, over the first few real sessions, at
// whatever layout the editor is currently trying -- so the user edits the
// actual table, never a mockup.
func (m Model) editorPreview(styles Styles, layout tableLayout) string {
	lines := []string{renderHeader(styles, layout)}
	now := time.Now()
	for i, item := range m.list.Items() {
		if i >= editorPreviewRows {
			break
		}
		if it, ok := item.(sessionItem); ok {
			c := cell{styles: styles, home: m.home, now: now, session: it.Session, spinner: m.spinner}
			lines = append(lines, renderRow(layout, c, 0, false))
		}
	}
	return strings.Join(lines, "\n")
}

var appStyle = lipgloss.NewStyle().Padding(1, 2)

type Model struct {
	list     list.Model
	spinner  spinner.Model
	delegate *rowDelegate // same pointer handed to list.New, so its spinner
	// field can be kept in sync with Model's on every spinner.TickMsg.

	// overlay is whatever has taken the keyboard: a huh rename or
	// kill-confirm form in the footer band, or the column editor in place of
	// the list body; nil (the common case) when the list has focus. See
	// overlay.go for the completion contract (overlayResult) that lets Model
	// handle any overlay kind through updateOverlay without knowing what's
	// actually running in it.
	overlay  tea.Model
	huhTheme *huh.Theme // built once from Palette (see newHuhTheme), handed to every overlay
	help     help.Model // renders appKeys (keys.go) for helpLine
	// cfg is the persisted configuration as loaded; columns below is the
	// live copy the layout reads. Kept so a column-editor apply can save the
	// whole file back without losing the icon/theme choices it doesn't edit.
	cfg Config
	// configErr is a config.json that exists but did not parse. It is its own
	// field, not m.err, because m.err is cleared by every successful reload
	// (~1s) -- which meant this notice was on screen for one frame and the
	// user never learned why their layout came back as the shipped one. It
	// clears only when a later save succeeds (see editorAppliedMsg).
	configErr error

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
	// who is "user@host" for the brand line, resolved once in New: neither
	// half can change while a popup is open, and this renders every frame.
	who string
}

// whoAmI is "user@host", degrading to whichever half is available rather than
// rendering an empty or half-formed identity in the brand line.
func whoAmI() string {
	name := ""
	if u, err := user.Current(); err == nil {
		name = u.Username
	}
	host, err := os.Hostname()
	if err != nil {
		host = ""
	}
	switch {
	case name != "" && host != "":
		return name + "@" + host
	case host != "":
		return host
	default:
		return name
	}
}

// New builds the initial model: reads the theme palette and the user's
// home directory once, up front.
func New() Model {
	home, _ := os.UserHomeDir()
	// The glyph set must be chosen before applyTheme, whose Icon values
	// capture glyphU's output; and before loadPalette only for tidiness.
	cfg, cfgErr := LoadConfig()
	palette, styles := restyle(cfg.Theme, cfg.Icons)
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

	// A corrupt config is surfaced in the footer rather than fatal: the
	// user still gets a working table (LoadConfig returns defaults on any
	// failure) and can see why their layout came back as the shipped one.
	m := Model{
		list: l, spinner: sp, delegate: delegate, home: home, styles: styles,
		huhTheme: newHuhTheme(palette), help: newHelp(palette),
		cfg: cfg, columns: cfg.Columns, configErr: cfgErr, who: whoAmI(),
	}
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
	case editorAppliedMsg:
		// The editor's result, delivered through the generic overlay path
		// (updateOverlay ran its apply Cmd). Columns, theme and glyph set
		// land in the config together; the theme is realised right here so
		// the list comes back already in the chosen look.
		m.columns = msg.columns
		m.cfg.Columns, m.cfg.Theme, m.cfg.Icons = msg.columns, msg.theme, msg.icons
		palette, styles := restyle(msg.theme, msg.icons)
		m.styles, m.delegate.styles = styles, styles
		m.huhTheme, m.help, m.list.Styles = newHuhTheme(palette), newHelp(palette), themedListStyles(palette)
		if err := SaveConfig(m.cfg); err != nil {
			m.err = err
		} else {
			m.configErr = nil // a good write replaces whatever failed to parse
		}
		// Popup geometry lives in tmux, not config.json: sessui.tmux reads
		// @sessui-width at open time, before this binary exists.
		if err := session.SetPopupWidth(msg.popupWidth); err != nil {
			m.err = err
		}
		m.relayout()
		return m, nil

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
		// ponytail: an open overlay keeps the width it was built with (see
		// contentWidth in handleKey) rather than being resized live -- a
		// mid-rename terminal resize is rare enough not to earn plumbing a
		// resize into overlayResult's contract for every future overlay kind.
		return m, nil

	case tea.KeyMsg:
		// Ctrl+C quits the whole program from anywhere, overlay or not --
		// checked here, before routing, so an open overlay can never
		// swallow it.
		if msg.Type == tea.KeyCtrlC {
			return m, tea.Quit
		}
		if m.overlay != nil {
			return m.updateOverlay(msg)
		}
		return m.handleKey(msg)
	}

	// Everything else -- FilterMatchesMsg, the list's spinner/status
	// timers, an open overlay's own cursor-blink/internal messages --
	// belongs to whichever of the two has focus.
	if m.overlay != nil {
		return m.updateOverlay(msg)
	}
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

// updateOverlay drives the active overlay and, once it reports finished
// (see overlayResult in overlay.go), clears it and hands back whatever
// command it produced -- the real action + reload on completion, nothing
// on abort. One path for every overlay kind: no switch on what's showing.
func (m Model) updateOverlay(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.overlay.Update(msg)
	m.overlay = next

	if r, ok := m.overlay.(overlayResult); ok {
		if finished, apply := r.result(); finished {
			m.overlay = nil
			return m, tea.Batch(cmd, apply)
		}
	}
	return m, cmd
}

// listSize is the list's content box: the full window minus appStyle's
// frame, one row for Model's own header line (always shown -- see
// headerLine), and, when a footer line is also showing, one more row for
// that.
func (m Model) listSize(footerLines int) (int, int) {
	_, v := appStyle.GetFrameSize()
	// -3: Model always shows the brand line, the count line and the
	// column-header line above the list (see View); footerLines is the
	// optional rename/kill/error row.
	return m.contentWidth(), max(0, m.height-v-3-footerLines)
}

// contentWidth is the app's content width (the window minus appStyle's
// horizontal frame) -- also handed to an open overlay's huh.Form so it
// sizes to the real popup instead of huh's own 80-column default.
func (m Model) contentWidth() int {
	h, _ := appStyle.GetFrameSize()
	return max(0, m.width-h)
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

// handleKey is only reached with the list in focus -- Update routes ctrl+c
// and an open overlay's keys elsewhere first (see Update/updateOverlay).
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
		// Backspace clears typed filter text. (Esc inside an overlay means
		// something different -- abort it -- and never reaches here, see
		// Update.)
		return m, tea.Quit

	case key.Matches(msg, appKeys.Enter):
		return m.selectOrCreate()

	case key.Matches(msg, appKeys.Rename):
		if s, ok := m.selected(); ok {
			m.overlay = renameOverlay(m.huhTheme, m.contentWidth(), s.Name, session.Rename)
			return m, m.overlay.Init()
		}
		return m, nil

	case key.Matches(msg, appKeys.Kill):
		if s, ok := m.selected(); ok {
			m.overlay = killOverlay(m.huhTheme, m.contentWidth(), s.Name, session.Kill)
			return m, m.overlay.Init()
		}
		return m, nil

	case key.Matches(msg, appKeys.Columns):
		m.overlay = newColumnEditor(m.styles, m.columns, m.showPeer, m.usableWidth(), session.PopupWidth(), m.editorPreview).
			withTheme(m.cfg.Theme, m.cfg.Icons, func(t ThemeConfig, g glyphSet) Styles { _, s := restyle(t, g); return s })
		return m, m.overlay.Init()

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

func (m Model) View() string {
	// The column editor replaces the list body: its live preview IS the
	// table, so drawing the list underneath would show two of them. Every
	// other overlay (huh rename/kill) is a single footer line and leaves the
	// list in place -- see footerLine.
	if editor, ok := m.overlay.(*columnEditor); ok {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, appStyle.Render(editor.View()))
	}

	lm := m.list
	lm.SetSize(m.listSize(1)) // brand + count + header (in listSize) + 1 footer line

	body := m.brandLine() + "\n" + m.countLine() + "\n" + m.headerLine() + "\n" + lm.View() + "\n" + m.footerLine()
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, appStyle.Render(body))
}

// countLine is the quiet session tally at the very top (list's own status bar
// is hidden, see New). Just a count -- per-session status (working, down,
// needs-you) already reads off the rows, so a stats bar here would be noise on
// a tool opened to switch, not to monitor. While filtering it doubles as
// match feedback ("3 of 13"), the one moment the number earns its place.
// brandLine is the decorative rule at the very top; who is resolved once in
// New, not per frame, because it cannot change while the popup is open.
func (m Model) brandLine() string {
	return brandLine(m.styles, m.who, m.usableWidth())
}

func (m Model) countLine() string {
	return renderHeaderLine(m.styles, len(m.list.Items()), len(m.list.VisibleItems()), m.filtering())
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

// footerLine is the one line sessui always shows below the list: the open
// overlay (rename/kill), a surfaced error, or -- the normal case -- the
// honest key hints.
func (m Model) footerLine() string {
	if m.overlay != nil {
		return m.overlay.View()
	}
	// A config that failed to parse outranks a transient reload error: it is
	// durable, it is the user's own file, and it explains why their layout
	// is not the one they saved.
	if m.configErr != nil {
		return m.styles.Error.Render("config: " + m.configErr.Error())
	}
	if m.err != nil {
		return m.styles.Error.Render("error: " + m.err.Error())
	}
	return m.helpLine()
}

// helpLine is the app's real controls, shown because the list's built-in
// help is hidden (its hints don't match -- see New). Rendered from appKeys
// (keys.go) via bubbles/help instead of a hand-typed string, so the legend
// can't drift from the bindings handleKey actually matches. No vim keys:
// letters feed the filter, so the arrows are the nav.
func (m Model) helpLine() string {
	return m.help.ShortHelpView(appKeys.ShortHelp())
}
