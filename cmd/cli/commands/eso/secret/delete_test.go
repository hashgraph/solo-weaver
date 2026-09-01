// SPDX-License-Identifier: Apache-2.0

package secret

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_validateDeleteFlags_Valid(t *testing.T) {
	cases := map[string]struct {
		name      string
		namespace string
	}{
		"kebab case":  {"grafana-alloy-secrets", "grafana-alloy"},
		"underscores": {"my_secret", "my_ns"},
		"mixed case":  {"GrafanaAlloy", "Default"},
		"digits":      {"secret1", "ns2"},
		"single char": {"a", "b"},
	}

	for tn, tc := range cases {
		t.Run(tn, func(t *testing.T) {
			require.NoError(t, validateDeleteFlags(tc.name, tc.namespace))
		})
	}
}

func Test_validateDeleteFlags_Invalid(t *testing.T) {
	// ValidateIdentifier allows only [A-Za-z0-9_-], so dots and slashes are
	// rejected here even though Kubernetes itself would accept a dotted name.
	// create.go validates identically, so anything creatable stays deletable.
	cases := map[string]struct {
		name      string
		namespace string
		wantErr   string
	}{
		"empty name":            {"", "grafana-alloy", "invalid --name"},
		"empty namespace":       {"grafana-alloy-secrets", "", "invalid --namespace"},
		"space in name":         {"bad name", "grafana-alloy", "invalid --name"},
		"dot in name":           {"secrets.v1", "grafana-alloy", "invalid --name"},
		"slash in name":         {"ns/secret", "grafana-alloy", "invalid --name"},
		"newline injection":     {"a\n  kind: Secret", "grafana-alloy", "invalid --name"},
		"metachar in namespace": {"grafana-alloy-secrets", "ns;rm -rf", "invalid --namespace"},
	}

	for tn, tc := range cases {
		t.Run(tn, func(t *testing.T) {
			require.ErrorContains(t, validateDeleteFlags(tc.name, tc.namespace), tc.wantErr)
		})
	}
}
