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
// push every column after it, so the table is held to two cells -- with the
// one explicitly accepted exception.
func TestASCIIGlyphs_FitTheIconSlot(t *testing.T) {
	for cp, ascii := range asciiGlyphs {
		if w := lipgloss.Width(ascii); w > iconSlotWidth && ascii != "cal" {
			t.Errorf("0x%X -> %q is %d cells; must be <= %d", cp, ascii, w, iconSlotWidth)
		}
		if ascii == "" {
			t.Errorf("0x%X has an empty stand-in; use %q so it is visible", cp, asciiUnknown)
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
	if got := glyphU(glyphSession); got != ">_" {
		t.Errorf("ascii: glyphU(session) = %q, want \">_\"", got)
	}
	if got := glyphU(0x1F600); got != asciiUnknown {
		t.Errorf("ascii: unknown codepoint = %q, want %q (visible, not blank)", got, asciiUnknown)
	}

	useGlyphs("wingdings")
	if activeGlyphs != glyphsNerd {
		t.Errorf("unrecognised set left activeGlyphs = %q, want nerd (safe default)", activeGlyphs)
	}
}
