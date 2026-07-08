// SPDX-License-Identifier: Apache-2.0

// Package prompt provides interactive CLI prompts for solo-provisioner commands.
//
// When a command is invoked without required flags, the prompt layer presents
// user-friendly forms (powered by charmbracelet/huh) to collect the missing
// values before the workflow starts. Prompts are skipped when:
//   - --non-interactive is set (CI/pipelines)
//   - --force / -y is set (auto-accept defaults)
//   - stdout is not a TTY (pipes, redirected output)
//   - the flag was already provided on the command line
//
// Each prompt shows the effective value (from config.yaml, env, or defaults)
// as the pre-selected/suggested value. The user can accept it with Enter or
// override it by typing/selecting a different value.
package prompt

import (
	"errors"
	"fmt"
	"os"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"

	"github.com/hashgraph/solo-weaver/internal/ui"
)

// ErrAborted is returned when the user cancels out of an interactive prompt.
// It is an errorx-typed sentinel so callers can `errors.Is(err, ErrAborted)`
// and the doctor layer can branch on the RejectedOperation namespace.
var ErrAborted = errorx.RejectedOperation.New("aborted by user")

// chosenPair holds a title/value pair for summary printing.
type chosenPair struct {
	title string
	value string
}

// ChosenValues collects prompted values across multiple Run*Prompts calls
// so they can be printed as a single summary block with a header.
type ChosenValues struct {
	pairs []chosenPair
}

// NewChosenValues creates a new collector for prompted values.
func NewChosenValues() *ChosenValues {
	return &ChosenValues{}
}

func (cv *ChosenValues) add(title, value string) {
	cv.pairs = append(cv.pairs, chosenPair{title: title, value: value})
}

// Print prints a styled header followed by indented chosen values.
// It is a no-op when no values were collected.
func (cv *ChosenValues) Print(header string) {
	if len(cv.pairs) == 0 {
		return
	}
	cyan := lipgloss.AdaptiveColor{Light: "#0891B2", Dark: "#22D3EE"}
	h := lipgloss.NewStyle().Foreground(cyan).Bold(true).Render(header)
	fmt.Fprintf(os.Stderr, "%s\n", h)
	for _, p := range cv.pairs {
		printChosenValue(p.title, p.value)
	}
}

// printChosenValue prints a styled summary line for a prompted value:
//
//	› Title: value
func printChosenValue(title, value string) {
	cyan := lipgloss.AdaptiveColor{Light: "#0891B2", Dark: "#22D3EE"}
	grey := lipgloss.AdaptiveColor{Light: "#6B7280", Dark: "#9CA3AF"}

	indicator := lipgloss.NewStyle().Foreground(cyan).SetString("›")
	label := lipgloss.NewStyle().Foreground(grey).Render(title + ":")
	val := lipgloss.NewStyle().Bold(true).Render(value)

	fmt.Fprintf(os.Stderr, "  %s %s %s\n", indicator, label, val)
}

// wrapFormError converts huh errors into prompt-layer errors.
func wrapFormError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, huh.ErrUserAborted) {
		return ErrAborted
	}
	return errorx.ExternalError.Wrap(err, "prompt error")
}

// SelectPrompt defines an interactive select prompt for a flag with a fixed set of choices.
type SelectPrompt struct {
	// FlagName is the Cobra flag name (e.g. "profile"). Used to detect whether
	// the user already supplied this flag on the command line.
	FlagName string

	// Title is displayed as the prompt header (e.g. "Deployment Profile").
	Title string

	// Description is displayed below the title (e.g. "Select the target network").
	Description string

	// Options are the allowed values the user can choose from.
	Options []string

	// EffectiveValue is the suggested default — pre-selected in the list.
	// It is resolved from the RSL / config before the prompt runs.
	EffectiveValue string

	// Target is a pointer to the flag variable that will receive the selected value.
	Target *string
}

// InputPrompt defines an interactive text-input prompt for a flag with free-form text.
type InputPrompt struct {
	// FlagName is the Cobra flag name. Used to detect if already supplied.
	FlagName string

	// Title is displayed as the prompt header.
	Title string

	// Description is displayed below the title.
	Description string

	// Placeholder is the greyed-out hint shown when the input is empty.
	Placeholder string

	// EffectiveValue is the suggested default — pre-filled in the input.
	EffectiveValue string

	// Target is a pointer to the flag variable that will receive the entered value.
	Target *string

	// Validate is an optional validation function applied on submit.
	Validate func(string) error
}

// ShouldPrompt returns true when interactive prompts should be presented.
// It returns false when the TUI is suppressed (non-interactive, not a TTY).
func ShouldPrompt(force bool) bool {
	if ui.IsUnformatted() {
		return false
	}
	if force {
		return false
	}
	return true
}

// flagWasSet returns true if the named flag was explicitly provided on the
// command line (as opposed to retaining its default/zero value).
func flagWasSet(cmd *cobra.Command, name string) bool {
	f := cmd.Flag(name)
	if f == nil {
		// Also check parent persistent flags.
		f = cmd.InheritedFlags().Lookup(name)
	}
	if f == nil {
		return false
	}
	return f.Changed
}

// Wizard accumulates huh groups (one per wizard page) plus post-run callbacks,
// then runs them all in a single huh.Form. Presenting every stage as one form is
// what makes the pages navigable back and forward (Shift+Tab / Tab) — huh's
// built-in navigation only crosses group boundaries within a single form.
//
// Each stage's post-processing (recording chosen values into ChosenValues,
// normalising flag targets, joining multi-selects) is deferred into an afterRun
// callback because a field's value is only final once the shared form completes.
type Wizard struct {
	groups   []*huh.Group
	afterRun []func()
}

