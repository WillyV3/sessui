# Gotchas

Each of these cost real time. Read before touching the relevant area.

### 1. GOBIN shadow — install to both places, bind the explicit path
`go install .` drops the binary in mise's GOBIN
(`~/.local/share/mise/installs/go/<ver>/bin`), which is **earlier on PATH** than
`~/.local/bin`. If you only `go build -o ~/.local/bin/sessui`, a bare `sessui`
(and anything resolving via PATH) can run the *stale* GOBIN copy. The tmux bind
therefore uses the explicit `$HOME/.local/bin/sessui`, and you install to both.
Symptom when this bites: "my changes aren't showing up on prefix+s."

### 2. tmux 3.7+ for a live popup
Live updates (spinner, marquee, second-counters) inside `display-popup` rely on
the popup-redraw fix in tmux 3.7 (upstream issue 4920, listed in the 3.6b→3.7 CHANGES — "popup overwritten by
background updates"). On older tmux the popup renders once and never animates.

### 3. The theme "muted" slot is a border colour, not a text colour
`omarchy-theme-color muted` returns a surface/border value (`#414868` on Tokyo
Night) that is unreadable as text on the dark ground. Use `Foreground + Faint`
for de-emphasised *text* (readable-dim, theme-independent, degrades to full
foreground if a terminal ignores Faint). The muted slot IS right as a subtle
selection *background* (`selectRow`).

### 4. `pane_title` is static and mostly echoes
Claude Code's `✳` pane title is set early and does **not** track live work, and
across live agents only ~1/3 carry a real task summary — the rest just echo
their own session/agent name. So: require the `✳` marker, reject name-echoes
(`NormalizeName` vs session and peer name), and treat it as a secondary signal
under a deliberately-set cp3 summary. Rendering an echo would read as
"plugin-dev — plugin-dev".

### 5. No vim nav keys
Type-to-filter commandeers letter keys, so `j`/`k` can't be nav — they filter.
Arrows are the only nav; the help line says so. Don't "add vim keys."

### 6. lipgloss strips colour in non-TTY tests
`go test` runs without a TTY, so lipgloss renders plain — you cannot assert a
colour in a unit test. Test the text/structure in Go; prove colour by driving a
tmux session and reading `capture-pane -pe` (the SGR).

### 7. Verify a glyph is in the font before shipping it
A missing codepoint renders as tofu with no error. Confirm presence first:
`otfinfo -u /usr/share/fonts/TTF/JetBrainsMonoNerdFont-Regular.ttf | grep -i <HEX>`
(or fontTools `getBestCmap()`). Glyph codepoints are stored as `glyphU(0x….)`
ints, not literal runes, so they can't get mangled in transport.

### 8. cp3 peer join is `(cwd, machine)`, never name
`ClaimWithFallback` appends `-<machine>`/`-<sess4>` on a contested name, so the
live peer for session `api-gateway` can be `deploy-bot-laptop`. Matching by name
gives a false DOWN. Match on cwd (+ machine). See ARCHITECTURE.

### 9. A full-row background needs reset-safe re-application
A plain `lipgloss.Style.Background(...)` over already-coloured cells dies at the
first cell's `\x1b[0m` (a full reset clears bg too), leaving holes. `selectRow`
prepends the bg SGR and re-inserts it after every `\x1b[0m`. Verify by counting
`48;2;` occurrences across the selected row in a `-pe` capture (should be many,
not 1).

### 10. The layout follows the popup width — but only one column flexes
Columns are data (`columns.go`): a `tableLayout` is computed from the real
width on every `WindowSizeMsg` and reload via `Model.relayout`, the single
writer of `delegate.layout`. Fixed columns keep their width; exactly one flex
column (status) absorbs the remainder, floored at its `minWidth`. So a wider
popup gives status more room; a popup too narrow for the fixed columns does
NOT reflow — it clips. `--dump` and the tests lay out at `defaultUsableWidth`
(108, the shipped geometry) so a user who never opens the column editor sees
byte-identical widths to before (`TestLayoutColumns_ShippedWidthsUnchanged`).
`ponytail:` a second flex column would need weight-based distribution.

### 11. Marquee frame resets on selection, keyed by name
The scroll frame counter must snap to 0 when the selection moves, or arrowing
onto a long row drops you mid-scroll. It's keyed by session **name**, not list
index, because the 2s background reload re-sorts and shifts indexes.

### 12. macOS config path — not `os.UserConfigDir`
On Darwin, `os.UserConfigDir()` returns `~/Library/Application Support` and
ignores `XDG_CONFIG_HOME` entirely — wrong for a dotfile-managed CLI (chezmoi
manages `~/.config` on the Mac exactly as on Linux). Broke the config tests
on macOS CI the first time they ran there. `configPath` (`config.go`) builds
the path by hand instead: `$XDG_CONFIG_HOME` or `$HOME/.config`, then
`sessui/config.json`, on every OS. Don't reach for `os.UserConfigDir` here.

### 13. A new Nerd glyph needs an ASCII stand-in before it compiles
Every codepoint that reaches `glyphU` (icons, header/column labels, the
header-pill bell) must have an entry in `asciiGlyphs` (`glyphs.go`), or a Mac
running `"icons": "ascii"` sees `asciiUnknown` ("*") where the icon should
be. `TestASCIIGlyphs_CoverEveryCodepoint` reads the package source for every
`0x…` literal and fails the build on a missing entry — so this is caught,
not silent, but budget the extra line in the same commit as the glyph. The
literal `"✉"` (`renderPeer`, `header.go`) is the one exception: a plain
Unicode character, not a Nerd Font PUA codepoint via `glyphU`, so it needs no
stand-in and this test doesn't cover it.

### 14. Don't undo the once-per-process Omarchy probe
`loadPalette` used to shell out to `omarchy-theme-color` once per colour slot
(11 calls) with no upfront check — on a box without it, that's 11 failed
execs at every launch of a tool opened ~120 times/hour, with the failure
hidden behind a fallback the user could neither see nor choose.
`omarchyThemeAvailable` (`style.go`) now calls `exec.LookPath` ONCE to decide
whether Omarchy is present at all: a pinned `dark`/`light` palette never
queries anything, and `auto` on a box without Omarchy short-circuits to the
built-in dark palette with zero real execs. `auto` on a real Omarchy box
still queries all 11 slots — that part is unavoidable and correct, the fix
was only for the "not present" case. Don't reintroduce a per-slot query with
no availability check in front of it.

### 15. A config-load error is NOT `m.err`
`m.err` is cleared by every successful reload (`case reloadMsg`), and the
first reload fires inside `Init()` — so anything parked there at startup is
on screen for one frame. A corrupt `config.json` used to be exactly that: it
degraded to defaults correctly, but the footer notice explaining why was
gone before the user could read it (found by the docs pass, verified live).
It now lives in its own field, `Model.configErr`, which `footerLine` renders
ahead of `m.err` and which only a later successful `SaveConfig` clears.
Pinned by `TestConfigError_SurvivesReload`. Don't route a durable, user-
actionable error through `m.err`; that field is for the last transient
failure only.

### A cache the UI reads must be corrected by the actions the UI takes

Killing a remote session worked and the row stayed on screen for up to 15
seconds. `Merge` renders from the `Watcher` cache, the poll is on a 15s tick,
and nothing told the cache what we had just done -- so the session was gone
from the host and still in the table, which reads as the kill having failed.

Measured before the fix:

    4s after kill    ubuntu-homelab: []    row still shows `newtest`
    18s after kill   (past the tick)       row gone

An action we performed ourselves is the one case where the cache can be
corrected without asking the network: `Watcher.Forget` and `RenameCached` are
called by the kill/rename closures, so the reload that immediately follows
already reflects the change. Any future action on a remote row needs the same
treatment -- the poll is too slow to be the only source of truth about our own
writes.

