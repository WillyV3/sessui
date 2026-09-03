package ui

// Header widgets: the right-hand section of the count line is a row of
// small, opt-in indicators -- attention pills, what's playing, which box you
// are on -- each collapsed to an icon until you focus it (^w), when it
// expands INTO THE SAME LINE. The header is one line, always: expansion is
// horizontal, never vertical, so the table below never moves. That invariant
// is the design; the tests pin it.
//
// A widget is any type with icon() and expand(). Two smaller, optional
// interfaces add keys (controllable -- the transport) and a background poll
// (poller -- a shell command, a media status). Interfaces are declared here,
// where they are consumed, and kept to one or two methods each so a widget
// author implements exactly what their widget needs and nothing else.
//
// Built-ins register in widgetCatalog by name; config picks and orders them
// (Config.Header.Widgets). The `shell` widget is the escape hatch for anyone
// who does not want to write Go: a command whose first output line is the
// widget, Omarchy-plugin style -- name it in config, done.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/willyv3/sessui/internal/session"
)

// widgetState is everything a widget may read to render one frame. A
// value, so a widget cannot mutate the model's state through it.
type widgetState struct {
	sessions     []session.Session
	now          time.Time
	styles       Styles
	filtering    bool
	shown, total int
}

// widget is one item in the header's widget section.
type widget interface {
	// icon is the collapsed form: a glyph, a badge, a word -- a few cells.
	// "" hides the widget this frame (nothing playing, nothing pending).
	icon(widgetState) string
	// expand is the focused form: one line, at most width cells.
	expand(st widgetState, width int) string
}

// controllable is a widget that takes keys while it is expanded -- the
// transport is the one shipped example. keys feeds the footer legend.
type controllable interface {
	widget
	handle(tea.KeyMsg) tea.Cmd
	keys() []key.Binding
}

// poller is a widget whose content comes from outside the process. poll
// runs off the main loop on the reload tick and returns a Cmd that delivers
// a widgetPollMsg; absorb stores whatever came back. A widget that renders
// purely from widgetState never implements this.
type poller interface {
	widget
	poll() tea.Cmd
	absorb(widgetPollMsg)
}

// widgetPollMsg carries a poll's result back to the widget that asked. The
// poller rides in the message (identity, not a catalog name) so two shell
// widgets -- a clock and a battery -- each get their own result, and Model
// dispatches without a lookup.
type widgetPollMsg struct {
	src    poller
	output string
	err    error
}

// widgetSetting is one configured widget: its catalog name and any args.
type widgetSetting struct {
	Name string            `json:"name"`
	Args map[string]string `json:"args,omitempty"`
}

// HeaderConfig is the widget section: which widgets, in what order.
type HeaderConfig struct {
	Widgets []widgetSetting `json:"widgets,omitempty"`
}

// widgets is the configured list, or the shipped default when the file
// names none. Resolved here at use rather than filled in by withDefaults so
// SaveConfig never pins today's default into the user's file -- a later
// default reaches everyone who never chose.
func (h HeaderConfig) widgets() []widgetSetting {
	if len(h.Widgets) == 0 {
		return defaultHeaderWidgets()
	}
	return h.Widgets
}

// defaultHeaderWidgets is the header a fresh install shows. now-playing
// sits left of the attention pills and hides itself when nothing plays (and
// on a box without omarchy-shell), so the count line stays byte-identical to
// before widgets existed until there is something to show. ^w lands on
// now-playing first -- the widget with controls is the one you focus for.
func defaultHeaderWidgets() []widgetSetting {
	return []widgetSetting{{Name: "now-playing"}, {Name: "attention"}}
}

// namedWidget pairs a catalog name with an instance so Model can route a
// widgetPollMsg back to the widget that asked for it.
type namedWidget struct {
	name string
	widget
}

// widgetCatalog is every widget sessui ships, by name. A constructor takes
// the config args so a widget can be configured (the shell command, an
// icon) and returns an error for a bad one, which Model surfaces instead of
// silently dropping the widget.
var widgetCatalog = map[string]func(args map[string]string) (widget, error){
	"attention": func(map[string]string) (widget, error) { return attentionWidget{}, nil },
	"host":      newHostWidget,
	"agents":    func(map[string]string) (widget, error) { return agentsWidget{}, nil },
	"shell":     newShellWidget,
	// now-playing lives in nowplaying.go: Omarchy's media service as a widget.
	"now-playing": newNowPlayingWidget,
}

