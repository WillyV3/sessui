package ui

// glyphSet selects how a codepoint renders: as the Nerd Font glyph, or as a
// plain-ASCII stand-in for a terminal without a patched font, where a missing
// glyph renders as tofu with no error.
type glyphSet string

const (
	glyphsNerd  glyphSet = "nerd"
	glyphsASCII glyphSet = "ascii"
)

// activeGlyphs is set once at startup, before applyTheme builds the icon
// table, because those Icon values capture glyphU's output.
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

// glyphU turns a codepoint into what the terminal should show for it. Every
// glyph in the app is built from a plain hex int through here, so the
// codepoint is visible at the call site and cannot be mangled in transport,
// and this is the single point where the ASCII set substitutes in.
func glyphU(codepoint rune) string {
	if activeGlyphs == glyphsASCII {
		if ascii, ok := asciiGlyphs[codepoint]; ok {
			return ascii
		}
		return asciiUnknown
	}
	return string(codepoint)
}

// asciiUnknown stands in for any codepoint the ASCII table does not cover:
// visible, not blank, and easy to grep for.
const asciiUnknown = "??"

// asciiGlyphs is the stand-in for every Nerd Font codepoint the app draws, at
// most two cells each because iconSlotWidth reserves two per icon. Keyed by
// codepoint so header glyphs, which are bare codepoints, are covered too.
//
// Two rules keep the set reading as a design rather than a fallback: a column
// header has no stand-in ("session", "cwd" need no picture), and every app is
// a unique two-letter lowercase tag so a row of apps aligns.
var asciiGlyphs = map[rune]string{
	// column headers: none, by rule
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
	0x25C6: "*", // settings editor: armed-for-move (◆)

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
	0xF001:  "mu", // music
	0xF05A9: "wi", // impala / wifi
	0xF057E: "au", // wiremix / audio
	// Host-arrival fill frames (hostedit.go). ASCII never animates; these
	// exist so the codepoints have stand-ins if rendered directly.
	0xF0A9E: "*",  // md-circle_slice_1
	0xF0A9F: "*",  // md-circle_slice_2
	0xF0AA0: "*",  // md-circle_slice_3
	0xF0AA1: "*",  // md-circle_slice_4
	0xF0AA2: "*",  // md-circle_slice_5
	0xF0AA3: "*",  // md-circle_slice_6
	0xF0AA4: "*",  // md-circle_slice_7
	0xF0AA5: "*",  // md-circle_slice_8
	0xF0765: "*",  // md-circle
	0xF00AF: "bl", // bluetooth
	0xF01EE: "ml", // mail
	0xF028C: "ch", // weechat / chat
	0xF0395: "fd", // newsboat / feeds
	0xF00ED: "ca", // calcurse / calendar
}

// glyphOr is glyphU with a stand-in chosen at the call site, for the places
// where the table entry is an app tag but the context wants a word.
func glyphOr(codepoint rune, ascii string) string {
	if activeGlyphs == glyphsASCII {
		return ascii
	}
	return string(codepoint)
}

// joinGlyph puts a glyph before a label with one space between, and no stray
// space when either side is empty.
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
