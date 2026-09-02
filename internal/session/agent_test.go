package session

import "testing"

func TestClassify(t *testing.T) {
	cases := []struct {
		name    string
		agent   bool
		bell    bool
		capture string
		want    AgentState
	}{
		{"no agent, no bell, no capture", false, false, "", StateNone},
		{"no agent even if bell somehow set", false, true, "✻ Thinking…", StateNone},
		{"agent, bell wins regardless of empty capture", true, true, "", StateNotify},
		{"agent, bell wins even mid-generation", true, true, "esc to interrupt", StateNotify},
		{"agent, idle, empty capture", true, false, "", StateIdle},
		{"agent, esc to interrupt literal", true, false, "Editing file... (esc to interrupt)", StateWorking},
		{"agent, esc to interrupt case-insensitive", true, false, "Esc To Interrupt", StateWorking},
		{"agent, token counter word", true, false, "✢ Julienning… (32m 38s · ↓ 110.6k tokens)", StateWorking},
		{"agent, hammer glyph without the word tokens", true, false, "⚒ building", StateWorking},
		{"agent, known working verb alone", true, false, "✻ Thinking…", StateWorking},
		{
			"agent, the ✢ spinner glyph alone (no verb, no tokens, no done) -- seen live on this fleet",
			true, false, "✢ frobnicating…", StateWorking,
		},
		{
			"agent, permission prompt is not a working indicator",
			true, false,
			"Do you want to make this edit?\n❯ 1. Yes\n  2. No, and tell Claude what to do differently",
			StateIdle,
		},
		{
			"agent, bare middle-dot alone is not working (persistent status-bar noise)",
			true, false,
			"~ │ ● jim · 12 peers │ 5h █░░░░ 14% │ 7d █░░░░ 24% │ fable ██░░░ 33%",
			StateIdle,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Classify(c.agent, c.bell, c.capture); got != c.want {
				t.Errorf("Classify(%v, %v, %q) = %v, want %v", c.agent, c.bell, c.capture, got, c.want)
			}
		})
	}
}

