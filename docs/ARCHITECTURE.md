# Architecture

Two layers, deliberately split so the data half is UI-free and cheap to test.

## `internal/session` — data (no UI dependency)

`List()` shells out to `tmux list-sessions` + `tmux list-panes -a` and (best
effort) `cp3 peers --json`, then `Build()` does the pure parse+combine (table
-tested without shelling). Each `Session` carries:

- `Name, Created, Activity, Attached, Windows, CWD, Apps` — from tmux.
- `Agent, State` — is an AI agent running, and its live state.
- `WorkingVerb, WorkingElapsed` — parsed off a working agent's pane.
- `PeerName, Machine, OwedMail` — its claude-peers binding.
- `Summary` — the agent's native pane title (see below).

### Agent state (`agent.go`)
`Classify(agent, bell, capture)` → `StateNone / Idle / Working / Notify`.

- **Notify** = tmux `window_bell_flag` set (agent rang for the user).
- **Working** = the captured pane tail matches a working indicator:
  `esc to interrupt`, the `⚒` glyph, a `↓ … tokens` counter, or a rotating
  verb line (`^\s*[glyph]?\s*([\p{L}-]{3,})…` — claude cycles ~100 verbs like
  "Kneading…", so match the *shape*, not a word list). Regexes are anchored /
  gated against real false-match shapes captured from live panes — see the
  test tables before loosening them.
- **Idle** = agent present, none of the above.

### Peer binding — join by `(cwd, machine)`, NOT name
`cp3 peers --json` gives `{name, up, pending, machine, cwd, summary}`. A
session's peer is the row whose **cwd matches** and is `up`. Do **not** match by
session name: claude-peers' `ClaimWithFallback` appends `-<machine>` when a name
is contested (session `api-gateway` → peer `deploy-bot` / `deploy-bot-laptop`), so name
-matching misses. Down/zombie workspace = a `.claude-peers-agent` marker present
but no live peer (`AgentExited()`). `OwedMail` = the peer's `pending > 0`.

**A marker is only evidence once cp3 has answered.** `fetchPeers` returns a
`peerFleet{Rows, Reachable}`, and `Build` reads the marker only when
`Reachable` — because "absent from a roster" and "we never received a roster"
are opposite facts that a bare `[]peerRow` cannot tell apart. Without the
distinction, a machine with no cp3 installed renders every marked workspace as
a dead peer: a column of red dots asserting the fleet is down on a box that
simply never asked.

An empty-but-`Reachable` roster is deliberately still evidence: cp3 answering
"no one is up" genuinely means the marker's peer is down. That includes the
older `cp3 peers` plain-table fallback — verified against macbook1's pre-`--json`
cp3, whose table parses cleanly into 13 rows, so its down-dots are real. Pinned
by `TestBuild_PeerJoin_CP3Unreachable`, whose second subtest is the
true-positive control.

### Native summary — `Summary` + `EffectiveSummary()`
Claude Code sets its terminal title to a `✳`-prefixed task summary, exposed as
tmux `#{pane_title}`. `Summary` is that title with the `✳ ` stripped — but only
if it (a) has the marker and (b) isn't just an echo of the session/peer name
(`NormalizeName`). `EffectiveSummary()` prefers a deliberately-set cp3 summary
over the scraped title. **Caveat:** the pane title is set early and does *not*
track live work, and only ~1/3 of agents carry a real one — it's a secondary
signal, not truth (see GOTCHAS).

## `internal/ui` — bubbletea

`bubbles/list.Model` drives cursor/filter/pagination; a custom `rowDelegate`
renders each row. `Model` adds three things list doesn't: the live tick, the
background reload, and the tmux actions (switch/rename/kill).

### Rows & columns (`columns.go`, `delegate.go`)
Columns are data. `columnCatalog` is every column sessui can draw, keyed by a
stable `columnID` (the string that persists to disk); each `column` carries
its label, header glyph, fixed `width` (0 = the single flex column), a
`minWidth` floor, and a pure `render(cell) string`. The user's configuration
is `[]columnSetting` (id + optional width override, slice order = display
order); `resolveColumns` turns it into renderable columns, dropping unknown
ids so a newer build's config never bricks an older one, and hiding the peer
column at runtime when no cp3 peers are in use. `layoutColumns(cols, width)`
produces the `tableLayout` — resolved widths for one render — and
`Model.relayout` is its only writer, called on resize, reload and (later)
settings changes. The delegate just walks the layout: fixed-width cells
joined with `JoinHorizontal`, status absorbing what the others leave.
`fixedCol` = `Inline + Width + MaxWidth` so long content truncates on one
line instead of wrapping the row. Four columns exist in the catalog that the
default layout does not show — age, windows, attached, machine — all from
data `session.List` already fetches. `renderStatus`
priority: **needs you** (notify) → **EffectiveSummary** → **verb + elapsed**
(working) → **idle** (agent parked > `idleThreshold` = 15m) → blank.

