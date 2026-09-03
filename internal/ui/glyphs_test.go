package ui

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// TestASCIIGlyphs_CoverEveryCodepoint is the completeness guard: every
// codepoint the app hands to glyphU must have an ASCII stand-in, or a Mac
// without the font sees the asciiUnknown marker where an icon should be.
// It finds the codepoints by reading the source, so a new glyph added
// anywhere in the package fails this test until it gets an entry.
func TestASCIIGlyphs_CoverEveryCodepoint(t *testing.T) {
	src := readPackageSource(t)
	// glyphU(0xF489) and `glyphSession = 0xF489` both name a codepoint; the
	// table itself is excluded so it cannot vouch for its own keys.
	re := regexp.MustCompile(`(?:glyphU\(|= )0x([0-9A-Fa-f]{4,5})\b`)
	seen := map[string]bool{}
	for file, text := range src {
		if strings.HasSuffix(file, "glyphs.go") {
			continue
		}
		for _, m := range re.FindAllStringSubmatch(text, -1) {
			seen[strings.ToUpper(m[1])] = true
		}
	}
	if len(seen) == 0 {
		t.Fatal("found no codepoints in the package source; the regexp is broken")
	}
	for hex := range seen {
		cp, err := strconv.ParseUint(hex, 16, 32)
		if err != nil {
			t.Fatalf("parse %s: %v", hex, err)
		}
		if _, ok := asciiGlyphs[rune(cp)]; !ok {
			t.Errorf("codepoint 0x%s is used in the app but has no ASCII stand-in", hex)
		}
	}
}

// readPackageSource returns every non-test Go file in this package, by
// name, so a test can audit the source it lives beside.
func readPackageSource(t *testing.T) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	src := map[string]string{}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		b, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		src[name] = string(b)
	}
	return src
}

// TestASCIIGlyphs_FitTheIconSlot: a stand-in wider than iconSlotWidth would
// push every column after it, so the table is held to two cells. Only a
// column header may be empty (it renders as its label alone); anything
// else empty would vanish.
func TestASCIIGlyphs_FitTheIconSlot(t *testing.T) {
	header := map[rune]bool{}
	for _, c := range columnCatalog {
		header[c.glyph] = true
	}
	for cp, ascii := range asciiGlyphs {
		if w := lipgloss.Width(ascii); w > iconSlotWidth {
			t.Errorf("0x%X -> %q is %d cells; must be <= %d", cp, ascii, w, iconSlotWidth)
		}
		if ascii == "" && !header[cp] {
			t.Errorf("0x%X has an empty stand-in and is not a column header; it would vanish", cp)
		}
	}
}

// TestGlyphU_SwitchesSets pins the behaviour the setting exists for: the
// same codepoint renders as the font glyph in one set and its stand-in in
// the other, an unknown codepoint is visible rather than blank, and an
// unrecognised set name degrades to Nerd.
func TestGlyphU_SwitchesSets(t *testing.T) {
	t.Cleanup(func() { useGlyphs(glyphsNerd) })

	useGlyphs(glyphsNerd)
	if got := glyphU(glyphSession); got != string(rune(glyphSession)) {
		t.Errorf("nerd: glyphU(session) = %q, want the raw glyph", got)
	}

	useGlyphs(glyphsASCII)
	if got := glyphU(glyphSession); got != "" {
		t.Errorf("ascii: glyphU(session) = %q, want \"\" (a header is its label alone)", got)
	}
	if got := glyphU(0xE6AE); got != "nv" {
		t.Errorf("ascii: glyphU(neovim) = %q, want the two-letter tag \"nv\"", got)
	}
	if got := glyphU(0x1F600); got != asciiUnknown {
		t.Errorf("ascii: unknown codepoint = %q, want %q (visible, not blank)", got, asciiUnknown)
	}

	useGlyphs("wingdings")
	if activeGlyphs != glyphsNerd {
		t.Errorf("unrecognised set left activeGlyphs = %q, want nerd (safe default)", activeGlyphs)
	}
}

// TestASCIIGlyphs_DesignRules pins the two rules the table is built on:
// column headers have no stand-in; every app tag is two cells and unique.
func TestASCIIGlyphs_DesignRules(t *testing.T) {
	for _, c := range columnCatalog {
		if c.glyph == 0 {
			continue
		}
		if got, ok := asciiGlyphs[c.glyph]; !ok || got != "" {
			t.Errorf("column %s: ASCII stand-in = %q, want none (headers read as plain labels)", c.id, got)
		}
	}
	seen := map[string]rune{}
	for cp, tag := range asciiGlyphs {
		if tag == "" || cp == glyphNeedsYou || cp == glyphArmed {
			continue
		}
		if len([]rune(tag)) != 2 {
			t.Errorf("0x%X: tag %q is %d cells, want exactly 2 so app rows align", cp, tag, len([]rune(tag)))
		}
		if other, dup := seen[tag]; dup {
			t.Errorf("tag %q is used by both 0x%X and 0x%X", tag, other, cp)
		}
		seen[tag] = cp
	}
}

// TestASCIIHeaders_NoStraySpace: in ASCII mode a header cell is exactly its
// label -- no leading space where the glyph used to be.
func TestASCIIHeaders_NoStraySpace(t *testing.T) {
	useGlyphs(glyphsASCII)
	t.Cleanup(func() { useGlyphs(glyphsNerd) })
	col := columnCatalog[colSession]
	if got := stripLabel(col, false); got != col.label {
		t.Errorf("stripLabel = %q, want %q", got, col.label)
	}
	if got := col.headerCell(testStyles()); strings.HasPrefix(got, " ") || !strings.Contains(got, col.label) {
		t.Errorf("headerCell = %q", got)
	}
	if got := stripLabel(col, true); got != "* "+col.label {
		t.Errorf("armed stripLabel = %q, want the * marker", got)
	}
	if got := glyphOr(glyphNote, "vol"); got != "vol" {
		t.Errorf("glyphOr in ASCII = %q", got)
	}
}

// TestASCIIHeaders_GlyphOnlyColumnsGetAWord: active and attached have no
// label in Nerd mode (the glyph is the header); in ASCII they must not go
// blank.
func TestASCIIHeaders_GlyphOnlyColumnsGetAWord(t *testing.T) {
	useGlyphs(glyphsASCII)
	t.Cleanup(func() { useGlyphs(glyphsNerd) })
	if got := stripLabel(columnCatalog[colActive], false); got != "ago" {
		t.Errorf("active header in ASCII = %q, want \"ago\"", got)
	}
	if got := stripLabel(columnCatalog[colAttached], false); got != "on" {
		t.Errorf("attached header in ASCII = %q, want \"on\"", got)
	}
	useGlyphs(glyphsNerd)
	if got := stripLabel(columnCatalog[colActive], false); got != string(rune(glyphActive)) {
		t.Errorf("active header in Nerd = %q, want the glyph alone", got)
	}
}
