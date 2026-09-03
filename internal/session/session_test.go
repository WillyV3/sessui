package session

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// Sample raw output captured from real `tmux list-sessions`,
// `tmux list-panes -a`, and `cp3 peers` invocations, pane titles added per
// the shapes verified live on this fleet on 2026-09-02 (real claude titles
// carry "✳ "; shell panes just show the prompt title).
const sampleSessOut = `Jim|1788360311|1788364693|0|1
audio-viz|1788360310|1788360313|0|1
doorboard|1788361652|1788364000|0|1
sontara-sales|1788360311|1788364833|1|1
website|1788360311|1788360313|0|2
`

// samplePaneOut columns: session_name|pane_current_path|pane_current_command|
// window_active|pane_active|window_bell_flag|pane_id|pane_title (paneFormat).
// Jim and doorboard each run claude in their window's focused pane
// (pane_active=1) alongside an unfocused nvim pane; sontara-sales is the
// same shape but with its window's bell flag set, to exercise Notify.
// website runs no agent. Jim's claude pane title merely echoes the session
// name ("✳ Jim"); doorboard's and sontara-sales' carry a genuine live
// summary.
const samplePaneOut = `Jim|/home/willy|claude|1|1|0|%1|✳ Jim
Jim|/home/willy|nvim|1|0|0|%2|willy@omarchy:~
audio-viz|/home/willy/projects/audio-viz|bash|1|1|0|%3|willy@omarchy:~/projects/audio-viz
audio-viz|/home/willy/projects/audio-viz|nvim|1|0|0|%4|willy@omarchy:~/projects/audio-viz
doorboard|/home/willy/projects/doorboard|claude|1|1|0|%5|✳ Galaxy tablets setup and redirection
doorboard|/home/willy/projects/doorboard|nvim|1|0|0|%6|willy@omarchy:~/projects/doorboard
sontara-sales|/home/willy/hfl/sontara-sales|claude|1|1|1|%7|✳ Report to caretaker
sontara-sales|/home/willy/hfl/sontara-sales|bash|1|0|1|%8|willy@omarchy:~/hfl/sontara-sales
website|/home/willy/projects/v3Consult|bash|1|1|0|%9|willy@omarchy:~/projects/v3Consult
website|/home/willy/projects|bash|1|0|0|%10|willy@omarchy:~/projects
website|/home/willy/projects/v3Consult|bash|0|1|0|%11|willy@omarchy:~/projects/v3Consult
website|/home/willy/projects/v3Consult|nvim|0|0|0|%12|willy@omarchy:~/projects/v3Consult
`

const samplePeerOut = `AGENT       MACHINE  CWD
doorboard   omarchy  /home/willy/projects/doorboard
jim         omarchy  /home/willy
stilgar     omarchy  /home/willy/hfl-projects/sontara-sales
`

// sampleGroundTruthJSON is `cp3 peers --json`'s real shape, captured live
// (see the task spec) -- an array server-filtered to peers that are either
// up or carry pending mail. sontara-web is the real live example of a down
// peer with unread mail and no cwd/machine at all.
const sampleGroundTruthJSON = `[{"name":"astrobot","up":true,"pending":0,"machine":"omarchy","cwd":"/home/willy/hfl-projects/astrobot"},{"name":"astrobot-omarchy","up":true,"pending":0,"machine":"omarchy","cwd":"/home/willy/hfl-projects/astrobot"},{"name":"doorboard","up":true,"pending":0,"machine":"omarchy","cwd":"/home/willy/projects/doorboard","summary":"doorboard: blocked on caretaker...","last_mail_secs":12064},{"name":"sontara-web","up":false,"pending":1}]`

func TestParseSessions(t *testing.T) {
	got := parseSessions(sampleSessOut)
	if len(got) != 5 {
		t.Fatalf("got %d sessions, want 5", len(got))
	}

	want := sessionMeta{
		name:     "sontara-sales",
		created:  time.Unix(1788360311, 0),
		activity: time.Unix(1788364833, 0),
		attached: true,
		windows:  1,
	}
	var found *sessionMeta
	for i := range got {
		if got[i].name == "sontara-sales" {
			found = &got[i]
		}
	}
	if found == nil {
		t.Fatal("sontara-sales not found")
	}
	if *found != want {
		t.Errorf("got %+v, want %+v", *found, want)
	}
}

