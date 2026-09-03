package ui

import (
	"errors"
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// pwDumpFixture is the shape pw-dump gave on omarchy, 2026-09-03, trimmed to
// the objects that matter plus the traps: a "settings" metadata object whose
// values are numbers (must not break the parse), an input stream, a second
// sink that is not the default. Node 90 is invented on the same shape: a
// suspended, muted cliamp -- an app with no MPRIS at all.
const pwDumpFixture = `[
 {"id":0,"type":"PipeWire:Interface:Metadata","props":{"metadata.name":"settings"},"metadata":[{"subject":0,"key":"clock.rate","type":"","value":48000},{"subject":0,"key":"log.level","type":"","value":"2"}]},
 {"id":1,"type":"PipeWire:Interface:Metadata","props":{"metadata.name":"default"},"metadata":[{"subject":0,"key":"default.audio.sink","type":"Spa:String:JSON","value":{"name":"alsa_output.pci-0000_05_00.6.analog-stereo"}},{"subject":0,"key":"default.audio.source","type":"Spa:String:JSON","value":{"name":"alsa_input.usb"}}]},
 {"id":45,"type":"PipeWire:Interface:Node","info":{"state":"running","props":{"node.name":"alsa_output.pci-0000_05_00.6.analog-stereo","node.description":"Ryzen HD Audio Controller Analog Stereo","media.class":"Audio/Sink"},"params":{"Props":[{"volume":1.0,"mute":false,"channelVolumes":[0.274617,0.274617]}]}}},
 {"id":55,"type":"PipeWire:Interface:Node","info":{"state":"suspended","props":{"node.name":"alsa_output.hdmi","node.description":"Radeon HDMI","media.class":"Audio/Sink"},"params":{"Props":[{"volume":1.0,"mute":false,"channelVolumes":[0.064,0.064]}]}}},
 {"id":84,"type":"PipeWire:Interface:Node","info":{"state":"running","props":{"application.name":"Chromium","media.name":"Chromium input","node.name":"Chromium","media.class":"Stream/Input/Audio"},"params":{"Props":[{"volume":1.0,"mute":false,"channelVolumes":[1.0]}]}}},
 {"id":85,"type":"PipeWire:Interface:Node","info":{"state":"running","props":{"application.name":"Chromium","media.name":"Playback","node.name":"Chromium","media.class":"Stream/Output/Audio"},"params":{"Props":[{"volume":1.0,"mute":false,"channelVolumes":[1.0,1.0]}]}}},
 {"id":90,"type":"PipeWire:Interface:Node","info":{"state":"suspended","props":{"application.name":"cliamp","media.name":"Playback","node.name":"cliamp","media.class":"Stream/Output/Audio"},"params":{"Props":[{"volume":1.0,"mute":true,"channelVolumes":[0.512,0.512]}]}}},
 {"id":91,"type":"PipeWire:Interface:Port","info":{"props":{"port.id":0,"port.name":"output_FL"}}}
]`

// statusIdle is `omarchy-shell media status` captured live on omarchy,
// 2026-09-03: a Chromium player registered, nothing loaded.
const statusIdle = `{"hasPlayer":true,"hasMedia":false,"playing":false,"identity":"Chromium","desktopEntry":"","title":"","artist":"","album":"","artUrl":"file:///tmp/.org.chromium.Chromium.12mZu6","canGoNext":false,"canGoPrevious":false,"canTogglePlaying":false}`

// statusPlaying is the same shape with a track loaded in Chromium --
// constructed from the captured field set, not captured itself.
const statusPlaying = `{"hasPlayer":true,"hasMedia":true,"playing":true,"identity":"Chromium","desktopEntry":"chromium","title":"Blue in Green","artist":"Miles Davis","album":"Kind of Blue","artUrl":"","canGoNext":true,"canGoPrevious":true,"canTogglePlaying":true}`

type fakeAudio struct {
	dump  string
	err   error
	calls []string
}

func (f *fakeAudio) graph() ([]byte, error) { return []byte(f.dump), f.err }
func (f *fakeAudio) adjust(target string, delta int) error {
	f.calls = append(f.calls, "adjust "+target+" "+strconv.Itoa(delta))
	return nil
}
func (f *fakeAudio) toggleMute(target string) error {
	f.calls = append(f.calls, "mute "+target)
	return nil
}

type fakeMedia struct {
	payload string
	err     error
	calls   []string
}

func (f *fakeMedia) status() ([]byte, error) { return []byte(f.payload), f.err }
func (f *fakeMedia) media(m string) error    { f.calls = append(f.calls, "media "+m); return nil }
func (f *fakeMedia) volume(v string) error   { f.calls = append(f.calls, "volume "+v); return nil }

// drain runs a Cmd the way the runtime would and feeds a widgetPollMsg back
// into its source, exactly as Model does.
func drain(t *testing.T, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return
	}
	if msg, ok := cmd().(widgetPollMsg); ok {
		msg.src.absorb(msg)
	}
}

