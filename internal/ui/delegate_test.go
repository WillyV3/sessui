package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/lipgloss"

	"github.com/willyv3/sessui/internal/session"
)

// testStyles builds a Styles bundle from literal colors, so these tests
// never shell out to omarchy-theme-color the way New() does. applyTheme
// also seeds icon colors and pathPalette (renderCWD/pathColor divides by
// len(pathPalette), so that has to run before any renderRow call).
func testStyles() Styles {
	palette := Palette{
		Accent: "#00AFFF", Background: "#1E1E2E", Foreground: "#CDD6F4", Muted: "#6C7086",
		Red: "#F38BA8", Green: "#A6E3A1", Yellow: "#F9E2AF", Blue: "#89B4FA",
		Magenta: "#F5C2E7", Cyan: "#94E2D5", Orange: "#FAB387",
	}
	applyTheme(palette)
	return newStyles(palette)
}

func TestRenderStatus_Priority(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	styles := testStyles()

	cases := []struct {
		name string
		s    session.Session
		want string
	}{
		{
			"Notify wins outright, over a summary and a working verb",
			session.Session{State: session.StateNotify, Summary: "Report to caretaker", WorkingVerb: "Kneading", WorkingElapsed: "1m"},
			"needs you",
		},
		{
			"a peer's own cp3 summary wins over a live working verb",
			session.Session{State: session.StateWorking, WorkingVerb: "Kneading", WorkingElapsed: "1m", PeerSummary: "doorboard: blocked on caretaker..."},
			"doorboard: blocked on caretaker...",
		},
		{
			"the native title summary wins over a working verb when there's no cp3 summary",
			session.Session{State: session.StateWorking, WorkingVerb: "Kneading", WorkingElapsed: "1m", Summary: "Galaxy tablets setup and redirection"},
			"Galaxy tablets setup and redirection",
		},
		{
			"a working verb shows when there's no summary at all",
			session.Session{State: session.StateWorking, WorkingVerb: "Concocting", WorkingElapsed: "4m 34s"},
			"Concocting… 4m 34s",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := renderStatus(styles, now, c.s)
			if !strings.Contains(got, c.want) {
				t.Errorf("renderStatus() = %q, want to contain %q", got, c.want)
			}
		})
	}

	// A plain shell (not an agent) with nothing to report is blank -- its
	// last-active time is its own column now.
	if got := renderStatus(styles, now, session.Session{State: session.StateIdle, Activity: now.Add(-5 * time.Minute)}); got != "" {
		t.Errorf("renderStatus() non-agent = %q, want \"\"", got)
	}

	// An agent parked past idleThreshold reads "idle"...
	parked := session.Session{Agent: true, State: session.StateIdle, Activity: now.Add(-idleThreshold - time.Minute)}
	if got := renderStatus(styles, now, parked); !strings.Contains(got, "idle") {
		t.Errorf("renderStatus() parked agent = %q, want \"idle\"", got)
	}
	// ...but a freshly-idle agent (under the threshold, may resume) stays blank.
	fresh := session.Session{Agent: true, State: session.StateIdle, Activity: now.Add(-time.Minute)}
	if got := renderStatus(styles, now, fresh); got != "" {
		t.Errorf("renderStatus() fresh idle agent = %q, want \"\" (under idleThreshold)", got)
	}
}

func TestRenderName(t *testing.T) {
	// The name cell is just the session name now (peer moved to its own
	// column) -- and never carries a peer suffix, even for a peer workspace.
	got := renderName(testStyles(), session.Session{Name: "sontara", Machine: "omarchy", PeerName: "astrobot"})
	if !strings.Contains(got, "sontara") || strings.Contains(got, "astrobot") {
		t.Errorf("renderName() = %q, want just \"sontara\" (peer belongs in its own column)", got)
	}
}

func TestRenderPeer(t *testing.T) {
	styles := testStyles()

	if got := renderPeer(styles, session.Session{Name: "audio-viz"}); got != "" {
		t.Errorf("no peer relation: renderPeer() = %q, want \"\"", got)
	}

	// Up + same name: filled dot, no name suffix (colour, stripped by lipgloss
	// in a non-TTY test, carries up/down -- a trivial Machine!="" branch).
	if up := renderPeer(styles, session.Session{Name: "doorboard", Machine: "omarchy", PeerName: "doorboard"}); !strings.Contains(up, "●") || strings.Contains(up, "doorboard") {
		t.Errorf("up, same name: renderPeer() = %q, want a filled dot and no name", up)
	}

	// Down + differing name (shared-cwd suffix case): hollow dot + the peer name.
	if down := renderPeer(styles, session.Session{Name: "sontara", PeerName: "astrobot"}); !strings.Contains(down, "○") || !strings.Contains(down, "astrobot") {
		t.Errorf("down, differing name: renderPeer() = %q, want a hollow dot + peer name", down)
	}

	// Same by case/hyphenation (Jim==jim): no name suffix.
	if got := renderPeer(styles, session.Session{Name: "Jim", Machine: "omarchy", PeerName: "jim"}); strings.Contains(got, "jim") {
		t.Errorf("case-only difference: renderPeer() = %q, want no name suffix", got)
	}

	// Owed mail: a ✉ marker.
	if got := renderPeer(styles, session.Session{Name: "sontara-web", PeerName: "sontara-web", OwedMail: true}); !strings.Contains(got, "✉") {
		t.Errorf("owed mail: renderPeer() = %q, want a ✉ marker", got)
	}
}