// TestClassify_RealFleetCaptures pins the classifier against real
// `tmux capture-pane -p` output read (read-only, non-destructive) from live
// claude panes on this fleet on 2026-09-02 -- not synthetic text. This is
// what caught two false positives the spec's raw candidate signal list
// would have produced: a bare "·" sits in claude's persistent bottom status
// bar on every pane regardless of state, and claude leaves one of the given
// spinner glyphs on screen in its completed-turn summary line ("✻ Sautéed
// for 16m 0s · done 12:52 PM") after a turn ends, not just while generating.
func TestClassify_RealFleetCaptures(t *testing.T) {
	// A live status line while claude delegates to a background agent --
	// genuinely Working (it's actively orchestrating), even though no
	// user-facing text is streaming.
	const liveDelegating = `  ⎿  Stop says: ⚠ gates-check: source files were edited and the work was marked done, but no quality gate ran this session —
     consider the quality-gates skill (CRAP/coverage/mutation) before shipping.

✻ Waiting for 1 background agent to finish

❯ wait for the agent then test prefix s`
	if got := Classify(true, false, liveDelegating); got != StateWorking {
		t.Errorf("Classify(liveDelegating) = %v, want %v", got, StateWorking)
	}

	// The completed-turn footer claude leaves on screen after a turn ends.
	// Contains a spinner glyph AND a bare "·" but must classify Idle.
	const completedTurnFooter = `✻ Sautéed for 16m 0s · done 12:52 PM
                                                                  Update available! Run: mise upgrade claude
──────────────────────────────────────────────────────────────────────────────────────────────────────────────
❯ double check we are on corrct postmarket and nothing wonky happeingn -
──────────────────────────────────────────────────────────────────────────────────────────────────────────────
  ~/p/doorboard │ ● doorboard · 12 peers │ 5h █░░░░ 14% │ 7d █░░░░ 24% │ fable ██░░░ 32%                 /rc
  ⏵⏵ bypass permissions on (shift+tab to cycle)`
	if got := Classify(true, false, completedTurnFooter); got != StateIdle {
		t.Errorf("Classify(completedTurnFooter) = %v, want %v (done-footer glyph must not read as working)", got, StateIdle)
	}

	// A genuine live spinner+verb+token-counter line -- unambiguously Working.
	const liveWorking = `✢ Julienning… (32m 38s · ↓ 110.6k tokens)
  ⎿  Tip: Use /clear to start fresh when switching topics and free up context
                                                                                                                                             Update available! Run: mise upgrade claude
─────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────── @builder-build-impl ─
❯ `
	if got := Classify(true, false, liveWorking); got != StateWorking {
		t.Errorf("Classify(liveWorking) = %v, want %v", got, StateWorking)
	}

	// A genuinely idle claude pane sitting at an empty prompt -- no spinner,
	// no verb, no token counter -- despite the same persistent "·" status bar.
	const genuinelyIdle = `  RetroArch is already running. Nothing pending; next action is yours: say if you want LtvLauncher set back as HOME. (disable recaps
  in /config)
                                                                                         Update available! Run: mise upgrade claude
─────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
❯
─────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
  ~/p/l/src │  master ● │ ● ltv-dev · 12 peers │ 5h █░░░░ 13% │ 7d █░░░░ 24% │ fable ██░░░ 32%                                  /rc
  ⏵⏵ bypass permissions on (shift+tab to cycle) · ← for agents`
	if got := Classify(true, false, genuinelyIdle); got != StateIdle {
		t.Errorf("Classify(genuinelyIdle) = %v, want %v", got, StateIdle)
	}

	// A live "down-arrow tokens" counter, mid-delegation to a background
	// agent -- genuinely Working. Note the bare "·" leading the status verb
	// here: real evidence that "·" is one of claude's actual spinner frames,
	// but this capture is still correctly caught via the ↓+tokens counter,
	// so dropping the bare-dot spinner match (see workingSpinnerGlyphs) costs
	// nothing here.
	const liveTokenCounterWithDot = `· Dilly-dallying… (6m 33s · ↓ 7.0k tokens)
                                                             76% context used
───────────────────────────────────────────────────────────── sontara-omarchy ─
❯ `
	if got := Classify(true, false, liveTokenCounterWithDot); got != StateWorking {
		t.Errorf("Classify(liveTokenCounterWithDot) = %v, want %v", got, StateWorking)
	}

	// A genuinely idle pane whose ONLY match against the original "tokens"
	// signal is claude's unrelated, persistent "/clear to save Nk tokens"
	// context-usage hint -- present whether the session is working or idle.
	// Requiring "↓" alongside "tokens" (not "tokens" alone) is what keeps
	// this Idle instead of a false Working.
	const idleWithStaleTokensHint = `  Games feature is done and independently verified. Standing by.

✻ Cogitated for 18s · done 11:45 AM

※ recap: nothing pending; next action is yours.
                                                                                               new task? /clear to save 239k tokens
─────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
❯
─────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
  ~/p/l/src │  master ● │ ● ltv-dev · 12 peers │ 5h █░░░░ 13% │ 7d █░░░░ 24% │ fable ██░░░ 32%                                  /rc
  ⏵⏵ bypass permissions on (shift+tab to cycle) · ← for agents`
	if got := Classify(true, false, idleWithStaleTokensHint); got != StateIdle {
		t.Errorf("Classify(idleWithStaleTokensHint) = %v, want %v (stale 'tokens' hint without a down-arrow must not read as working)", got, StateIdle)
	}

	// A genuinely idle pane whose persistent bottom status bar happens to
	// get truncated by pane width right after a run of letters ("fable…"
	// instead of "fable ██░░░ 32%"), pinned from a real narrower capture of
	// this same fleet status bar on 2026-09-02: without workingVerbLine
	// anchored to a line's start, this reads as a false verb match
	// ("fable…") and misclassifies an idle pane as Working.
	const idleWithTruncatedStatusBar = `  RetroArch is already running. Nothing pending; next action is yours.
                                                                                         Update available! Run: mise upgrade claude
─────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
❯
─────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
  ~/h/a/s/sontara │  main ● │ ● astrobot · 11 peers │ 5h ██░░░ 33% │ 7d █░░░░ 24% │ fable…
  ⏵⏵ bypass permissions on (shift+tab to cycle) · ← for agents`
	if got := Classify(true, false, idleWithTruncatedStatusBar); got != StateIdle {
		t.Errorf("Classify(idleWithTruncatedStatusBar) = %v, want %v (status bar truncated after a letter run must not read as a working verb)", got, StateIdle)
	}
}

func TestAgentState_String(t *testing.T) {
	cases := map[AgentState]string{
		StateNone:    "none",
		StateIdle:    "idle",
		StateWorking: "working",
		StateNotify:  "notify",
	}
	for state, want := range cases {
		if got := state.String(); got != want {
			t.Errorf("%v.String() = %q, want %q", int(state), got, want)
		}
	}
}

