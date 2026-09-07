package ui

// glyphSet selects how a codepoint renders: as the Nerd Font glyph itself,
// or as a plain-ASCII stand-in for a terminal without a patched font. On
// Omarchy the font is a given; on a Mac it is not, and a missing glyph
// renders as tofu with no error (GOTCHAS #7) -- so this is the Mac case,
// handled at the one place every glyph in the app passes through.
type glyphSet string

const (
	glyphsNerd  glyphSet = "nerd"
	glyphsASCII glyphSet = "ascii"
)

// activeGlyphs is set once at startup from Config.Icons, before applyTheme
// builds the icon table (those Icon values capture glyphU's output). A
// package var, like the icons themselves: it is a startup decision, not
// per-render state.
var activeGlyphs = glyphsNerd

// useGlyphs installs the set. Anything unrecognised means Nerd, so a typo in
// config.json degrades to the shipped look rather than to blank icons.
func useGlyphs(set glyphSet) {
	if set == glyphsASCII {
		activeGlyphs = glyphsASCII
		return
	}
	activeGlyphs = glyphsNerd
}

// glyphU turns a codepoint into what the terminal should show for it. A
// function, not a literal, so every glyph in the app is built from a plain
// hex int -- immune to literal Nerd Font runes getting mangled in transport,
// and the codepoint is visible at the call site either way. It is also the
// single point where the ASCII set substitutes in.
func glyphU(codepoint rune) string {
	if activeGlyphs == glyphsASCII {
		if ascii, ok := asciiGlyphs[codepoint]; ok {
			return ascii
		}
		return asciiUnknown
	}
	return string(codepoint)
}

// asciiUnknown stands in for any codepoint the ASCII table does not cover,
// so a newly added Nerd glyph shows *something* on a Mac until it gets an
// entry -- visible, not blank, and easy to grep for.
const asciiUnknown = "??"

// asciiGlyphs maps every codepoint the app uses to a stand-in of at most two
// cells, because iconSlotWidth reserves two per icon and a wider stand-in
// would push the column. Keyed by codepoint rather than by icon so the
// header/column glyphs (which are bare codepoints, not Icons) are covered by
// the same table. TestASCIIGlyphs_CoverEveryCodepoint keeps this complete.

// asciiGlyphs is the stand-in for every Nerd Font codepoint the app draws,
// for a terminal without the font (icons: "ascii" -- the usual Mac case).
//
// Two rules make the set read as a design rather than a fallback:
//
//   - A column header has NO stand-in ("" -- headerCell and stripLabel drop
//     the glyph and its space). "session", "apps", "cwd" need no picture;
//     ">_ session" and "(( status" only added noise.
//   - Every app is a two-letter lowercase tag, unique across the set, so a
//     row of apps aligns ("cl nv gt") and no two apps share a tag. Where a
//     tool's own name has an obvious pair, use it (jq, go, py); otherwise
//     the first two consonant-ish letters (dk docker, k8 kubernetes).
//
// Anything else the app draws -- a pill, a marker -- gets the one ASCII
// character that carries the meaning.
var asciiGlyphs = map[rune]string{
	// column headers (columns.go): none, by rule
	0xF489:  "", // session
	0xF0430: "", // active
	0xF43C:  "", // status
	0xEA83:  "", // cwd
	0xF0318: "", // peer
	0xF0150: "", // age
	0xF0E1E: "", // windows
	0xF0E7E: "", // attached
	0xF0EA1: "", // machine

	// markers
	0x25C6: "*", // column editor: armed-for-move (◆)

	// coding agents
	0xEC82:  "cl", // claude
	0xF06A9: "ai", // generic agent (robot)
	0xEC81:  "cx", // codex / openai
	0xF121:  "oc", // opencode
	0xEC1E:  "cp", // copilot
	0xE7F0:  "gm", // gemini
	0xF219:  "cr", // crush
	0xEC20:  "ad", // aider

	// editors and dev tools
	0xE6AE:  "nv", // neovim
	0xE62B:  "vi", // vim
	0xEAC4:  "hx", // helix
	0xEC6F:  "gt", // git
	0xE7B0:  "dk", // docker
	0xF10FE: "k8", // kubernetes
	0xEB0F:  "jq", // jq
	0xF0B5F: "ba", // bat
	0xE724:  "go", // go
	0xE7A8:  "rs", // rust
	0xE606:  "py", // python
	0xE718:  "js", // node

	// system and files
	0xF0E4:  "tp", // top / btop (tachometer)
	0xF02CA: "du", // dust / disk
	0xF06F3: "nw", // bandwhich / network
	0xF0349: "sr", // search (magnify)
	0xF024B: "fm", // file manager (folder)
	0xF14E:  "zx", // zoxide (jump)
	0xF0502: "tv", // television
	0xF135:  "ss", // starship (rocket)
	0xF31A:  "ff", // fastfetch
	0xF02DA: "hi", // atuin / history
	0xEA85:  "sh", // shell
	0xF0C8:  "..", // other / unknown app (square)

	// media and comms
	0xF040A: "mp", // mpv
	0xF02E9: "im", // imv / image
	0xF0EA2: "eq", // cava / equalizer
	0xF04C7: "sp", // spotify
	0xF001:  "mu", // cliamp / music
	0xF05A9: "wi", // impala / wifi
	0xF057E: "au", // wiremix / audio
	0xF00AF: "bl", // bluetooth
	0xF01EE: "ml", // mail
	0xF028C: "ch", // weechat / chat
	0xF0395: "fd", // newsboat / feeds
	0xF00ED: "ca", // calcurse / calendar
}

// glyphOr is glyphU with a stand-in chosen at the call site: for the few
// places where the codepoint's table entry is an app tag but the context
// wants a word ("vol", not "mu").
func glyphOr(codepoint rune, ascii string) string {
	if activeGlyphs == glyphsASCII {
		return ascii
	}
	return string(codepoint)
}

// joinGlyph puts a glyph before a label with one space between -- and no
// stray space when either side is empty (an ASCII header has no glyph; a
// glyph-only cell has no label).
func joinGlyph(glyph, label string) string {
	switch {
	case glyph == "":
		return label
	case label == "":
		return glyph
	default:
		return glyph + " " + label
	}
}