func TestParseSessions_SkipsMalformedLines(t *testing.T) {
	raw := "good|1|2|0|1\nnot-enough-fields|1|2\n\n"
	got := parseSessions(raw)
	if len(got) != 1 || got[0].name != "good" {
		t.Fatalf("got %+v, want single 'good' entry", got)
	}
}

func TestParsePanes_CWDPrefersActiveWindow(t *testing.T) {
	cwds, _, _, _, _ := parsePanes(samplePaneOut)

	cases := map[string]string{
		"Jim":           "/home/willy",
		"audio-viz":     "/home/willy/projects/audio-viz",
		"sontara-sales": "/home/willy/hfl/sontara-sales",
		// website: only the active window's panes should be used, not the
		// inactive window's v3Consult panes.
		"website": "/home/willy/projects/v3Consult",
	}
	for name, want := range cases {
		if got := cwds[name]; got != want {
			t.Errorf("cwd[%s] = %q, want %q", name, got, want)
		}
	}
}

func TestParsePanes_CWDFallsBackWhenNoActivePane(t *testing.T) {
	raw := "sess|/home/willy/fallback|bash|0|0|0|%1|willy@omarchy:~\n"
	cwds, _, _, _, _ := parsePanes(raw)
	if got := cwds["sess"]; got != "/home/willy/fallback" {
		t.Errorf("cwd = %q, want fallback path", got)
	}
}

func TestParsePanes_AppsDistinctNonShell(t *testing.T) {
	_, apps, _, _, _ := parsePanes(samplePaneOut)

	cases := map[string][]string{
		"Jim":           {"claude", "nvim"},
		"audio-viz":     {"nvim"},
		"doorboard":     {"claude", "nvim"},
		"sontara-sales": {"claude"},
		"website":       {"nvim"},
	}
	for name, want := range cases {
		got := apps[name]
		if !reflect.DeepEqual(got, want) {
			t.Errorf("apps[%s] = %v, want %v", name, got, want)
		}
	}
}

func TestParsePanes_Bell(t *testing.T) {
	_, _, bell, _, _ := parsePanes(samplePaneOut)

	cases := map[string]bool{
		"Jim": false, "audio-viz": false, "doorboard": false,
		"sontara-sales": true, "website": false,
	}
	for name, want := range cases {
		if got := bell[name]; got != want {
			t.Errorf("bell[%s] = %v, want %v", name, got, want)
		}
	}
}

func TestParsePanes_AgentPanePrefersFocusedPane(t *testing.T) {
	_, _, _, agentPane, _ := parsePanes(samplePaneOut)

	cases := map[string]string{
		"Jim":           "%1", // claude, the focused pane in its window
		"doorboard":     "%5",
		"sontara-sales": "%7",
	}
	for name, want := range cases {
		if got := agentPane[name]; got != want {
			t.Errorf("agentPane[%s] = %q, want %q", name, got, want)
		}
	}
	for _, name := range []string{"audio-viz", "website"} {
		if got, ok := agentPane[name]; ok {
			t.Errorf("agentPane[%s] = %q, want no entry (no agent app running)", name, got)
		}
	}
}

func TestParsePanes_AgentPaneFallsBackToFirstSeenWhenNoneFocused(t *testing.T) {
	// claude is running in an unfocused pane (window_active=0, pane_active=0);
	// the session's truly-focused pane runs bash. Nothing matches the
	// window_active&&pane_active case, so the first agent pane seen wins.
	raw := "sess|/path|claude|0|0|0|%1|✳ Thinking\nsess|/path|bash|1|1|0|%2|willy@omarchy:~\n"
	_, _, _, agentPane, _ := parsePanes(raw)
	if got := agentPane["sess"]; got != "%1" {
		t.Errorf("agentPane[sess] = %q, want %q (fallback to first agent pane)", got, "%1")
	}
}

