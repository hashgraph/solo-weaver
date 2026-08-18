// SPDX-License-Identifier: Apache-2.0

package doctor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"testing"

	"github.com/automa-saga/errx"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/require"
)

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// capturePanel runs fn with stdout and stderr redirected and returns the output
// with ANSI colour codes stripped, so assertions can match the plain text.
func capturePanel(t *testing.T, fn func()) string {
	t.Helper()

	r, w, err := os.Pipe()
	require.NoError(t, err)

	// Drain concurrently: output past the pipe buffer would block fn() forever.
	var buf bytes.Buffer
	done := make(chan error, 1)
	go func() {
		_, copyErr := io.Copy(&buf, r)
		done <- copyErr
	}()

	origOut, origErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = w, w
	fn()
	os.Stdout, os.Stderr = origOut, origErr
	require.NoError(t, w.Close())

	require.NoError(t, <-done)
	return ansiRE.ReplaceAllString(buf.String(), "")
}

// TestToErrorCode_NamespaceChoice is the guardrail behind the "declare a typed
// error as a subtype of the built-in" rule in docs/dev/error-handling.md. A fresh
// namespace severs IsOfType against the built-in, which silently reclassifies the
// operator-visible error code. If this test has to change, the codes changed —
// make that deliberate rather than a side effect of a package migration.
func TestToErrorCode_NamespaceChoice(t *testing.T) {
	subtype := errorx.IllegalArgument.NewSubtype("namespace_choice_subtype")
	freshNS := errorx.NewNamespace("namespace_choice_fresh")
	freshType := freshNS.NewType("rejected")
	traitNS := errorx.NewNamespace("namespace_choice_trait", errorx.NotFound())

	t.Run("built-in namespace", func(t *testing.T) {
		require.Equal(t, 10400, toErrorCode(errorx.IllegalArgument.New("bad flag")))
	})

	t.Run("subtype of a built-in keeps the code", func(t *testing.T) {
		err := subtype.New("bad flag")
		require.True(t, errorx.IsOfType(err, errorx.IllegalArgument),
			"a subtype must still match its parent")
		require.True(t, errorx.IsOfType(err, subtype),
			"and must be matchable as itself — the whole point of declaring it")
		require.Equal(t, 10400, toErrorCode(err))
	})

	t.Run("fresh namespace loses the code", func(t *testing.T) {
		err := freshType.New("bad flag")
		require.False(t, errorx.IsOfType(err, errorx.IllegalArgument),
			"precondition: a fresh namespace does not inherit a built-in")
		require.Equal(t, 10500, toErrorCode(err),
			"10400 -> 10500 is the regression the subtype rule exists to prevent")
	})

	t.Run("traits survive a fresh namespace", func(t *testing.T) {
		// Only parent-type identity is lost, so a NotFound namespace still maps.
		require.Equal(t, 10404, toErrorCode(traitNS.NewType("missing").New("gone")))
	})

	t.Run("the namespace fallback degrades too", func(t *testing.T) {
		require.Equal(t,
			[]string{"Ensure all required arguments are provided with valid values."},
			findResolution(subtype.New("bad flag")))
		require.Equal(t,
			[]string{"Check error message for details or contact support"},
			findResolution(freshType.New("bad flag")))
	})
}

// TestPropertyIdentity pins the aliasing: errorx compares properties by pointer, so
// re-registering either label would hide every legacy hint and reason from errx.
// Asserts the behaviour rather than the alias — require.Equal would accept a copy.
func TestPropertyIdentity(t *testing.T) {
	err := errorx.IllegalState.New("boom").
		WithProperty(models.ErrPropertyResolution, []string{"legacy step"}).
		WithProperty(models.ErrPropertyReason, reasons.FileMissing.String())

	hints, ok := errx.Hints(err)
	require.True(t, ok, "legacy hints must be visible to errx.Hints")
	require.Equal(t, []string{"legacy step"}, hints)

	reason, ok := errx.ReasonOf(err)
	require.True(t, ok, "legacy reason must be visible to errx.ReasonOf")
	require.Equal(t, reasons.FileMissing, reason)
}

