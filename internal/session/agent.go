package session

import (
	"os/exec"
	"regexp"
	"strings"
)

// workingVerbLine matches claude's animated status verb line: a word
// immediately followed by a horizontal ellipsis ("Kneading…",
// "Julienning…", and hyphenated ones like "Dilly-dallying…"), optionally
// preceded by a single spinner glyph. claude cycles through ~100 such
// verbs, so matching the shape catches them all from the first frame
// instead of maintaining a hardcoded list. The completed-turn summary uses
// past tense with no ellipsis ("Sautéed for 16m · done"), so it won't
// match. Anchored to the start of the line (not matched anywhere in it) --
// pinned by TestClassify_RealFleetCaptures and
// TestExtractWorkingStatus_RealFleetCaptures against two real false-match
// shapes otherwise indistinguishable from a genuine status line, both
// pulled from live claude panes on this fleet on 2026-09-02: a long tool
// call's own output truncated mid-word with "…" ("...--update-en… (33s ·
// 4 lines)"), and claude's persistent bottom status bar truncated by pane
// width right after a run of letters ("...7d █░░░░ 24% │ fable…"). Both
// sit deep in a line with heavy content before them; the real status line
// never does. The capture group is unused by isWorking's MatchString but
// lets extractWorkingStatus pull the verb text out of the same pattern.
var workingVerbLine = regexp.MustCompile(`^\s*[^\s\p{L}]?\s*([\p{L}-]{3,})…`)

// elapsedInParens matches the first "(...)" on a status line, e.g.
// "(4m 34s · ↓ 18.6k tokens)" or "(1s)".
var elapsedInParens = regexp.MustCompile(`\(([^)]+)\)`)

// AgentState is a session's AI-agent activity, derived from tmux's window
// bell flag and (for agent sessions) the agent's own pane content. It lives
// in this UI-free package -- not internal/ui -- because Session.State is
// typed with it and internal/ui already imports internal/session; putting
// the type the other way round would be an import cycle.
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

// agentApps are commands that run an AI coding agent. Exported via
// IsAgentApp so internal/ui can pick out which of a session's app icons is
// the one whose glyph should reflect the agent's live State.
var agentApps = map[string]bool{
	"claude": true, "opencode": true, "codex": true, "copilot": true,
	"crush": true, "gemini": true, "aider": true,
	"cursor": true, "cline": true, "amp": true, "goose": true,
}

// IsAgentApp reports whether cmd is a recognized AI coding agent command.
func IsAgentApp(cmd string) bool { return agentApps[cmd] }

// workingSpinnerGlyphs are the live-spinner shapes claude prints beside its
// status verb while generating (✻✳✽ from the spec's candidate list, plus ✢
// -- seen live on this fleet, e.g. "✢ Julienning… (32m 38s · ↓ 110.6k
// tokens)"). A bare middle-dot ("·") was in the original candidate list
// alongside these but is dropped here: captured against real claude panes
// on this fleet, "·" sits in claude's persistent bottom status bar
// ("● name · N peers │ ...") on every single pane regardless of state, so
// it never discriminates working from idle -- see TestClassify_RealFleetCaptures.
const workingSpinnerGlyphs = "✻✳✽✢"

// Classify derives a session's AgentState from whether it runs an agent,
// its tmux bell flag, and (for a non-notify agent session) the last ~20
// lines of its agent pane. Pure and side-effect free, so it is fully
// table-tested without shelling out to tmux.
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

// isWorking matches claude's on-screen working indicators. The verb list is
// matched anywhere in the capture; the token counter and the spinner glyphs
// are matched more narrowly than the original candidate signal list, both
// corrections pinned by real captures from live claude panes on this fleet
// on 2026-09-02 (see TestClassify_RealFleetCaptures):
//
//   - Token counter: claude's live counter always pairs a down-arrow with
//     the count ("↓ 110.6k tokens"). Requiring "↓" alongside "tokens"
//     excludes claude's unrelated persistent "/clear to save 239k tokens"
//     context-usage hint, which contains the bare word "tokens" whether the
//     session is working or sitting idle -- caught live on session ltv-dev.
//   - Spinner glyphs: matched per line, because claude also prints one of
//     these glyphs in the completed-turn summary line it leaves on screen
//     after a turn ends ("✻ Sautéed for 16m 0s · done 12:52 PM"). Gating on
//     "done" not appearing on that same line keeps that from reading as
//     "still working" -- caught live on session doorboard.
func isWorking(capture string) bool {
	lower := strings.ToLower(capture)

	if strings.Contains(lower, "esc to interrupt") {
		return true
	}
	if strings.Contains(capture, "⚒") {
		return true
	}
	if strings.Contains(capture, "↓") && strings.Contains(lower, "tokens") {
		return true
	}
	for _, line := range strings.Split(capture, "\n") {
		if strings.Contains(line, "done") {
			continue // completed-turn summary, not a live status line
		}
		// Any of claude's rotating working verbs ("Kneading…") or a spinner
		// glyph on a non-summary line means it's generating.
		if workingVerbLine.MatchString(line) || strings.ContainsAny(line, workingSpinnerGlyphs) {
			return true
		}
	}
	return false
}

// extractWorkingStatus pulls the live status verb and elapsed time off a
// Working agent's capture, e.g. "✽ Concocting… (4m 34s · ↓ 18.6k tokens)"
// -> ("Concocting", "4m 34s"). It scans from the BOTTOM of the capture
// (closest to the prompt) for the first line carrying one of claude's
// rotating verbs (workingVerbLine, itself anchored to a line's start so it
// only matches the real status line -- see its doc comment) and, on that
// same line, takes the text before the first "·" inside the first "(...)"
// -- dropping the token-counter half. Bottom-up defends the same case in
// depth: were the anchor ever loosened, a long tool call's own truncated
// output sitting above the real status line would otherwise win on a
// top-down scan -- pinned by
// TestExtractWorkingStatus_RealFleetCapture_PicksBottomLine, a real
// capture from session sontara/pane %24 on 2026-09-02. Either return is ""
// when the capture doesn't match that shape at all (e.g. Working via the
// spinner-glyph or token-counter path alone, with no verb line) -- callers
// must tolerate empty.
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

// capturePane returns the last n lines of a tmux pane's visible content
// (read-only, non-destructive). Empty on any error -- List treats that the
// same as "no working indicator found", i.e. Idle.
func capturePane(paneID string, n int) string {
	out, err := exec.Command("tmux", "capture-pane", "-t", paneID, "-p").Output()
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
