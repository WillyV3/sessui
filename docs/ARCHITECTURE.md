# Architecture

Two layers, split so the data half is UI-free and cheap to test.

## `internal/session` — data

`List()` shells out to `tmux list-sessions`, `tmux list-panes -a` and, best
effort, `cp3 peers --json`; `Build()` does the pure parse and combine. Each
`Session` carries:

- `Name, Created, Activity, Attached, Windows, LastAttached, CWD, Apps` — from tmux.
- `Host` — the ssh alias it lives on; `""` for local.
- `Agent, State` — is an AI agent running, and its live state.
- `WorkingVerb, WorkingElapsed` — parsed off a working agent's pane.
- `PeerUp, PeerName, OwedMail, PeerSummary` — its cp3 binding.
- `Summary` — the agent's pane title, filtered (below).

`ListFast()` is `List()` without cp3 and without pane captures: tmux alone,
in one batched invocation. It paints the first frame; `List()` fills in the
rest behind it.

### Agent state (`agent.go`)

`Classify(agent, bell, capture)` → `StateNone / Idle / Working / Notify`.

- **Notify** — tmux `window_bell_flag` is set.
- **Working** — the pane tail matches a working indicator: `esc to interrupt`,
  the `⚒` glyph, a `↓ … tokens` counter, or a verb line
  (`^\s*[glyph]?\s*([\p{L}-]{3,})…`). Agents cycle through ~100 verbs, so the
  shape is matched, not a word list. The regex is anchored to the line start
  because truncated tool output and status bars also end words with `…`.
- **Idle** — agent present, none of the above.

### Peer binding — join by cwd, not name

`cp3 peers --json` gives `{name, up, pending, machine, cwd, summary}`. A
session's peer is the row whose **cwd matches** and is `up`. Do not match by
session name: cp3's contested-name fallback appends a suffix, so name matching
misses. A workspace with a `.claude-peers-agent` marker but no live peer is
`AgentExited()`.

**A marker is only evidence once cp3 has answered.** `fetchPeers` returns
`peerFleet{Rows, Reachable}`, and `Build` reads the marker only when
`Reachable`: "absent from a roster" and "we never received a roster" are
opposite facts a bare `[]peerRow` cannot tell apart. Without the distinction,
a machine with no cp3 renders every marked workspace as a dead peer.

### Summary — `Summary` and `EffectiveSummary()`

An agent sets its terminal title to a `✳`-prefixed task summary, exposed as
tmux `#{pane_title}`. `Summary` is that title with the marker stripped, but
only if it has the marker and is not an echo of the session or peer name
(`NormalizeName`). `EffectiveSummary()` prefers a cp3 summary the peer set
deliberately. The pane title is set early and does not track live work; it is
a secondary signal.

## `internal/ui` — bubbletea

`bubbles/list.Model` drives cursor, filter and pagination; `rowDelegate`
renders each row. `Model` adds the live ticks, the background reloads, the
overlays, and the tmux actions.

### Columns are data (`columns.go`, `delegate.go`)

`columnCatalog` is every column sessui can draw, keyed by a stable `columnID`
that persists to disk. Each `column` has a label, a header glyph, a fixed
`width` (0 = the single flex column), a `minWidth`, and a pure
`render(cell) string`. Configuration is `[]columnSetting` (id + optional width
override, slice order = display order). `resolveColumns` drops unknown ids so
a newer build's config never bricks an older one, and hides the peer column
when no cp3 peers are in use. `layoutColumns(cols, width)` produces the
`tableLayout`; `Model.relayout` is its only writer.

`fixedCol` is `Inline + Width + MaxWidth`, so long content truncates on one
line instead of wrapping the row. `renderStatus` priority: **needs you** →
**EffectiveSummary** → **verb + elapsed** → **idle** (agent parked past
`idleThreshold`) → blank.

### Marquee

The selected row's over-long cells scroll via `x/ansi.Cut`, which is
wide-char aware and preserves SGR. `marqueeOffset` ping-pongs with a hold at
each end. The frame counter resets on selection change, keyed by session
**name** because a reload re-sorts and shifts indexes.

### Full-row highlight (`selectRow`)

A plain lipgloss `Background` does not survive a multi-coloured row: every
cell ends with `\x1b[0m`, which clears the background too. `selectRow`
re-applies the SGR after each reset.

### Filtering and keys

Type-to-filter with no `/`: a printable rune arms the list's filter. Arrows
navigate in every state — bubbles/list disables its own cursor keys while
filtering, so the public `CursorUp()/CursorDown()` are called directly. Since
letters feed the filter there are no vim keys. Backspacing the filter empty
restores the column headers.

