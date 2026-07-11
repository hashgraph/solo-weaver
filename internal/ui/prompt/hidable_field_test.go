// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package prompt

import (
	"testing"

	"github.com/charmbracelet/huh"
)

// TestHidableField_TogglesSkipAndView verifies the wrapper reflects its live
// hidden predicate: when hidden it is skipped and renders empty; when shown it
// delegates Skip()/View() to the wrapped field. The predicate is re-read on
// every call so a mid-wizard flip (e.g. a chart-version edit) is honored.
func TestHidableField_TogglesSkipAndView(t *testing.T) {
	hidden := true
	var value string
	inner := huh.NewInput().Key("k").Title("Title").Value(&value)
	f := newHidableField(inner, func() bool { return hidden })

	if !f.Skip() {
		t.Fatalf("expected Skip()==true while hidden")
	}
	if f.View() != "" {
		t.Fatalf("expected empty View() while hidden, got %q", f.View())
	}

	hidden = false
	if f.Skip() {
		t.Fatalf("expected Skip()==false once shown (Input never self-skips)")
	}
	if f.View() == "" {
		t.Fatalf("expected non-empty View() once shown")
	}
}
