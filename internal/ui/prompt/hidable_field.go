// SPDX-License-Identifier: Apache-2.0

package prompt

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
)

// hidableField wraps a huh.Field so that a single group can contain fields that
// individually appear or disappear at runtime. huh v1.0.0 can hide only whole
// groups (huh.Group.WithHideFunc), not individual fields — huh.Group renders
// every field's View() and the built-in huh.Input never reports Skip()==true.
// To keep several conditionally-shown inputs on one wizard page (instead of one
// page per input), we intercept Skip() and View():
//
//   - Skip() reports true while hidden(), so group navigation (Tab/Shift+Tab)
//     steps over the field. huh recomputes field positions on every keypress
//     (Form.UpdateFieldPositions), so a hidden() that flips mid-wizard — e.g. an
//     optional storage path that becomes (in)applicable after the operator edits
//     the chart version earlier in the same form — is honored live.
//   - View() renders empty while hidden(), so the field occupies no visible
//     lines on the page.
//
// Validation is expected to be suppressed independently while hidden (the field
// builder gates its validator on the same predicate), so a hidden field never
// contributes a blocking Error() to the group.
type hidableField struct {
	huh.Field
	hidden func() bool
}

// newHidableField wraps field so it is skipped and rendered empty whenever
// hidden returns true.
func newHidableField(field huh.Field, hidden func() bool) *hidableField {
	return &hidableField{Field: field, hidden: hidden}
}

// Skip reports whether huh should step over this field during navigation. A
// hidden field is always skipped; otherwise the wrapped field's own Skip()
// decision is honored.
func (h *hidableField) Skip() bool {
	if h.hidden() {
		return true
	}
	return h.Field.Skip()
}

// View renders nothing while the field is hidden so it takes up no page space;
// otherwise it delegates to the wrapped field.
func (h *hidableField) View() string {
	if h.hidden() {
		return ""
	}
	return h.Field.View()
}

// Update delegates to the wrapped field but re-wraps the returned model so huh
// keeps addressing the wrapper (and thus our Skip()/View() overrides) after it
// stores the updated field back into the group.
func (h *hidableField) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m, cmd := h.Field.Update(msg)
	h.Field = m.(huh.Field)
	return h, cmd
}
