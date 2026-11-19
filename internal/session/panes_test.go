package session

import (
	"strings"
	"testing"
)

// panesFixture is `list-panes -a -F paneFormat` output, in order, covering what
// the parser has to get right: a session whose active pane is not its agent, a
// session whose agent pane is not in the active window, a bell, a repeated
// command, and a title holding the separator.
const panesFixture = "" +
	"$work|1|1|0|%1|51|||nvim|/home/u/work|editing\n" +
	"$work|1|0|0|%2|40|||claude|/home/u/work|work · claude\n" +
	"$work|0|1|0|%3|51|||bash|/home/u/other|shell\n" +
	"$rang|1|1|1|%4|30|||claude|/home/u/rang|needs you\n" +
	"$deep|1|1|0|%5|20|||bash|/home/u/deep|shell\n" +
	"$deep|0|1|0|%6|25|||claude|/home/u/deep|deep · claude\n" +
	"$dupe|1|1|0|%7|10|||nvim|/home/u/dupe|a|b|c\n" +
	"$dupe|0|1|0|%8|10|||nvim|/home/u/dupe|second nvim\n"

func TestParsePanesPicksAgentPane(t *testing.T) {
	cwds, apps, bell, agentPane, titles, _ := parsePanes(panesFixture, map[string]string{"$work": "work", "$rang": "rang", "$deep": "deep", "$dupe": "dupe"})

	// The active window's path wins over a later pane's.
	if got := cwds["work"]; got != "/home/u/work" {
		t.Errorf("cwd: got %q, want %q", got, "/home/u/work")
	}
	// An agent in the active window but an inactive pane is still the agent.
	if got := agentPane["work"]; got.ID != "%2" || got.Height != 40 {
		t.Errorf("work agent pane: got %+v, want {%%2 40}", got)
	}
	// No agent pane in an active window: the first one seen is the fallback.
	if got := agentPane["deep"]; got.ID != "%6" {
		t.Errorf("deep agent pane: got %+v, want %%6", got)
	}
	if !bell["rang"] {
		t.Error("rang: bell flag lost")
	}
	if bell["work"] {
		t.Error("work: bell flag invented")
	}
	// A title may contain the field separator; it is the last field.
	if got := titles["work"]; got != "work · claude" {
		t.Errorf("work title: got %q", got)
	}
	// Shells are not apps, and a command is listed once per session.
	if got := apps["dupe"]; len(got) != 1 || got[0] != "nvim" {
		t.Errorf("dupe apps: got %v, want [nvim]", got)
	}
	if got := apps["deep"]; len(got) != 1 || got[0] != "claude" {
		t.Errorf("deep apps: got %v, want [claude]", got)
	}
}

func TestBuildExcludesCurrentAndSortsByLastAttached(t *testing.T) {
	// id|created|activity|attached|windows|last_attached|name
	sessions := "" +
		"$work|1700000000|1700000900|1|2|1700000900|work\n" +
		"$rang|1700000000|1700000800|0|1|1700000500|rang\n" +
		"$here|1700000000|1700000700|1|1|1700000999|here\n"

	built, agentPane := Build(sessions, panesFixture, peerFleet{}, "here")

	if len(built) != 2 {
		t.Fatalf("got %d sessions, want 2 (current excluded)", len(built))
	}
	if built[0].Name != "work" {
		t.Errorf("first row: got %q, want work (most recently attached)", built[0].Name)
	}
	if !built[0].Agent {
		t.Error("work: agent not detected")
	}
	// A bell outranks a capture: the pane is never read for a notified session.
	for _, s := range built {
		if s.Name == "rang" && s.State != StateNotify {
			t.Errorf("rang state: got %v, want %v", s.State, StateNotify)
		}
	}
	if agentPane["work"].ID != "%2" {
		t.Errorf("agent pane not returned: %v", agentPane)
	}
}

// What a program reports about itself outranks what the bell and the screen
// suggest, and it is heard from every pane of the session, not only the
// agent's own.
func TestReportedStateIsBelieved(t *testing.T) {
	var sessions strings.Builder
	for _, name := range []string{"ask", "fail", "busy", "rest", "tool", "make", "rang", "odd", "dead"} {
		sessions.WriteString("$" + name + "|1700000000|1700000900|0|1|1700000900|" + name + "\n")
	}
	// id|window_active|pane_active|bell|pane|height|reported|message|command|cwd|title
	const panes = "" +
		"$ask|1|1|0|%1|40|idle||claude|/w/ask|at rest in the focused pane\n" +
		"$ask|1|0|0|%2|40|blocked|approve Bash: ls ¦ wc|codex|/w/ask|asking in a split\n" +
		"$fail|1|1|0|%3|40|error|401: key expired|pi|/w/fail|pi\n" +
		"$busy|1|1|0|%4|40|working||claude|/w/busy|busy\n" +
		"$rest|1|1|0|%5|40|done|wrote the report|claude|/w/rest|rest\n" +
		"$tool|1|1|0|%6|40|blocked|Apply 3 to add?|terraform|/w/tool|not an agent\n" +
		"$make|1|1|0|%7|40|working||cargo|/w/make|not an agent\n" +
		"$rang|1|1|1|%8|40|working||claude|/w/rang|bell not yet answered\n" +
		"$odd|1|1|0|%9|40|napping||claude|/w/odd|a state nobody defined\n" +
		"$dead|1|1|0|%10|40|blocked|Apply 3 to add?|bash|/w/dead|killed at its prompt, shell is back\n"

	built, agentPane := Build(sessions.String(), panes, peerFleet{}, "")

	// The screen says the opposite of each program: a status line in the
	// scrollback of the one at rest, nothing on the one at work.
	working := "✽ Precipitating… (2m 17s · ↓ 8.8k tokens)"
	refs := make([]agentPaneRef, 0, len(agentPane))
	for _, ref := range agentPane {
		refs = append(refs, ref)
	}
	lastPanes.put(refs) // so the captures below are taken as read, and tmux is not asked
	classifyAgents(built, agentPane, map[string]string{"%1": "", "%4": "", "%5": working, "%9": ""})

	want := map[string]struct {
		state  AgentState
		notice string
	}{
		"ask":  {StateNotify, "approve Bash: ls ¦ wc"},
		"fail": {StateNotify, "failed: 401: key expired"},
		"busy": {StateWorking, ""},
		"rest": {StateIdle, ""},
		"tool": {StateNotify, "Apply 3 to add?"},
		"make": {StateNone, ""},
		"rang": {StateNotify, ""},
		"odd":  {StateIdle, ""},
		"dead": {StateNone, ""},
	}
	if len(built) != len(want) {
		t.Fatalf("got %d sessions, want %d", len(built), len(want))
	}
	for _, s := range built {
		if w := want[s.Name]; s.State != w.state || s.Notice != w.notice {
			t.Errorf("%s: got %v %q, want %v %q", s.Name, s.State, s.Notice, w.state, w.notice)
		}
	}
}