func TestParsePanes_Titles(t *testing.T) {
	_, _, _, _, titles := parsePanes(samplePaneOut)

	cases := map[string]string{
		"Jim":           "✳ Jim",
		"doorboard":     "✳ Galaxy tablets setup and redirection",
		"sontara-sales": "✳ Report to caretaker",
	}
	for name, want := range cases {
		if got := titles[name]; got != want {
			t.Errorf("titles[%s] = %q, want %q", name, got, want)
		}
	}
	for _, name := range []string{"audio-viz", "website"} {
		if got, ok := titles[name]; ok {
			t.Errorf("titles[%s] = %q, want no entry (no agent app running)", name, got)
		}
	}
}

func TestParsePanes_TitleFallsBackWhenNoneFocused(t *testing.T) {
	raw := "sess|/path|claude|0|0|0|%1|✳ Background work\nsess|/path|bash|1|1|0|%2|willy@omarchy:~\n"
	_, _, _, _, titles := parsePanes(raw)
	if got := titles["sess"]; got != "✳ Background work" {
		t.Errorf("titles[sess] = %q, want %q (fallback to first agent pane's title)", got, "✳ Background work")
	}
}

func TestParsePanes_TitleWithPipeSurvivesSplit(t *testing.T) {
	// pane_title is last in paneFormat specifically so a title containing
	// "|" (claude can put arbitrary text there) doesn't get truncated or
	// dropped by the field-count guard.
	raw := "sess|/path|claude|1|1|0|%1|✳ Report to caretaker | with a pipe\n"
	_, _, _, _, titles := parsePanes(raw)
	if got := titles["sess"]; got != "✳ Report to caretaker | with a pipe" {
		t.Errorf("titles[sess] = %q, want the full title with its pipe intact", got)
	}
}