func newFakeAudio(media string) (*audioWidget, *fakeAudio, *fakeMedia) {
	pw := &fakeAudio{dump: pwDumpFixture}
	w := &audioWidget{pw: pw}
	var md *fakeMedia
	if media != "" {
		md = &fakeMedia{payload: media}
		w.media = md
	}
	return w, pw, md
}

func keyRune(r rune) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}} }

func TestParsePWDump(t *testing.T) {
	g, err := parsePWDump([]byte(pwDumpFixture))
	if err != nil {
		t.Fatal(err)
	}
	if g.Sink.ID != 45 || g.Sink.App != "out" || g.Sink.Muted || !g.Sink.Running {
		t.Errorf("sink = %+v, want node 45 'out' unmuted running", g.Sink)
	}
	if got := int(g.Sink.Volume*100 + 0.5); got != 65 {
		t.Errorf("sink volume = %d%%, want 65 (cbrt of 0.2746, what wpctl prints)", got)
	}
	if len(g.Streams) != 2 {
		t.Fatalf("streams = %+v, want Chromium and cliamp only (no input stream, no second sink)", g.Streams)
	}
	if c := g.Streams[0]; c.ID != 85 || c.App != "Chromium" || !c.Running || c.Muted || c.Volume != 1 {
		t.Errorf("stream 0 = %+v", c)
	}
	if c := g.Streams[1]; c.ID != 90 || c.App != "cliamp" || c.Running || !c.Muted || int(c.Volume*100+0.5) != 80 {
		t.Errorf("stream 1 = %+v, want suspended muted cliamp at 80%%", c)
	}

	if _, err := parsePWDump([]byte(`[{"id":1,"type":"PipeWire:Interface:Node","info":{"props":{"media.class":"Stream/Output/Audio"}}}]`)); err == nil {
		t.Error("a dump with no default sink must be an error")
	}
}

func TestAudioWidget_ChipsAndPick(t *testing.T) {
	st := widgetState{styles: testStyles()}
	w, pw, _ := newFakeAudio("")
	drain(t, w.poll())

	if got := w.icon(st); !strings.Contains(got, "65%") {
		t.Errorf("icon = %q, want the master volume", got)
	}
	got := w.expand(st, 120)
	for _, want := range []string{"out 65%", "Chromium 100%", "cliamp muted"} {
		if !strings.Contains(got, want) {
			t.Errorf("expand = %q, want %q", got, want)
		}
	}
	if strings.Contains(got, glyphPlay) || strings.Contains(got, glyphPause) {
		t.Errorf("expand = %q: no media service, so no track line", got)
	}

	// The sink is picked first; → walks the streams and wraps.
	drain(t, w.handle(keyRune('+')))
	if len(pw.calls) != 1 || pw.calls[0] != "adjust @DEFAULT_AUDIO_SINK@ 5" {
		t.Errorf("+ on the sink without omarchy = %v, want wpctl on the default sink", pw.calls)
	}
	w.handle(tea.KeyMsg{Type: tea.KeyRight})
	if w.selected().App != "Chromium" {
		t.Fatalf("→ picked %q, want Chromium", w.selected().App)
	}
	pw.calls = nil
	drain(t, w.handle(keyRune('-')))
	drain(t, w.handle(keyRune('m')))
	if want := []string{"adjust 85 -5", "mute 85"}; strings.Join(pw.calls, ",") != strings.Join(want, ",") {
		t.Errorf("stream keys = %v, want %v", pw.calls, want)
	}
	w.handle(tea.KeyMsg{Type: tea.KeyRight})
	w.handle(tea.KeyMsg{Type: tea.KeyRight})
	if w.selected().ID != 45 {
		t.Errorf("→→ from Chromium should wrap to the sink, got %+v", w.selected())
	}
	w.handle(tea.KeyMsg{Type: tea.KeyLeft})
	if w.selected().App != "cliamp" {
		t.Errorf("← from the sink should wrap to the last stream, got %+v", w.selected())
	}
}