func TestRenderHeader_HasColumnLabels(t *testing.T) {
	header := renderHeader(testStyles())
	for _, label := range []string{"apps", "session", "cwd", "peer", "status"} {
		if !strings.Contains(header, label) {
			t.Errorf("renderHeader() = %q, want to contain %q", header, label)
		}
	}
	// The last-active column is labelled by its pulse glyph alone (no word).
	if !strings.Contains(header, glyphU(glyphActive)) {
		t.Errorf("renderHeader() = %q, want the active pulse glyph", header)
	}
}

func TestMarqueeOffset(t *testing.T) {
	// Nothing to reveal -> always pinned at 0, whatever the frame.
	for f := 0; f < 64; f++ {
		if got := marqueeOffset(f, 0); got != 0 {
			t.Fatalf("marqueeOffset(%d, 0) = %d, want 0", f, got)
		}
	}

	const overflow = 8
	// At frame 0 the schedule holds at 0 -- given the Model resets the frame
	// to 0 on every selection change (see TestMarquee_ResetsOnSelectionChange),
	// this is what makes a freshly-selected row show its start, not its middle.
	if got := marqueeOffset(0, overflow); got != 0 {
		t.Fatalf("marqueeOffset(0, %d) = %d, want 0 (start hold)", overflow, got)
	}

	// Over a full ping-pong cycle the offset stays in range and visits both
	// ends (so the whole string is revealed and the start comes back).
	seenStart, seenEnd := false, false
	for f := 0; f < 4*(2*5+overflow); f++ {
		o := marqueeOffset(f, overflow)
		if o < 0 || o > overflow {
			t.Fatalf("marqueeOffset(%d, %d) = %d, out of [0,%d]", f, overflow, o, overflow)
		}
		seenStart = seenStart || o == 0
		seenEnd = seenEnd || o == overflow
	}
	if !seenStart || !seenEnd {
		t.Fatalf("marquee never reached both ends: seenStart=%v seenEnd=%v", seenStart, seenEnd)
	}

	// The forward leg only ever moves right -- no backward jitter while it's
	// scrolling out to reveal the end.
	prev := 0
	for f := 5; f <= 5+overflow; f++ {
		if o := marqueeOffset(f, overflow); o < prev {
			t.Fatalf("forward leg jumped backward at frame %d: %d < %d", f, o, prev)
		} else {
			prev = o
		}
	}
}

// TestMarqueeCell_RevealsTail proves the feature end to end: a name too long
// for its column is permanently cut off on an unselected row, but the selected
// row scrolls far enough -- somewhere in its cycle -- to show the hidden tail.
func TestMarqueeCell_RevealsTail(t *testing.T) {
	styles := testStyles()
	sp := spinner.New(spinner.WithSpinner(spinner.MiniDot))
	now := time.Now()
	long := session.Session{Name: "this-is-a-very-long-session-name-indeed", Activity: now}

	if row := renderRow(styles, "/home/willy", now, long, sp, 0, false); strings.Contains(row, "indeed") {
		t.Fatalf("unselected row must truncate the tail, but it showed: %q", row)
	}

	revealed := false
	for f := 0; f < 200 && !revealed; f++ {
		revealed = strings.Contains(renderRow(styles, "/home/willy", now, long, sp, f, true), "indeed")
	}
	if !revealed {
		t.Fatal("selected row never scrolled far enough to reveal the hidden tail")
	}
}

// TestRenderRow_NoWrapAtPopupWidth proves the fixedCol fix (Inline + Width
// + MaxWidth) actually holds against real, long content: doorboard's real
// 190-odd-character cp3 summary and a real 19-character session name
// (peer-mcp-maintainer). Without Inline, lipgloss word-wraps content past
// its Width instead of the MaxWidth truncating it, breaking row alignment.
func TestRenderRow_NoWrapAtPopupWidth(t *testing.T) {
	styles := testStyles()
	sp := spinner.New(spinner.WithSpinner(spinner.MiniDot))
	now := time.Now()

	const longSummary = "doorboard: blocked on caretaker for NAND dump. Doing no-device prep: lpunpack (present at /usr/bin), RK3326 mainline DTs (odroid-go/rg351), homelab renderer + Sonia phone-app sketch."
	s := session.Session{
		Name:        "peer-mcp-maintainer",
		Created:     now.Add(-23 * time.Hour),
		Activity:    now,
		CWD:         "/home/willy/projects/peer-mcp-maintainer",
		Apps:        []string{"claude", "nvim"},
		Agent:       true,
		Machine:     "omarchy",
		PeerName:    "peer-mcp-maintainer",
		PeerSummary: longSummary,
	}

	row := renderRow(styles, "/home/willy", now, s, sp, 0, false)
	if h := lipgloss.Height(row); h != 1 {
		t.Fatalf("renderRow() height = %d, want 1 (long content must truncate, not wrap the row)", h)
	}
	if w := lipgloss.Width(row); w > usableWidth {
		t.Errorf("renderRow() width = %d, want <= %d (usable width inside the popup)", w, usableWidth)
	}

	header := renderHeader(styles)
	if hh := lipgloss.Height(header); hh != 1 {
		t.Errorf("renderHeader() height = %d, want 1", hh)
	}
	if hw := lipgloss.Width(header); hw > usableWidth {
		t.Errorf("renderHeader() width = %d, want <= %d", hw, usableWidth)
	}
}