// TestOperatorMessage pins the reason-surface split: log lines keep "{reason: X}"
// inline, human surfaces drop it. Nothing else about the message may change.
func TestOperatorMessage(t *testing.T) {
	otherProp := errorx.RegisterPrintableProperty("operator_message_test_prop")

	t.Run("drops the reason group", func(t *testing.T) {
		err := errx.Decorate(errorx.IllegalArgument.New("bad flag value"), reasons.InvalidArgument)
		require.Contains(t, err.Error(), "{reason: InvalidArgument}", "precondition: errx renders it")
		require.Equal(t, "common.illegal_argument: bad flag value", OperatorMessage(err))
	})

	t.Run("keeps other printable properties", func(t *testing.T) {
		err := errx.WithReason(
			errorx.IllegalArgument.New("bad flag value").WithProperty(otherProp, "kept"),
			reasons.InvalidArgument)
		got := OperatorMessage(err)
		require.Contains(t, got, "{operator_message_test_prop: kept}")
		require.NotContains(t, got, "reason")
	})

	t.Run("keeps the underlying error and the cause chain", func(t *testing.T) {
		underlying := errorx.ExternalError.New("permission denied")
		inner := errorx.IllegalState.New("cannot read /opt/solo").WithUnderlyingErrors(underlying)
		err := errx.WithReason(errorx.InternalError.Wrap(inner, "preflight"), reasons.Internal)

		got := OperatorMessage(err)
		require.NotContains(t, got, "reason")
		require.Contains(t, got, "preflight")
		require.Contains(t, got, "cannot read /opt/solo")
		require.Contains(t, got, "hidden: ")
		require.Contains(t, got, "permission denied")
	})

	t.Run("drops a reason nested in the cause chain", func(t *testing.T) {
		// err.Error() renders every cause, so stripping only the outermost reason
		// (what errx.ReasonOf reports) leaks the inner one onto the panel.
		inner := errx.Decorate(errorx.IllegalArgument.New("inner"), reasons.InvalidArgument)
		err := errx.Decorate(errorx.InternalError.Wrap(inner, "outer"), reasons.Internal)

		require.Equal(t,
			"common.internal_error: outer, cause: common.illegal_argument: inner",
			OperatorMessage(err))
	})

	t.Run("keeps other printable properties on a nested error", func(t *testing.T) {
		inner := errx.WithReason(
			errorx.IllegalArgument.New("inner").WithProperty(otherProp, "kept"),
			reasons.InvalidArgument)
		err := errx.Decorate(errorx.InternalError.Wrap(inner, "outer"), reasons.Internal)

		got := OperatorMessage(err)
		require.Contains(t, got, "{operator_message_test_prop: kept}",
			"only the reason pair leaves the nested group")
		require.NotContains(t, got, "reason")
	})

	t.Run("undecorated error is unchanged", func(t *testing.T) {
		err := errorx.IllegalState.New("boom")
		require.Equal(t, err.Error(), OperatorMessage(err))
	})

	t.Run("nil and non-errorx errors", func(t *testing.T) {
		require.Equal(t, "", OperatorMessage(nil))
		require.Equal(t, "EOF", OperatorMessage(io.EOF), "sanity: a plain error renders verbatim")
	})
}

// TestToErrorMessage_CauseDropsReason covers the panel's Cause line, which renders
// the cause in full and so would otherwise repeat the reason.
func TestToErrorMessage_CauseDropsReason(t *testing.T) {
	t.Run("single reason", func(t *testing.T) {
		inner := errx.Decorate(errorx.IllegalArgument.New("bad flag value"), reasons.InvalidArgument)
		msg, cause := toErrorMessage(errorx.IllegalState.Wrap(inner, "preflight failed"))

		require.Equal(t, "preflight failed", msg)
		require.Equal(t, "common.illegal_argument: bad flag value", cause)
	})

	t.Run("a reason at every level of the cause chain", func(t *testing.T) {
		inner := errx.Decorate(errorx.IllegalArgument.New("bad flag value"), reasons.InvalidArgument)
		mid := errx.Decorate(errorx.ExternalError.Wrap(inner, "probe"), reasons.Internal)
		msg, cause := toErrorMessage(errorx.IllegalState.Wrap(mid, "preflight failed"))

		require.Equal(t, "preflight failed", msg)
		require.Equal(t,
			"common.external_error: probe, cause: common.illegal_argument: bad flag value",
			cause)
	})
}

