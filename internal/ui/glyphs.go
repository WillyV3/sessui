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
const asciiUnknown = "*"

// asciiGlyphs maps every codepoint the app uses to a stand-in of at most two
// cells, because iconSlotWidth reserves two per icon and a wider stand-in
// would push the column. Keyed by codepoint rather than by icon so the
// header/column glyphs (which are bare codepoints, not Icons) are covered by
// the same table. TestASCIIGlyphs_CoverEveryCodepoint keeps this complete.
var asciiGlyphs = map[rune]string{
	// header / column glyphs (columns.go, delegate.go)
	0xF489:  ">_", // session (terminal)
	0xF0430: "~",  // active (pulse)
	0xF43C:  "((", // status (broadcast)
	0xEA83:  "/",  // cwd (folder)
	0xF0318: "<>", // peer (lan-connect)
	0xF0150: "t",  // age (calendar-clock)
	0xF0E1E: "#",  // windows (dock-window)
	0xF0E7E: "@",  // attached (monitor)
	0xF0EA1: "m",  // machine (server)
	0xF0F3:  "!",  // needs-you pill (bell)

	// app icons (icons.go)
	0xEC82:  "C",   // claude
	0xF06A9: "A",   // generic agent
	0xEC81:  "ai",  // codex / openai
	0xE6AE:  "nv",  // neovim
	0xF121:  "oc",  // opencode
	0xEC1E:  "cp",  // copilot
	0xE7F0:  "G",   // gemini
	0xF219:  "cr",  // crush
	0xEC20:  "ad",  // aider
	0xE62B:  "vi",  // vim
	0xEAC4:  "hx",  // helix
	0xEC6F:  "gt",  // git
	0xE7B0:  "dk",  // docker
	0xF10FE: "k8",  // kubernetes
	0xF0E4:  "%",   // monitor / top
	0xF02CA: "du",  // dust / disk
	0xF06F3: "nw",  // bandwhich / network
	0xF0349: "?",   // search
	0xF024B: "f",   // files
	0xF14E:  "z",   // zoxide
	0xF0502: "tv",  // television
	0xF135:  "^",   // starship
	0xF31A:  "ff",  // fastfetch
	0xEB0F:  "jq",  // jq
	0xF0B5F: "b",   // bat
	0xE724:  "go",  // go
	0xE7A8:  "rs",  // rust
	0xE606:  "py",  // python
	0xE718:  "js",  // node
	0xF040A: "mp",  // mpv
	0xF02E9: "im",  // imv / image
	0xF0EA2: "eq",  // cava / equalizer
	0xF04C7: "sp",  // spotify
	0xF05A9: "wi",  // impala / wifi
	0xF057E: "au",  // wiremix / audio
	0xF00AF: "bt",  // bluetooth
	0xF01EE: "@",   // mail
	0xF028C: "ch",  // weechat / chat
	0xF0395: "rs",  // newsboat / rss
	0xF00ED: "cal", // calcurse -- 3 cells; ponytail: accepted, calendar apps are rare in a session
	0xF02DA: "h",   // atuin / history
	0xEA85:  "$",   // shell
	0xF0C8:  "[]",  // other
}
