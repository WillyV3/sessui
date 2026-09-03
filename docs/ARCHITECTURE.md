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
is contested (session `sontara` → peer `astrobot` / `astrobot-omarchy`), so name
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
All colour comes from `omarchy-theme-color` via `loadPalette` (`style.go`),
never hardcoded hex. `activeHeat` colours last-active by recency (green < 1m,
cyan < 1h, yellow < 1d, faint after). Muted text is `Foreground + Faint`, not
the theme's "muted" slot (see GOTCHAS).
