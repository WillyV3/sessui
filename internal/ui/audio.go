package ui

// audio: every outgoing stream on the box, and a hand on each one.
//
// The PipeWire graph is the source of truth -- `pw-dump` lists the default
// sink and every playback stream with its app, state and volume, whether or
// not the app speaks MPRIS (cliamp does not; a browser tab, a game, a
// notification sound do not) -- and `wpctl` turns each knob by node id.
// Omarchy's media service (`omarchy-shell media`) is layered on top for the
// one thing the graph cannot say: which track is playing and how to skip
// it; its volume script is preferred for the master so the desktop OSD
// fires. Without PipeWire (a Mac today) the widget is absent.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os/exec"
	"slices"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// ---- the graph

// audioNode is the default sink or one playback stream. Volume is cubic --
// the number wpctl prints and a person expects -- derived from PipeWire's
// linear channelVolumes.
type audioNode struct {
	ID      int
	App     string
	Volume  float64
	Muted   bool
	Running bool
}

type audioGraph struct {
	Sink    audioNode
	Streams []audioNode // playback streams, in node-id order
}

// pwObject is the slice of a pw-dump entry this file reads. Metadata values
// stay raw because other metadata objects carry numbers and strings in the
// same slot; only default.audio.sink is decoded.
type pwObject struct {
	ID    int    `json:"id"`
	Type  string `json:"type"`
	Props struct {
		MetadataName string `json:"metadata.name"`
	} `json:"props"`
	Metadata []struct {
		Key   string          `json:"key"`
		Value json.RawMessage `json:"value"`
	} `json:"metadata"`
	Info struct {
		State string `json:"state"`
		Props struct {
			App        string `json:"application.name"`
			NodeName   string `json:"node.name"`
			NodeDesc   string `json:"node.description"`
			MediaClass string `json:"media.class"`
		} `json:"props"`
		Params struct {
			Props []struct {
				Mute           bool      `json:"mute"`
				ChannelVolumes []float64 `json:"channelVolumes"`
			} `json:"Props"`
		} `json:"params"`
	} `json:"info"`
}

const (
	pwTypeNode      = "PipeWire:Interface:Node"
	pwTypeMetadata  = "PipeWire:Interface:Metadata"
	pwClassSink     = "Audio/Sink"
	pwClassPlayback = "Stream/Output/Audio"
	pwStateRunning  = "running"
)

// parsePWDump reads the default sink and every playback stream out of a
// pw-dump. The default sink is named by the "default" metadata object; a
// dump without one (no audio device at all) is an error, not a silent
// empty graph.
func parsePWDump(data []byte) (audioGraph, error) {
	var objs []pwObject
	if err := json.Unmarshal(data, &objs); err != nil {
		return audioGraph{}, fmt.Errorf("pw-dump: %w", err)
	}
	defaultSink := ""
	for _, o := range objs {
		if o.Type != pwTypeMetadata || o.Props.MetadataName != "default" {
			continue
		}
		for _, m := range o.Metadata {
			if m.Key == "default.audio.sink" {
				var v struct {
					Name string `json:"name"`
				}
				_ = json.Unmarshal(m.Value, &v)
				defaultSink = v.Name
			}
		}
	}
	var g audioGraph
	found := false
	for _, o := range objs {
		if o.Type != pwTypeNode {
			continue
		}
		p := o.Info.Props
		switch {
		case p.MediaClass == pwClassSink && p.NodeName == defaultSink:
			g.Sink, found = pwNode(o, "out"), true
		case p.MediaClass == pwClassPlayback:
			g.Streams = append(g.Streams, pwNode(o, cmp(p.App, p.NodeName)))
		}
	}
	if !found {
		return g, errors.New("pw-dump: no default sink")
	}
	return g, nil
}

func cmp(first, fallback string) string {
	if first != "" {
		return first
	}
	return fallback
}

