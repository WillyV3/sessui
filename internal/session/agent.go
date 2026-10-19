package session

import (
	"os/exec"
	"regexp"
	"strings"
	"sync"
)

// workingVerbLine matches an agent's live status line: a verb ending in an
// ellipsis ("Kneading…"), optionally preceded by one spinner glyph. Agents
// rotate through ~100 such verbs, so the shape is matched rather than a list.
//
// Anchored to the start of the line on purpose. A tool call's own output
// truncated with "…", or a status bar cut off by pane width, both end a word
// with an ellipsis deep inside a line; the real status line never does.
var workingVerbLine = regexp.MustCompile(`^\s*[^\s\p{L}]?\s*([\p{L}-]{3,})…`)

// elapsedInParens matches the first "(...)" on a status line, e.g. "(4m 34s · ↓ 18.6k tokens)".
var elapsedInParens = regexp.MustCompile(`\(([^)]+)\)`)

// AgentState is a session's AI-agent activity. It lives here rather than in
// the UI package because Session.State is typed with it.
type AgentState int

const (
	StateNone    AgentState = iota // no AI agent running in the session
	StateIdle                      // agent present, waiting for input
	StateWorking                   // agent is actively generating
	StateNotify                    // agent needs the user (tmux bell rang)
)

func (s AgentState) String() string {
	switch s {
	case StateIdle:
		return "idle"
	case StateWorking:
		return "working"
	case StateNotify:
		return "notify"
	default:
		return "none"
	}
}

// agentApps are commands that run an AI coding agent.
var agentApps = map[string]bool{
	"claude": true, "opencode": true, "codex": true, "copilot": true,
	"crush": true, "gemini": true, "aider": true,
	"cursor": true, "cline": true, "amp": true, "goose": true,
}

// IsAgentApp reports whether cmd is a recognized AI coding agent command.
func IsAgentApp(cmd string) bool { return agentApps[cmd] }

// liveStatusLine matches the whole shape of an agent's live status line: a
// word ending in an ellipsis, then a parenthesised running clock —
// "✽ Precipitating… (2m 17s · ↓ 8.8k tokens)".
//
// Every part of the shape must be on ONE line, and the clock is what makes it
// a status line rather than page text. An idle pane holds plenty of each half
// on its own: scrollback prose ends sentences in an ellipsis, chrome
// parenthesises hints ("(ctrl+End)", "(shift+tab to cycle)"), and the
// context-usage hint carries both a token count and a "↓" on unrelated lines.
var liveStatusLine = regexp.MustCompile(`\p{L}….*\([^)]*\b\d+(?:\.\d+)?\s*[smh]\b`)

// Classify derives a session's AgentState from whether it runs an agent, its
// tmux bell flag, and the tail of its agent pane. Pure, so it can be tested
// without tmux.
func Classify(agent, bell bool, capture string) AgentState {
	switch {
	case !agent:
		return StateNone
	case bell:
		return StateNotify
	case isWorking(capture):
		return StateWorking
	default:
		return StateIdle
	}
}

// isWorking matches an agent's on-screen working indicators, line by line.
//
// Nothing is matched across lines: two idle lines can otherwise satisfy one
// rule between them, which is how a pane parked at its prompt reads as busy.
// A line containing "done" is skipped — the completed-turn summary
// ("✻ Sautéed for 16m · done") keeps the verb on screen after the turn ends.
func isWorking(capture string) bool {
	for line := range strings.Lines(capture) {
		// Gate on a byte scan before folding case or running the pattern, both
		// of which cost orders of magnitude more. Every way a line can say
		// "working" carries one of these three: the status shape needs its
		// ellipsis, the interrupt hint spells it in either case, and the hammer
		// is itself. A pane of ordinary prose matches none and is skipped whole.
		if !strings.Contains(line, "…") &&
			!strings.Contains(line, "⚒") &&
			!strings.Contains(line, "nterrupt") {
			continue
		}
		lower := strings.ToLower(line)
		if strings.Contains(lower, "done") {
			continue
		}
		if strings.Contains(line, "⚒") ||
			strings.Contains(lower, "esc to interrupt") ||
			liveStatusLine.MatchString(line) {
			return true
		}
	}
	return false
}

