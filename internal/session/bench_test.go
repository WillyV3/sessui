package session

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
)

// Fixtures sized like a busy server: 20 sessions, 49 panes, agent panes whose
// screens are the 20 lines a classification reads.
func benchFixture() (sessOut, paneOut string) {
	var sess, panes strings.Builder
	for i := range 20 {
		name := "session-" + strconv.Itoa(i)
		fmt.Fprintf(&sess, "%s|1700000000|1700000900|0|2|17000009%02d\n", name, i)
		fmt.Fprintf(&panes, "%s|/home/u/p/%s|nvim|1|1|0|%%%d|51|editing %s\n", name, name, i*3, name)
		fmt.Fprintf(&panes, "%s|/home/u/p/%s|claude|1|0|0|%%%d|51|%s · claude\n", name, name, i*3+1, name)
		if i%2 == 0 {
			fmt.Fprintf(&panes, "%s|/home/u/p/%s|bash|0|1|0|%%%d|51|shell\n", name, name, i*3+2)
		}
	}
	return sess.String(), panes.String()
}

func BenchmarkBuild(b *testing.B) {
	sessOut, paneOut := benchFixture()
	fleet := peerFleet{Reachable: true, Rows: []peerRow{
		{Name: "session-1", Up: true, Summary: "doing a thing", Cwd: "/home/u/p/session-1"},
		{Name: "session-4", Up: true, Summary: "doing another", Cwd: "/home/u/p/session-4"},
	}}
	b.ReportAllocs()
	for b.Loop() {
		Build(sessOut, paneOut, fleet, "session-0")
	}
}

func BenchmarkClassifyCapture(b *testing.B) {
	// A working pane: the status line is the last thing on screen.
	var screen strings.Builder
	for i := range 19 {
		fmt.Fprintf(&screen, "  some tool output line %d with a bit of width to it\n", i)
	}
	screen.WriteString("✽ Precipitating… (2m 17s · ↓ 8.8k tokens)")
	capture := screen.String()

	b.ReportAllocs()
	for b.Loop() {
		if Classify(true, false, capture) != StateWorking {
			b.Fatal("fixture no longer classifies as working")
		}
		extractWorkingStatus(capture)
	}
}

func BenchmarkClassifyIdle(b *testing.B) {
	// The expensive shape: nothing matches, so every line is examined.
	var screen strings.Builder
	for i := range 20 {
		fmt.Fprintf(&screen, "│ conversation text line %d, no status line here at all │\n", i)
	}
	capture := screen.String()

	b.ReportAllocs()
	for b.Loop() {
		if Classify(true, false, capture) != StateIdle {
			b.Fatal("fixture no longer classifies as idle")
		}
	}
}