func pwNode(o pwObject, label string) audioNode {
	n := audioNode{ID: o.ID, App: label, Running: o.Info.State == pwStateRunning}
	for _, pr := range o.Info.Params.Props {
		if len(pr.ChannelVolumes) == 0 {
			continue
		}
		n.Muted = pr.Mute
		n.Volume = math.Cbrt(slices.Max(pr.ChannelVolumes))
		break
	}
	return n
}

// ---- the knobs

// audioBackend is the two commands the graph half of the widget runs. An
// interface so the tests drive it with a fake instead of a sound card.
type audioBackend interface {
	graph() ([]byte, error)                // pw-dump
	adjust(target string, delta int) error // wpctl set-volume <target> N%+|-
	toggleMute(target string) error        // wpctl set-mute <target> toggle
}

// volumeStep is one keypress, in percent -- Omarchy's own step.
const volumeStep = 5

// sinkTarget is wpctl's name for the default sink.
const sinkTarget = "@DEFAULT_AUDIO_SINK@"

type pipewireAudio struct{}

func (pipewireAudio) graph() ([]byte, error) {
	cmd, cancel := widgetCommand("pw-dump")
	defer cancel()
	return cmd.Output()
}

func (pipewireAudio) adjust(target string, delta int) error {
	sign := "+"
	if delta < 0 {
		sign, delta = "-", -delta
	}
	// -l 1.0: a stream above 100% only clips; the sink is Omarchy's script's
	// business (below) and it applies its own ceiling.
	cmd, cancel := widgetCommand("wpctl", "set-volume", "-l", "1.0", target, fmt.Sprintf("%d%%%s", delta, sign))
	defer cancel()
	return cmd.Run()
}

func (pipewireAudio) toggleMute(target string) error {
	cmd, cancel := widgetCommand("wpctl", "set-mute", target, "toggle")
	defer cancel()
	return cmd.Run()
}

// audioAvailable is true where PipeWire's tools are on PATH: probed once.
var audioAvailable = func() bool {
	_, dump := exec.LookPath("pw-dump")
	_, ctl := exec.LookPath("wpctl")
	return dump == nil && ctl == nil
}()

// ---- Omarchy's media service: the track, the transport, the OSD volume

// mediaStatus is `omarchy-shell media status`, the fields this widget reads.
// Captured live 2026-09-03 (audio_test.go carries the payload).
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

// mediaClient is the two Omarchy commands the widget shells out to.
type mediaClient interface {
	status() ([]byte, error)
	media(method string) error // omarchy-shell media <method>
	volume(verb string) error  // omarchy-audio-output-volume <verb>
}

type omarchyMedia struct{}

func (omarchyMedia) status() ([]byte, error) {
	cmd, cancel := widgetCommand("omarchy-shell", "media", "status")
	defer cancel()
	return cmd.Output()
}

func (omarchyMedia) media(method string) error {
	cmd, cancel := widgetCommand("omarchy-shell", "media", method)
	defer cancel()
	return cmd.Run()
}

func (omarchyMedia) volume(verb string) error {
	cmd, cancel := widgetCommand("omarchy-audio-output-volume", verb)
	defer cancel()
	return cmd.Run()
}

// mediaAvailable is true where omarchy-shell is on PATH: probed once.
var mediaAvailable = func() bool {
	_, err := exec.LookPath("omarchy-shell")
	return err == nil
}()

// widgetCommand is an exec bounded by widgetExecTimeout: quickshell
// restarts on every theme switch, and a call that hangs across one must not
// pile up a process per tick.
func widgetCommand(name string, args ...string) (*exec.Cmd, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(context.Background(), widgetExecTimeout)
	return exec.CommandContext(ctx, name, args...), cancel
}

// ---- the widget

const (
	glyphNote  = 0xF001 // nf-fa-music, the codepoint iconMusic uses
	glyphPlay  = "▶"
	glyphPause = "⏸"
)

