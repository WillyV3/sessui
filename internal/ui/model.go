// Package ui is the bubbletea layer. bubbles/list does cursor movement,
// filtering and pagination; this package adds the live ticks, the background
// reloads, the overlays, and the tmux actions list does not know about.
package ui

import (
	"context"
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

	"github.com/WillyV3/sessui/internal/session"
)

type tickMsg time.Time

// themeTickMsg drives the check for an Omarchy theme switch: its own tick,
// because the work is a tiny file read and should not be coupled to how often
// sessions are re-listed.
type themeTickMsg time.Time

const themeInterval = 700 * time.Millisecond

func themeTickCmd() tea.Cmd {
	return tea.Tick(themeInterval, func(t time.Time) tea.Msg { return themeTickMsg(t) })
}

type reloadTickMsg time.Time
type marqueeTickMsg time.Time

type reloadMsg struct {
	sessions []session.Session
	err      error
	// partial marks the tmux-only first pass (session.ListFast). A partial
	// that arrives late must not overwrite richer data with poorer.
	partial bool
}

// editorAppliedMsg is the settings editor's result, ready for Model to
// persist and relayout from. The editor never touches Model directly.
type editorAppliedMsg struct {
	columns    []columnSetting
	popupWidth int // for @sessui-width; takes effect on the next open
	theme      ThemeConfig
	icons      glyphSet
	hosts      []string
}

// restyle applies a theme choice process-wide -- the glyph set, the icon
// colours -- and returns the palette and Styles for it. New, the editor's
// live preview and the apply path all go through here.
func restyle(t ThemeConfig, g glyphSet) (Palette, Styles) {
	useGlyphs(g)
	p := loadPalette(t.Palette)
	applyTheme(p)
	return p, newStyles(p, t.Roles)
}

// retheme realises a theme choice across every styled thing the Model owns.
// There are five and they move together; this is the only place that knows
// the list. It does not relayout: the editor sets the popup width first, and
// layout depends on it.
func (m *Model) retheme(t ThemeConfig, g glyphSet) {
	palette, styles := restyle(t, g)
	m.styles, m.delegate.styles = styles, styles
	m.huhTheme = newHuhTheme(palette)
	m.help = newHelp(palette)
	m.list.Styles = themedListStyles(palette)
}

// editorPreviewRows caps how many rows the editor's live preview draws.
const editorPreviewRows = 6

// editorPreview is the editor's preview: the same renderers the live list
// uses, over the first few real sessions, at whatever layout the editor is
// trying.
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

// appStyle has no top padding so the brand line sits on the popup's first
// row; defaultUsableWidth assumes the horizontal 2+2.
var appStyle = lipgloss.NewStyle().Padding(0, 2, 1, 2)

type Model struct {
	list     list.Model
	spinner  spinner.Model
	delegate *rowDelegate // the pointer handed to list.New, so its spinner stays in sync

	// overlay is whatever has taken the keyboard: a rename or kill form in
	// the footer, or the settings editor in place of the list; nil when the
	// list has focus. See overlayResult.
	overlay  tea.Model
	huhTheme *huh.Theme
	help     help.Model
	// cfg is the persisted configuration; columns is the live copy the
	// layout reads. Kept so an apply can save the whole file back without
	// losing choices it does not edit.
	cfg Config
	// configErr is a config.json that exists but did not parse. Its own field
	// because m.err is cleared by every successful reload, which would leave
	// this notice on screen for one frame. Only a later successful save
	// clears it.
	configErr error

	err           error
	home          string
	width, height int
	styles        Styles
	// columns is the configured table; showPeer is the runtime fact that can
	// hide the peer column regardless. Both feed relayout.
	columns  []columnSetting
	showPeer bool
	// marqueeFrame advances the selected row's scroll; marqueeFor is the name
	// it counts for, so the frame resets when the selection moves. Keyed by
	// name, not index, because a reload re-sorts.
	marqueeFrame int
	marqueeFor   string
	// who is "user@host", resolved once: neither half can change while a
	// popup is open, and this renders every frame.
	who string
	// watcher is a pointer because it holds a mutex and bubbletea passes
	// Models by value; copying one is a copylocks race.
	watcher *session.Watcher
	// createTarget is which machine a new session is made on: 0 is local,
	// 1..n index cfg.Hosts. createTargetFor is the name it was chosen for;
	// retype the name and the choice lapses.
	createTarget    int
	createTargetFor string
	// loaded is false until the first list arrives; before that the header
	// says so rather than claiming "0 sessions". fullLoaded guards against a
	// late partial replacing a complete list.
	loaded     bool
	fullLoaded bool
	// themeStamp is the Omarchy theme the current Styles were built from.
	themeStamp string
}

