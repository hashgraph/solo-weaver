// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"strings"
	"testing"

	"github.com/hashgraph/solo-weaver/pkg/software"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test_resolveCatalogChart_AllStepNames asserts that every catalog name the
// Helm workflow steps look up in Phase 3 (issue #589) is present in the
// embedded infrastructure catalog, with all integrity fields populated.
// This guards the implicit contract between step source and catalog YAML —
// renaming a cluster entry without updating both is otherwise only caught at
// runtime when the install step fires.
func Test_resolveCatalogChart_AllStepNames(t *testing.T) {
	// Names must match the strings each step passes to resolveCatalogChart.
	names := []string{
		"alloy",
		"node-exporter",
		"metallb",
		"metrics-server",
		"prometheus-operator-crds",
		"external-secrets",
		"teleport-cluster-agent",
		"solo-operator",
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			spec, err := resolveCatalogChart(name)
			require.NoError(t, err)
			require.NotNil(t, spec)
			assert.NotEmpty(t, spec.Chart, "chart reference must be set")
			assert.NotEmpty(t, spec.Version, "default version must be resolved")
			assert.Equal(t, "sha256", spec.Algorithm)
			assert.NotEmpty(t, spec.Checksum)
			assert.NotEmpty(t, spec.Namespace, "namespace must be set")
			assert.NotEmpty(t, spec.Release, "release must be set")
			switch spec.Type {
			case software.ChartTypeClassic:
				assert.NotEmpty(t, spec.Repo, "classic charts must declare a repo")
				// RepoAlias is what AddRepo registers the repo under and
				// must match the prefix of the chartRef so `helm pull
				// <alias>/<name>` resolves. A drift between the two would
				// surface at install time as "repo X not found".
				require.NotEmpty(t, spec.RepoAlias, "classic charts must derive a repo alias")
				prefix, _, ok := strings.Cut(spec.Chart, "/")
				require.True(t, ok, "classic chartRef must be <alias>/<chart>")
				assert.Equal(t, prefix, spec.RepoAlias)
			case software.ChartTypeOCI:
				assert.Empty(t, spec.Repo, "oci charts must not declare a separate repo")
				assert.Empty(t, spec.RepoAlias, "oci charts have no repo alias")
			default:
				t.Fatalf("unexpected chart type %q", spec.Type)
			}
		})
	}
}

func Test_resolveCatalogChart_UnknownName(t *testing.T) {
	_, err := resolveCatalogChart("does-not-exist")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does-not-exist")
}
