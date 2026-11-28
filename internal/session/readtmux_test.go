package session

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// privateServer starts a tmux server of its own, so a test can kill panes
// without touching whatever sessions the machine running it is using.
func privateServer(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("no tmux")
	}
	// Not t.TempDir(): tmux puts its socket under this, a socket path is
	// capped near 100 bytes, and macOS's per-test temp path alone is longer.
	// The server then never starts and every test here quietly skips.
	dir, err := os.MkdirTemp("/tmp", "sessui")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	t.Setenv("TMUX_TMPDIR", dir)
	t.Setenv("TMUX", "") // never inherit the ambient server

	if out, err := exec.Command("tmux", "new-session", "-d", "-s", "probe", "-x", "80", "-y", "24", "cat").CombinedOutput(); err != nil {
		t.Skipf("tmux would not start a private server: %v: %s", err, out)
	}
	t.Cleanup(func() { exec.Command("tmux", "kill-server").Run() })
}

// The solid popup's edge must get popup-style as tmux expands it. Raw, a
// popup-style that is a format (bg=#{@thm_bg}) reaches -S unexpanded and the
// edge is left unstyled; a quote inside such a format broke the shell
// command. Binds the real sessui.tmux, expands its run-shell command as a
// keypress would, and reads the argv the shell hands to display-popup.
func TestPluginPopupEdgeGetsTheExpandedPopupStyle(t *testing.T) {
	privateServer(t)
	realTmux, _ := exec.LookPath("tmux")
	dir := t.TempDir()
	script, err := os.ReadFile("../../sessui.tmux")
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"sessui.tmux": string(script),
		"sessui":      "#!/bin/sh\n[ \"$1\" = -transparent ] && echo 0\nexit 0\n",
		// Keeps the run-shell command bind-key is given, then binds as usual.
		"wrap/tmux": "#!/bin/sh\n[ \"$1\" = bind-key ] && for a; do printf %s \"$a\" > \"$BOUND\"; done\nexec " + realTmux + " \"$@\"\n",
		// Stands in for the tmux the keypress runs: prints its argv.
		"argv/tmux": "#!/bin/sh\nfor a; do printf '%s\\n' \"$a\"; done\n",
	}
	for name, body := range files {
		path := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	bound := filepath.Join(dir, "bound")
	load := exec.Command("bash", filepath.Join(dir, "sessui.tmux"))
	load.Env = append(os.Environ(), "PATH="+filepath.Join(dir, "wrap")+":"+os.Getenv("PATH"), "BOUND="+bound)
	if out, err := load.CombinedOutput(); err != nil {
		t.Fatalf("plugin load: %v: %s", err, out)
	}
	tpl, err := os.ReadFile(bound)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(tpl), "-b padded") {
		t.Skipf("tmux before 3.3 binds the popup unstyled: %s", tpl)
	}

	// press expands the bound command as a keypress would and returns the
	// argv the shell hands to display-popup.
	set := func(option, value string) {
		t.Helper()
		if out, err := exec.Command("tmux", "set", "-g", option, value).CombinedOutput(); err != nil {
			t.Fatalf("%s %q: %v: %s", option, value, err, out)
		}
	}
	press := func() ([]string, error) {
		cmd, err := exec.Command("tmux", "display-message", "-p", "-t", "probe", string(tpl)).Output()
		if err != nil {
			t.Fatal(err)
		}
		sh := exec.Command("sh", "-c", strings.TrimSuffix(string(cmd), "\n"))
		sh.Env = append(os.Environ(), "PATH="+filepath.Join(dir, "argv")+":"+os.Getenv("PATH"))
		out, err := sh.CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("%v: %s", err, out)
		}
		return strings.Split(strings.TrimSuffix(string(out), "\n"), "\n"), nil
	}
	flag := func(args []string, name string) string {
		for i, a := range args {
			if a == name && i+1 < len(args) {
				return args[i+1]
			}
		}
		return "(none)"
	}

	// Border off: the flush edge takes popup-style, expanded.
	set("@thm_bg", "colour236")
	set("@sessui-borderless", "1")
	for style, want := range map[string]string{
		"bg=#1e1e2e,fg=#cdd6f4":          "bg=#1e1e2e,fg=#cdd6f4",
		"bg=#{@thm_bg},fg=colour250":     "bg=colour236,fg=colour250",
		"bg=#{?#{==:a,'},red,colour236}": "bg=colour236",
		"fg=red bg=blue":                 "fg=red bg=blue",
	} {
		set("popup-style", style)
		args, err := press()
		if err != nil {
			t.Errorf("popup-style %q: the keypress command failed: %v", style, err)
			continue
		}
		if got := flag(args, "-S"); got != want || flag(args, "-b") != "padded" {
			t.Errorf("popup-style %q: edge got -b %q -S %q, want padded and %q", style, flag(args, "-b"), got, want)
		}
	}

	// Border on, the default: tmux's own popup, nothing styled from here.
	set("@sessui-borderless", "0")
	set("popup-border-style", "fg=#{@thm_bg},bg=colour236")
	args, err := press()
	if err != nil {
		t.Fatalf("border on: the keypress command failed: %v", err)
	}
	for _, name := range []string{"-b", "-s", "-S"} {
		if got := flag(args, name); got != "(none)" {
			t.Errorf("border on, solid: display-popup got %s %q, want tmux's own", name, got)
		}
	}
	// Transparent with the border on: the border keeps its style and only
	// its background becomes the terminal's.
	set("@sessui-transparent", "1")
	if args, err = press(); err != nil {
		t.Fatalf("border on, transparent: the keypress command failed: %v", err)
	}
	if b, s, S := flag(args, "-b"), flag(args, "-s"), flag(args, "-S"); b != "(none)" || s != "bg=terminal" || S != "fg=colour236,bg=colour236,bg=terminal" {
		t.Errorf("border on, transparent: got -b %q -s %q -S %q", b, s, S)
	}
}

