// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"fmt"
	"strings"

	"github.com/automa-saga/errx"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/joomcode/errorx"
)

// secretFieldReader reads one field of a Kubernetes object; "" means absent.
type secretFieldReader interface {
	GetResourceNestedString(ctx context.Context, apiVersion, kind, namespace, name string, fields ...string) (string, error)
}

// requireSecretEntries fails when the Secret lacks any of the entries the
// ConsensusCapsule will reference, so a bad Secret stops the install in preflight.
func requireSecretEntries(ctx context.Context, r secretFieldReader, namespace, secret, flag string, entries ...string) error {
	var missing []string
	for _, entry := range entries {
		v, err := r.GetResourceNestedString(ctx, "v1", "Secret", namespace, secret, "data", entry)
		if err != nil {
			return errx.Decorate(
				errorx.IllegalState.Wrap(err, "failed to read Secret %s", secret),
				reasons.PreconditionNotMet,
				"Verify cluster connectivity and that your kubeconfig has RBAC to read Secrets")
		}
		if v == "" {
			missing = append(missing, entry)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return errx.Decorate(
		errorx.IllegalState.New("Secret %q (from %s) in namespace %s is missing entries: %s",
			secret, flag, namespace, strings.Join(missing, ", ")),
		reasons.PreconditionNotMet,
		fmt.Sprintf("Inspect the entries: kubectl -n %s get secret %s -o jsonpath='{.data}'", namespace, secret),
		"Re-create the node keys with `consensus keys import`, or name a different Secret with "+flag)
}
