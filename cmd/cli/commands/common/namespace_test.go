// SPDX-License-Identifier: Apache-2.0

package common

import (
	"testing"

	"github.com/automa-saga/errx"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/require"
)

// TestValidateNamespaceFlag_RejectsEmpty pins the fail-fast contract: an empty or
// whitespace-only --namespace is rejected with an IllegalArgument/InvalidArgument
// error carrying an actionable hint, so it can never silently fall through to the
// Kubernetes "default" namespace.
func TestValidateNamespaceFlag_RejectsEmpty(t *testing.T) {
	for _, ns := range []string{"", " ", "\t", "  \n ", "\t \t"} {
		err := ValidateNamespaceFlag(ns)
		require.Error(t, err, "namespace %q must be rejected", ns)
		require.True(t, errorx.IsOfType(err, errorx.IllegalArgument),
			"namespace %q must fail as IllegalArgument", ns)

		reason, ok := errx.ReasonOf(err)
		require.True(t, ok, "namespace %q must carry a reason code", ns)
		require.Equal(t, reasons.InvalidArgument, reason, "for namespace %q", ns)

		hints, ok := errx.Hints(err)
		require.True(t, ok, "namespace %q must carry a remediation hint", ns)
		require.NotEmpty(t, hints, "for namespace %q", ns)
	}
}

// TestValidateNamespaceFlag_AcceptsNonEmpty confirms a real namespace passes: the
// validator only guards against empty/whitespace, leaving well-formed values (and
// the command defaults) untouched.
func TestValidateNamespaceFlag_AcceptsNonEmpty(t *testing.T) {
	for _, ns := range []string{"hiero-network-1", "default", "my-ns"} {
		require.NoError(t, ValidateNamespaceFlag(ns), "namespace %q must be accepted", ns)
	}
}