### Marquee (`marqueeCell` / `marqueeOffset`)
The *selected* row's over-long cells scroll instead of truncating, via
`x/ansi.Cut(content, off, off+width)` (grapheme/wide-char aware, preserves SGR).
`marqueeOffset` ping-pongs with a hold at each end. The frame counter
(`marqueeTick`, 200ms) **resets to 0 on selection change, keyed by session
name** (a 2s reload re-sorts, so index would be wrong) — so a freshly selected
row shows its head, not mid-scroll.

### Full-row highlight (`selectRow` in `style.go`)
The selected row gets a background across all columns. A plain lipgloss
`Background` does **not** work: every cell ends with `\x1b[0m` which also clears
the bg, leaving holes past the first cell. `selectRow` re-applies the bg SGR
after each reset. (Measured, not assumed.)

### Filtering & keys (`model.go handleKey`)
Type-to-filter with **no `/`**: a printable rune arms `list.SetFilterState`.
Arrows nav in *every* state — bubbles/list disables its own CursorUp/Down while
filtering, so we call the list's public `CursorUp()/CursorDown()` directly.
Because letters feed the filter there are **no vim nav keys** — arrows are the
nav, and that's stated in the honest help line (the list's built-in help is
hidden because its hints don't match). Backspacing the filter empty restores the
column headers (`filtering()` = state Filtering *and* non-empty text).

### Colour
`loadPalette(source)` (`style.go`) resolves `Config.Theme.Palette`
(`"auto"` | `"dark"` | `"light"`), never hardcoded hex anywhere else. A
pinned `dark`/`light` returns a complete `builtinPalettes` entry (Catppuccin
Mocha / Latte) and shells out to nothing. `auto` checks
`omarchyThemeAvailable()` — one `exec.LookPath`, not a real exec — and if
Omarchy isn't there, returns the dark builtin with zero queries; if it is,
`themeColorQuery` asks `omarchy-theme-color` once per slot (up to 11) and any
slot it doesn't answer falls back to the dark builtin's value individually,
so a partial theme degrades per-colour rather than all-or-nothing. `New`
calls this once at startup, not per render. `activeHeat` colours last-active
by recency (green < 1m, cyan < 1h, yellow < 1d, faint after). Muted text is
`Foreground + Faint`, not the theme's "muted" slot (see GOTCHAS).

### Glyph sets — Nerd vs ASCII (`glyphs.go`)
Every glyph in the app — app icons (`icons.go`), header/column labels
(`columns.go`, `delegate.go`), the header-pill bell (`header.go`) — is a
codepoint that passes through `glyphU`, and that is the one place
`Config.Icons` (`"nerd"` | `"ascii"`) substitutes: ASCII mode looks the
codepoint up in `asciiGlyphs` and returns a ≤2-cell stand-in (matching
`iconSlotWidth`), or the visible `asciiUnknown` ("*") if it isn't in the
table — never blank. `activeGlyphs` is set once via `useGlyphs(cfg.Icons)`
before `applyTheme` runs (`applyTheme`'s `Icon` values capture `glyphU`'s
output, so order matters). `TestASCIIGlyphs_CoverEveryCodepoint` reads the
package source for every `0x…` literal and fails the build if one has no
entry — a new Nerd glyph can't ship without an ASCII stand-in. The one
exception: `renderPeer`'s and `header.go`'s literal `"✉"` is a plain Unicode
character passed directly, not a Nerd Font PUA codepoint via `glyphU`, so it
needs no stand-in and isn't covered by that test.

### Config persistence (`config.go`)
`Config` (`Columns`, `Icons`, `Theme.Palette`) is everything sessui owns
between runs, at `configPath()` — `$XDG_CONFIG_HOME/sessui/config.json`,
defaulting to `$HOME/.config/sessui/config.json` on every OS including
macOS (deliberately not `os.UserConfigDir`, which returns `~/Library/Application
Support` on Darwin and ignores `XDG_CONFIG_HOME` — see GOTCHAS). Popup
geometry (`@sessui-width` etc.) is deliberately NOT here: tmux needs it
before this process exists, so it stays a tmux option `sessui.tmux` reads at
open time. `LoadConfig` never fails the program: a missing file is the
normal first run (silent, shipped defaults via `withDefaults`); a file that
exists but won't parse returns defaults too, but its error is threaded back
to the caller instead of swallowed. `SaveConfig` writes atomically (sibling
temp file, then rename) and pretty-printed for hand-editing and diffs — it
is written by the `ctrl+e` editor on apply.

### The brand line (`header.go`)
One chrome row above the column headers: the session tally on the left, `user@host`
centred and bold, the wordmark right. It replaced a plain tally row plus a widget
section that expanded under `^w`; the widgets were removed, and folding the tally
into this line gave the list its row back. `who` is centred on the FULL width, not
merely placed between the two ends, so it does not drift sideways as the tally
changes width under a filter. The line sheds parts rather than wrapping — wordmark
first, then the tally, identity last — because a second line would push the table
down on every render.

## Remote hosts

Sessions from other machines sit in the same table as local ones. Three pieces,
each deliberately small.