// whoAmI is "user@host", degrading to whichever half is available.
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

// New builds the initial model.
func New() Model {
	home, _ := os.UserHomeDir()
	cfg, cfgErr := LoadConfig()
	palette, styles := restyle(cfg.Theme, cfg.Icons)
	sp := spinner.New(spinner.WithSpinner(spinner.MiniDot))
	delegate := &rowDelegate{styles: styles, home: home, spinner: sp}

	l := list.New(nil, delegate, 0, 0)
	l.Title = "tmux sessions"
	l.Styles = themedListStyles(palette)
	// Model draws its own header, count and help lines, so list's built-in
	// ones are hidden: its help hints would be wrong here anyway (no "/" to
	// filter, esc quits, letters feed the filter).
	l.SetShowTitle(false)
	l.SetShowFilter(false)
	l.SetShowStatusBar(false)
	// Tolerate a stray space in the typed filter, so "name " matches "name"
	// instead of creating a session literally named "name ".
	l.Filter = trimmedFilter
	l.SetShowHelp(false)

	m := Model{
		list: l, spinner: sp, delegate: delegate, home: home, styles: styles,
		huhTheme: newHuhTheme(palette), help: newHelp(palette),
		cfg: cfg, columns: cfg.Columns, configErr: cfgErr, who: whoAmI(),
		themeStamp: ThemeStamp(),
		watcher:    &session.Watcher{},
	}
	m.relayout()
	return m
}

// usableWidth is the row width the list has to work with: the window minus
// appStyle's frame, or the shipped default before the first WindowSizeMsg
// (and for --dump, which never gets one).
func (m Model) usableWidth() int {
	if m.width == 0 {
		return defaultUsableWidth
	}
	w, _ := m.listSize(0)
	return w
}

// relayout recomputes the column layout from configuration, peer visibility
// and width. It is the single writer of delegate.layout.
func (m *Model) relayout() {
	m.delegate.layout = layoutColumns(resolveColumns(m.columns, m.showPeer), m.usableWidth())
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(reloadFastCmd(m.watcher, m.cfg.Hosts), reloadCmd(m.watcher, m.cfg.Hosts),
		reloadTickCmd(), tickCmd(), marqueeTickCmd(), m.spinner.Tick, themeTickCmd(),
		hostTickCmd(), refreshHostsCmd(m.watcher, m.cfg.Hosts))
}

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func reloadTickCmd() tea.Cmd {
	return tea.Tick(2*time.Second, func(t time.Time) tea.Msg { return reloadTickMsg(t) })
}

// marqueeTickCmd drives the selected row's scroll: fast enough to reveal a
// long name in a second or two, slow enough to read as it moves.
func marqueeTickCmd() tea.Cmd {
	return tea.Tick(200*time.Millisecond, func(t time.Time) tea.Msg { return marqueeTickMsg(t) })
}

// reloadFastCmd is the first paint: tmux only. Fired alongside reloadCmd at
// startup so rows appear while the slow half is still running.
func reloadFastCmd(w *session.Watcher, hosts []string) tea.Cmd {
	return func() tea.Msg {
		local, err := session.ListFast()
		return reloadMsg{sessions: session.Merge(local, w, hosts), err: err, partial: true}
	}
}