// TestExtractPropertyMatchesErrx pins the traversal contract: the doctor's reader
// must see exactly what errx sees, including through non-errorx wrappers. Parity
// covers blind spots too — in the join cases both sides agree the hints are NOT
// found (errors.Unwrap does not follow Unwrap() []error); what is lost and why
// is pinned separately by TestJoinHidesDecoration.
func TestExtractPropertyMatchesErrx(t *testing.T) {
	chains := map[string]error{
		"direct":           errx.WithHints(errorx.IllegalState.New("boom"), "fix it"),
		"errorx wrap":      errorx.IllegalState.Wrap(errx.WithHints(errorx.ExternalError.New("refused"), "fix it"), "probe"),
		"plain %w wrapper": fmt.Errorf("probe: %w", errx.WithHints(errorx.ExternalError.New("refused"), "fix it")),
		"errors.Join":      errors.Join(errx.WithHints(errorx.ExternalError.New("refused"), "fix it")),
		"wrap over a join": errorx.IllegalState.Wrap(errors.Join(errx.WithHints(errorx.ExternalError.New("refused"), "fix it")), "probe"),
		"nothing attached": errorx.IllegalState.New("boom"),
	}

	for name, err := range chains {
		t.Run(name, func(t *testing.T) {
			want, wantOK := errx.Hints(err)
			got, gotOK := resolutionHints(err)

			require.Equal(t, wantOK, gotOK, "doctor and errx disagree on whether hints are present")
			require.Equal(t, want, got)
		})
	}
}

// TestJoinHidesDecoration pins the errx v1.0.0 blind spot documented in
// docs/dev/error-handling.md: errors.Join exposes its children via
// Unwrap() []error, which neither errx nor the doctor's traversal follows, so
// decoration on a join operand is silently lost. Hence the rule: decorate on
// top of the join, never an operand. If the "lost" subtest starts failing, errx
// has learned to traverse joins — flip its assertions and drop the join warning
// from the doc.
func TestJoinHidesDecoration(t *testing.T) {
	sentinel := errorx.RejectedOperation.New("aborted by user")

	t.Run("a decorated join operand is lost", func(t *testing.T) {
		err := errors.Join(sentinel,
			errx.Decorate(errorx.ExternalError.New("boom"), reasons.FileMissing, "fix it"))

		require.True(t, errors.Is(err, sentinel), "identity matching is what the join is for")

		_, ok := errx.ReasonOf(err)
		require.False(t, ok,
			"errx now traverses joins — flip this test and update docs/dev/error-handling.md")
		_, ok = errx.Hints(err)
		require.False(t, ok)
		_, ok = resolutionHints(err)
		require.False(t, ok, "the doctor must not disagree with errx, even about a blind spot")
	})

	t.Run("decorating on top of the join survives", func(t *testing.T) {
		err := errx.Decorate(
			errorx.Decorate(errors.Join(sentinel, errorx.ExternalError.New("boom")), "aborting install"),
			reasons.FileMissing, "fix it")

		require.True(t, errors.Is(err, sentinel),
			"errorx.Decorate wraps transparently, so errors.Is still reaches the sentinel")

		reason, ok := errx.ReasonOf(err)
		require.True(t, ok)
		require.Equal(t, reasons.FileMissing, reason)

		hints, ok := resolutionHints(err)
		require.True(t, ok)
		require.Equal(t, []string{"fix it"}, hints)
	})
}

func TestFindResolution(t *testing.T) {
	t.Run("hints attached via errx", func(t *testing.T) {
		err := errx.WithHints(errorx.IllegalState.New("boom"), "step one", "step two")
		require.Equal(t, []string{"step one", "step two"}, findResolution(err))
	})

	t.Run("hints attached via errx.Decorate", func(t *testing.T) {
		err := errx.Decorate(errorx.IllegalState.New("boom"), reasons.FileMissing, "step one")
		require.Equal(t, []string{"step one"}, findResolution(err))
	})

	t.Run("errx hints deep in the cause chain", func(t *testing.T) {
		inner := errx.WithHints(errorx.ExternalError.New("refused"), "check the firewall")
		outer := errorx.IllegalState.Wrap(errorx.InternalError.Wrap(inner, "probe"), "preflight")
		require.Equal(t, []string{"check the firewall"}, findResolution(outer))
	})

	t.Run("legacy []string hints", func(t *testing.T) {
		err := errorx.IllegalState.New("boom").
			WithProperty(models.ErrPropertyResolution, []string{"legacy step"})
		require.Equal(t, []string{"legacy step"}, findResolution(err))
	})

	t.Run("legacy bare-string hints", func(t *testing.T) {
		err := errorx.IllegalState.New("boom").
			WithProperty(models.ErrPropertyResolution, "use 'block node install'")
		require.Equal(t, []string{"use 'block node install'"}, findResolution(err))
	})

	t.Run("undecorated error keeps the namespace fallback", func(t *testing.T) {
		require.Equal(t,
			[]string{"Ensure provided data is in correct format."},
			findResolution(errorx.IllegalFormat.New("bad yaml")))
	})

	t.Run("empty bare-string hint falls back", func(t *testing.T) {
		err := errorx.IllegalFormat.New("bad yaml").
			WithProperty(models.ErrPropertyResolution, "")
		require.Equal(t,
			[]string{"Ensure provided data is in correct format."},
			findResolution(err))
	})

	t.Run("empty hint slice falls back", func(t *testing.T) {
		err := errorx.IllegalFormat.New("bad yaml").
			WithProperty(models.ErrPropertyResolution, []string{})
		require.Equal(t,
			[]string{"Ensure provided data is in correct format."},
			findResolution(err))
	})
}

