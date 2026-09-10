package ui

import (
	"errors"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

// The overlay slot is a single tea.Model that takes the keyboard for anything
// needing more than one key press: rename, kill confirmation, the settings
// editor. huh drives the two forms here, and formOverlay is the one place
// huh's completion state is translated into the contract Model consumes, so
// Model never imports huh.

// errNameRequired is the validation error for an empty name. Errors are not
// shown (see compactForm), so the effect is that enter does not submit.
var errNameRequired = errors.New("name required")

// overlayResult is the one contract every overlay reports completion through,
// so Model needs one type assertion after Update and never a switch on which
// kind of overlay is showing.
type overlayResult interface {
	// result reports whether the overlay is finished and, if so, the command
	// to run: the real action when the user completed it, nil when they
	// aborted. Either way the caller drops the overlay.
	result() (finished bool, apply tea.Cmd)
}

// formOverlay adapts a *huh.Form to the overlay contract.
type formOverlay struct {
	form *huh.Form
	// apply builds the side-effecting command from what the fields collected;
	// called once, only on completion.
	apply func() tea.Cmd
}

func (o *formOverlay) Init() tea.Cmd { return o.form.Init() }

func (o *formOverlay) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// huh binds only ctrl+c to quit, and Model intercepts that for the whole
	// program first; esc has no binding in huh's default keymap, so abort here.
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

// compactForm builds a single-group form pinned to one line: no help row, no
// error row, theme and width fixed up front so huh's own window-size resize
// never fires. Model owns window sizing and must not have huh fight it for
// the footer's height.
func compactForm(theme *huh.Theme, width int, group *huh.Group) *huh.Form {
	return huh.NewForm(group).
		WithTheme(theme).
		WithShowHelp(false).
		WithShowErrors(false).
		WithWidth(width).
		WithHeight(1)
}

// renameOverlay is a one-field form prefilled with the current name. rename
// is injected so completing the form never has to shell out to be tested.
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
				return nil // unchanged name, nothing to do
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

// killOverlay is a one-field confirm defaulting to "no". kill runs only on an
// affirmative completion.
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
				return nil // "no": same as esc
			}
			return doAndReload(func() error { return kill(target) })
		},
	}
}

// newHuhTheme builds a *huh.Theme from the palette. It starts from ThemeBase
// and strips the focused field's left border and padding: that accent bar is
// exactly the height the footer band cannot afford.
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

	// Single-field groups never blur, so Blurred never renders; kept equal to
	// Focused so a future multi-field overlay is not surprised.
	t.Blurred = t.Focused

	return t
}