// A speculated pane that has closed makes tmux exit non-zero, and the session
// and pane lists it already printed must survive that. Otherwise the build
// cannot finish, the dead pane is never forgotten, and every refresh after it
// fails the same way.
func TestReadTmuxSurvivesDeadSpeculatedPane(t *testing.T) {
	privateServer(t)

	reading, err := readTmux([]agentPaneRef{{ID: "%99999", Height: 24}})
	if err != nil {
		t.Fatalf("readTmux: %v", err)
	}
	if !strings.Contains(reading.sessOut, "probe") {
		t.Errorf("session list lost: %q", reading.sessOut)
	}
	if reading.paneOut == "" {
		t.Error("pane list lost")
	}
	if reading.captures != nil {
		t.Errorf("captures should be absent, got %v", reading.captures)
	}
	// The pane list has to be usable, not merely non-empty: a build runs on it.
	if _, agentPane := Build(reading.sessOut, reading.paneOut, peerFleet{}, ""); agentPane == nil {
		t.Error("Build rejected the recovered pane list")
	}
}

// The ordinary path: a live pane is captured in the same call as the lists.
func TestReadTmuxCapturesSpeculatedPane(t *testing.T) {
	privateServer(t)

	out, err := exec.Command("tmux", "list-panes", "-t", "probe", "-F", "#{pane_id}|#{pane_height}").Output()
	if err != nil {
		t.Fatalf("list-panes: %v", err)
	}
	id, height, _ := strings.Cut(strings.TrimSpace(string(out)), "|")
	if height != "24" {
		t.Skipf("pane height is %q, not the 24 asked for", height)
	}

	reading, err := readTmux([]agentPaneRef{{ID: id, Height: 24}})
	if err != nil {
		t.Fatalf("readTmux: %v", err)
	}
	if _, ok := reading.captures[id]; !ok {
		t.Errorf("pane %s not captured in the batch: %v", id, reading.captures)
	}
	if got := len(reading.captures); got != 1 {
		t.Errorf("captured %d panes, want 1", got)
	}
}

// A reading with no speculation asks for no captures at all.
func TestReadTmuxWithoutSpeculation(t *testing.T) {
	privateServer(t)

	reading, err := readTmux(nil)
	if err != nil {
		t.Fatalf("readTmux: %v", err)
	}
	if reading.captures != nil {
		t.Errorf("captures taken without being asked: %v", reading.captures)
	}
	if strings.Contains(reading.paneOut, listMarkerCaptures) {
		t.Error("capture marker leaked into the pane list")
	}
}
