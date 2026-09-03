package ui

// now-playing: what is playing and a transport for it, as a header widget.
// A thin client of Omarchy's own media service -- `omarchy-shell media
// status` for the state, `omarchy-shell media <method>` for control,
// `omarchy-audio-output-volume` for volume -- so it sees whatever Omarchy's
// bar sees, for every player, and carries no MPRIS code of its own. Off a
// box without omarchy-shell (a Mac today) the widget simply hides.

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// mediaStatus is `omarchy-shell media status`, the fields this widget reads.
// Captured live 2026-09-03 (nowplaying_test.go carries the payload).
type mediaStatus struct {
	HasPlayer        bool   `json:"hasPlayer"`
	HasMedia         bool   `json:"hasMedia"`
	Playing          bool   `json:"playing"`
	Identity         string `json:"identity"`
	Title            string `json:"title"`
	Artist           string `json:"artist"`
	CanGoNext        bool   `json:"canGoNext"`
	CanGoPrevious    bool   `json:"canGoPrevious"`
	CanTogglePlaying bool   `json:"canTogglePlaying"`
}

// mediaClient is the two commands the widget shells out to. An interface so
// the tests drive the widget with a fake instead of a desktop.
type mediaClient interface {
	status() ([]byte, error)
	media(method string) error // omarchy-shell media <method>
	volume(verb string) error  // omarchy-audio-output-volume <verb>
}

// omarchyMedia shells out with the same timeout the shell widget uses:
// quickshell restarts on every theme switch, and a call that hangs across
// one must not pile up a goroutine per tick.
type omarchyMedia struct{}

func mediaCommand(name string, args ...string) (*exec.Cmd, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(context.Background(), widgetExecTimeout)
	return exec.CommandContext(ctx, name, args...), cancel
}

func (omarchyMedia) status() ([]byte, error) {
	cmd, cancel := mediaCommand("omarchy-shell", "media", "status")
	defer cancel()
	return cmd.Output()
}

func (omarchyMedia) media(method string) error {
	cmd, cancel := mediaCommand("omarchy-shell", "media", method)
	defer cancel()
	return cmd.Run()
}

func (omarchyMedia) volume(verb string) error {
	cmd, cancel := mediaCommand("omarchy-audio-output-volume", verb)
	defer cancel()
	return cmd.Run()
}

// mediaAvailable is true where omarchy-shell is on PATH: probed once.
var mediaAvailable = func() bool {
	_, err := exec.LookPath("omarchy-shell")
	return err == nil
}()

// nowPlayingKeys dispatches; the legend pairs prev/next and up/down into one
// entry each (track, vol) so the whole transport fits the footer at 108
// beside the three focus keys.
var nowPlayingKeys = struct{ toggle, prev, next, up, down, mute, source, track, vol key.Binding }{
	toggle: key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "play/pause")),
	prev:   key.NewBinding(key.WithKeys("left")),
	next:   key.NewBinding(key.WithKeys("right")),
	up:     key.NewBinding(key.WithKeys("+", "=")),
	down:   key.NewBinding(key.WithKeys("-")),
	mute:   key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "mute")),
	source: key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "source")),
	track:  key.NewBinding(key.WithKeys("left", "right"), key.WithHelp("←→", "track")),
	vol:    key.NewBinding(key.WithKeys("+", "-"), key.WithHelp("+/-", "vol")),
}

const (
	glyphNote  = 0xF001 // nf-fa-music, the codepoint iconMusic uses
	glyphPlay  = "▶"
	glyphPause = "⏸"
)

type nowPlayingWidget struct {
	client    mediaClient
	available bool
	status    mediaStatus
	err       error
}

func newNowPlayingWidget(map[string]string) (widget, error) {
	return &nowPlayingWidget{client: omarchyMedia{}, available: mediaAvailable}, nil
}

// icon is the note, coloured live while playing and muted while paused;
// nothing at all when no player has media -- a silent desk shows no widget.
func (w *nowPlayingWidget) icon(st widgetState) string {
	if !w.available || w.err != nil || !w.status.HasMedia {
		return ""
	}
	note := glyphU(glyphNote)
	if w.status.Playing {
		return st.styles.Working.Render(note)
	}
	return st.styles.Muted.Render(note)
}

func (w *nowPlayingWidget) expand(st widgetState, width int) string {
	switch {
	case !w.available:
		return st.styles.Muted.Render("no media service on this box")
	case w.err != nil:
		return ansi.Truncate(st.styles.Error.Render("media: "+w.err.Error()), width, "…")
	case !w.status.HasMedia:
		return st.styles.Muted.Render("nothing playing")
	}
	state := glyphPause
	if w.status.Playing {
		state = glyphPlay
	}
	track := w.status.Title
	if w.status.Artist != "" {
		track = w.status.Artist + " – " + track
	}
	line := st.styles.Working.Render(state) + " " + track + st.styles.Muted.Render("  ·  "+w.status.Identity)
	return ansi.Truncate(line, width, "…")
}

func (w *nowPlayingWidget) poll() tea.Cmd {
	if !w.available {
		return nil
	}
	return func() tea.Msg {
		out, err := w.client.status()
		return widgetPollMsg{src: w, output: string(out), err: err}
	}
}

func (w *nowPlayingWidget) absorb(msg widgetPollMsg) {
	w.err = msg.err
	if msg.err != nil {
		return
	}
	w.err = json.Unmarshal([]byte(strings.TrimSpace(msg.output)), &w.status)
}

// handle runs the transport command off the loop and then polls at once, in
// the same closure, so the header reflects the key the moment the service
// does -- not up to two seconds later on the next tick.
func (w *nowPlayingWidget) handle(k tea.KeyMsg) tea.Cmd {
	if !w.available {
		return nil
	}
	var run func() error
	switch {
	case key.Matches(k, nowPlayingKeys.toggle):
		run = func() error { return w.client.media("playPause") }
	case key.Matches(k, nowPlayingKeys.prev):
		run = func() error { return w.client.media("previous") }
	case key.Matches(k, nowPlayingKeys.next):
		run = func() error { return w.client.media("next") }
	case key.Matches(k, nowPlayingKeys.source):
		run = func() error { return w.client.media("sourceNext") }
	case key.Matches(k, nowPlayingKeys.up):
		run = func() error { return w.client.volume("raise") }
	case key.Matches(k, nowPlayingKeys.down):
		run = func() error { return w.client.volume("lower") }
	case key.Matches(k, nowPlayingKeys.mute):
		run = func() error { return w.client.volume("mute-toggle") }
	default:
		return nil
	}
	poll := w.poll()
	return func() tea.Msg {
		if err := run(); err != nil {
			return widgetPollMsg{src: w, err: fmt.Errorf("%s: %w", k, err)}
		}
		return poll()
	}
}

func (w *nowPlayingWidget) keys() []key.Binding {
	k := nowPlayingKeys
	return []key.Binding{k.toggle, k.track, k.vol, k.mute, k.source}
}
