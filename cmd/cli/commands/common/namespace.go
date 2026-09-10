// SPDX-License-Identifier: Apache-2.0

package common

import (
	"strings"

	"github.com/automa-saga/errx"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/joomcode/errorx"
)

// ValidateNamespaceFlag rejects an empty or whitespace-only --namespace before it
// can silently fall through to Kubernetes' "default" namespace. Consensus
// subcommands derive both the target namespace and spec.orbit verbatim from this
// flag, so an unset shell variable (e.g. `--namespace "$NS2"` with NS2 unset)
// would otherwise submit a CR into the wrong namespace with an empty orbit and
// only fail later at the admission webhook. Fail fast here instead.
func ValidateNamespaceFlag(namespace string) error {
	if strings.TrimSpace(namespace) == "" {
		return errx.Decorate(
			errorx.IllegalArgument.New("--namespace is required and was empty"),
			reasons.InvalidArgument,
			"Pass a non-empty --namespace (check that the env var you expanded, e.g. $NS, is actually set)")
	}
	return nil
}
