// Command sessui is a live-updating tmux session switcher.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"runtime/debug"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/WillyV3/sessui/internal/session"
	"github.com/WillyV3/sessui/internal/ui"
)

// version is stamped by goreleaser on a release build; "dev" otherwise.
var version = "dev"

// resolvedVersion prefers the stamped tag, then the module version recorded
// at build time, so `go install ...@latest` reports its tag rather than "dev".
func resolvedVersion() string {
	if version != "dev" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		if v := bi.Main.Version; v != "" && v != "(devel)" {
			return v
		}
	}
	return version
}

func main() {
	dump := flag.Bool("dump", false, "print rendered session rows as plain text and exit (no TUI)")
	hosts := flag.Bool("hosts", false, "list ssh hosts and their tmux sessions as plain text and exit (no TUI)")
	showVersion := flag.Bool("version", false, "print the release version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(resolvedVersion())
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

// runHosts exercises the remote half without the UI: discover hosts (or take
// them as args), fan out, print what came back. The elapsed time is printed
// because a fan-out that cannot hide behind the reload interval does not
// belong in a popup.
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
