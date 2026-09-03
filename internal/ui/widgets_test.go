package ui

import (
	"strings"
	"testing"

	"github.com/willyv3/sessui/internal/session"
)

func TestResolveWidgets(t *testing.T) {
	t.Run("every catalog name resolves", func(t *testing.T) {
		var settings []widgetSetting
		for _, n := range widgetNames() {
			settings = append(settings, widgetSetting{Name: n, Args: map[string]string{"cmd": "true"}})
		}
		ws, err := resolveWidgets(settings)
		if err != nil {
			t.Fatalf("resolveWidgets(catalog): %v", err)
		}
		if len(ws) != len(widgetCatalog) {
			t.Errorf("got %d widgets, want %d", len(ws), len(widgetCatalog))
		}
	})

	t.Run("an unknown name is reported and the rest survive", func(t *testing.T) {
		ws, err := resolveWidgets([]widgetSetting{{Name: "attention"}, {Name: "weather"}, {Name: "host"}})
		if err == nil || !strings.Contains(err.Error(), `"weather"`) {
			t.Fatalf("want an error naming weather, got %v", err)
		}
		if len(ws) != 2 {
			t.Errorf("got %d widgets, want the 2 known ones", len(ws))
		}
	})

	t.Run("shell without a command is a config error", func(t *testing.T) {
		if _, err := resolveWidgets([]widgetSetting{{Name: "shell"}}); err == nil || !strings.Contains(err.Error(), "cmd") {
			t.Errorf("want an error about args.cmd, got %v", err)
		}
	})
}

func TestAttentionWidget(t *testing.T) {
	st := widgetState{styles: testStyles(), sessions: []session.Session{
		{Name: "jim", State: session.StateNotify},
		{Name: "caretaker", OwedMail: true},
		{Name: "quiet"},
	}}
	w := attentionWidget{}

	icon := w.icon(st)
	if !strings.Contains(icon, glyphU(glyphNeedsYou)+" 1") || !strings.Contains(icon, glyphMail+" 1") {
		t.Errorf("icon = %q, want both pills at 1", icon)
	}
	expanded := w.expand(st, 80)
	for _, want := range []string{"jim", "caretaker"} {
		if !strings.Contains(expanded, want) {
			t.Errorf("expand = %q, want it to name %s", expanded, want)
		}
	}
	if strings.Contains(expanded, "quiet") {
		t.Errorf("expand = %q, must not name a session that needs nothing", expanded)
	}

	if got := w.icon(widgetState{styles: st.styles}); got != "" {
		t.Errorf("icon with nothing pending = %q, want hidden", got)
	}
}

func TestAgentsWidget(t *testing.T) {
	styles := testStyles()
	w := agentsWidget{}

	if got := w.icon(widgetState{styles: styles, sessions: []session.Session{{Name: "shell"}}}); got != "" {
		t.Errorf("icon with no agents = %q, want hidden", got)
	}

	st := widgetState{styles: styles, sessions: []session.Session{
		{State: session.StateWorking}, {State: session.StateWorking},
		{State: session.StateIdle}, {State: session.StateNotify},
	}}
	if got := w.icon(st); !strings.Contains(got, "2") || !strings.Contains(got, "/1") {
		t.Errorf("icon = %q, want working/idle as 2/1", got)
	}
	if got := w.expand(st, 80); !strings.Contains(got, "2 working · 1 idle · 1 need you") {
		t.Errorf("expand = %q", got)
	}
}

func TestHostWidget(t *testing.T) {
	st := widgetState{styles: testStyles(), sessions: make([]session.Session, 3)}
	w := hostWidget{}
	if got := w.icon(st); !strings.Contains(got, hostname()) {
		t.Errorf("icon = %q, want the hostname", got)
	}
	if got := w.expand(st, 80); !strings.Contains(got, "3 sessions") {
		t.Errorf("expand = %q, want the session count", got)
	}
}

// runPoll executes a poller's Cmd synchronously and feeds the result back,
// the way Model does across the event loop.
func runPoll(t *testing.T, p poller) widgetPollMsg {
	t.Helper()
	msg, ok := p.poll()().(widgetPollMsg)
	if !ok {
		t.Fatalf("poll produced %T, want widgetPollMsg", msg)
	}
	p.absorb(msg)
	return msg
}

func TestShellWidget(t *testing.T) {
	styles := testStyles()

	t.Run("first output line is the widget", func(t *testing.T) {
		w, err := newShellWidget(map[string]string{"cmd": "printf 'one\\ntwo\\n'", "icon": "%"})
		if err != nil {
			t.Fatal(err)
		}
		sw := w.(*shellWidget)
		if got := sw.icon(widgetState{styles: styles}); got != "" {
			t.Errorf("icon before the first poll = %q, want hidden", got)
		}
		if msg := runPoll(t, sw); msg.err != nil {
			t.Fatalf("poll: %v", msg.err)
		}
		if got := sw.icon(widgetState{styles: styles}); !strings.Contains(got, "%") {
			t.Errorf("icon = %q, want the configured glyph", got)
		}
		got := sw.expand(widgetState{styles: styles}, 80)
		if !strings.Contains(got, "one") || strings.Contains(got, "two") {
			t.Errorf("expand = %q, want only the first line", got)
		}
	})

	t.Run("a failing command shows as an error, never hides", func(t *testing.T) {
		w, _ := newShellWidget(map[string]string{"cmd": "exit 3"})
		sw := w.(*shellWidget)
		if msg := runPoll(t, sw); msg.err == nil {
			t.Fatal("want a non-nil err from exit 3")
		}
		if got := sw.icon(widgetState{styles: styles}); !strings.Contains(got, "$") {
			t.Errorf("icon after failure = %q, want the default glyph visible", got)
		}
		if got := sw.expand(widgetState{styles: styles}, 80); !strings.Contains(got, "exit status 3") {
			t.Errorf("expand after failure = %q, want the error", got)
		}
	})

	t.Run("expand truncates to width", func(t *testing.T) {
		w, _ := newShellWidget(map[string]string{"cmd": "true"})
		sw := w.(*shellWidget)
		sw.absorb(widgetPollMsg{output: strings.Repeat("x", 200)})
		if got := sw.expand(widgetState{styles: styles}, 20); len([]rune(got)) > 20 {
			t.Errorf("expand at 20 = %d runes: %q", len([]rune(got)), got)
		}
	})
}
