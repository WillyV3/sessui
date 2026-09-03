package ui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// statusIdle is `omarchy-shell media status` captured live on omarchy,
// 2026-09-03: a Chromium player registered, nothing loaded.
const statusIdle = `{"hasPlayer":true,"hasMedia":false,"playing":false,"identity":"Chromium","desktopEntry":"","title":"","artist":"","album":"","artUrl":"file:///tmp/.org.chromium.Chromium.12mZu6","canGoNext":false,"canGoPrevious":false,"canTogglePlaying":false}`

// statusPlaying is the same shape with a track loaded -- constructed from the
// captured field set, not captured itself.
const statusPlaying = `{"hasPlayer":true,"hasMedia":true,"playing":true,"identity":"Spotify","desktopEntry":"spotify","title":"Blue in Green","artist":"Miles Davis","album":"Kind of Blue","artUrl":"","canGoNext":true,"canGoPrevious":true,"canTogglePlaying":true}`

// fakeMedia records every command the widget issues.
type fakeMedia struct {
	payload string
	err     error
	calls   []string
}

func (f *fakeMedia) status() ([]byte, error) { return []byte(f.payload), f.err }
func (f *fakeMedia) media(m string) error    { f.calls = append(f.calls, "media "+m); return nil }
func (f *fakeMedia) volume(v string) error   { f.calls = append(f.calls, "volume "+v); return nil }

func newFakeNowPlaying(payload string) (*nowPlayingWidget, *fakeMedia) {
	f := &fakeMedia{payload: payload}
	return &nowPlayingWidget{client: f, available: true}, f
}

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

func TestNowPlaying_HiddenUntilSomethingPlays(t *testing.T) {
	st := widgetState{styles: testStyles()}
	w, _ := newFakeNowPlaying(statusIdle)
	drain(t, w.poll())
	if got := w.icon(st); got != "" {
		t.Errorf("icon with no media = %q, want hidden", got)
	}
	if got := w.expand(st, 80); !strings.Contains(got, "nothing playing") {
		t.Errorf("expand with no media = %q", got)
	}
}

func TestNowPlaying_ShowsTheTrack(t *testing.T) {
	st := widgetState{styles: testStyles()}
	w, _ := newFakeNowPlaying(statusPlaying)
	drain(t, w.poll())
	if got := w.icon(st); !strings.Contains(got, glyphU(glyphNote)) {
		t.Errorf("icon = %q, want the note", got)
	}
	got := w.expand(st, 80)
	for _, want := range []string{glyphPlay, "Miles Davis – Blue in Green", "Spotify"} {
		if !strings.Contains(got, want) {
			t.Errorf("expand = %q, want %q", got, want)
		}
	}
	if narrow := w.expand(st, 20); len([]rune(narrow)) > 20 {
		t.Errorf("expand at 20 = %d runes: %q", len([]rune(narrow)), narrow)
	}
}

func TestNowPlaying_TransportKeysThenRefresh(t *testing.T) {
	w, f := newFakeNowPlaying(statusPlaying)

	cases := []struct {
		key  tea.KeyMsg
		want string
	}{
		{tea.KeyMsg{Type: tea.KeySpace}, "media playPause"},
		{tea.KeyMsg{Type: tea.KeyLeft}, "media previous"},
		{tea.KeyMsg{Type: tea.KeyRight}, "media next"},
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}}, "media sourceNext"},
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'+'}}, "volume raise"},
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'='}}, "volume raise"},
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'-'}}, "volume lower"},
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}}, "volume mute-toggle"},
	}
	for _, c := range cases {
		f.calls = nil
		f.payload = statusIdle // the poll after the key must be what the widget shows
		drain(t, w.handle(c.key))
		if len(f.calls) != 1 || f.calls[0] != c.want {
			t.Errorf("%s: calls = %v, want [%s]", c.key, f.calls, c.want)
		}
		if w.status.HasMedia {
			t.Errorf("%s: widget did not refresh after the command", c.key)
		}
		f.payload = statusPlaying
	}

	f.calls = nil
	if cmd := w.handle(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}}); cmd != nil || len(f.calls) != 0 {
		t.Errorf("an unbound key ran something: cmd=%v calls=%v", cmd, f.calls)
	}
}

func TestNowPlaying_ServiceErrorIsShownNotHidden(t *testing.T) {
	st := widgetState{styles: testStyles()}
	w, f := newFakeNowPlaying("")
	f.err = errors.New("exit status 1")
	drain(t, w.poll())
	if got := w.expand(st, 80); !strings.Contains(got, "exit status 1") {
		t.Errorf("expand after a failed status = %q, want the error", got)
	}
	f.err = nil
	f.payload = "not json"
	drain(t, w.poll())
	if w.err == nil {
		t.Error("a malformed payload must be an error, not silently empty")
	}
}

func TestNowPlaying_UnavailableBoxHidesItself(t *testing.T) {
	w := &nowPlayingWidget{client: &fakeMedia{}, available: false}
	if w.poll() != nil {
		t.Error("an unavailable widget must not poll")
	}
	if got := w.icon(widgetState{styles: testStyles()}); got != "" {
		t.Errorf("icon = %q, want hidden", got)
	}
}
