package session

import "testing"

// panesFixture is `list-panes -a -F paneFormat` output, in order, covering what
// the parser has to get right: a session whose active pane is not its agent, a
// session whose agent pane is not in the active window, a bell, a repeated
// command, and a title holding the separator.
const panesFixture = "" +
	"$work|1|1|0|%1|51|nvim|/home/u/work|editing\n" +
	"$work|1|0|0|%2|40|claude|/home/u/work|work · claude\n" +
	"$work|0|1|0|%3|51|bash|/home/u/other|shell\n" +
	"$rang|1|1|1|%4|30|claude|/home/u/rang|needs you\n" +
	"$deep|1|1|0|%5|20|bash|/home/u/deep|shell\n" +
	"$deep|0|1|0|%6|25|claude|/home/u/deep|deep · claude\n" +
	"$dupe|1|1|0|%7|10|nvim|/home/u/dupe|a|b|c\n" +
	"$dupe|0|1|0|%8|10|nvim|/home/u/dupe|second nvim\n"

func TestParsePanesPicksAgentPane(t *testing.T) {
	cwds, apps, bell, agentPane, titles := parsePanes(panesFixture, map[string]string{"$work": "work", "$rang": "rang", "$deep": "deep", "$dupe": "dupe"})

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
