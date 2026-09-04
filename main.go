// Command sessui is a live-updating tmux session switcher.
package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/willyv3/sessui/internal/session"
	"github.com/willyv3/sessui/internal/ui"
)

// version is stamped by goreleaser (-X main.version={{ .Tag }}) on a
// release build; "dev" from a plain go build. The fleet's dotfiles apply
// compares it to the pinned release before fetching (run_onchange_after_sessui).
var version = "dev"

func main() {
	dump := flag.Bool("dump", false, "print rendered session rows as plain text and exit (no TUI)")
	showVersion := flag.Bool("version", false, "print the release version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}
	if *dump {
		os.Exit(runDump())
	}

	if _, err := tea.NewProgram(ui.New(), tea.WithAltScreen()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "sessui:", err)
		os.Exit(1)
	}
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
