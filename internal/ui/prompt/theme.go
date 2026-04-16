// SPDX-License-Identifier: Apache-2.0

package prompt

import (
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

// SoloTheme returns a huh.Theme styled to match the solo-provisioner TUI.
//
// The color palette mirrors the existing view.go/style constants:
//
//	"6" = cyan (spinners, accents)
//	"2" = green (success, cursor)
//	"1" = red (errors)
//	"8" = grey (descriptions, durations)
//	"3" = yellow (warnings, selectors)
func SoloTheme() *huh.Theme {
	t := huh.ThemeBase()

	cyan := lipgloss.AdaptiveColor{Light: "#0891B2", Dark: "#22D3EE"}  // matches TUI color "6"
	green := lipgloss.AdaptiveColor{Light: "#16A34A", Dark: "#22C55E"} // matches TUI color "2"
	red := lipgloss.AdaptiveColor{Light: "#DC2626", Dark: "#EF4444"}   // matches TUI color "1"
	grey := lipgloss.AdaptiveColor{Light: "#6B7280", Dark: "#9CA3AF"}  // matches TUI color "8"
	normalFg := lipgloss.AdaptiveColor{Light: "235", Dark: "252"}

	// ── Focused ──────────────────────────────────────────────────────────
	t.Focused.Base = t.Focused.Base.BorderForeground(cyan)
	t.Focused.Card = t.Focused.Base
	t.Focused.Title = t.Focused.Title.Foreground(cyan).Bold(true)
	t.Focused.NoteTitle = t.Focused.NoteTitle.Foreground(cyan).Bold(true).MarginBottom(1)
	t.Focused.Description = t.Focused.Description.Foreground(grey)
	t.Focused.ErrorIndicator = t.Focused.ErrorIndicator.Foreground(red)
	t.Focused.ErrorMessage = t.Focused.ErrorMessage.Foreground(red)

	// Select styles
	t.Focused.SelectSelector = t.Focused.SelectSelector.Foreground(cyan)
	t.Focused.NextIndicator = t.Focused.NextIndicator.Foreground(cyan)
	t.Focused.PrevIndicator = t.Focused.PrevIndicator.Foreground(cyan)
	t.Focused.Option = t.Focused.Option.Foreground(normalFg)

	// Multi-select styles
	t.Focused.MultiSelectSelector = t.Focused.MultiSelectSelector.Foreground(cyan)
	t.Focused.SelectedOption = t.Focused.SelectedOption.Foreground(green)
	t.Focused.SelectedPrefix = lipgloss.NewStyle().Foreground(cyan).SetString("› ")
	t.Focused.UnselectedOption = t.Focused.UnselectedOption.Foreground(normalFg)
	t.Focused.UnselectedPrefix = lipgloss.NewStyle().Foreground(grey).SetString("• ")

	// Button styles
	t.Focused.FocusedButton = t.Focused.FocusedButton.Foreground(lipgloss.Color("0")).Background(cyan)
	t.Focused.Next = t.Focused.FocusedButton
	t.Focused.BlurredButton = t.Focused.BlurredButton.Foreground(normalFg).Background(lipgloss.AdaptiveColor{Light: "252", Dark: "237"})

	// Text input styles
	t.Focused.TextInput.Cursor = t.Focused.TextInput.Cursor.Foreground(green)
	t.Focused.TextInput.Placeholder = t.Focused.TextInput.Placeholder.Foreground(grey)
	t.Focused.TextInput.Prompt = t.Focused.TextInput.Prompt.Foreground(cyan)

	// Directory / file picker
	t.Focused.Directory = t.Focused.Directory.Foreground(cyan)

	// ── Blurred ──────────────────────────────────────────────────────────
	t.Blurred = t.Focused
	t.Blurred.Base = t.Focused.Base.BorderStyle(lipgloss.HiddenBorder())
	t.Blurred.Card = t.Blurred.Base
	t.Blurred.NextIndicator = lipgloss.NewStyle()
	t.Blurred.PrevIndicator = lipgloss.NewStyle()

	// ── Group ────────────────────────────────────────────────────────────
	t.Group.Title = t.Focused.Title
	t.Group.Description = t.Focused.Description

	return t
}