### Discovery (`hosts.go`) — the library does it
`DiscoverHosts` parses `~/.ssh/config` with `github.com/kevinburke/ssh_config`,
the same parser `charmbracelet/wishlist` uses for this. Importing wishlist
itself would have been the obvious move and is wrong: its `Endpoint` type lives
beside an SSH *server*, so pulling in the parser costs **16MB and 135
dependencies against 3MB and one** (measured). Wishlist needs a Go SSH client
because it is a server proxying on behalf of whoever connects; sessui runs as
the user, in their terminal, and can do better.

Aliases are deduped by **resolved endpoint, not name**. A real config had
`inspiron`, `inspiron-omarchy` and the typo `insipron` all pointing at
`willy@100.96.252.51:22`; without this the same sessions appear three times
under three names. `ssh -G` does the resolving (7ms, no network), so Match
blocks and Include are understood because ssh understands them.

Wildcards are dropped and **nothing is polled automatically**. Discovery
produces the set worth *offering*; the user picks. That same config contained
devices powered off most of the day and a typo that will never answer.

### Transport (`remote.go`) — shell out to `ssh`
`sshArgs` is the whole decision, in one function. Shelling out means
`~/.ssh/config` governs everything — ProxyJump, certificates, agent forwarding,
Match, Include, Tailscale names — because it *is* ssh. A Go client would
reimplement that, partially.

`ControlMaster` is what makes a fan-out affordable: **385ms cold, 36-64ms
multiplexed** against a fleet host. Both tmux queries ride ONE round trip split
by a marker; two calls would double the handshake for nothing.

The remote command ends `exit 0`. tmux exits 1 when no server is running, so
without it a healthy machine with no sessions open reads as UNREACHABLE —
reachability is what ssh says about the *connection*, and an empty session list
is allowed to be empty.

### Non-blocking is the shape, not a flag
`Watcher.Snapshot` is a map read and never touches the network, so rendering a
frame cannot stall on a sleeping laptop. `Refresh` does the I/O and runs from a
goroutine on its own 15s tick — far slower than the 2s local reload, because
each tick is one ssh round trip per host. A failed poll KEEPS the previous
sessions and records the error, so a host going quiet dims its rows instead of
erasing them. `hostTimeout` exists because a sleeping laptop drops the SYN
rather than refusing it.

`Model.watcher` is a **pointer**: `Watcher` holds a mutex and bubbletea passes
Models by value, so a value field is a copylocks race that `go vet` catches.

### Attaching — proxy, don't mirror
tmux cannot switch a client across machines, so "switch to a remote session" is
really "attach over ssh". `AttachRemote` wraps that attachment in a LOCAL
session named `host/name`, after which the row is an ordinary local session:
every later switch is an instant `switch-client`, with no round trip and no
special case anywhere in the UI. `Merge` drops a remote session whose proxy
already exists locally, so attaching does not duplicate the row.

The separator is `/` and **not** `:`. tmux accepts a session named
`inspiron:oc` and then cannot target it — `:` is its session:window separator,
so `has-session -t inspiron:oc` answers "can't find window: oc". A proxy you
can create but never switch to is worse than none.

### Actions know which machine they are on
`runOn` is the single place that decides local-vs-remote. Kill and rename
shipped local-only while the list already showed remote rows, so acting on a
session that lived elsewhere ran tmux *here* and failed. Any action added to a
row needs to go through it.

An action we performed ourselves is the one case where the cache can be
corrected without asking the network: `Forget` and `RenameCached` run in the
kill/rename closures, so the reload that immediately follows agrees with
reality instead of showing a ghost row until the next 15s poll.

### The hosts row (`hostedit.go`)
Same surface, same verbs as the column rows: `↑↓` row, `←→` select, `space`
toggles. `enter` is deliberately **unbound** — it means arm-to-move on the
strip, hosts have no order to change, and a row-specific meaning would break
"the same verbs always mean the same thing" for one convenience the refresh
already provides.

Arriving on the row starts one fan-out across every discovered machine, not
just watched ones, because "which of these can I add right now" is the question
the row exists to answer. Chips resolve in place as replies land.

A chip plays a fill as its machine answers: `md-circle_slice_1` through `_8`,
then `md-circle`, then the ordinary dot -- one family drawn at one size, so it
reads as a circle filling rather than glyphs of different sizes swapping. It
ends on exactly the mark the row would have shown anyway, so the finished UI is
unchanged; only the transition into it is new. The tick runs solely while a chip
is mid-animation and stops on its own. Opening against a warm cache marks
already-reachable hosts as settled rather than replaying it. ASCII mode does not
animate -- the frames are PUA codepoints and would flicker through identical
stand-ins.

This only reads as an animation because chips resolve INDEPENDENTLY:
`refreshHostsCmd` is one Cmd per host, not one that waits on all of them. A
single Cmd reported nothing until the slowest machine answered, so with a
sleeping laptop in the list every chip sat unresolved until the full timeout.

The row is a **viewport**, not a line. With sixteen machines it is wider than a
112-column popup, and before that the cursor could sit on host 12 while the row
still showed 1-8 — `space` would toggle something invisible. It scrolls like a
table: the window moves only when the cursor would leave it, and `‹` `›` are
reserved INSIDE the width budget, never added on top of it.

