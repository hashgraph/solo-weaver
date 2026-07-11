// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package prompt

import (
	"strings"
	"testing"

	"github.com/charmbracelet/huh"
)

// TestSpacedField_TogglesSkipAndView verifies the wrapper reflects its live
// hidden predicate: when hidden it is skipped and renders empty; when shown it
// delegates Skip()/View() to the wrapped field. The predicate is re-read on
// every call so a mid-wizard flip (e.g. a chart-version edit) is honored.
func TestSpacedField_TogglesSkipAndView(t *testing.T) {
	hidden := true
	var value string
	inner := huh.NewInput().Key("k").Title("Title").Value(&value)
	f := newSpacedField(inner, func() bool { return hidden }, func() bool { return false })

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

// TestSpacedField_LeadingGap verifies a shown field prepends the blank-line gap
// only when leadingGap reports another field precedes it, and never while hidden.
func TestSpacedField_LeadingGap(t *testing.T) {
	var value string
	inner := huh.NewInput().Key("k").Title("Title").Value(&value)

	// Shown with a leading gap → view starts with fieldGap.
	withGap := newSpacedField(inner, func() bool { return false }, func() bool { return true })
	if !strings.HasPrefix(withGap.View(), fieldGap) {
		t.Fatalf("expected view to start with fieldGap when leadingGap is true, got %q", withGap.View())
	}

	// Shown without a leading gap (first visible field) → no leading blank lines.
	noGap := newSpacedField(inner, func() bool { return false }, func() bool { return false })
	if strings.HasPrefix(noGap.View(), fieldGap) {
		t.Fatalf("expected no leading gap when leadingGap is false, got %q", noGap.View())
	}

	// Hidden → empty regardless of leadingGap.
	hiddenWithGap := newSpacedField(inner, func() bool { return true }, func() bool { return true })
	if hiddenWithGap.View() != "" {
		t.Fatalf("expected empty view while hidden even with leadingGap, got %q", hiddenWithGap.View())
	}
}