func TestDiagnose_Reason(t *testing.T) {
	t.Run("surfaces the reason and hints of a decorated error", func(t *testing.T) {
		err := errx.Decorate(errorx.IllegalState.New("boom"), reasons.FileMissing, "do the thing")
		d := Diagnose(context.Background(), err)
		require.Equal(t, "FileMissing", d.Reason)
		require.Equal(t, []string{"do the thing"}, d.Resolution)
	})

	t.Run("leaves Reason empty for an undecorated error", func(t *testing.T) {
		// Empty, not "unknown": checkErrVerbose omits the Reason line entirely.
		d := Diagnose(context.Background(), errorx.IllegalState.New("boom"))
		require.Equal(t, "", d.Reason)
	})
}

// TestCheckErrVerbose_Reason pins the verbose panel's reason line, printed bare so
// it greps against the log's reason= field.
func TestCheckErrVerbose_Reason(t *testing.T) {
	t.Run("a decorated error renders the reason", func(t *testing.T) {
		d := Diagnose(context.Background(),
			errx.Decorate(errorx.IllegalState.New("boom"), reasons.FileMissing, "do the thing"))
		out := capturePanel(t, func() { checkErrVerbose(d) })
		require.Contains(t, out, "Reason: FileMissing")
	})

	t.Run("an undecorated error omits the line entirely", func(t *testing.T) {
		d := Diagnose(context.Background(), errorx.IllegalState.New("boom"))
		out := capturePanel(t, func() { checkErrVerbose(d) })
		require.NotContains(t, out, "Reason:", "an empty reason must not render an empty line")
	})
}

// TestPanelResolutionIsTheOnlyHintChannel pins the single-channel contract: both
// panels number the hints attached to the error, and there is no second channel
// that could render the same hints a different way.
func TestPanelResolutionIsTheOnlyHintChannel(t *testing.T) {
	decorated := func() *ErrorDiagnosis {
		return Diagnose(context.Background(), errx.Decorate(
			errorx.IllegalState.New("boom"), reasons.FileMissing, "first step", "second step"))
	}

	t.Run("compact panel numbers the hints", func(t *testing.T) {
		out := capturePanel(t, func() { checkErrCompact(decorated()) })
		require.Contains(t, out, "1. first step")
		require.Contains(t, out, "2. second step")
	})

	t.Run("verbose panel numbers them identically", func(t *testing.T) {
		out := capturePanel(t, func() { checkErrVerbose(decorated()) })
		require.Contains(t, out, "1. first step")
		require.Contains(t, out, "2. second step")
	})

	t.Run("legacy []string hints are numbered too", func(t *testing.T) {
		d := Diagnose(context.Background(), errorx.IllegalState.New("boom").
			WithProperty(models.ErrPropertyResolution, []string{"step one", "step two"}))
		out := capturePanel(t, func() { checkErrCompact(d) })
		require.Contains(t, out, "1. step one")
		require.Contains(t, out, "2. step two")
		require.NotContains(t, out, "[step one", "must not render the Go slice literal")
	})

	t.Run("an undecorated error still gets the namespace fallback", func(t *testing.T) {
		d := Diagnose(context.Background(), errorx.IllegalState.New("boom"))
		out := capturePanel(t, func() { checkErrCompact(d) })
		require.Contains(t, out, "1. ", "the fallback must render as a numbered step")
	})
}
