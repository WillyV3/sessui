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
the bottom. Above the table, a quiet count line ("14 sessions") grows a
right-aligned attention pill for each fleet-wide signal that's actually
pending — a bell + count for sessions that need you, an ✉ + count for peers
owed a reply — and says nothing when neither is.

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

`@sessui-width` is read at **open** time (not bind time), so a change takes
effect on the very next `prefix + <key>` with no `tmux source`. The table's
columns really do adapt to it — below ~112 the status column narrows first,
floored so it never goes negative; it does not reflow to multiple lines.

## Config file

Separate from the tmux options above (which tmux needs before the binary
even starts), sessui persists its own settings at
`$XDG_CONFIG_HOME/sessui/config.json`, defaulting to
`~/.config/sessui/config.json` — **on every OS, macOS included** (not
`~/Library/Application Support`; chezmoi manages `~/.config` on the Mac the
same as on Linux). No file yet, or one that fails to parse: sessui falls back
to the defaults below and still opens.

| Field           | Values                            | Default | Meaning |
|-----------------|------------------------------------|---------|---------|
| `icons`         | `"nerd"` \| `"ascii"`              | `nerd`  | glyph set — `ascii` swaps every Nerd Font icon and header glyph for a plain stand-in, for a terminal without a Nerd Font (the usual Mac case) |
| `theme.palette` | `"auto"` \| `"dark"` \| `"light"`  | `auto`  | `auto` follows the Omarchy theme when it's on the box, else the built-in dark palette; `dark`/`light` pin a complete built-in (Catppuccin Mocha / Latte) and never query Omarchy |
| `columns`       | array of `{"id": ..., "width": ...}` | the 6 columns below | which columns show, in what order, and any width override (`width` optional) |
| `header.widgets` | array of `{"name": ..., "args": {...}}` | `[{"name":"attention"}]` | the header's widget section, in order — see [Header widgets](#header-widgets--ctrlw) |

```json
{
  "icons": "ascii",
  "theme": { "palette": "light" }
}
```

The shipped table is `session`, `apps`, last-active, `cwd`, `peer`, `status` —
`peer` disappears outright when no [claude-peers] is in use, regardless of
configuration. Four more columns exist and can be added today by hand-editing
`columns` (ids in `internal/ui/columns.go`): `age` (session created), `windows`
(window count), `attached` (a client is on it right now), `machine` (the bound
peer's machine, on its own).

### The column editor — `ctrl+e`

The header row of the table is the editing surface; the real table redraws
live underneath every edit. Three rows, `↑↓` picks which one you're on:

| row       | what it is                                  | keys                                              |
|-----------|---------------------------------------------|---------------------------------------------------|
| **strip** | the visible columns, exactly as laid out    | `←→` select · `enter` arm, then `←→` swaps · `space` hide · `+/-` resize |
| **add**   | hidden columns as chips — age, windows, attached, machine | `←→` select · `space` add (it returns to the slot it left) |
| **popup** | the tmux popup width                        | `+/-` in steps of 4                               |

`esc` applies and closes — there is no confirm step, on purpose. `ctrl+z`
abandons (closes, applies nothing). `ctrl+r` resets everything to the
shipped table and width.

**Where the popup width lives:** the `popup` row writes the `@sessui-width`
tmux option, the same one you can set in `tmux.conf`. It takes effect on the
**next** `prefix + s` — the popup you're looking at can't resize itself, and
the row says so. Columns are saved to `~/.config/sessui/config.json`.

### Header widgets — `ctrl+w`

The right side of the count line ("14 sessions … ") is a row of widgets. Each
is an icon until you focus it; a focused widget expands **into the same
line** — the header is always exactly one line, so the table underneath never
moves. `ctrl+w` focuses the first widget, `tab` / `shift+tab` move along the
row, `esc` returns to the list. A widget with controls of its own (a
transport, say) takes every other key while it is focused, and the footer
shows what they are.

| widget      | collapsed                     | expanded                                      |
|-------------|-------------------------------|-----------------------------------------------|
| `attention` | `󰂚 2  ✉ 1` pills, nothing at zero | the names of the sessions that need you / owe mail |
| `host`      | the short hostname            | hostname · session count                      |
| `agents`    | `working/idle` counts, hidden with no agents | `2 working · 8 idle · 1 need you` |
| `shell`     | `args.icon` (default `$`)     | the first line of `args.cmd`'s output          |
| `now-playing` | a note while a player has media, hidden otherwise | `▶ Artist – Title · player`, with a transport: `space` play/pause · `←→` track · `+/-` volume · `m` mute · `s` source |

`now-playing` is a thin client of Omarchy's own media service
(`omarchy-shell media status|playPause|next|previous|sourceNext` and
`omarchy-audio-output-volume`), so it controls whatever Omarchy's bar
controls — every player, no MPRIS code in sessui. On a box without
`omarchy-shell` (a Mac) it hides itself.

`shell` is the plugin escape hatch — any command, run on the 2 s refresh
with a 1.5 s timeout, off the UI thread. Two are fine; each keeps its own
output:

```json
{
  "header": {
    "widgets": [
      { "name": "attention" },
      { "name": "shell", "args": { "cmd": "date +%H:%M", "icon": "" } },
      { "name": "shell", "args": { "cmd": "cat /sys/class/power_supply/BAT0/capacity", "icon": "" } },
      { "name": "host" }
    ]
  }
}
```

An unknown widget name shows up as a `config:` error in the footer and the
rest of the header still renders. Widgets written in Go implement `icon` and
`expand` (`internal/ui/widgets.go`); `controllable` adds keys, `poller` adds
a refresh.

## Keys

| Key            | Action                                  |
|----------------|-----------------------------------------|
| `↑` / `↓`      | move                                    |
| type any text  | filter (no `/` needed)                  |
| `enter`        | switch to the highlighted session, or create one named by the filter |
| `ctrl+r`       | rename the highlighted session          |
| `ctrl+x`       | kill the highlighted session            |
| `ctrl+e`       | column editor                           |
| `ctrl+w`       | focus the header widgets                |
| `esc`          | close                                   |

Letters feed the filter, so there are no vim (`j`/`k`) nav keys — the arrows are
the nav.

Rename (`ctrl+r`) and kill (`ctrl+x`) are `huh` forms on the single footer
line — the list stays in place above them, `esc` backs out with no side
effect. The help line at the bottom is generated from the one keymap that
also dispatches the keys, so it cannot drift from what the keys do.

## Requirements

- **tmux 3.8+** — the live-updating popup (spinners, scrolling, recency
  counters) relies on the popup-redraw fix in 3.8; on older tmux the popup
  paints once and won't animate.
- **Go** — to build the binary (install and update only).
- A **Nerd Font** in your terminal for the app / column icons — or set
  `"icons": "ascii"` in the [config file](#config-file) if you don't have one
  (the usual Mac case).

The peer column is hidden outright when [claude-peers] is not on the
network — not blank, absent — and everything else works the same.

## Development

`CLAUDE.md` (repo root) is the agent/dev context — constraints, build/verify
workflow, code map. `docs/ARCHITECTURE.md` explains the two layers;
`docs/GOTCHAS.md` collects the traps (GOBIN shadow, tmux 3.8, theme colours,
the fixed-width layout) — read it before touching layout, colour, filtering, or
the tmux bind.

[claude-peers]: https://github.com/WillyV3/claude-peers
