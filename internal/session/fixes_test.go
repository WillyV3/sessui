package session

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

// tmux installed with nothing running is an empty list, not a failure.
func TestNoServerIsAnEmptyList(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("no tmux")
	}
	dir, err := os.MkdirTemp("/tmp", "sessui")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	t.Setenv("TMUX_TMPDIR", dir)
	t.Setenv("TMUX", "")

	for name, list := range map[string]func() ([]Session, error){"List": List, "ListFast": ListFast} {
		if sessions, err := list(); err != nil || len(sessions) != 0 {
			t.Errorf("%s with no server: %d sessions, err %v", name, len(sessions), err)
		}
	}
}

// ssh exits 255 whatever went wrong; the error must carry what it printed.
func TestHostErrSaysWhy(t *testing.T) {
	_, err := exec.Command("sh", "-c",
		`echo "Warning: noise" >&2; echo "ssh: connect to host 10.0.0.9 port 22: No route to host" >&2; exit 255`).Output()
	if got := hostErr(context.Background(), err).Error(); got != "connect to host 10.0.0.9 port 22: No route to host" {
		t.Errorf("hostErr = %q", got)
	}

	// Nothing printed: the exit status is all there is.
	_, err = exec.Command("sh", "-c", "exit 255").Output()
	if got := hostErr(context.Background(), err).Error(); got != "exit status 255" {
		t.Errorf("silent failure: hostErr = %q", got)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := hostErr(ctx, errors.New("signal: killed")).Error(); !strings.Contains(got, "no answer in") {
		t.Errorf("timed out: hostErr = %q", got)
	}
}

func TestAbbreviatePath(t *testing.T) {
	for _, tc := range []struct{ path, home, want string }{
		{"/home/u/projects/work/app", "/home/u", "~/p/w/app"},
		{"/home/u", "/home/u", "~"},
		{"/home/user2/x", "/home/u", "/h/u/x"}, // a home that is only a prefix of the name is not home
		{"/var/log/nginx", "/home/u", "/v/l/nginx"},
		{"/home/u/été/notes", "/home/u", "~/é/notes"}, // first rune, not first byte
		{"/", "/home/u", "/"},
		{"", "/home/u", ""},
		{"/srv/app", "", "/s/app"},
	} {
		if got := AbbreviatePath(tc.path, tc.home); got != tc.want {
			t.Errorf("AbbreviatePath(%q, %q) = %q, want %q", tc.path, tc.home, got, tc.want)
		}
	}
}

func TestHumanizeDuration(t *testing.T) {
	for _, tc := range []struct {
		d    time.Duration
		want string
	}{
		{0, "now"},
		{-time.Minute, "now"}, // a clock that disagrees is not "-60s ago"
		{8 * time.Second, "8s"},
		{59 * time.Second, "59s"},
		{time.Minute, "1m"},
		{2 * time.Hour, "2h"},
		{24 * time.Hour, "1d"},
		{6 * 24 * time.Hour, "6d"},
		{7 * 24 * time.Hour, "1w"},
		{30 * 24 * time.Hour, "4w"},
	} {
		if got := humanizeDuration(tc.d); got != tc.want {
			t.Errorf("humanizeDuration(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}

// fakeSSH puts a stand-in ssh first on PATH: it records that it ran and then
// stays up, as an attached ssh would. Call it before privateServer, because a
// tmux server runs its panes with the PATH it was started with.
func fakeSSH(t *testing.T) (ran string) {
	t.Helper()
	dir := t.TempDir()
	ran = filepath.Join(dir, "ssh-ran")
	script := "#!/bin/sh\ntouch " + ran + "\nexec sleep 60\n"
	if err := os.WriteFile(filepath.Join(dir, "ssh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return ran
}

// Entering a proxy session takes the local tmux out of the way: a proxy made
// now, and one an older sessui left on the server. A session that only looks
// like a proxy by its name keeps its prefix, and so does everything else.
func TestLocalTmuxStepsAsideInAProxy(t *testing.T) {
	sshRan := fakeSSH(t)
	privateServer(t)
	option := func(session, name string) string {
		t.Helper()
		// -A: an option the session does not set reads as the one it inherits.
		out, err := exec.Command("tmux", "show-options", "-A", "-v", "-t", exact(session)+":", name).Output()
		if err != nil {
			t.Fatalf("show-options %s %s: %v", session, name, err)
		}
		return strings.TrimSuffix(strings.TrimSpace(string(out)), "*")
	}
	settings := func(session string) string {
		return option(session, "prefix") + " " + option(session, "key-table") + " " + option(session, "status")
	}
	ordinary := settings("probe")

	old, lookalike := ProxyName("mac", "old"), "box/api"
	for name, cmd := range map[string]string{old: proxyCommand("mac", "old"), lookalike: "cat"} {
		if err := exec.Command("tmux", "new-session", "-d", "-s", name, cmd).Run(); err != nil {
			t.Fatal(err)
		}
	}
	// No client is attached in a test, so each switch itself fails; what it
	// does to the session on the way is what is under test.
	AttachRemote("mac", "new") // from the remote row: creates the proxy
	Switch(old)                // from the local row a proxy becomes
	Switch(lookalike)
	Switch("probe")

	// tmux returns before the pane's command has started.
	for deadline := time.Now().Add(3 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		if _, err := os.Stat(sshRan); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Error("the new proxy did not run ssh")
			break
		}
	}
	want := map[string]string{
		"prefix": "None", "prefix2": "None", "key-table": proxyTable,
		"status": "off", "detach-on-destroy": "off",
	}
	for _, proxy := range []string{old, ProxyName("mac", "new")} {
		for name, value := range want {
			if got := option(proxy, name); got != value {
				t.Errorf("%s: %s = %q, want %q", proxy, name, got, value)
			}
		}
	}
	for _, name := range []string{lookalike, "probe"} {
		if got := settings(name); got != ordinary {
			t.Errorf("%s is not a proxy and changed: %q, was %q", name, got, ordinary)
		}
	}
}

// From a view that is already inside another tmux, a remote row does not
// open and no ssh runs: not to attach, and not to create a session first.
func TestNestedViewOpensNoRemote(t *testing.T) {
	sshRan := fakeSSH(t)
	privateServer(t)
	t.Setenv("SESSUI_NESTED", "1")
	if err := AttachRemote("mac", "work"); !errors.Is(err, errNested) {
		t.Errorf("AttachRemote from a nested view: %v", err)
	}
	if err := NewRemote("mac", "work"); !errors.Is(err, errNested) {
		t.Errorf("NewRemote from a nested view: %v", err)
	}
	if _, err := os.Stat(sshRan); err == nil {
		t.Error("ssh ran from a nested view")
	}
	if hasSession(ProxyName("mac", "work")) {
		t.Error("a proxy session was left behind")
	}
	// A proxy that is already open over here is a local row, and leads back
	// out just the same.
	open := ProxyName("home", "base")
	if err := exec.Command("tmux", "new-session", "-d", "-s", open, proxyCommand("home", "base")).Run(); err != nil {
		t.Fatal(err)
	}
	if err := Switch(open); !errors.Is(err, errNested) {
		t.Errorf("Switch to an open proxy from a nested view: %v", err)
	}
}