// reloadCmd reads local tmux and folds in what the watcher already has.
// Merge is a cache read, so the reload never waits on ssh.
func reloadCmd(w *session.Watcher, hosts []string) tea.Cmd {
	return func() tea.Msg {
		local, err := session.List()
		return reloadMsg{sessions: session.Merge(local, w, hosts), err: err}
	}
}

// hostRefreshInterval is how often watched hosts are polled: far slower than
// the local reload, since each tick is one ssh round trip per host.
const hostRefreshInterval = 15 * time.Second

type hostTickMsg struct{}
type hostsRefreshedMsg struct{}

func hostTickCmd() tea.Cmd {
	return tea.Tick(hostRefreshInterval, func(time.Time) tea.Msg { return hostTickMsg{} })
}

// refreshHostsCmd does the ssh fan-out off the UI thread, one Cmd per host, so
// each answer lands on its own: a reachable machine shows up in milliseconds
// while a dead one is still being waited on.
func refreshHostsCmd(w *session.Watcher, hosts []string) tea.Cmd {
	if len(hosts) == 0 {
		return nil
	}
	cmds := make([]tea.Cmd, 0, len(hosts))
	for _, h := range hosts {
		cmds = append(cmds, func() tea.Msg {
			w.RefreshOne(context.Background(), h)
			return hostsRefreshedMsg{}
		})
	}
	return tea.Batch(cmds...)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case editorAppliedMsg:
		m.columns = msg.columns
		m.cfg.Columns, m.cfg.Theme, m.cfg.Icons = msg.columns, msg.theme, msg.icons
		// A newly watched host must show its sessions now, not on the next
		// poll; the editor just probed them, so the reload is a cache read.
		hostsChanged := !slices.Equal(m.cfg.Hosts, msg.hosts)
		m.cfg.Hosts = msg.hosts
		m.retheme(msg.theme, msg.icons)
		if err := SaveConfig(m.cfg); err != nil {
			m.err = err
		} else {
			m.configErr = nil // a good write replaces whatever failed to parse
		}
		if err := session.SetPopupWidth(msg.popupWidth); err != nil {
			m.err = err
		}
		m.relayout()
		if hostsChanged {
			return m, reloadCmd(m.watcher, m.cfg.Hosts)
		}
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

	case themeTickMsg:
		// Only "auto" follows Omarchy: a pinned palette is a choice.
		if m.cfg.Theme.Palette == paletteAuto {
			if now := ThemeStamp(); now != m.themeStamp {
				m.themeStamp = now
				m.retheme(m.cfg.Theme, m.cfg.Icons)
				m.relayout()
			}
		}
		return m, themeTickCmd()

	case reloadTickMsg:
		return m, tea.Batch(reloadCmd(m.watcher, m.cfg.Hosts), reloadTickCmd())

	case hostTickMsg:
		return m, tea.Batch(hostTickCmd(), refreshHostsCmd(m.watcher, m.cfg.Hosts))

	case hostsRefreshedMsg:
		// An open editor renders every host's reachability, so it needs this
		// before the list does; Model's own switch runs before the overlay
		// fallthrough below.
		if m.overlay != nil {
			return m.updateOverlay(msg)
		}
		return m, reloadCmd(m.watcher, m.cfg.Hosts)

	case reloadMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		// A partial that lost the race to the full load knows nothing about
		// peers or agent state; applying it would blank correct columns.
		if msg.partial && m.fullLoaded {
			return m, nil
		}
		m.loaded = true
		if !msg.partial {
			m.fullLoaded = true
		}
		m.err = nil
		return m, m.applyReload(msg.sessions)

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.list.SetSize(m.listSize(0))
		m.relayout()
		// An open overlay keeps the width it was built with. A mid-rename
		// resize is rare enough not to earn a resize in overlayResult's
		// contract.
		return m, nil

	case tea.KeyMsg:
		// Ctrl+C quits from anywhere, checked before routing so an overlay
		// can never swallow it.
		if msg.Type == tea.KeyCtrlC {
			return m, tea.Quit
		}
		if m.overlay != nil {
			return m.updateOverlay(msg)
		}
		return m.handleKey(msg)
	}

	// Everything else belongs to whichever of the two has focus.
	if m.overlay != nil {
		return m.updateOverlay(msg)
	}
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