func TestParsePeers(t *testing.T) {
	got := parsePeers(samplePeerOut)
	want := map[string]string{
		"doorboard": "omarchy",
		"jim":       "omarchy",
		"stilgar":   "omarchy",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParsePeers_EmptyInput(t *testing.T) {
	if got := parsePeers(""); len(got) != 0 {
		t.Errorf("got %v, want empty", got)
	}
	if got := parsePeers("AGENT  MACHINE  CWD\n"); len(got) != 0 {
		t.Errorf("got %v, want empty (header only)", got)
	}
}

func TestParsePeersPlain(t *testing.T) {
	got := parsePeersPlain(samplePeerOut)
	want := []peerRow{
		{Name: "doorboard", Up: true, Machine: "omarchy", Cwd: "/home/willy/projects/doorboard"},
		{Name: "jim", Up: true, Machine: "omarchy", Cwd: "/home/willy"},
		{Name: "stilgar", Up: true, Machine: "omarchy", Cwd: "/home/willy/hfl-projects/sontara-sales"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestParsePeersJSON(t *testing.T) {
	rows, err := parsePeersJSON(sampleGroundTruthJSON)
	if err != nil {
		t.Fatalf("parsePeersJSON: %v", err)
	}
	if len(rows) != 4 {
		t.Fatalf("got %d rows, want 4", len(rows))
	}

	byName := map[string]peerRow{}
	for _, r := range rows {
		byName[r.Name] = r
	}

	doorboard := byName["doorboard"]
	want := peerRow{
		Name: "doorboard", Up: true, Pending: 0, Machine: "omarchy",
		Cwd: "/home/willy/projects/doorboard", Summary: "doorboard: blocked on caretaker...",
		LastMailSecs: 12064,
	}
	if doorboard != want {
		t.Errorf("doorboard row = %+v, want %+v", doorboard, want)
	}

	sontaraWeb := byName["sontara-web"]
	if want := (peerRow{Name: "sontara-web", Up: false, Pending: 1}); sontaraWeb != want {
		t.Errorf("sontara-web row = %+v, want %+v (down, no cwd/machine at all)", sontaraWeb, want)
	}

	astrobot, astrobotOmarchy := byName["astrobot"], byName["astrobot-omarchy"]
	if astrobot.Cwd != astrobotOmarchy.Cwd || astrobot.Cwd != "/home/willy/hfl-projects/astrobot" {
		t.Errorf("astrobot/astrobot-omarchy should share a cwd (the ClaimWithFallback suffix case), got %q / %q",
			astrobot.Cwd, astrobotOmarchy.Cwd)
	}
}

func TestParsePeersJSON_Error(t *testing.T) {
	if _, err := parsePeersJSON("not json"); err == nil {
		t.Error("parsePeersJSON(invalid) = nil error, want an error")
	}
}

func TestBuild_ExcludesCurrentAndMergesEverything(t *testing.T) {
	got, agentPane := Build(sampleSessOut, samplePaneOut, peerFleet{Rows: parsePeersPlain(samplePeerOut), Reachable: true}, "Jim")

	names := make([]string, len(got))
	for i, s := range got {
		names[i] = s.Name
	}
	for _, n := range names {
		if n == "Jim" {
			t.Fatalf("current session Jim should be excluded, got names %v", names)
		}
	}
	if len(got) != 4 {
		t.Fatalf("got %d sessions, want 4 (5 minus current)", len(got))
	}

	var doorboard, sontaraSales, audioViz Session
	for _, s := range got {
		switch s.Name {
		case "doorboard":
			doorboard = s
		case "sontara-sales":
			sontaraSales = s
		case "audio-viz":
			audioViz = s
		}
	}

	if !doorboard.IsPeer() || doorboard.Machine != "omarchy" {
		t.Errorf("doorboard should be a peer on omarchy, got Machine=%q", doorboard.Machine)
	}
	if doorboard.CWD != "/home/willy/projects/doorboard" {
		t.Errorf("doorboard CWD = %q", doorboard.CWD)
	}
	if !reflect.DeepEqual(doorboard.Apps, []string{"claude", "nvim"}) {
		t.Errorf("doorboard Apps = %v", doorboard.Apps)
	}
	if got := doorboard.EffectiveSummary(); got != "Galaxy tablets setup and redirection" {
		t.Errorf("doorboard EffectiveSummary() = %q, want the real title summary (no cp3 summary in the plain-fallback path)", got)
	}

	// sontara-sales' cwd (/home/willy/hfl/sontara-sales) does not match
	// peer "stilgar"'s cwd (/home/willy/hfl-projects/sontara-sales) --
	// close but not equal -- so per the cwd join it must NOT be tagged as a
	// peer even though the names might tempt a name-based match.
	if sontaraSales.IsPeer() {
		t.Errorf("sontara-sales should not cwd-match any peer, got Machine=%q", sontaraSales.Machine)
	}
	if !sontaraSales.Attached {
		t.Errorf("sontara-sales should be attached")
	}
	if got := sontaraSales.EffectiveSummary(); got != "Report to caretaker" {
		t.Errorf("sontara-sales EffectiveSummary() = %q, want its own live title summary even though it's not a peer", got)
	}

	if audioViz.IsPeer() {
		t.Errorf("audio-viz has no matching peer, got Machine=%q", audioViz.Machine)
	}
	if audioViz.PeerName != "" {
		t.Errorf("audio-viz should have no peer relation at all, got PeerName=%q", audioViz.PeerName)
	}

	// Agent/State: doorboard and sontara-sales run claude, audio-viz doesn't.
	// sontara-sales' window has its bell flag set, so it must classify as
	// Notify even though Build never shells out to capture-pane.
	if !doorboard.Agent || doorboard.State != StateIdle {
		t.Errorf("doorboard Agent=%v State=%v, want Agent=true State=%v", doorboard.Agent, doorboard.State, StateIdle)
	}
	if !sontaraSales.Agent || sontaraSales.State != StateNotify {
		t.Errorf("sontara-sales Agent=%v State=%v, want Agent=true State=%v", sontaraSales.Agent, sontaraSales.State, StateNotify)
	}
	if audioViz.Agent || audioViz.State != StateNone {
		t.Errorf("audio-viz Agent=%v State=%v, want Agent=false State=%v", audioViz.Agent, audioViz.State, StateNone)
	}

	// agentPane names the focused pane for each agent session, for List to
	// target with capture-pane.
	if agentPane["doorboard"] != "%5" {
		t.Errorf("agentPane[doorboard] = %q, want %q", agentPane["doorboard"], "%5")
	}
	if agentPane["sontara-sales"] != "%7" {
		t.Errorf("agentPane[sontara-sales] = %q, want %q", agentPane["sontara-sales"], "%7")
	}
}

// TestBuild_SummaryEchoRejectedForOwnName runs the real Jim/jim shape
// through Build end-to-end (nobody excluded this time, so Jim itself is
// inspectable): its claude pane title is "✳ Jim", a pure echo of the
// session's own name, so Summary must come back "" -- even though Jim's
// cwd cwd-matches peer "jim" (lowercase), proving the echo check is
// case/hyphenation-insensitive (see NormalizeName), not a raw string ==.
func TestBuild_SummaryEchoRejectedForOwnName(t *testing.T) {
	got, _ := Build(sampleSessOut, samplePaneOut, peerFleet{Rows: parsePeersPlain(samplePeerOut), Reachable: true}, "nobody-is-current")

	var jim Session
	found := false
	for _, s := range got {
		if s.Name == "Jim" {
			jim, found = s, true
		}
	}
	if !found {
		t.Fatal("Jim not found")
	}
	if jim.Machine != "omarchy" || jim.PeerName != "jim" {
		t.Fatalf("Jim should cwd-match peer 'jim', got Machine=%q PeerName=%q", jim.Machine, jim.PeerName)
	}
	if jim.Summary != "" {
		t.Errorf("Jim.Summary = %q, want \"\" (title \"✳ Jim\" echoes the session's own name)", jim.Summary)
	}
}

// TestBuild_PeerJoin_SharedCWD exercises the ClaimWithFallback suffix case:
// two live peers (astrobot, astrobot-omarchy) registered at the same cwd.
// Both local sessions must resolve to the SAME peer identity -- whichever
// up row at that cwd came first -- since the join key is cwd, not name.
func TestBuild_PeerJoin_SharedCWD(t *testing.T) {
	const cwd = "/home/willy/hfl-projects/astrobot"
	sessOut := "astrobot|1788360311|1788364693|0|1\n" +
		"astrobot-omarchy|1788360311|1788364693|0|1\n"
	paneOut := "astrobot|" + cwd + "|claude|1|1|0|%1|✳ astrobot\n" +
		"astrobot-omarchy|" + cwd + "|claude|1|1|0|%2|✳ astrobot-omarchy\n"
	peers := []peerRow{
		{Name: "astrobot", Up: true, Machine: "omarchy", Cwd: cwd},
		{Name: "astrobot-omarchy", Up: true, Machine: "omarchy", Cwd: cwd},
	}

	got, _ := Build(sessOut, paneOut, peerFleet{Rows: peers, Reachable: true}, "current")
	byName := map[string]Session{}
	for _, s := range got {
		byName[s.Name] = s
	}

	for _, name := range []string{"astrobot", "astrobot-omarchy"} {
		s, ok := byName[name]
		if !ok {
			t.Fatalf("%s not found", name)
		}
		if s.Machine != "omarchy" || s.PeerName != "astrobot" {
			t.Errorf("%s: Machine=%q PeerName=%q, want omarchy/astrobot (first up row at the shared cwd)", name, s.Machine, s.PeerName)
		}
	}
}

// TestBuild_PeerJoin covers the down side of the join: a workspace with
// only a marker and no matching cp3 row at all (a plain zombie), one whose
// marker names a down peer that still has unread mail (the real
// sontara-web shape -- no cwd/machine on that row, so only the marker-name
// lookup can find it), a workspace whose marker is present but is live
// (cwd-matches an up row, and whose title also happens to just echo its
// peer's name), and an ordinary session with no marker at all.
func TestBuild_PeerJoin(t *testing.T) {
	root := t.TempDir()
	zombieDir := filepath.Join(root, "zombie")
	pendingDir := filepath.Join(root, "pending")
	liveDir := filepath.Join(root, "live")
	normalDir := filepath.Join(root, "normal")
	for _, d := range []string{zombieDir, pendingDir, liveDir, normalDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("MkdirAll(%s): %v", d, err)
		}
	}
	if err := os.WriteFile(filepath.Join(zombieDir, spawnPeerMarker), []byte("zombie-peer\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pendingDir, spawnPeerMarker), []byte("sontara-web\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(liveDir, spawnPeerMarker), []byte("live-peer\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	sessOut := "zombie|1788360311|1788364693|0|1\n" +
		"pending|1788360311|1788364693|0|1\n" +
		"live|1788360311|1788364693|0|1\n" +
		"normal|1788360311|1788364693|0|1\n"
	paneOut := "zombie|" + zombieDir + "|bash|1|1|0|%1|willy@omarchy:~\n" +
		"pending|" + pendingDir + "|bash|1|1|0|%2|willy@omarchy:~\n" +
		"live|" + liveDir + "|claude|1|1|0|%3|✳ live-peer\n" +
		"normal|" + normalDir + "|bash|1|1|0|%4|willy@omarchy:~\n"
	// Real shape: a down peer's row (sontara-web) carries no cwd/machine at
	// all -- only the workspace's own marker file can name it.
	peers := []peerRow{
		{Name: "live-peer", Up: true, Machine: "omarchy", Cwd: liveDir},
		{Name: "sontara-web", Up: false, Pending: 1},
	}

	got, _ := Build(sessOut, paneOut, peerFleet{Rows: peers, Reachable: true}, "current")
	byName := map[string]Session{}
	for _, s := range got {
		byName[s.Name] = s
	}

	if s := byName["zombie"]; !s.AgentExited() || s.OwedMail {
		t.Errorf("zombie: AgentExited()=%v OwedMail=%v, want true/false (marker present, no matching cp3 row at all)",
			s.AgentExited(), s.OwedMail)
	}
	if s := byName["pending"]; !s.AgentExited() || !s.OwedMail {
		t.Errorf("pending: AgentExited()=%v OwedMail=%v, want true/true (marker names a down peer with pending mail -- the real sontara-web shape)",
			s.AgentExited(), s.OwedMail)
	}
	if s := byName["live"]; s.AgentExited() || s.Machine != "omarchy" {
		t.Errorf("live: AgentExited()=%v Machine=%q, want false/omarchy (marker present but cwd-matches a live up peer)",
			s.AgentExited(), s.Machine)
	}
	if s := byName["live"]; s.Summary != "" {
		t.Errorf("live: Summary=%q, want \"\" (title \"✳ live-peer\" echoes the matched PEER name, not the session name)", s.Summary)
	}
	if s := byName["normal"]; s.AgentExited() || s.PeerName != "" {
		t.Errorf("normal: AgentExited()=%v PeerName=%q, want false/\"\" (no marker, no peer relation at all)",
			s.AgentExited(), s.PeerName)
	}
}

func TestSession_AgentExited(t *testing.T) {
	cases := []struct {
		name              string
		peerName, machine string
		want              bool
	}{
		{"down known peer", "sontara-web", "", true},
		{"live peer", "doorboard", "omarchy", false},
		{"no peer relation at all", "", "", false},
	}
	for _, c := range cases {
		s := Session{PeerName: c.peerName, Machine: c.machine}
		if got := s.AgentExited(); got != c.want {
			t.Errorf("%s: AgentExited() = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestSession_EffectiveSummary(t *testing.T) {
	cases := []struct {
		name string
		s    Session
		want string
	}{
		{
			"a deliberate cp3 summary beats a static terminal title",
			Session{PeerSummary: "doorboard: blocked on caretaker...", Summary: "Galaxy tablets setup and redirection"},
			"doorboard: blocked on caretaker...",
		},
		{"falls back to the native title summary", Session{Summary: "Report to caretaker"}, "Report to caretaker"},
		{"neither available", Session{}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.s.EffectiveSummary(); got != c.want {
				t.Errorf("EffectiveSummary() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestHasSpawnPeerMarker(t *testing.T) {
	dir := t.TempDir()
	if hasSpawnPeerMarker(dir) {
		t.Error("hasSpawnPeerMarker on empty dir = true, want false")
	}
	if hasSpawnPeerMarker("") {
		t.Error("hasSpawnPeerMarker(\"\") = true, want false")
	}
	if err := os.WriteFile(filepath.Join(dir, spawnPeerMarker), []byte("agent-name\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if !hasSpawnPeerMarker(dir) {
		t.Error("hasSpawnPeerMarker after writing marker = false, want true")
	}
}

func TestPeerMarkerName(t *testing.T) {
	dir := t.TempDir()
	if got := peerMarkerName(dir); got != "" {
		t.Errorf("peerMarkerName(no marker) = %q, want \"\"", got)
	}
	if got := peerMarkerName(""); got != "" {
		t.Errorf("peerMarkerName(\"\") = %q, want \"\"", got)
	}
	if err := os.WriteFile(filepath.Join(dir, spawnPeerMarker), []byte("sontara-web\nextra line\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if got := peerMarkerName(dir); got != "sontara-web" {
		t.Errorf("peerMarkerName = %q, want %q (first line only)", got, "sontara-web")
	}
}

func TestStripAgentTitleMarker(t *testing.T) {
	cases := []struct{ title, want string }{
		{"✳ Report to caretaker", "Report to caretaker"},
		{"willy@omarchy:~", ""},
		{"✳ Jim", "Jim"},
		{"", ""},
	}
	for _, c := range cases {
		if got := stripAgentTitleMarker(c.title); got != c.want {
			t.Errorf("stripAgentTitleMarker(%q) = %q, want %q", c.title, got, c.want)
		}
	}
}

func TestNormalizeName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Jim", "jim"},
		{"peer mcp maintainer", "peer-mcp-maintainer"},
		{"doorboard", "doorboard"},
		{"Report to caretaker", "report-to-caretaker"},
	}
	for _, c := range cases {
		if got := NormalizeName(c.in); got != c.want {
			t.Errorf("NormalizeName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestAgentSummary(t *testing.T) {
	cases := []struct {
		name                         string
		title, sessionName, peerName string
		want                         string
	}{
		{"real summary survives", "✳ Report to caretaker", "sontara-sales", "stilgar", "Report to caretaker"},
		{"shell title, no marker at all", "willy@omarchy:~/x", "sontara-sales", "", ""},
		{"session-name echo", "✳ Jim", "Jim", "", ""},
		{"session-name echo, case/hyphen-insensitive", "✳ peer mcp maintainer", "peer-mcp-maintainer", "", ""},
		{"peer-name echo (differs from session name)", "✳ astrobot", "astrobot-omarchy", "astrobot", ""},
		{"real summary with a matched peer name set", "✳ Galaxy tablets setup and redirection", "doorboard", "doorboard", "Galaxy tablets setup and redirection"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := agentSummary(c.title, c.sessionName, c.peerName); got != c.want {
				t.Errorf("agentSummary(%q, %q, %q) = %q, want %q", c.title, c.sessionName, c.peerName, got, c.want)
			}
		})
	}
}

func TestAge(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		created time.Time
		want    string
	}{
		{now.Add(-30 * time.Second), "30s"},
		{now.Add(-5 * time.Minute), "5m"},
		{now.Add(-5 * time.Hour), "5h"},
		{now.Add(-25 * time.Hour), "1d"},
		{now.Add(-72 * time.Hour), "3d"},
		{now.Add(-15 * 24 * time.Hour), "2w"},
		{now.Add(time.Hour), "now"}, // clock skew: never negative
	}
	for _, c := range cases {
		if got := Age(c.created, now); got != c.want {
			t.Errorf("Age(%v) = %q, want %q", c.created, got, c.want)
		}
	}
}

func TestLastActive(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		activity time.Time
		want     string
	}{
		{now, "now"},
		{now.Add(-500 * time.Millisecond), "now"},
		{now.Add(-8 * time.Second), "8s"},
		{now.Add(-59 * time.Second), "59s"},
		{now.Add(-3 * time.Minute), "3m"},
		{now.Add(-59 * time.Minute), "59m"},
		{now.Add(-2 * time.Hour), "2h"},
		{now.Add(-23 * time.Hour), "23h"},
		{now.Add(-5 * 24 * time.Hour), "5d"},
	}
	for _, c := range cases {
		if got := LastActive(c.activity, now); got != c.want {
			t.Errorf("LastActive(-%v) = %q, want %q", now.Sub(c.activity), got, c.want)
		}
	}
}

func TestAbbreviatePath(t *testing.T) {
	const home = "/home/willy"
	cases := []struct {
		path, want string
	}{
		{"/home/willy/projects/willys-tmux", "~/p/willys-tmux"},
		{"/home/willy/projects/work/willys-tmux", "~/p/w/willys-tmux"},
		{"/home/willy", "~"},
		{"/etc/nginx/sites-available/default", "/e/n/s/default"},
		{"/", "/"},
		{"", ""},
		{"/home/willy/projects", "~/projects"},
		{"/home/willywood/x", "/h/w/x"}, // not a home subpath (prefix collision), abbreviated as absolute
	}
	for _, c := range cases {
		if got := AbbreviatePath(c.path, home); got != c.want {
			t.Errorf("AbbreviatePath(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}

// TestBuild_PeerJoin_CP3Unreachable pins the difference between "cp3 says
// this peer is down" and "cp3 never answered". A .claude-peers-agent marker
// is only evidence of a DOWN peer when there is a roster to be absent from;
// with cp3 uninstalled or failing, the same marker means nothing about
// liveness.
//
// This is not hypothetical: ~/projects and ~/hfl-projects are Syncthing-
// replicated across the fleet, so those markers land on machines that have
// never run cp3 (macbook1 carries 8 of them with no cp3 installed). Before
// this, every one of them rendered as a dead peer -- a column of red dots
// claiming the fleet was down on a machine that had simply never asked.
//
// The reachable subtest is the true-positive control: identical input, an
// empty but ANSWERED roster, must still report the workspace as down. Without
// it this test would also pass if Build stopped reading markers entirely.
func TestBuild_PeerJoin_CP3Unreachable(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, spawnPeerMarker), []byte("caretaker\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	sessOut := "builder-area|1788360311|1788364693|0|1\n"
	paneOut := "builder-area|" + workspace + "|bash|1|1|0|%1|willy@omarchy:~\n"

	build := func(t *testing.T, fleet peerFleet) Session {
		t.Helper()
		got, _ := Build(sessOut, paneOut, fleet, "current")
		if len(got) != 1 {
			t.Fatalf("Build returned %d sessions, want 1", len(got))
		}
		return got[0]
	}

	t.Run("unreachable cp3 claims nothing about the peer", func(t *testing.T) {
		s := build(t, peerFleet{}) // zero value: cp3 absent or failed

		if s.PeerName != "" {
			t.Errorf("PeerName = %q, want \"\" (cp3 never answered, so the marker proves nothing)", s.PeerName)
		}
		if s.AgentExited() {
			t.Error("AgentExited() = true, want false (reporting a peer down on a roster we never received)")
		}
		if s.Machine != "" {
			t.Errorf("Machine = %q, want \"\"", s.Machine)
		}
	})

	t.Run("reachable cp3 with an empty roster does report the peer down", func(t *testing.T) {
		s := build(t, peerFleet{Reachable: true}) // answered; nobody is up

		if s.PeerName != "caretaker" {
			t.Errorf("PeerName = %q, want \"caretaker\" (marker names it, and cp3 answered)", s.PeerName)
		}
		if !s.AgentExited() {
			t.Error("AgentExited() = false, want true (cp3 answered and this peer was not in the roster)")
		}
	})
}
