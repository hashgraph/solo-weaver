// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/automa-saga/errx"
	"github.com/joomcode/errorx"
	"helm.sh/helm/v3/pkg/release"

	"github.com/hashgraph/solo-weaver/pkg/helm"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/hashgraph/solo-weaver/pkg/software"
)

// deployedRelease returns the release of spec when its last revision is
// deployed, or nil when the release does not exist. Every other state is
// refused: a failed, pending or uninstalled revision may not match what is
// running, and Helm cannot install over it either.
func deployedRelease(hm helm.Manager, spec *helmChartSpec) (*release.Release, error) {
	rel, err := hm.GetRelease(spec.Release, spec.Namespace)
	if err != nil {
		if errorx.IsOfType(err, helm.ErrNotFound) {
			return nil, nil
		}
		return nil, errx.Decorate(
			errorx.Decorate(err, "cannot read Helm release %s in namespace %s", spec.Release, spec.Namespace),
			reasons.K8sAPIFailed,
			"Check that the API server is up: kubectl get --raw /readyz",
			fmt.Sprintf("Check the release: helm status %s -n %s", spec.Release, spec.Namespace))
	}
	if rel == nil {
		return nil, releaseRefused(spec, "has no release record")
	}

	status := releaseStatus(rel)
	if status != release.StatusDeployed {
		return nil, releaseRefused(spec, fmt.Sprintf("is in state %q, not deployed", status))
	}
	return rel, nil
}

func releaseRefused(spec *helmChartSpec, problem string) error {
	return errx.Decorate(
		errorx.IllegalState.New("Helm release %s in namespace %s %s, so it was left unchanged", spec.Release, spec.Namespace, problem),
		reasons.PreconditionNotMet,
		fmt.Sprintf("Inspect it: helm history %s -n %s", spec.Release, spec.Namespace),
		"Finish or roll back the pending operation, or uninstall the release, then re-run the command")
}

// resolveDeployedChart returns the catalog chart to deploy for spec's release:
// the version the release runs when it exists, otherwise the catalog default.
// existing reports whether the release was already there. A release whose
// chart is not this chart, or whose version this build's catalog does not
// list, is refused rather than moved to the default.
func resolveDeployedChart(hm helm.Manager, name string) (spec *helmChartSpec, existing bool, err error) {
	defaults, err := resolveCatalogChart(name)
	if err != nil {
		return nil, false, err
	}
	rel, err := deployedRelease(hm, defaults)
	if err != nil {
		return nil, false, err
	}
	if rel == nil {
		return defaults, false, nil
	}

	if rel.Chart == nil || rel.Chart.Metadata == nil {
		return nil, false, releaseRefused(defaults, "has no chart metadata")
	}
	chart := catalogChartName(defaults)
	if got := rel.Chart.Metadata.Name; got != chart {
		return nil, false, releaseRefused(defaults, fmt.Sprintf("runs chart %q, not %q", got, chart))
	}
	version := rel.Chart.Metadata.Version
	if version == "" {
		return nil, false, releaseRefused(defaults, "has no chart version")
	}

	spec, err = resolveCatalogChartVersion(name, version)
	if err != nil {
		return nil, false, errx.Decorate(
			errorx.IllegalState.New("Helm release %s in namespace %s runs %s chart %s, which this build does not support, so it was left unchanged",
				defaults.Release, defaults.Namespace, chart, version),
			reasons.PreconditionNotMet,
			fmt.Sprintf("This build supports %s chart versions: %s", chart, strings.Join(catalogChartVersions(name), ", ")),
			"Use a solo-provisioner release that supports the installed chart version")
	}
	return spec, true, nil
}

// catalogChartName is the chart name in spec's chart reference, which is what
// Helm records as the release's chart name.
func catalogChartName(spec *helmChartSpec) string {
	return path.Base(spec.Chart)
}

func catalogChartVersions(name string) []string {
	meta := software.MustGetClusterComponent(name)
	versions := make([]string, 0, len(meta.Versions))
	for v := range meta.Versions {
		versions = append(versions, string(v))
	}
	sort.Strings(versions)
	return versions
}