// updateOverlay drives the active overlay and, once it reports finished,
// clears it and returns whatever command it produced. One path for every
// overlay kind.
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

// listSize is the list's content box: the window minus appStyle's frame, the
// brand line, two blank rows, the column header, and any footer line.
// Miscounting here scrolls the list under a row that is still drawn over it.
func (m Model) listSize(footerLines int) (int, int) {
	_, v := appStyle.GetFrameSize()
	return m.contentWidth(), max(0, m.height-v-4-footerLines)
}

// contentWidth is the window minus appStyle's horizontal frame; also handed
// to an overlay's form so it sizes to the popup instead of huh's default.
func (m Model) contentWidth() int {
	h, _ := appStyle.GetFrameSize()
	return max(0, m.width-h)
}

// trimmedFilter wraps list.DefaultFilter to tolerate surrounding whitespace in
// the typed term.
func trimmedFilter(term string, targets []string) []list.Rank {
	return list.DefaultFilter(strings.TrimSpace(term), targets)
}

// sortSessions orders sessions for display: any that need the user first,
// then by last activity. Exited-agent workspaces are not promoted; they are
// informational, often many, and already styled to stand out. Shared with
// DumpRows so --dump matches the interactive order.
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

// applyReload re-selects by name: sorting reorders the list on every reload,
// so a raw index would point at the wrong row.
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

// handleKey is reached only with the list in focus; Update routes ctrl+c and
// an open overlay's keys first.
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Arrows scroll in every state. bubbles/list disables its own CursorUp/
	// CursorDown while the filter is being typed, which would strand the user
	// mid-filter, so drive the list's public nav API directly.
	switch msg.Type {
	case tea.KeyUp:
		m.list.CursorUp()
		m.resetMarquee()
		return m, nil
	case tea.KeyDown:
		m.list.CursorDown()
		m.resetMarquee()
		return m, nil
	case tea.KeyLeft, tea.KeyRight:
		// While the filter matches nothing there is no selection to disturb,
		// so the one state where enter creates is the one where ←→ are free
		// to say where. Enter alone still creates locally.
		if m.creating() {
			m.moveCreateTarget(map[tea.KeyType]int{tea.KeyLeft: -1, tea.KeyRight: 1}[msg.Type])
		}
		return m, nil
	}

	switch {
	case msg.Type == tea.KeyEsc:
		// esc closes the switcher outright: one key out beats a
		// clear-then-quit two-step in a tool opened dozens of times an hour.
		return m, tea.Quit

	case key.Matches(msg, appKeys.Enter):
		return m.selectOrCreate()

	case key.Matches(msg, appKeys.Rename):
		if s, ok := m.selected(); ok {
			host, w := s.Host, m.watcher
			m.overlay = renameOverlay(m.huhTheme, m.contentWidth(), s.Name,
				func(old, new string) error {
					if err := session.RenameOn(host, old, new); err != nil {
						return err
					}
					// Correct the cache ourselves: the reload that follows
					// reads it, and the next poll is up to 15s away.
					if host != "" {
						w.RenameCached(host, old, new)
					}
					return nil
				})
			return m, m.overlay.Init()
		}
		return m, nil

	case key.Matches(msg, appKeys.Kill):
		if s, ok := m.selected(); ok {
			host, w := s.Host, m.watcher
			m.overlay = killOverlay(m.huhTheme, m.contentWidth(), s.Name,
				func(name string) error {
					if err := session.KillOn(host, name); err != nil {
						return err
					}
					if host != "" {
						w.Forget(host, name)
					}
					return nil
				})
			return m, m.overlay.Init()
		}
		return m, nil

	case key.Matches(msg, appKeys.Columns):
		m.overlay = newColumnEditor(m.styles, m.columns, m.showPeer, m.usableWidth(), session.PopupWidth(), m.editorPreview).
			withTheme(m.cfg.Theme, m.cfg.Icons, func(t ThemeConfig, g glyphSet) Styles { _, s := restyle(t, g); return s }).
			withHosts(session.DiscoverHosts(), m.cfg.Hosts, m.watcher)
		return m, m.overlay.Init()

	}

	// Type-to-filter without "/": the first printable key arms the list's own
	// filter; from then on the list handles the typing.
	if msg.Type == tea.KeyRunes && m.list.FilterState() == list.Unfiltered {
		m.list.SetFilterState(list.Filtering)
		m.list.FilterInput.Focus()
	}

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
// selected.
func (m Model) selectedName() string {
	if s, ok := m.selected(); ok {
		return s.Name
	}
	return ""
}