func TestExtractWorkingStatus(t *testing.T) {
	cases := []struct {
		name        string
		capture     string
		wantVerb    string
		wantElapsed string
	}{
		{
			"spec sample: verb + elapsed + token counter",
			"✽ Concocting… (4m 34s · ↓ 18.6k tokens)",
			"Concocting", "4m 34s",
		},
		{
			"spec sample: short verb, elapsed only, no token counter",
			"* Kneading… (1s)",
			"Kneading", "1s",
		},
		{"idle line: no verb, no parens", "❯ ", "", ""},
		{"empty capture", "", "", ""},
		{
			"real fleet capture: ✢ spinner + verb + elapsed + tokens (multi-line)",
			"✢ Julienning… (32m 38s · ↓ 110.6k tokens)\n  ⎿  Tip: Use /clear to start fresh\n❯ ",
			"Julienning", "32m 38s",
		},
		{
			"real fleet capture: bare middle-dot spinner + verb + elapsed + tokens",
			"· Dilly-dallying… (6m 33s · ↓ 7.0k tokens)\n                 76% context used\n❯ ",
			"Dilly-dallying", "6m 33s",
		},
		{
			"real fleet capture: Working via spinner glyph, no verb line -- must tolerate empty",
			"✻ Waiting for 1 background agent to finish\n\n❯ wait for the agent then test",
			"", "",
		},
		{
			"completed-turn footer: past tense, no ellipsis -- must not match",
			"✻ Sautéed for 16m 0s · done 12:52 PM\n❯ ",
			"", "",
		},
		{
			"tool-call output truncated mid-word with an ellipsis -- must not match",
			"     us-central1 --memory=8Gi --cpu=2 --update-en… (33s · 4 lines)\n❯ ",
			"", "",
		},
		{
			"status bar truncated by pane width right after a letter run -- must not match",
			"  ~/h/a/s/sontara │  main ● │ ● astrobot · 11 peers │ 5h ██░░░ 33% │ 7d █░░░░ 24% │ fable…\n❯ ",
			"", "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			verb, elapsed := extractWorkingStatus(c.capture)
			if verb != c.wantVerb || elapsed != c.wantElapsed {
				t.Errorf("extractWorkingStatus(%q) = (%q, %q), want (%q, %q)",
					c.capture, verb, elapsed, c.wantVerb, c.wantElapsed)
			}
		})
	}
}

// TestExtractWorkingStatus_RealFleetCapture_PicksBottomLine pins a bug
// found live on this fleet (session sontara, pane %24, 2026-09-02): claude
// had printed a long shell command above its live status line, itself
// truncated mid-word with an ellipsis ("...--update-en… (33s · 4 lines)").
// Before workingVerbLine was anchored to a line's start, that indented
// fragment also matched the verb shape, and (on a naive top-down scan)
// would have won over the real, bottom-most status line ("Smooshing",
// "11m 1s"). The anchor alone now rejects it (it sits deep in a heavily
// indented line, not at the line's start); the bottom-up scan direction is
// kept as redundant defense-in-depth for the same case, per this test.
func TestExtractWorkingStatus_RealFleetCapture_PicksBottomLine(t *testing.T) {
	const capture = `● Investigating code mode enforcement for agent site building · 33s
  ⎿  $ unset CLOUDSDK_CORE_ACCOUNT; for s in u-user39zm-60b27d94
     u-user3dhm-1774ab27 u-user3dhm-5e493e97 u-user3dhm-8239ccd5
     u-user3dhm-9bbfe311 u-user3i9w-76f73590; do r=$(rtk proxy gcloud run
     services update "$s-builder" --project astrobot-hfl-prod --region
     us-central1 --memory=8Gi --cpu=2 --update-en… (33s · 4 lines)
     (ctrl+b ctrl+b (twice) to run in background)

✻ Smooshing… (11m 1s · ↓ 24.5k tokens)
                                                             87% context used
───────────────────────────────────────────────────────────── sontara-omarchy ─
❯ `
	verb, elapsed := extractWorkingStatus(capture)
	if verb != "Smooshing" || elapsed != "11m 1s" {
		t.Errorf(`extractWorkingStatus(realCapture) = (%q, %q), want ("Smooshing", "11m 1s")`, verb, elapsed)
	}
}

func TestIsAgentApp(t *testing.T) {
	for _, cmd := range []string{"claude", "opencode", "crush", "aider", "codex", "cursor", "gemini", "cline"} {
		if !IsAgentApp(cmd) {
			t.Errorf("IsAgentApp(%q) = false, want true", cmd)
		}
	}
	for _, cmd := range []string{"nvim", "bash", "git", ""} {
		if IsAgentApp(cmd) {
			t.Errorf("IsAgentApp(%q) = true, want false", cmd)
		}
	}
}