// NewWizard returns an empty wizard ready to accumulate pages.
func NewWizard() *Wizard {
	return &Wizard{}
}

// addGroups appends one or more groups (pages) and an optional post-run callback.
// The callback is fired, in add order, after the wizard's single form completes.
func (w *Wizard) addGroups(after func(), groups ...*huh.Group) {
	w.groups = append(w.groups, groups...)
	if after != nil {
		w.afterRun = append(w.afterRun, after)
	}
}

// Run executes all accumulated groups in a single huh form, then fires the
// afterRun callbacks (which is where all ChosenValues interaction happens). It is
// a no-op (returns nil) when no pages were added — the same behaviour the
// per-stage runners had when every prompt was skipped.
func (w *Wizard) Run() error {
	if len(w.groups) == 0 {
		return nil
	}

	form := huh.NewForm(w.groups...).
		WithTheme(SoloTheme()).
		WithShowHelp(true)

	if err := form.Run(); err != nil {
		return wrapFormError(err)
	}

	for _, fn := range w.afterRun {
		fn()
	}

	return nil
}

// AddSelectPrompts appends a wizard page (one group) containing a select field for
// each prompt whose flag was not already supplied on the command line. Nothing is
// added when every prompt is skipped. Each prompt's Target pointer is pre-seeded
// with its effective value so pressing Enter accepts it; chosen values are recorded
// into cv by an afterRun callback once the wizard form completes.
func AddSelectPrompts(w *Wizard, cmd *cobra.Command, prompts []SelectPrompt, cv *ChosenValues) {
	var fields []huh.Field
	var prompted []*SelectPrompt

	for i := range prompts {
		p := &prompts[i]

		// Skip if the user already supplied this flag on the CLI.
		if flagWasSet(cmd, p.FlagName) {
			continue
		}

		// Pre-select the effective value so pressing Enter accepts it.
		if p.EffectiveValue != "" {
			*p.Target = p.EffectiveValue
		}

		options := make([]huh.Option[string], len(p.Options))
		for j, opt := range p.Options {
			options[j] = huh.NewOption(opt, opt)
		}

		field := huh.NewSelect[string]().
			Key(p.FlagName).
			Title(p.Title).
			Description(p.Description).
			Options(options...).
			Value(p.Target)

		fields = append(fields, field)
		prompted = append(prompted, p)
	}

	if len(fields) == 0 {
		return
	}

	w.addGroups(func() {
		for _, p := range prompted {
			cv.add(p.Title, *p.Target)
		}
	}, huh.NewGroup(fields...))
}

// RunSelectPrompts presents interactive select prompts for any flags that were
// not explicitly set on the command line. Returns nil if all flags are already
// set or prompts are not applicable.
//
// It runs the selects as a single-page wizard; multi-stage callers should use
// AddSelectPrompts to combine this page with others into one navigable form.
func RunSelectPrompts(cmd *cobra.Command, prompts []SelectPrompt, cv *ChosenValues) error {
	w := NewWizard()
	AddSelectPrompts(w, cmd, prompts, cv)
	return w.Run()
}

// AddInputPrompts appends a wizard page (one group) containing a text-input field
// for each prompt whose flag was not already supplied on the command line and is
// registered on the command. Nothing is added when every prompt is skipped. Each
// prompt's Target pointer is pre-filled with its effective value; chosen values are
// recorded into cv by an afterRun callback once the wizard form completes.
func AddInputPrompts(w *Wizard, cmd *cobra.Command, prompts []InputPrompt, cv *ChosenValues) {
	var fields []huh.Field
	var prompted []*InputPrompt

	for i := range prompts {
		p := &prompts[i]

		if flagWasSet(cmd, p.FlagName) {
			continue
		}

		// Skip prompts for flags not registered on this command (neither local nor inherited).
		if cmd.Flag(p.FlagName) == nil && cmd.InheritedFlags().Lookup(p.FlagName) == nil {
			continue
		}

		// Pre-fill the effective value.
		if p.EffectiveValue != "" {
			*p.Target = p.EffectiveValue
		}

		field := huh.NewInput().
			Key(p.FlagName).
			Title(p.Title).
			Description(p.Description).
			Placeholder(p.Placeholder).
			Value(p.Target)

		if p.Validate != nil {
			field = field.Validate(p.Validate)
		}

		fields = append(fields, field)
		prompted = append(prompted, p)
	}

	if len(fields) == 0 {
		return
	}

	w.addGroups(func() {
		for _, p := range prompted {
			cv.add(p.Title, *p.Target)
		}
	}, huh.NewGroup(fields...))
}

// RunInputPrompts presents interactive text-input prompts for any flags that
// were not explicitly set on the command line.
//
// It runs the inputs as a single-page wizard; multi-stage callers should use
// AddInputPrompts to combine this page with others into one navigable form.
func RunInputPrompts(cmd *cobra.Command, prompts []InputPrompt, cv *ChosenValues) error {
	w := NewWizard()
	AddInputPrompts(w, cmd, prompts, cv)
	return w.Run()
}

// RunConfirm presents a styled confirmation prompt and returns the user's choice.
// The defaultValue determines which option is pre-selected.
func RunConfirm(title string, description string, defaultValue bool) (bool, error) {
	var result = defaultValue

	field := huh.NewConfirm().
		Title(title).
		Description(description).
		Affirmative("Yes").
		Negative("No").
		Value(&result)

	form := huh.NewForm(huh.NewGroup(field)).
		WithTheme(SoloTheme()).
		WithShowHelp(false)

	if err := form.Run(); err != nil {
		return false, wrapFormError(err)
	}

	return result, nil
}