// resetMarquee snaps the selected row's scroll back to its start the instant
// the cursor moves, so a long name shows its head first.
func (m *Model) resetMarquee() {
	m.marqueeFrame, m.marqueeFor = 0, m.selectedName()
	m.delegate.marqueeFrame = 0
}

// selectOrCreate switches to the highlighted session or, if the filter
// matched nothing, creates one named by the filter text -- unless that text
// exactly names an existing session, in which case it switches instead of
// spawning a duplicate.
func (m Model) selectOrCreate() (tea.Model, tea.Cmd) {
	if s, ok := m.selected(); ok {
		// tmux cannot switch a client across machines; AttachRemote proxies
		// the session locally, after which it is an ordinary local row.
		if s.Host != "" {
			return m, doAndQuit(func() error { return session.AttachRemote(s.Host, s.Name) })
		}
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
	if host := m.createHost(); host != "" {
		return m, doAndQuit(func() error { return session.NewRemote(host, name) })
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

// doAndReload runs an action and reloads only on success, so a failure's
// error is not stomped by a clean reload.
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
	// The settings editor replaces the list body: its preview IS the table.
	// Every other overlay is a single footer line and leaves the list in place.
	if editor, ok := m.overlay.(*columnEditor); ok {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, appStyle.Render(editor.View()))
	}

	lm := m.list
	lm.SetSize(m.listSize(1)) // brand + 2 blanks + header (in listSize) + 1 footer line

	body := m.brandLine() + "\n\n\n" + m.headerLine() + "\n" + lm.View() + "\n" + m.footerLine()
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, appStyle.Render(body))
}

// brandLine is the chrome row above the column headers.
func (m Model) brandLine() string {
	if !m.loaded {
		return brandLineLoading(m.styles, m.who, m.spinner.View(), m.usableWidth())
	}
	return brandLine(m.styles, m.who, len(m.list.Items()), len(m.list.VisibleItems()), m.filtering(), m.usableWidth())
}

// filtering reports whether text is typed in the filter. bubbles/list stays
// in Filtering state after the text is backspaced to empty, so an empty
// filter counts as not filtering, which brings the column headers back.
func (m Model) filtering() bool {
	return m.list.FilterState() == list.Filtering && m.list.FilterInput.Value() != ""
}

// headerLine is the line above the list: the filter input while typing,
// otherwise the column headers.
func (m Model) headerLine() string {
	if m.filtering() {
		return m.list.FilterInput.View()
	}
	return renderHeader(m.styles, m.delegate.layout)
}

// footerLine is the line below the list: the open overlay, a surfaced error,
// the create bar, or the key hints.
func (m Model) footerLine() string {
	if m.overlay != nil {
		return m.overlay.View()
	}
	// A config that failed to parse outranks a transient reload error: it is
	// durable, it is the user's own file, and it explains why their layout is
	// not the one they saved.
	if m.configErr != nil {
		return m.styles.Error.Render("config: " + m.configErr.Error())
	}
	if m.err != nil {
		return m.styles.Error.Render("error: " + m.err.Error())
	}
	if m.creating() {
		if bar := m.renderCreateBar(); bar != "" {
			return bar
		}
	}
	return m.helpLine()
}

// helpLine renders appKeys through bubbles/help, so the legend cannot drift
// from the bindings handleKey matches.
func (m Model) helpLine() string {
	return m.help.ShortHelpView(appKeys.ShortHelp())
}
