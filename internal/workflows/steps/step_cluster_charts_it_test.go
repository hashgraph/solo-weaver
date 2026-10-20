// SPDX-License-Identifier: Apache-2.0

//go:build integration

package steps

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/cli/values"
	"helm.sh/helm/v3/pkg/getter"
	"helm.sh/helm/v3/pkg/releaseutil"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"

	"github.com/hashgraph/solo-weaver/pkg/helm"
)

// renderCatalogChart pulls a catalog chart through PullAndVerify, so the
// archive matches its catalog checksum, and renders it client-side with the
// given values against Kubernetes 1.36.5.
func renderCatalogChart(t *testing.T, spec *helmChartSpec, vals []string) []unstructured.Unstructured {
	t.Helper()
	hm := isolatedHelm(t)
	if spec.Repo != "" {
		_, err := hm.AddRepo(spec.RepoAlias, spec.Repo, helm.RepoAddOptions{})
		require.NoError(t, err)
	}
	local, err := hm.PullAndVerify(context.Background(), t.TempDir(), spec.Chart, spec.Version, spec.Algorithm, spec.Checksum)
	require.NoError(t, err)

	chrt, err := loader.Load(local)
	require.NoError(t, err)
	merged, err := (&values.Options{Values: vals}).MergeValues(getter.All(cli.New()))
	require.NoError(t, err)

	install := action.NewInstall(&action.Configuration{})
	install.DryRun = true
	install.ClientOnly = true
	install.IncludeCRDs = true
	install.ReleaseName = spec.Release
	install.Namespace = spec.Namespace
	install.KubeVersion = &chartutil.KubeVersion{Version: "v1.36.5", Major: "1", Minor: "36"}
	rel, err := install.Run(chrt, merged)
	require.NoError(t, err)

	var objs []unstructured.Unstructured
	for _, doc := range releaseutil.SplitManifests(rel.Manifest) {
		var obj map[string]interface{}
		require.NoError(t, yaml.Unmarshal([]byte(doc), &obj))
		if len(obj) > 0 {
			objs = append(objs, unstructured.Unstructured{Object: obj})
		}
	}
	require.NotEmpty(t, objs)
	return objs
}

func hasObject(objs []unstructured.Unstructured, kind, name string) bool {
	for _, o := range objs {
		if o.GetKind() == kind && o.GetName() == name {
			return true
		}
	}
	return false
}

// Both MetalLB versions must render without frr-k8s with our values: 0.16.1 on
// new clusters, 0.15.2 on clusters built by earlier releases. Both must also
// create everything CheckClusterHealth requires.
func Test_MetalLBChart_RendersL2OnlyWithHealthCheckResources_Integration(t *testing.T) {
	for _, version := range []string{"0.15.2", "0.16.1"} {
		t.Run(version, func(t *testing.T) {
			spec, err := resolveCatalogChartVersion("metallb", version)
			require.NoError(t, err)
			objs := renderCatalogChart(t, spec, metalLBHelmValues())

			for _, o := range objs {
				assert.NotContains(t, o.GetName(), "frr", "%s %s", o.GetKind(), o.GetName())
				if o.GetKind() == "CustomResourceDefinition" {
					assert.False(t, strings.HasSuffix(o.GetName(), ".frrk8s.metallb.io"), o.GetName())
				}
				if o.GetKind() == "DaemonSet" || o.GetKind() == "Deployment" {
					containers, _, _ := unstructured.NestedSlice(o.Object, "spec", "template", "spec", "containers")
					for _, c := range containers {
						name, _ := c.(map[string]interface{})["name"].(string)
						assert.NotContains(t, name, "frr", "%s %s container", o.GetKind(), o.GetName())
					}
				}
			}

			assert.True(t, hasObject(objs, "Deployment", "metallb-controller"))
			assert.True(t, hasObject(objs, "DaemonSet", "metallb-speaker"))
			for _, ref := range clusterServices {
				if ns, name := splitIntoNamespaceAndName(ref); ns == spec.Namespace {
					assert.True(t, hasObject(objs, "Service", name), ref)
				}
			}
			for _, ref := range clusterConfigMaps {
				if ns, name := splitIntoNamespaceAndName(ref); ns == spec.Namespace && name != "kube-root-ca.crt" {
					assert.True(t, hasObject(objs, "ConfigMap", name), ref)
				}
			}
			for _, crd := range clusterCRDs {
				if strings.HasSuffix(crd, ".metallb.io") {
					assert.True(t, hasObject(objs, "CustomResourceDefinition", crd), crd)
				}
			}
		})
	}
}

// metrics-server keeps the values it had on 3.13.0: kubelet TLS is not verified
// and the APIService skips TLS verification.
func Test_MetricsServerChart_RendersInsecureKubeletTLS_Integration(t *testing.T) {
	for _, version := range []string{metricsServerLegacyChartVersion, chartSpec("metrics-server").Version} {
		t.Run(version, func(t *testing.T) {
			spec, err := resolveCatalogChartVersion("metrics-server", version)
			require.NoError(t, err)
			objs := renderCatalogChart(t, spec, defaultMetricsServerValues().Values)

			var args []interface{}
			var skipTLS bool
			for _, o := range objs {
				switch o.GetKind() {
				case "Deployment":
					containers, _, _ := unstructured.NestedSlice(o.Object, "spec", "template", "spec", "containers")
					require.NotEmpty(t, containers)
					args, _, _ = unstructured.NestedSlice(containers[0].(map[string]interface{}), "args")
				case "APIService":
					skipTLS, _, _ = unstructured.NestedBool(o.Object, "spec", "insecureSkipTLSVerify")
				}
			}
			assert.Contains(t, args, "--kubelet-insecure-tls")
			assert.True(t, skipTLS)
			assert.True(t, hasObject(objs, "Service", "metrics-server"))
		})
	}
}
