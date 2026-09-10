# Gotchas

Each of these cost real time. Read before touching the relevant area.

### 1. tmux 3.7+ for a live popup

Live updates inside `display-popup` rely on the popup-redraw fix in tmux 3.7
(upstream issue 4920). On older tmux the popup renders once and never
animates.

### 2. The theme "muted" slot is a border colour, not a text colour

`omarchy-theme-color muted` returns a surface value that is unreadable as
text. Use `Foreground + Faint` for de-emphasised text. The muted slot is right
as the selection background.

### 3. `pane_title` is static and mostly echoes

An agent's `✳` pane title is set early and does not track live work, and most
just echo the session name. Require the marker, reject name echoes, and treat
it as secondary to a deliberately set cp3 summary.

### 4. No vim nav keys

Type-to-filter takes the letter keys, so `j`/`k` filter. Arrows are the nav.

### 5. lipgloss strips colour without a TTY

`go test` runs without a TTY, so lipgloss renders plain. Test structure in Go;
prove colour by driving a tmux session and reading `capture-pane -pe`, or
call `lipgloss.SetColorProfile(termenv.TrueColor)` in the test.

### 6. Verify a glyph is in the font before shipping it

A missing codepoint renders as tofu with no error:
`otfinfo -u <font file> | grep -i <HEX>`. Codepoints are `glyphU(0x…)` ints,
not literal runes, so they cannot be mangled in transport.

### 7. cp3 peer join is by cwd, never name

cp3 appends a suffix to a contested name, so the live peer for a session can
carry a different name. Matching by name gives a false DOWN.

### 8. A full-row background needs reset-safe re-application

`lipgloss.Style.Background` over coloured cells dies at the first `\x1b[0m`.
`selectRow` re-inserts the background SGR after every reset. Verify by
counting `48;2;` occurrences across a selected row in a `-pe` capture.

### 9. Only one column flexes

Fixed columns keep their width; the status column absorbs the remainder,
floored at its `minWidth`. A popup too narrow for the fixed columns clips
rather than reflowing.

### 10. Marquee frame resets on selection, keyed by name

Keyed by session name, not index: the background reload re-sorts.

### 11. macOS config path — not `os.UserConfigDir`

It returns `~/Library/Application Support` and ignores `XDG_CONFIG_HOME`.
`configPath` builds `$XDG_CONFIG_HOME` or `$HOME/.config` by hand.

### 12. A new Nerd glyph needs an ASCII stand-in

Every codepoint reaching `glyphU` needs an `asciiGlyphs` entry, or ASCII mode
shows `asciiUnknown`. The literal `"✉"` is plain Unicode, not a PUA codepoint,
and is the one exception.

### 13. Don't undo the once-per-process Omarchy probe

`loadPalette` once shelled out per colour slot with no upfront check: eleven
failed execs at every launch on a box without Omarchy. `omarchyThemeAvailable`
calls `exec.LookPath` once. Don't reintroduce a per-slot query without it.

### 14. A config-load error is not `m.err`

`m.err` is cleared by every successful reload, and the first reload fires
inside `Init()`, so anything parked there at startup is on screen for one
frame. A corrupt `config.json` lives in `Model.configErr`, which only a later
successful save clears.

### 15. A cache the UI reads must be corrected by the actions the UI takes

Killing a remote session works, and the row stays until the next 15s poll
unless the cache is told. `Watcher.Forget` and `RenameCached` are called from
the kill/rename closures. Any future action on a remote row needs the same.

### 16. `#{client_session}`, not `#S`

Inside a popup `#S` resolves against a target that is not the attached client
and answers with an unrelated session. `#{client_session}` is correct even
with `TMUX` unset.

### 17. The proxy separator is `/`

tmux accepts a session named `host:name` and then cannot target it: `:` is its
session:window separator.

### 18. The remote command ends `exit 0`

tmux exits 1 with no server running. Without `exit 0`, a healthy machine with
no sessions reads as unreachable. A host with no tmux at all is reported
separately by `noTmuxMarker`.