### Colour (`style.go`)

`loadPalette(source)` resolves `Config.Theme.Palette`. A pinned `dark`/`light`
is a complete built-in (Catppuccin Mocha / Latte) and shells out to nothing.
`auto` checks `omarchyThemeAvailable()` — one `exec.LookPath` — and if Omarchy
is absent returns the dark built-in with zero queries; if present, each slot
is queried once and any slot that fails falls back individually. A 700ms tick
re-reads Omarchy's theme stamp file and rethemes live when it changes.

Muted text is `Foreground + Faint`, not the theme's muted slot, which is a
surface colour and unreadable as text. That slot is right as the selection
background.

### Glyph sets (`glyphs.go`)

Every glyph is a codepoint that passes through `glyphU`, the one place
`Config.Icons` substitutes: ASCII mode looks the codepoint up in `asciiGlyphs`
and returns a ≤2-cell stand-in, or the visible `asciiUnknown` if it is
missing. `activeGlyphs` is set before `applyTheme` runs, because `Icon` values
capture `glyphU`'s output. A new Nerd glyph needs an `asciiGlyphs` entry in
the same commit.

### Config (`config.go`)

`$XDG_CONFIG_HOME/sessui/config.json`, defaulting to
`~/.config/sessui/config.json` on every OS — deliberately not
`os.UserConfigDir`, which on Darwin returns `~/Library/Application Support`
and ignores `XDG_CONFIG_HOME`. Popup geometry is a tmux option, not config:
tmux needs it before this process exists. `LoadConfig` never fails the
program; a file that will not parse yields defaults and an error the footer
shows. `SaveConfig` writes atomically and pretty-printed.

### The brand line (`header.go`)

One row above the column headers: the tally left, `user@host` centred and
bold, the wordmark right. The identity is centred on the full width so it does
not drift as the tally changes under a filter. The line sheds parts rather
than wrapping — wordmark, then tally, then everything but the identity.

## Remote hosts

### Discovery (`hosts.go`)

`DiscoverHosts` parses `~/.ssh/config` with `kevinburke/ssh_config`, the
parser wishlist uses. Importing wishlist itself would cost 16MB and 135
dependencies against 3MB and one, because its `Endpoint` type lives beside an
SSH server.

Aliases are deduped by **resolved endpoint**, not name: several aliases for one
machine would list the same sessions several times. `ssh -G` does the
resolving with no network, so `Match` and `Include` are understood because ssh
understands them. Wildcards are dropped and **nothing is polled
automatically** — discovery offers, the user picks.

### Transport (`remote.go`)

`sshArgs` is the whole transport decision: shell out to `ssh`, so
`~/.ssh/config` governs ProxyJump, certificates, agent forwarding, Match,
Include. `ControlMaster` makes a fan-out affordable (~385ms cold, ~40ms
multiplexed). Both tmux queries ride one round trip split by a marker. The
remote command ends `exit 0`: tmux exits 1 when no server is running, which
would report a healthy machine as unreachable.

### Non-blocking is the shape

`Watcher.Snapshot` is a map read and never touches the network, so a frame
cannot stall on a sleeping laptop. `RefreshOne` does the I/O, one host per
Cmd, so each answer lands on its own. Hosts are polled every 15s — far slower
than the 2s local reload, since each tick is an ssh round trip per host. A
failed poll **drops** the host's sessions: a session on a machine that just
refused to answer is not something you can switch to. `hostTimeout` exists
because a sleeping laptop drops the SYN rather than refusing it.

`Model.watcher` is a pointer: `Watcher` holds a mutex and bubbletea passes
Models by value.

### Attaching — proxy, don't mirror

tmux cannot switch a client across machines, so `AttachRemote` wraps the ssh
attachment in a local session named `host/name`. After that the row is an
ordinary local session: every later switch is an instant `switch-client`.
`Merge` drops a remote session whose proxy already exists locally.

The separator is `/`, not `:` — tmux accepts a session named `host:name` and
then cannot target it, because `:` is its session:window separator.

### Actions know which machine they are on

`runOn` is the single place that decides local-vs-remote; every action a row
offers goes through it. `Forget` and `RenameCached` correct the cache in the
kill/rename closures, so the reload that follows agrees with reality instead
of showing a ghost row until the next poll.

### The hosts row (`hostedit.go`)

Same surface, same verbs as the column rows. `enter` is unbound: it means
arm-to-move on the strip, and hosts have no order to change. Opening the
editor starts one fan-out across every discovered machine; chips resolve in
place as replies land, with a fill animation on arrival. The row is a
viewport: it scrolls like a table, and `‹` `›` are reserved inside the width
budget.
