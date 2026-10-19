package session

import "testing"

// Captures taken off real panes. The idle cases are the ones that used to read
// as Working: each holds one half of a status line, and the halves used to be
// matched across separate lines.
func TestClassifyAgentPane(t *testing.T) {
	tests := []struct {
		name    string
		capture string
		want    AgentState
	}{
		{
			name:    "live status line",
			capture: "✽ Precipitating… (2m 17s · ↓ 8.8k tokens)",
			want:    StateWorking,
		},
		{
			name:    "multi-word verb",
			capture: "✽ Compacting conversation… (1m 19s · ↓ 414 tokens)",
			want:    StateWorking,
		},
		{
			name:    "interrupt hint without a verb",
			capture: "• Working (4s • esc to interrupt)",
			want:    StateWorking,
		},
		{
			name:    "token hint and a scroll marker on separate lines",
			capture: "│ wiring the buzzer   9 new messages (ctrl+End) ↓ │\n  new task? /clear to save 368.3k tokens",
			want:    StateIdle,
		},
		{
			name:    "glyph in scrollback prose",
			capture: "│ ✻ Bug report drafted: the model took down a device │\n  new task? /clear to save 196.3k tokens",
			want:    StateIdle,
		},
		{
			name:    "ellipsis in truncated chrome",
			capture: "  ~/p/demo │ ● peer · 12 peers · ✖ NO CHANNEL (relaunch:…\n  ⏵⏵ auto mode on (shift+tab to cycle)",
			want:    StateIdle,
		},
		{
			name:    "completed-turn summary",
			capture: "✻ Sautéed for 16m 4s · done (3 tool uses)",
			want:    StateIdle,
		},
		{
			name:    "empty capture",
			capture: "",
			want:    StateIdle,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Classify(true, false, tt.capture); got != tt.want {
				t.Errorf("Classify() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestClassifyPrecedence(t *testing.T) {
	working := "✽ Precipitating… (2m 17s · ↓ 8.8k tokens)"
	if got := Classify(false, false, working); got != StateNone {
		t.Errorf("no agent: got %v, want %v", got, StateNone)
	}
	if got := Classify(true, true, working); got != StateNotify {
		t.Errorf("bell wins: got %v, want %v", got, StateNotify)
	}
}

func TestExtractWorkingStatus(t *testing.T) {
	verb, elapsed := extractWorkingStatus("✽ Precipitating… (2m 17s · ↓ 8.8k tokens)")
	if verb != "Precipitating" || elapsed != "2m 17s" {
		t.Errorf("got (%q, %q), want (%q, %q)", verb, elapsed, "Precipitating", "2m 17s")
	}
}
