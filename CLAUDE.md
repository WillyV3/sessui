# sessui — agent context

A fast tmux **session switcher** TUI (bound to `prefix + s`): a live board of
tmux sessions and the AI agents running in them. Go + Charmbracelet
(bubbletea / bubbles / lipgloss). Module `github.com/willyv3/sessui`, repo
`WillyV3/sessui` (private).

**Ownership:** future sessui dev is the `config` (tmux-manager) peer's lane.
This file + `docs/` exist so it can pick up cold.

## Non-negotiable constraints (these were learned the hard way)

1. **Charmbracelet ecosystem only. NO hand-rolling, NO new deps.** Before
   writing UI logic, exhaust what bubbles/lipgloss/x-ansi already do. The
   marquee uses `x/ansi.Cut` (the primitive `bubbles/viewport` scrolls with),
   not a hand-rolled string slicer. Filtering is `list.Model`'s own filter, not
   a custom one. If you're about to fight the library, stop — you're holding it
   wrong.
2. **Ponytail.** Laziest construction that fully does the job. Delete over add.
   A `ponytail:` comment marks a deliberate simplification + its ceiling.
3. **Fixed-width table, ~112 columns.** Every cell is a fixed-width lipgloss
   cell joined with `JoinHorizontal` (see `usableWidth` in `delegate.go`). The
   layout assumes the popup is ~112 wide. A responsive/dynamic-width layout is
   an unbuilt follow-up, not the current design — don't assume it adapts.

## Dev workflow

```sh
go test ./...                         # table tests, must stay green
go build -o ~/.local/bin/sessui .     # deploy for prefix+s (see GOBIN gotcha)
go install .                          # keep GOBIN copy in lockstep
```

**Install to BOTH** — the tmux bind uses the explicit path
`$HOME/.local/bin/sessui`, but `go install` drops a copy in mise's GOBIN which
is *earlier* on PATH. If they drift, a bare `sessui` runs the stale one. See
`docs/GOTCHAS.md`.

**Verify visuals with a real receipt, never by reasoning about the code.**
lipgloss strips color in a non-TTY `go test`, so colors/layout are proven by
driving a throwaway tmux session and capturing it:

```sh
tmux new-session -d -s t -x 112 -y 28 'exec ~/.local/bin/sessui'; sleep 1.3
tmux capture-pane -t t -p        # plain text (layout)
tmux capture-pane -t t -pe       # with SGR (colors)
tmux kill-session -t t
sessui --dump                    # headless one-shot row render (no interaction)
```

## Requirements

- **tmux 3.8+** — the live-updating popup relies on the popup-redraw fix in
  3.8; older tmux paints once and won't animate the spinner/marquee.
- **A Nerd Font** (JetBrainsMono NF is the omarchy default) for the icons.
  Verify any new glyph is actually in the font before shipping it (`otfinfo -u`).
- `omarchy-theme-color` for theme colors (falls back to defaults off Omarchy).
- `cp3` (claude-peers) is optional. When it is not installed the peer column is
  hidden outright — `.claude-peers-agent` markers are NOT read as down peers,
  since a roster we never received is no evidence of liveness (`peerFleet`).

## Layout of the code

- `internal/session/` — pure data layer (tmux + cp3 parse), no UI import, fully
  table-tested. `agent.go` = agent-state detection; `session.go` = the model.
- `internal/ui/` — the bubbletea layer. `model.go` = the `tea.Model`;
  `delegate.go` = row/header rendering + marquee + widths; `style.go` = theme
  palette + styles; `icons.go` = the app→glyph table.
- `sessui.tmux` = TPM plugin entry (build-on-install + keybind). `main.go` =
  entry + `--dump`.

Read `docs/ARCHITECTURE.md` for how the layers work and `docs/GOTCHAS.md`
before touching layout, color, filtering, or the tmux bind.
