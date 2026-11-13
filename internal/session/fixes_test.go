package session

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// tmux tries a bare target as a pattern and then a prefix. With "work" gone,
// killing it must fail, not take "work-prod" with it; the same guess sent a
// remote attach into the wrong session.
func TestTargetsNameExactlyOneSession(t *testing.T) {
	privateServer(t)
	for _, name := range []string{"work-prod", "box/api-v2"} {
		if out, err := exec.Command("tmux", "new-session", "-d", "-s", name, "cat").CombinedOutput(); err != nil {
			t.Fatalf("new-session %s: %v: %s", name, err, out)
		}
	}

	if err := Kill("work"); err == nil {
		t.Error(`Kill("work") succeeded with no such session`)
	}
	if err := Rename("work", "stolen"); err == nil {
		t.Error(`Rename("work") succeeded with no such session`)
	}
	if !hasSession("work-prod") {
		t.Fatal(`"work-prod" was killed or renamed by a command aimed at "work"`)
	}
	if hasSession("box/api") {
		t.Error(`hasSession("box/api") matched "box/api-v2"`)
	}

	if err := Rename("work-prod", "work"); err != nil || !hasSession("work") {
		t.Errorf("an exact rename must still work: %v", err)
	}
	if err := Kill("work"); err != nil || hasSession("work") {
		t.Errorf("an exact kill must still work: %v", err)
	}
}

// A session whose name holds "|" used to split its own record and vanish, and
// with no locale -- what a non-interactive ssh has -- tmux prints "café" as
// "caf_" unless told otherwise. Read from a real server, because both claims
// are about what tmux prints.
func TestOddSessionNamesAreListed(t *testing.T) {
	privateServer(t)
	for _, v := range []string{"LANG", "LC_ALL", "LC_CTYPE"} {
		t.Setenv(v, "")
	}
	want := []string{"a|b", "café", "trailing|"}
	for _, name := range want {
		if out, err := exec.Command("tmux", "new-session", "-d", "-s", name, "cat").CombinedOutput(); err != nil {
			t.Fatalf("new-session %q: %v: %s", name, err, out)
		}
	}
	reading, err := readTmux(nil)
	if err != nil {
		t.Fatal(err)
	}
	sessions, _ := Build(reading.sessOut, reading.paneOut, peerFleet{}, "")
	got := map[string]Session{}
	for _, s := range sessions {
		got[s.Name] = s
	}
	// Windows is the field before the name: a record split in the wrong place
	// would lose one or the other. Nothing about the pane is asserted, since
	// its process may not have started yet.
	for _, name := range want {
		if s, ok := got[name]; !ok || s.Windows != 1 {
			t.Errorf("%q: listed=%v, record %+v", name, ok, s)
		}
	}
}

// The same path exists on every machine that syncs a home directory, so a
// session joins only a peer running on this one.
func TestPeerJoinsBySameMachine(t *testing.T) {
	rows := []peerRow{
		{Name: "elsewhere", Up: true, Machine: "xps", Cwd: "/home/u/app"},
		{Name: "here", Up: true, Machine: "Omarchy", Cwd: "/home/u/app"},
		{Name: "old-cp3", Up: true, Cwd: "/home/u/legacy"}, // a cp3 that reports no machine
	}
	for _, tc := range []struct {
		machine, cwd, want string
	}{
		{"omarchy", "/home/u/app", "here"},
		{"xps", "/home/u/app", "elsewhere"},
		{"homelab", "/home/u/app", ""},
		{"homelab", "/home/u/legacy", "old-cp3"},
		{"", "/home/u/app", "elsewhere"}, // machine unknown: path alone, first row wins
	} {
		byCWD, byName := indexPeers(peerFleet{Rows: rows, Machine: tc.machine})
		if got := byCWD[tc.cwd]; got != tc.want {
			t.Errorf("machine %q, cwd %s: joined %q, want %q", tc.machine, tc.cwd, got, tc.want)
		}
		if len(byName) != len(rows) {
			t.Errorf("machine %q: names are fleet-wide, got %d of %d", tc.machine, len(byName), len(rows))
		}
	}
}

// cp3 renamed its marker file; a workspace may hold either, and the current
// name wins.
func TestPeerMarkerName(t *testing.T) {
	write := func(dir, file, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, file), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	none, old, current, both := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	write(old, ".claude-peers-agent", "legacy\n")
	write(current, ".cp3-peer-name", "  keeper \nsecond line\n")
	write(both, ".claude-peers-agent", "legacy\n")
	write(both, ".cp3-peer-name", "keeper\n")

	for dir, want := range map[string]string{none: "", old: "legacy", current: "keeper", both: "keeper", "": ""} {
		if got := peerMarkerName(dir); got != want {
			t.Errorf("peerMarkerName(%q) = %q, want %q", dir, got, want)
		}
	}
}

func TestPiIsAnAgent(t *testing.T) {
	if !IsAgentApp("pi") || IsAgentApp("pip") {
		t.Error("pi must count as an agent, and only pi")
	}
}
