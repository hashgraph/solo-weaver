// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"fmt"
	"path"
	"strings"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/pkg/helm"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/software"
)

// chartDownloadsSubdir is the subdirectory of WeaverPaths.DownloadsDir where
// PullAndVerify writes verified Helm chart archives. Keeping pulled charts
// under the shared downloads tree (rather than a temp dir) lets operators
// inspect the on-disk artifacts after an install/upgrade and lines up with
// other downloaded artifacts (binaries, archives) the provisioner manages.
const chartDownloadsSubdir = "charts"

// newHelmManager constructs the helm.Manager every workflow step uses to
// drive a chart lifecycle. Centralising the construction here keeps the
// step call sites compact and gives us a single place to tweak shared
// helm.Manager options in the future.
func newHelmManager() (helm.Manager, error) {
	l := logx.As()
	return helm.NewManager(helm.WithLogger(*l))
}

// chartDownloadsDir returns the destination directory PullAndVerify writes
// chart archives into. It mirrors pkg/software/base_installer.go which uses
// models.Paths().DownloadsDir for host artifacts — charts land in a
// "charts" subdirectory of the same tree so operators can find everything
// solo-provisioner has fetched in one place.
func chartDownloadsDir() string {
	return path.Join(models.Paths().DownloadsDir, chartDownloadsSubdir)
}

// helmChartSpec is the install plan for a single Helm chart, resolved from
// the infrastructure catalog at step-execution time. It captures the version,
// integrity, and installation-topology values the catalog declares as
// authoritative, so workflow steps no longer pin any of these in code.
type helmChartSpec struct {
	Chart     string             // chartRef (e.g. "metallb/metallb" or "oci://...")
	Version   string             // default version from the catalog
	Algorithm string             // checksum algorithm (currently always sha256)
	Checksum  string             // expected checksum value (hex, no prefix)
	Repo      string             // classic-only repository URL; empty for OCI
	RepoAlias string             // classic-only Helm repo alias derived from chartRef; empty for OCI
	Namespace string             // Kubernetes namespace to install into
	Release   string             // Helm release name
	Type      software.ChartType // classic vs oci, to gate repo-add side-effects
}

// chartSpec returns the install plan for a named cluster component. It
// panics if the catalog can't be loaded or the name isn't present in
// `cluster:` — both conditions are impossible at runtime because the
// catalog is embedded into the binary and validated at load time. The
// only way this panics is a build-time programming error (e.g. a step
// referencing a name that does not exist in the catalog), and panicking
// in that case is preferable to returning an error every step has to
// thread through closures it cannot meaningfully handle.
func chartSpec(name string) *helmChartSpec {
	spec, err := resolveCatalogChart(name)
	if err != nil {
		panic(fmt.Sprintf("infrastructure catalog: chart %q: %v", name, err))
	}
	return spec
}

// resolveCatalogChart loads the embedded infrastructure catalog and returns
// the install plan for the named cluster component. Returning an error
// (rather than panicking) lets tests assert on misconfigurations; production
// callers should prefer chartSpec, which crashes loudly on the same
// impossible-in-practice conditions.
func resolveCatalogChart(name string) (*helmChartSpec, error) {
	catalog, err := software.LoadInfrastructureCatalog()
	if err != nil {
		return nil, fmt.Errorf("load infrastructure catalog: %w", err)
	}
	meta, err := catalog.GetClusterComponent(name)
	if err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	version, err := meta.GetDefaultVersion()
	if err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	sum, ok := meta.Versions[software.Version(version)]
	if !ok {
		return nil, fmt.Errorf("catalog: chart %q version %q has no checksum entry", name, version)
	}
	return &helmChartSpec{
		Chart:     meta.Chart,
		Version:   version,
		Algorithm: sum.Algorithm,
		Checksum:  sum.Value,
		Repo:      meta.Repo,
		RepoAlias: classicRepoAlias(meta),
		Namespace: meta.Namespace,
		Release:   meta.Release,
		Type:      meta.Type,
	}, nil
}

// classicRepoAlias derives the Helm repo alias from the chart reference for
// classic charts. Helm's classic-repo chartRef is "<alias>/<chartName>", and
// the alias must match a previously-registered repo for `helm pull` to
// resolve the URL. Returns "" for OCI charts since AddRepo is not used on
// that path.
func classicRepoAlias(meta *software.ChartMetadata) string {
	if meta.Type != software.ChartTypeClassic {
		return ""
	}
	if i := strings.Index(meta.Chart, "/"); i > 0 {
		return meta.Chart[:i]
	}
	return ""
}
