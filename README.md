# sessui

A fast tmux session switcher — a live board of your sessions and the agents
running in them, on a keystroke.

Open it (`prefix + s`), and every session is one row: the name, its app icons,
how long since it was last active (colour-warmed by recency), its working
directory, its bound [claude-peers] agent (● up / ○ down, ✉ when it owes a
reply), and a **status** — the agent's own live summary (Claude Code pane title
or a peer's `set_summary`), its cooking verb while it's generating, `needs you`
when it rang the bell, or `idle` when it's been parked a while. Just start
typing to filter; `enter` switches; the selected row scrolls anything too long
to fit.

Sessions that need you sort to the top and blink; dead agent workspaces sink to
the bottom.

## Install

With [TPM](https://github.com/tmux-plugins/tpm), add to `~/.config/tmux/tmux.conf`:

```tmux
set -g @plugin 'WillyV3/sessui'
```

Then `prefix + I` to fetch and build it. **Building needs [Go](https://go.dev)
on your PATH** — the plugin compiles the binary on install (and rebuilds it on
`prefix + U`).

Not using TPM? Clone it and build:

```sh
git clone https://github.com/WillyV3/sessui ~/.config/tmux/plugins/sessui
go build -C ~/.config/tmux/plugins/sessui -o sessui .
# then in tmux.conf:  run-shell ~/.config/tmux/plugins/sessui/sessui.tmux
```

## Configure

Set these before the `run '.../tpm'` line:

| Option             | Default | Meaning                                   |
|--------------------|---------|-------------------------------------------|
| `@sessui-key`      | `s`     | `prefix + <key>` opens the switcher       |
| `@sessui-width`    | `112`   | popup width (the table wants ~112 columns)|
| `@sessui-height`   | `26`    | popup height                              |

```tmux
set -g @sessui-key 'j'
set -g @sessui-width '90%'
```

## Keys

| Key            | Action                                  |
|----------------|-----------------------------------------|
| `↑` / `↓`      | move                                    |
| type any text  | filter (no `/` needed)                  |
| `enter`        | switch to the highlighted session, or create one named by the filter |
| `ctrl+r`       | rename the highlighted session          |
| `ctrl+x`       | kill the highlighted session            |
| `esc`          | close                                   |

Letters feed the filter, so there are no vim (`j`/`k`) nav keys — the arrows are
the nav.

## Requirements

- **tmux 3.8+** — the live-updating popup (spinners, scrolling, recency
  counters) relies on the popup-redraw fix in 3.8; on older tmux the popup
  paints once and won't animate.
- **Go** — to build the binary (install and update only).
- A **Nerd Font** in your terminal for the app / column icons.

The peer column lights up when [claude-peers] is on the network; without it,
that column is simply empty and everything else works the same.

[claude-peers]: https://github.com/WillyV3/claude-peers
