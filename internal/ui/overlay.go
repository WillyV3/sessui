// The overlay slot: a single tea.Model that takes over the footer band
// (see footerLine in model.go) for anything that needs more than one key
// press -- rename, kill confirmation, and later a hand-rolled column
// editor. huh drives the two overlays built here; formOverlay is the one
// place huh's own completion state (form.State) gets translated into the
// contract Model actually consumes, so Model itself never imports huh.
package ui

import (
	"errors"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

// errNameRequired is requireNonEmpty's validation error -- huh renders it
// only if WithShowErrors is on, which compactForm turns off to keep the
// footer to one line (see compactForm); the practical effect is just that
// enter on an empty name silently doesn't submit.
var errNameRequired = errors.New("name required")

// overlayResult is the one contract every overlay reports completion
// through -- a huh form today, a hand-rolled column editor tomorrow -- so
// Model needs exactly one type assertion after Update (see updateOverlay),
// never a switch on which kind of overlay is showing.
type overlayResult interface {
	// result reports whether the overlay is finished, and if so, the
	// command to run: the real action (already wrapped with a reload) when
	// the user completed it, or nil when they aborted. Either way the
	// caller drops the overlay and returns focus to the list.
	result() (finished bool, apply tea.Cmd)
}

// formOverlay adapts a *huh.Form to the overlay contract. huh reports
// completion via form.State (StateCompleted / StateAborted); this is the
// only place that state gets read, so every other overlay -- including
// ones that aren't huh forms at all -- reports through the same result()
// method instead of a per-kind branch in Model.
type formOverlay struct {
	form *huh.Form
	// apply is called once, only when the form completes, to build the
	// real side-effecting command from whatever the fields collected.
	apply func() tea.Cmd
}

func (o *formOverlay) Init() tea.Cmd { return o.form.Init() }

func (o *formOverlay) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// huh's own Quit keymap only binds ctrl+c, and Model intercepts that
	// for the whole program before an overlay ever sees a message (see
	// Update) -- esc has no binding in huh's default keymap at all, so
	// abort it directly here: no side effects, just report finished.
	if k, ok := msg.(tea.KeyMsg); ok && k.Type == tea.KeyEsc {
		o.form.State = huh.StateAborted
		return o, nil
	}
	next, cmd := o.form.Update(msg)
	o.form = next.(*huh.Form)
	return o, cmd
}

func (o *formOverlay) View() string { return o.form.View() }

func (o *formOverlay) result() (finished bool, apply tea.Cmd) {
	switch o.form.State {
	case huh.StateCompleted:
		return true, o.apply()
	case huh.StateAborted:
		return true, nil
	default:
		return false, nil
	}
}

// compactForm builds a single-group huh.Form pinned to one line: no help
// row, no error row, theme + width fixed up front so the height-0/width-0
// auto-resize path in huh's own Update (driven by a real tea.WindowSizeMsg)
// never fires -- Model already owns window sizing (see Update/listSize) and
// must not have huh fight it for the footer band's height.
//
// This -- plus stripping the field's left border/padding in newHuhTheme --
// is the compact route tried first, and it's sufficient: a one-field group
// with no title/description on the GROUP (only the field) and help/errors
// off renders as exactly one line (verified with a tmux capture, see the
// track report). No 2-line fallback was needed.
func compactForm(theme *huh.Theme, width int, group *huh.Group) *huh.Form {
	return huh.NewForm(group).
		WithTheme(theme).
		WithShowHelp(false).
		WithShowErrors(false).
		WithWidth(width).
		WithHeight(1)
}

// renameOverlay is a one-field huh form prefilled with the session's
// current name; validation blocks submitting an empty name. rename is the
// action to invoke on completion -- session.Rename in production, a fake
// in tests, so completing the form never has to shell out to tmux to be
// tested (see model_test.go).
func renameOverlay(theme *huh.Theme, width int, target string, rename func(old, new string) error) tea.Model {
	name := target
	field := huh.NewInput().
		Title("rename (esc cancel): ").
		Inline(true).
		Prompt("").
		CharLimit(128).
		Value(&name).
		Validate(requireNonEmpty)

	return &formOverlay{
		form: compactForm(theme, width, huh.NewGroup(field)),
		apply: func() tea.Cmd {
			newName := strings.TrimSpace(name)
			if newName == "" || newName == target {
				return nil // no-op: unchanged name, nothing to do
			}
			return doAndReload(func() error { return rename(target, newName) })
		},
	}
}

func requireNonEmpty(s string) error {
	if strings.TrimSpace(s) == "" {
		return errNameRequired
	}
	return nil
}

// killOverlay is a one-field huh confirm defaulting to "no" (huh's zero
// value); "y"/"Y" submits true, "n"/"N"/enter-on-default submits false --
// matching the hand-rolled prompt's y/n behaviour exactly. kill is the
// action to invoke on an affirmative completion.
func killOverlay(theme *huh.Theme, width int, target string, kill func(name string) error) tea.Model {
	var confirmed bool
	field := huh.NewConfirm().
		Title(fmt.Sprintf("kill %s? (esc cancel) ", target)).
		Inline(true).
		Value(&confirmed)

	return &formOverlay{
		form: compactForm(theme, width, huh.NewGroup(field)),
		apply: func() tea.Cmd {
			if !confirmed {
				return nil // "no": no-op, same as esc
			}
			return doAndReload(func() error { return kill(target) })
		},
	}
}

// newHuhTheme builds a *huh.Theme from the app's own Palette (see
// style.go) so the overlay matches the rest of sessui instead of huh's
// baked-in Charm colours. Starts from huh.ThemeBase() and strips the
// focused field's left border + padding -- ThemeBase's default marks the
// focused field with a bordered accent bar, which is exactly the "title
// padding" the footer band can't afford (see compactForm).
func newHuhTheme(p Palette) *huh.Theme {
	t := huh.ThemeBase()

	t.Focused.Base = lipgloss.NewStyle()
	t.Focused.Title = t.Focused.Title.Foreground(p.Accent).Bold(true)
	t.Focused.ErrorIndicator = t.Focused.ErrorIndicator.Foreground(p.Red)
	t.Focused.ErrorMessage = t.Focused.ErrorMessage.Foreground(p.Red)
	t.Focused.TextInput.Text = t.Focused.TextInput.Text.Foreground(p.Foreground)
	t.Focused.TextInput.Cursor = t.Focused.TextInput.Cursor.Foreground(p.Accent)
	t.Focused.TextInput.Placeholder = t.Focused.TextInput.Placeholder.Foreground(p.Foreground).Faint(true)
	t.Focused.FocusedButton = t.Focused.FocusedButton.Foreground(p.Background).Background(p.Red).Bold(true)
	t.Focused.BlurredButton = t.Focused.BlurredButton.Foreground(p.Foreground).Background(p.Background)

	// The one field in these forms is always focused (single-field groups,
	// nothing to tab away to), so Blurred never actually renders -- kept
	// equal to Focused (minus the border, already stripped) only so a
	// theme built here never surprises a future multi-field overlay.
	t.Blurred = t.Focused

	return t
}
