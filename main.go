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

func main() {
	dump := flag.Bool("dump", false, "print rendered session rows as plain text and exit (no TUI)")
	flag.Parse()

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
