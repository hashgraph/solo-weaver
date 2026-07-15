// SPDX-License-Identifier: Apache-2.0

package prompt

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
)

// fieldGap is the blank-line separator a spacedField prepends before its own
// view. It mirrors huh's default FieldSeparator ("\n\n" — one blank line between
// fields), which is why the enclosing group must run with the separator zeroed
// (soloThemeNoFieldSeparator); otherwise the gaps would double up.
const fieldGap = "\n\n"

// spacedField wraps a huh.Field so that a single group can contain fields that
// individually appear or disappear at runtime with even spacing. huh v1.0.0 can
// hide only whole groups (huh.Group.WithHideFunc), not individual fields — a
// huh.Group renders every field's View() and inserts a FieldSeparator between
// each adjacent pair, and the built-in huh.Input never reports Skip()==true. To
// keep several conditionally-shown inputs on one wizard page (instead of one
// page per input) without a doubled gap around each hidden field, spacedField:
//
//   - Skip() reports true while hidden(), so group navigation (Tab/Shift+Tab)
//     steps over the field. huh recomputes field positions on every keypress
//     (Form.UpdateFieldPositions), so a hidden() that flips mid-wizard — e.g. an
//     optional storage path that becomes (in)applicable after the operator edits
//     the chart version earlier in the same form — is honored live.
//   - View() renders empty (no content, no separator) while hidden(), so a hidden
//     field occupies zero lines. When shown, it prepends fieldGap iff leadingGap()
//     reports another field is already visible before it. Because the group runs
//     with huh's own separator zeroed, this makes every visible field own exactly
//     one leading gap, so spacing stays even no matter which fields are hidden.
//
// Validation is expected to be suppressed independently while hidden (the field
// builder gates its validator on the same predicate), so a hidden field never
// contributes a blocking Error() to the group.
type spacedField struct {
	huh.Field
	hidden     func() bool
	leadingGap func() bool
}

// newSpacedField wraps field so it is skipped and rendered empty whenever hidden
// returns true, and prepends a leading gap whenever leadingGap returns true (and
// the field is shown).
func newSpacedField(field huh.Field, hidden, leadingGap func() bool) *spacedField {
	return &spacedField{Field: field, hidden: hidden, leadingGap: leadingGap}
}

// Skip reports whether huh should step over this field during navigation. A
// hidden field is always skipped; otherwise the wrapped field's own Skip()
// decision is honored.
func (h *spacedField) Skip() bool {
	if h.hidden() {
		return true
	}
	return h.Field.Skip()
}

// View renders nothing while the field is hidden so it takes up no page space.
// When shown it prepends the leading gap (if another field precedes it) and then
// delegates to the wrapped field.
func (h *spacedField) View() string {
	if h.hidden() {
		return ""
	}
	if h.leadingGap() {
		return fieldGap + h.Field.View()
	}
	return h.Field.View()
}

// Update delegates to the wrapped field but re-wraps the returned model so huh
// keeps addressing the wrapper (and thus our Skip()/View() overrides) after it
// stores the updated field back into the group.
func (h *spacedField) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m, cmd := h.Field.Update(msg)
	h.Field = m.(huh.Field)
	return h, cmd
}
