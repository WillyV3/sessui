# Decisions

Appended when a backlog item ships. Each entry: the choice, the reason, the
commit. Reversing one means a new entry, not an edit.

## 2026-09-03

- **Columns are data; the layout follows the popup width.** `columnCatalog`, `tableLayout`, `Model.relayout` as the single writer. One flex column. Shipped widths byte-identical for a default config (test-pinned). `82a0e06`.
- **Config lives at `~/.config/sessui/config.json` on every OS.** Not `os.UserConfigDir` (ignores XDG on Darwin). Atomic write. Popup geometry stays in the `@sessui-width` tmux option because tmux needs it before the binary runs. `d69e84a`, `46a3f62`.
- **ASCII glyph set via `glyphU`** — the one choke point every codepoint passes through; a completeness test fails the build on a glyph with no stand-in. `6c93a92`.
- **Omarchy probed once; explicit light/dark built-ins.** `theme.palette` auto|dark|light. 0 execs on a box without Omarchy. `c0cf7ae`.
- **Header carries two signals that exist nowhere else on screen**: needs-you and unread mail, as pills, only when nonzero. Ambient counts (working/idle) stay off by default — they were tried and removed once (`d7819a3`). `dab2e92`.
- **One overlay slot, pull-based contract** `overlayResult.result() (finished, apply)`. huh replaces hand-rolled rename/kill; both render as one footer line. A push `overlayDoneMsg` was built twice and killed. `7772a58`.
- **Column editor: the header row is the surface.** Strip = visible columns at the preview's exact layout; shelf = hidden columns as chips; width row. ↑↓ picks the row; five verbs mean the same thing everywhere. `esc` applies (no confirm — 120 opens/hour), `ctrl+z` abandons, `ctrl+r` resets. `d58e981`, `0e8afac`.
- **Theme customisation is role → palette slot, never raw hex.** A theme switch re-resolves the slot. `roleMail` governs the header pill; the inline ✉ stays yellow. `356c974`.
- **A config-parse error is its own field**, not `m.err`, which every reload clears. `bf7cadb`.
- **CI on Linux and macOS, release archives on a `v*` tag.** macOS caught the Darwin config-path bug on its first run. `869cb07`.
- **Team process:** one contract pinned before spawning; every agent in its own worktree; two builders at once max. Five agents in the shared tree cost a contract mismatch and a branch collision.
- **The editor renders at the window it is in.** Popup width is only a number on its row; laying the page out at the setting clipped everything once the setting differed from the window. Legends use `^r`/`^x` notation so they fit at 108 with no truncation. `7d6d714`.
- **The popup-width control is captioned "popup" and says "tmux popup width · takes effect next open".** "width" beside a column editor read as a column. `7d6d714`.
- **The header is one line, always; widgets expand INTO it.** `widget = icon()+expand(st, width)`; `controllable` adds keys, `poller` adds a tick-driven refresh. Focus and expansion are one state (`^w`, tab/shift+tab, esc) riding the existing overlay slot. Invariant pinned at 60/108/130 against a width-ignoring widget, mutation-checked. Default config (attention only) renders byte-identical to before. `ef5f185`.
- **Poll results route by identity, not catalog name.** `widgetPollMsg.src` is the poller; two `shell` widgets keep their own output. Pollers run at open as well as on the tick — the popup is rarely up 2s. `a36265b`.
- **`shell` is the plugin escape hatch**: `{"name":"shell","args":{"cmd","icon"}}`, first output line, 1.5s timeout, off the UI thread. Go widgets implement two methods. Schema documented in README for the user's own coding agent. `a36265b`.
- **now-playing is a thin client of Omarchy's media service.** `omarchy-shell media status` (JSON, captured live as the test fixture) on every poll; `playPause|next|previous|sourceNext` and `omarchy-audio-output-volume raise|lower|mute-toggle` on keys, each followed by an immediate re-poll in the same Cmd. No MPRIS in sessui; a box without `omarchy-shell` hides the widget. Transport legend pairs ←→/+- so it fits at 108 (test-pinned). `mediaClient` interface, fake in tests. `8ba9fcf`.