// resolveWidgets turns settings into widgets, in order. An unknown name is
// reported (the user typed it into config.json; they want to know), not
// fatal -- the rest of the header still renders.
func resolveWidgets(settings []widgetSetting) ([]namedWidget, error) {
	var (
		out  []namedWidget
		errs []string
	)
	for _, s := range settings {
		build, ok := widgetCatalog[s.Name]
		if !ok {
			errs = append(errs, fmt.Sprintf("unknown widget %q", s.Name))
			continue
		}
		w, err := build(s.Args)
		if err != nil {
			errs = append(errs, fmt.Sprintf("widget %q: %v", s.Name, err))
			continue
		}
		if w == nil {
			// Not applicable on this box (no media service on a Mac): not an
			// error, not a dead widget for ^w to land on -- simply absent.
			continue
		}
		out = append(out, namedWidget{name: s.Name, widget: w})
	}
	if len(errs) > 0 {
		return out, fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return out, nil
}

// widgetNames lists the catalog in stable order -- for a settings UI and for
// the unknown-widget error to be actionable.
func widgetNames() []string {
	names := make([]string, 0, len(widgetCatalog))
	for n := range widgetCatalog {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// ---- attention: needs-you and mail, the two signals the header was
// ---- created to carry. Collapsed it IS the old header's pills; expanded it
// ---- names the sessions, which is the question the pill makes you ask.

type attentionWidget struct{}

func (attentionWidget) counts(st widgetState) (needsYou, mail []string) {
	for _, s := range st.sessions {
		if s.State == session.StateNotify {
			needsYou = append(needsYou, s.Name)
		}
		if s.OwedMail {
			mail = append(mail, s.Name)
		}
	}
	return needsYou, mail
}

func (w attentionWidget) icon(st widgetState) string {
	needsYou, mail := w.counts(st)
	return attentionPills(st.styles, len(needsYou), len(mail))
}

func (w attentionWidget) expand(st widgetState, width int) string {
	needsYou, mail := w.counts(st)
	if len(needsYou)+len(mail) == 0 {
		return st.styles.Muted.Render("nothing needs you")
	}
	var parts []string
	if len(needsYou) > 0 {
		parts = append(parts, st.styles.NeedsYouPill.Render(glyphU(glyphNeedsYou)+" "+strings.Join(needsYou, ", ")))
	}
	if len(mail) > 0 {
		parts = append(parts, st.styles.MailPill.Render(glyphMail+" "+strings.Join(mail, ", ")))
	}
	return ansi.Truncate(strings.Join(parts, " "), width, "…")
}

// ---- host: which box this is. Matters now that the same UI runs on the
// ---- Mac; collapsed it is just the short hostname.

type hostWidget struct{ name string }

// hostname is the short host name, resolved once: a hostname does not change
// under a running popup, and icon() runs at spinner rate.
func hostname() string {
	h, err := os.Hostname()
	if err != nil {
		return "?"
	}
	short, _, _ := strings.Cut(h, ".")
	return short
}

func newHostWidget(map[string]string) (widget, error) { return hostWidget{name: hostname()}, nil }

func (w hostWidget) icon(st widgetState) string {
	return st.styles.Muted.Render(w.name)
}

func (w hostWidget) expand(st widgetState, width int) string {
	line := st.styles.Header.Render(w.name) + st.styles.Muted.Render(fmt.Sprintf("  ·  %d sessions", len(st.sessions)))
	return ansi.Truncate(line, width, "…")
}

// ---- agents: how many agent sessions are working / idle / need you. The
// ---- ambient count that was removed from the fixed header (DECISIONS,
// ---- d7819a3) -- back as an opt-in widget, where it costs nothing unless
// ---- asked for.

type agentsWidget struct{}

func (agentsWidget) tally(st widgetState) (working, idle, notify int) {
	for _, s := range st.sessions {
		switch s.State {
		case session.StateWorking:
			working++
		case session.StateIdle:
			idle++
		case session.StateNotify:
			notify++
		}
	}
	return working, idle, notify
}

func (w agentsWidget) icon(st widgetState) string {
	working, idle, _ := w.tally(st)
	if working+idle == 0 {
		return ""
	}
	return st.styles.Working.Render(fmt.Sprintf("%d", working)) + st.styles.Muted.Render(fmt.Sprintf("/%d", idle))
}

func (w agentsWidget) expand(st widgetState, width int) string {
	working, idle, notify := w.tally(st)
	line := fmt.Sprintf("%d working · %d idle · %d need you", working, idle, notify)
	return ansi.Truncate(st.styles.Muted.Render(line), width, "…")
}

// ---- shell: the plugin escape hatch. `{"name":"shell","args":{"cmd":"…",
// ---- "icon":"…"}}`. The command runs on the reload tick, off the main
// ---- loop, with a timeout; the first line of its output is the widget.

// widgetExecTimeout bounds every command a widget runs so none can stall the
// header. The reload tick is 2s; a widget that takes longer than this is
// simply stale until the next tick.
const widgetExecTimeout = 1500 * time.Millisecond

type shellWidget struct {
	cmd    string
	glyph  string
	output string
	err    error
}

func newShellWidget(args map[string]string) (widget, error) {
	cmd := strings.TrimSpace(args["cmd"])
	if cmd == "" {
		return nil, fmt.Errorf("needs args.cmd")
	}
	glyph := args["icon"]
	if glyph == "" {
		glyph = "$"
	}
	return &shellWidget{cmd: cmd, glyph: glyph}, nil
}

func (w *shellWidget) icon(st widgetState) string {
	if w.err != nil {
		return st.styles.Error.Render(w.glyph)
	}
	if w.output == "" {
		return ""
	}
	return st.styles.Muted.Render(w.glyph)
}

func (w *shellWidget) expand(st widgetState, width int) string {
	if w.err != nil {
		return ansi.Truncate(st.styles.Error.Render(w.glyph+" "+w.err.Error()), width, "…")
	}
	return ansi.Truncate(w.glyph+" "+w.output, width, "…")
}

func (w *shellWidget) poll() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), widgetExecTimeout)
		defer cancel()
		out, err := exec.CommandContext(ctx, "sh", "-c", w.cmd).Output()
		first, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
		return widgetPollMsg{src: w, output: first, err: err}
	}
}

func (w *shellWidget) absorb(msg widgetPollMsg) {
	w.output, w.err = msg.output, msg.err
}
