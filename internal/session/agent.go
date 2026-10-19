package session

import (
	"os/exec"
	"regexp"
	"strings"
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
	for _, line := range strings.Split(capture, "\n") {
		lower := strings.ToLower(line)
		if strings.Contains(lower, "done") {
			continue
		}
		if strings.Contains(lower, "esc to interrupt") ||
			strings.Contains(line, "⚒") ||
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
