# Gotchas

Each of these cost real time. Read before touching the relevant area.

### 1. GOBIN shadow — install to both places, bind the explicit path
`go install .` drops the binary in mise's GOBIN
(`~/.local/share/mise/installs/go/<ver>/bin`), which is **earlier on PATH** than
`~/.local/bin`. If you only `go build -o ~/.local/bin/sessui`, a bare `sessui`
(and anything resolving via PATH) can run the *stale* GOBIN copy. The tmux bind
therefore uses the explicit `$HOME/.local/bin/sessui`, and you install to both.
Symptom when this bites: "my changes aren't showing up on prefix+s."

### 2. tmux 3.8+ for a live popup
Live updates (spinner, marquee, second-counters) inside `display-popup` rely on
the popup-redraw fix in tmux 3.8 (upstream issue 4920 — "popup overwritten by
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
live peer for session `sontara` can be `astrobot-omarchy`. Matching by name
gives a false DOWN. Match on cwd (+ machine). See ARCHITECTURE.

### 9. A full-row background needs reset-safe re-application
A plain `lipgloss.Style.Background(...)` over already-coloured cells dies at the
first cell's `\x1b[0m` (a full reset clears bg too), leaving holes. `selectRow`
prepends the bg SGR and re-inserts it after every `\x1b[0m`. Verify by counting
`48;2;` occurrences across the selected row in a `-pe` capture (should be many,
not 1).

### 10. Fixed-width layout assumes ~112 columns
The table is a fixed 108-usable-column layout. `@sessui-width` as a percentage
on a narrow terminal will clip it. Making the columns responsive to the actual
popup width (the tea `WindowSizeMsg` is already received) is a real, unbuilt
follow-up — not something the current code does.

### 11. Marquee frame resets on selection, keyed by name
The scroll frame counter must snap to 0 when the selection moves, or arrowing
onto a long row drops you mid-scroll. It's keyed by session **name**, not list
index, because the 2s background reload re-sorts and shifts indexes.