// extractWorkingStatus pulls the verb and elapsed time off a Working agent's
// capture: "✽ Concocting… (4m 34s · ↓ 18.6k tokens)" -> ("Concocting", "4m 34s").
//
// It scans from the bottom, closest to the prompt, so a tool call's truncated
// output higher up can never win over the real status line. Both results are
// "" when the capture has no verb line; callers must tolerate that.
func extractWorkingStatus(capture string) (verb, elapsed string) {
	lines := strings.Split(capture, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		m := workingVerbLine.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		verb = m[1]
		if pm := elapsedInParens.FindStringSubmatch(lines[i]); pm != nil {
			inner, _, _ := strings.Cut(pm[1], "·")
			elapsed = strings.TrimSpace(inner)
		}
		return verb, elapsed
	}
	return "", ""
}

// agentPaneRef is the pane an agent session's state is read from. The height
// comes along because it is what lets several panes be captured in one call.
type agentPaneRef struct {
	ID     string
	Height int
}

// capturePanes returns the tail of every pane in refs, keyed by pane id.
//
// One tmux process serves them all. `capture-pane -p` emits exactly
// pane_height lines — blank lines included, alternate-screen panes included —
// so chained captures split on the heights we already asked tmux for, with no
// separator commands. That matters: interleaving `display-message` markers
// between the captures to label them measured 4x SLOWER than the nine separate
// spawns this replaces, while chaining the captures alone is faster than one.
//
// The split is only as good as its arithmetic, and a wrong offset would hand
// one session another's state. So the line count is checked against the sum of
// the heights, and anything unexpected — a pane that closed mid-call, a tmux
// that pads differently — falls back to reading each pane on its own.
func capturePanes(refs []agentPaneRef, n int) map[string]string {
	switch len(refs) {
	case 0:
		return nil
	case 1:
		return map[string]string{refs[0].ID: capturePane(refs[0].ID, n)}
	}

	args := make([]string, 0, len(refs)*6)
	total := 0
	for _, ref := range refs {
		if len(args) > 0 {
			args = append(args, ";")
		}
		args = append(args, "capture-pane", "-p", "-t", ref.ID)
		total += ref.Height
	}

	out, err := runTmux(args...)
	if err == nil {
		if captures, ok := splitCaptures(out, refs, n); ok {
			return captures
		}
	}
	return capturePanesEach(refs, n)
}

// splitCaptures cuts one batched capture into its panes, reporting false if the
// output does not account for every line the panes claimed to have.
func splitCaptures(out string, refs []agentPaneRef, n int) (map[string]string, bool) {
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	total := 0
	for _, ref := range refs {
		total += ref.Height
	}
	if len(lines) != total {
		return nil, false
	}

	captures := make(map[string]string, len(refs))
	at := 0
	for _, ref := range refs {
		pane := lines[at : at+ref.Height]
		if len(pane) > n {
			pane = pane[len(pane)-n:]
		}
		captures[ref.ID] = strings.Join(pane, "\n")
		at += ref.Height
	}
	return captures, true
}

// capturePanesEach reads every pane in its own process, concurrently. The
// fallback for a batch that did not add up.
func capturePanesEach(refs []agentPaneRef, n int) map[string]string {
	captures := make(map[string]string, len(refs))
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, ref := range refs {
		wg.Go(func() {
			capture := capturePane(ref.ID, n)
			mu.Lock()
			captures[ref.ID] = capture
			mu.Unlock()
		})
	}
	wg.Wait()
	return captures
}

// capturePane returns the last n lines of a pane's visible content, or "" on
// any error, which classifies as Idle.
func capturePane(paneID string, n int) string {
	out, err := exec.Command("tmux", "capture-pane", "-t", paneID, "-p").Output()
	if err != nil {
		return ""
	}
	return tailLines(string(out), n)
}

func tailLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
