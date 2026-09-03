package ui

import (
	"errors"
	"strings"
	"testing"

	"github.com/willyv3/sessui/internal/session"
)

// TestConfigError_SurvivesReload pins the fix for GOTCHAS #15: a config that
// failed to parse must stay visible in the footer across the successful
// reloads that fire every couple of seconds -- the first of them inside
// Init -- and must go away once a later save succeeds, because that save
// replaced the unparseable file with a good one.
func TestConfigError_SurvivesReload(t *testing.T) {
	useTempConfigDir(t)
	m := New()
	m.configErr = errors.New("parse config.json: unexpected end of JSON input")
	m.width, m.height = 112, 28

	if !strings.Contains(m.footerLine(), "config:") {
		t.Fatalf("footer before reload = %q, want the config error", m.footerLine())
	}

	// A successful reload clears m.err -- and must NOT clear configErr.
	next, _ := m.Update(reloadMsg{sessions: []session.Session{{Name: "x"}}})
	m = next.(Model)
	if !strings.Contains(m.footerLine(), "config:") {
		t.Fatalf("footer after a successful reload = %q, want the config error to survive", m.footerLine())
	}

	// A successful save of a new column set replaces the bad file; the
	// notice is now stale and goes.
	next, _ = m.Update(editorAppliedMsg{columns: defaultColumnSettings(), theme: ThemeConfig{Palette: paletteAuto}, icons: glyphsNerd})
	m = next.(Model)
	if strings.Contains(m.footerLine(), "config:") {
		t.Errorf("footer after a successful save = %q, want the config error cleared", m.footerLine())
	}
}