// audioKeys dispatches; track and vol are legend-only pairings so the
// transport fits the footer beside the three focus keys.
var audioKeys = struct{ pick, up, down, mute, toggle, prev, next, vol, track key.Binding }{
	pick:   key.NewBinding(key.WithKeys("left", "right"), key.WithHelp("←→", "pick")),
	up:     key.NewBinding(key.WithKeys("+", "=")),
	down:   key.NewBinding(key.WithKeys("-", "_")),
	mute:   key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "mute")),
	toggle: key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "play/pause")),
	prev:   key.NewBinding(key.WithKeys("[")),
	next:   key.NewBinding(key.WithKeys("]")),
	vol:    key.NewBinding(key.WithKeys("+", "-"), key.WithHelp("+/-", "vol")),
	track:  key.NewBinding(key.WithKeys("[", "]"), key.WithHelp("[ ]", "track")),
}

// audioSnapshot is one poll: the graph, and the media service's view when
// there is one.
type audioSnapshot struct {
	graph    audioGraph
	media    mediaStatus
	mediaErr error
}

type audioWidget struct {
	pw    audioBackend
	media mediaClient // nil where omarchy-shell is absent
	snap  audioSnapshot
	err   error
	// selID is the node the keys act on: 0 is the sink, else a stream's id.
	// Kept by id, not index, so a stream appearing or leaving between polls
	// does not move the selection to a different app.
	selID int
}

func newAudioWidget(map[string]string) (widget, error) {
	if !audioAvailable {
		return nil, nil
	}
	w := &audioWidget{pw: pipewireAudio{}}
	if mediaAvailable {
		w.media = omarchyMedia{}
	}
	return w, nil
}

// nodes is the pick order: the sink, then every stream.
func (w *audioWidget) nodes() []audioNode {
	return append([]audioNode{w.snap.graph.Sink}, w.snap.graph.Streams...)
}

func (w *audioWidget) selected() audioNode {
	for _, n := range w.snap.graph.Streams {
		if n.ID == w.selID {
			return n
		}
	}
	return w.snap.graph.Sink
}

func (w *audioWidget) pick(delta int) {
	nodes := w.nodes()
	i := slices.IndexFunc(nodes, func(n audioNode) bool { return n.ID == w.selected().ID })
	w.selID = nodes[(i+delta+len(nodes))%len(nodes)].ID
}

func (w *audioWidget) playing() bool {
	return slices.ContainsFunc(w.snap.graph.Streams, func(n audioNode) bool { return n.Running && !n.Muted })
}

// track is the media service's current track when it belongs to n: the
// sink stands for everything, a stream only for the player whose identity
// is its app (Omarchy's own bar matches the two the same way, by label).
func (w *audioWidget) track(n audioNode) (mediaStatus, bool) {
	m := w.snap.media
	if w.media == nil || !m.HasMedia {
		return m, false
	}
	return m, n.ID == w.snap.graph.Sink.ID || strings.EqualFold(n.App, m.Identity)
}

func percent(n audioNode) string {
	if n.Muted {
		return "muted"
	}
	return strconv.Itoa(int(math.Round(n.Volume*100))) + "%"
}

func trackTitle(m mediaStatus) string {
	state := glyphPause
	if m.Playing {
		state = glyphPlay
	}
	title := m.Title
	if m.Artist != "" {
		title = m.Artist + " – " + title
	}
	return state + " " + title
}

// icon is the note and the master volume: live while something is
// audible, muted otherwise. Nothing until the first poll has landed -- a
// "0%" that means "not asked yet" would be a lie for a whole frame.
func (w *audioWidget) icon(st widgetState) string {
	if w.err != nil {
		return st.styles.Error.Render(glyphU(glyphNote))
	}
	if w.snap.graph.Sink.ID == 0 {
		return ""
	}
	label := glyphU(glyphNote) + " " + percent(w.snap.graph.Sink)
	if w.playing() {
		return st.styles.Working.Render(label)
	}
	return st.styles.Muted.Render(label)
}

