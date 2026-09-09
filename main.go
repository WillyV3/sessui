// Command sessui is a live-updating tmux session switcher.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/WillyV3/sessui/internal/session"
	"github.com/WillyV3/sessui/internal/ui"
)

// version is stamped by goreleaser (-X main.version={{ .Tag }}) on a
// release build; "dev" from a plain go build. The fleet's dotfiles apply
// compares it to the pinned release before fetching (run_onchange_after_sessui).
var version = "dev"

func main() {
	dump := flag.Bool("dump", false, "print rendered session rows as plain text and exit (no TUI)")
	hosts := flag.Bool("hosts", false, "list ssh hosts and their tmux sessions as plain text and exit (no TUI)")
	showVersion := flag.Bool("version", false, "print the release version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}
	if *dump {
		os.Exit(runDump())
	}
	if *hosts {
		os.Exit(runHosts(flag.Args()))
	}

	if _, err := tea.NewProgram(ui.New(), tea.WithAltScreen()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "sessui:", err)
		os.Exit(1)
	}
}

// runHosts is the check that the remote half works before any of it reaches the
// UI: it discovers hosts (or takes them as args), fans out, and prints what came
// back. Timing is printed because the number is the design -- if a fan-out is
// not fast enough to hide behind a 2s reload, the feature does not belong in a
// popup opened a hundred times a day.
func runHosts(args []string) int {
	aliases := args
	if len(aliases) == 0 {
		aliases = session.DiscoverHosts()
		fmt.Printf("discovered %d host(s) in ~/.ssh/config\n\n", len(aliases))
	}
	if len(aliases) == 0 {
		fmt.Fprintln(os.Stderr, "sessui: no hosts")
		return 1
	}

	var w session.Watcher
	start := time.Now()
	w.Refresh(context.Background(), aliases)
	elapsed := time.Since(start)

	var reachable, total int
	for _, h := range w.Snapshot(aliases) {
		if h.Err != nil {
			fmt.Printf("%-22s unreachable\n", h.Alias)
			continue
		}
		reachable++
		total += len(h.Sessions)
		fmt.Printf("%-22s %d session(s)\n", h.Alias, len(h.Sessions))
		for _, s := range h.Sessions {
			fmt.Printf("    %-24s %d window(s)  %s\n", s.Name, s.Windows, s.CWD)
		}
	}
	fmt.Printf("\n%d/%d hosts reachable, %d sessions, %v\n", reachable, len(aliases), total, elapsed.Round(time.Millisecond))
	return 0
}

func runDump() int {
	sessions, err := session.List()
	if err != nil {
		fmt.Fprintln(os.Stderr, "sessui: dump:", err)
		return 1
	}
	fmt.Print(ui.DumpRows(sessions))
	return 0
}
