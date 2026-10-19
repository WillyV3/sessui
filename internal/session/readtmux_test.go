package session

import (
	"os/exec"
	"strings"
	"testing"
)

// privateServer starts a tmux server of its own, so a test can kill panes
// without touching whatever sessions the machine running it is using.
func privateServer(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("no tmux")
	}
	t.Setenv("TMUX_TMPDIR", t.TempDir())
	t.Setenv("TMUX", "") // never inherit the ambient server

	if out, err := exec.Command("tmux", "new-session", "-d", "-s", "probe", "-x", "80", "-y", "24", "cat").CombinedOutput(); err != nil {
		t.Skipf("tmux would not start a private server: %v: %s", err, out)
	}
	t.Cleanup(func() { exec.Command("tmux", "kill-server").Run() })
}

// A speculated pane that has closed makes tmux exit non-zero, and the session
// and pane lists it already printed must survive that. Otherwise the build
// cannot finish, the dead pane is never forgotten, and every refresh after it
// fails the same way.
func TestReadTmuxSurvivesDeadSpeculatedPane(t *testing.T) {
	privateServer(t)

	reading, err := readTmux([]agentPaneRef{{ID: "%99999", Height: 24}})
	if err != nil {
		t.Fatalf("readTmux: %v", err)
	}
	if !strings.Contains(reading.sessOut, "probe") {
		t.Errorf("session list lost: %q", reading.sessOut)
	}
	if reading.paneOut == "" {
		t.Error("pane list lost")
	}
	if reading.captures != nil {
		t.Errorf("captures should be absent, got %v", reading.captures)
	}
	// The pane list has to be usable, not merely non-empty: a build runs on it.
	if _, agentPane := Build(reading.sessOut, reading.paneOut, peerFleet{}, ""); agentPane == nil {
		t.Error("Build rejected the recovered pane list")
	}
}

// The ordinary path: a live pane is captured in the same call as the lists.
func TestReadTmuxCapturesSpeculatedPane(t *testing.T) {
	privateServer(t)

	out, err := exec.Command("tmux", "list-panes", "-t", "probe", "-F", "#{pane_id}|#{pane_height}").Output()
	if err != nil {
		t.Fatalf("list-panes: %v", err)
	}
	id, height, _ := strings.Cut(strings.TrimSpace(string(out)), "|")
	if height != "24" {
		t.Skipf("pane height is %q, not the 24 asked for", height)
	}

	reading, err := readTmux([]agentPaneRef{{ID: id, Height: 24}})
	if err != nil {
		t.Fatalf("readTmux: %v", err)
	}
	if _, ok := reading.captures[id]; !ok {
		t.Errorf("pane %s not captured in the batch: %v", id, reading.captures)
	}
	if got := len(reading.captures); got != 1 {
		t.Errorf("captured %d panes, want 1", got)
	}
}

// A reading with no speculation asks for no captures at all.
func TestReadTmuxWithoutSpeculation(t *testing.T) {
	privateServer(t)

	reading, err := readTmux(nil)
	if err != nil {
		t.Fatalf("readTmux: %v", err)
	}
	if reading.captures != nil {
		t.Errorf("captures taken without being asked: %v", reading.captures)
	}
	if strings.Contains(reading.paneOut, listMarkerCaptures) {
		t.Error("capture marker leaked into the pane list")
	}
}