// expand is the track for the picked node (if the media service knows one),
// then a chip per node -- out first, then each app -- with the picked one
// highlighted; a silent stream is faint.
func (w *audioWidget) expand(st widgetState, width int) string {
	if w.err != nil {
		return ansi.Truncate(st.styles.Error.Render("audio: "+w.err.Error()), width, "…")
	}
	var parts []string
	if m, ok := w.track(w.selected()); ok {
		parts = append(parts, st.styles.Working.Render(trackTitle(m)))
	}
	sel := w.selected().ID
	for _, n := range w.nodes() {
		label := n.App + " " + percent(n)
		switch {
		case n.ID == sel:
			label = highlightCell(st.styles.selectedBG, st.styles.Header.Render(" "+label+" "))
		case !n.Running || n.Muted:
			label = st.styles.Muted.Render(label)
		}
		parts = append(parts, label)
	}
	return ansi.Truncate(strings.Join(parts, "  "), width, "…")
}

func (w *audioWidget) poll() tea.Cmd {
	return func() tea.Msg {
		out, err := w.pw.graph()
		if err != nil {
			return widgetPollMsg{src: w, err: err}
		}
		g, err := parsePWDump(out)
		if err != nil {
			return widgetPollMsg{src: w, err: err}
		}
		snap := audioSnapshot{graph: g}
		if w.media != nil {
			raw, err := w.media.status()
			if err == nil {
				err = json.Unmarshal(bytes.TrimSpace(raw), &snap.media)
			}
			snap.mediaErr = err // the graph still renders; the track line just stays off
		}
		return widgetPollMsg{src: w, data: snap}
	}
}

func (w *audioWidget) absorb(msg widgetPollMsg) {
	w.err = msg.err
	if msg.err != nil {
		return
	}
	w.snap = msg.data.(audioSnapshot)
}

// handle: ←→ pick a node (no command); +/-/m act on it; space and [ ] drive
// the transport when the picked node has a track. Every command re-polls in
// the same closure so the header shows the result at once.
func (w *audioWidget) handle(k tea.KeyMsg) tea.Cmd {
	n := w.selected()
	isSink := n.ID == w.snap.graph.Sink.ID
	var run func() error
	switch {
	case key.Matches(k, audioKeys.pick):
		if key.Matches(k, key.NewBinding(key.WithKeys("left"))) {
			w.pick(-1)
		} else {
			w.pick(1)
		}
		return nil
	case key.Matches(k, audioKeys.up):
		run = w.volume(n, isSink, volumeStep, "raise")
	case key.Matches(k, audioKeys.down):
		run = w.volume(n, isSink, -volumeStep, "lower")
	case key.Matches(k, audioKeys.mute):
		if isSink && w.media != nil {
			run = func() error { return w.media.volume("mute-toggle") }
		} else {
			run = func() error { return w.pw.toggleMute(w.target(n, isSink)) }
		}
	case key.Matches(k, audioKeys.toggle):
		run = w.transport(n, "playPause")
	case key.Matches(k, audioKeys.prev):
		run = w.transport(n, "previous")
	case key.Matches(k, audioKeys.next):
		run = w.transport(n, "next")
	}
	if run == nil {
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

func (w *audioWidget) target(n audioNode, isSink bool) string {
	if isSink {
		return sinkTarget
	}
	return strconv.Itoa(n.ID)
}

// volume prefers Omarchy's script for the sink (it raises the OSD) and
// wpctl for everything else.
func (w *audioWidget) volume(n audioNode, isSink bool, delta int, verb string) func() error {
	if isSink && w.media != nil {
		return func() error { return w.media.volume(verb) }
	}
	return func() error { return w.pw.adjust(w.target(n, isSink), delta) }
}

// transport is a media-service call, only when the picked node has a track.
func (w *audioWidget) transport(n audioNode, method string) func() error {
	if _, ok := w.track(n); !ok {
		return nil
	}
	return func() error { return w.media.media(method) }
}

func (w *audioWidget) keys() []key.Binding {
	k := audioKeys
	bindings := []key.Binding{k.pick, k.vol, k.mute}
	if w.media != nil {
		bindings = append(bindings, k.toggle, k.track)
	}
	return bindings
}