func TestAudioWidget_SinkPrefersOmarchyOSD(t *testing.T) {
	w, pw, md := newFakeAudio(statusIdle)
	drain(t, w.poll())
	drain(t, w.handle(keyRune('+')))
	drain(t, w.handle(keyRune('m')))
	if len(pw.calls) != 0 || strings.Join(md.calls, ",") != "volume raise,volume mute-toggle" {
		t.Errorf("sink with omarchy: wpctl=%v omarchy=%v", pw.calls, md.calls)
	}
}

func TestAudioWidget_TrackFollowsThePickedApp(t *testing.T) {
	st := widgetState{styles: testStyles()}
	w, _, md := newFakeAudio(statusPlaying)
	drain(t, w.poll())

	// Sink picked: the track shows (the sink stands for everything).
	if got := w.expand(st, 140); !strings.Contains(got, glyphPlay+" Miles Davis – Blue in Green") {
		t.Errorf("sink picked: expand = %q, want the track", got)
	}
	w.handle(tea.KeyMsg{Type: tea.KeyRight}) // Chromium, whose identity matches
	if got := w.expand(st, 140); !strings.Contains(got, "Blue in Green") {
		t.Errorf("Chromium picked: expand = %q, want the track", got)
	}
	drain(t, w.handle(tea.KeyMsg{Type: tea.KeySpace}))
	drain(t, w.handle(keyRune(']')))
	if strings.Join(md.calls, ",") != "media playPause,media next" {
		t.Errorf("transport on Chromium = %v", md.calls)
	}

	w.handle(tea.KeyMsg{Type: tea.KeyRight}) // cliamp: no MPRIS, no track, no transport
	if got := w.expand(st, 140); strings.Contains(got, "Blue in Green") {
		t.Errorf("cliamp picked: expand = %q must not show Chromium's track", got)
	}
	md.calls = nil
	if cmd := w.handle(tea.KeyMsg{Type: tea.KeySpace}); cmd != nil || len(md.calls) != 0 {
		t.Errorf("space on cliamp ran something: %v %v", cmd, md.calls)
	}
}

func TestAudioWidget_PickSurvivesAStreamLeaving(t *testing.T) {
	w, pw, _ := newFakeAudio("")
	drain(t, w.poll())
	w.handle(tea.KeyMsg{Type: tea.KeyRight})
	w.handle(tea.KeyMsg{Type: tea.KeyRight}) // cliamp
	pw.dump = strings.Replace(pwDumpFixture, `"node.name":"cliamp","media.class":"Stream/Output/Audio"`, `"node.name":"cliamp","media.class":"Stream/Input/Audio"`, 1)
	drain(t, w.poll())
	if w.selected().ID != 45 {
		t.Errorf("after its stream left, the pick should fall back to the sink, got %+v", w.selected())
	}
}

func TestAudioWidget_ErrorsShowNotHide(t *testing.T) {
	st := widgetState{styles: testStyles()}
	w, pw, _ := newFakeAudio("")
	pw.err = errors.New("exit status 1")
	drain(t, w.poll())
	if got := w.expand(st, 80); !strings.Contains(got, "exit status 1") {
		t.Errorf("expand after a failed dump = %q", got)
	}
	if got := w.icon(st); !strings.Contains(got, glyphU(glyphNote)) {
		t.Errorf("icon after a failed dump = %q, want the note still visible", got)
	}
	pw.err, pw.dump = nil, "not json"
	drain(t, w.poll())
	if w.err == nil {
		t.Error("a malformed dump must be an error")
	}
}

func TestAudioWidget_AbsentWithoutPipeWire(t *testing.T) {
	was := audioAvailable
	audioAvailable = false
	t.Cleanup(func() { audioAvailable = was })
	w, err := newAudioWidget(nil)
	if w != nil || err != nil {
		t.Errorf("newAudioWidget without pipewire = %v, %v; want nil, nil", w, err)
	}
}
