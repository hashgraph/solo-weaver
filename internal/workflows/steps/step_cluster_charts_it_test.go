// SPDX-License-Identifier: Apache-2.0

//go:build integration

package steps

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/joomcode/errorx"
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

	"github.com/hashgraph/solo-weaver/internal/alloy"
	"github.com/hashgraph/solo-weaver/pkg/helm"
)

// renderCatalogChart pulls a catalog chart through PullAndVerify, so the
// archive matches its catalog checksum, and renders it client-side with the
// given values against Kubernetes 1.36.5.
func renderCatalogChart(t *testing.T, spec *helmChartSpec, vals []string) []unstructured.Unstructured {
	t.Helper()
	return renderCatalogChartWith(t, spec, &values.Options{Values: vals})
}

func renderCatalogChartWith(t *testing.T, spec *helmChartSpec, opts *values.Options) []unstructured.Unstructured {
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
	merged, err := opts.MergeValues(getter.All(cli.New()))
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
	// SplitManifests returns a map, so the order is random.
	sort.Slice(objs, func(i, j int) bool {
		return objs[i].GetKind()+"/"+objs[i].GetName() < objs[j].GetKind()+"/"+objs[j].GetName()
	})
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

func clusterRoleRules(t *testing.T, objs []unstructured.Unstructured) []interface{} {
	t.Helper()
	for _, o := range objs {
		if o.GetKind() == "ClusterRole" {
			rules, _, err := unstructured.NestedSlice(o.Object, "rules")
			require.NoError(t, err)
			return rules
		}
	}
	t.Fatal("no ClusterRole rendered")
	return nil
}

func withoutKind(objs []unstructured.Unstructured, kind string) []unstructured.Unstructured {
	var out []unstructured.Unstructured
	for _, o := range objs {
		if o.GetKind() != kind {
			out = append(out, o)
		}
	}
	return out
}

// Our per-version values change only the ClusterRole, and only by adding get on
// nodes/proxy to the chart's own rules. 1.4.0 keeps its chart defaults.
func Test_AlloyChart_VersionValuesOnlyAddNodesProxyGet_Integration(t *testing.T) {
	nodesProxyGet := map[string]interface{}{"apiGroups": []interface{}{""}, "resources": []interface{}{"nodes/proxy"}, "verbs": []interface{}{"get"}}
	for _, version := range []string{"1.4.0", "1.13.1"} {
		t.Run(version, func(t *testing.T) {
			spec, err := resolveCatalogChartVersion("alloy", version)
			require.NoError(t, err)
			jsonValues, err := alloy.ChartJSONValues(version)
			require.NoError(t, err)

			chartDefaults := renderCatalogChartWith(t, spec, &values.Options{Values: alloy.BaseHelmValues()})
			ours := renderCatalogChartWith(t, spec, &values.Options{Values: alloy.BaseHelmValues(), JSONValues: jsonValues})

			assert.Equal(t, withoutKind(chartDefaults, "ClusterRole"), withoutKind(ours, "ClusterRole"))
			defaultRules, ourRules := clusterRoleRules(t, chartDefaults), clusterRoleRules(t, ours)
			if version == "1.4.0" {
				assert.Equal(t, defaultRules, ourRules)
				return
			}
			assert.NotContains(t, defaultRules, nodesProxyGet)
			var added []interface{}
			for _, r := range ourRules {
				if !containsRule(defaultRules, r) {
					added = append(added, r)
				}
			}
			assert.Equal(t, []interface{}{nodesProxyGet}, added)
			for _, r := range defaultRules {
				assert.True(t, containsRule(ourRules, r), "chart rule dropped: %v", r)
			}
		})
	}
}

func containsRule(rules []interface{}, rule interface{}) bool {
	for _, r := range rules {
		if assert.ObjectsAreEqual(r, rule) {
			return true
		}
	}
	return false
}

// Both CRD chart versions install the same CRDs, with ServiceMonitor and
// PodMonitor served and stored as v1.
func Test_PrometheusOperatorCRDsChart_Integration(t *testing.T) {
	want := []string{
		"alertmanagerconfigs", "alertmanagers", "podmonitors", "probes", "prometheusagents",
		"prometheuses", "prometheusrules", "scrapeconfigs", "servicemonitors", "thanosrulers",
	}
	for _, version := range []string{"24.0.1", "32.0.1"} {
		t.Run(version, func(t *testing.T) {
			spec, err := resolveCatalogChartVersion("prometheus-operator-crds", version)
			require.NoError(t, err)
			objs := renderCatalogChart(t, spec, nil)

			var names []string
			for _, o := range objs {
				require.Equal(t, "CustomResourceDefinition", o.GetKind())
				names = append(names, strings.TrimSuffix(o.GetName(), ".monitoring.coreos.com"))
				if o.GetName() == "servicemonitors.monitoring.coreos.com" || o.GetName() == "podmonitors.monitoring.coreos.com" {
					versions, _, _ := unstructured.NestedSlice(o.Object, "spec", "versions")
					require.Len(t, versions, 1)
					v := versions[0].(map[string]interface{})
					assert.Equal(t, "v1", v["name"])
					assert.Equal(t, true, v["served"])
					assert.Equal(t, true, v["storage"])
				}
			}
			assert.ElementsMatch(t, want, names)
		})
	}
}

// Both kube agent chart versions render with the local test server's values:
// 18.11.3 on new clusters, 18.6.4 on clusters built by earlier releases or
// asked for with --version.
func Test_TeleportKubeAgentChart_Integration(t *testing.T) {
	for _, version := range []string{"18.6.4", "18.11.3"} {
		t.Run(version, func(t *testing.T) {
			spec, err := resolveCatalogChartVersion(teleportClusterAgent, version)
			require.NoError(t, err)
			objs := renderCatalogChartWith(t, spec, &values.Options{
				ValueFiles: []string{"../../../test/teleport/teleport-values-local.yaml"},
				Values:     []string{"authToken=test-token", "proxyAddr=192.0.2.10:3080"},
			})

			var images []string
			for _, o := range objs {
				if o.GetKind() != "StatefulSet" && o.GetKind() != "Deployment" {
					continue
				}
				containers, _, _ := unstructured.NestedSlice(o.Object, "spec", "template", "spec", "containers")
				for _, c := range containers {
					images = append(images, c.(map[string]interface{})["image"].(string))
				}
			}
			assert.Contains(t, images, "public.ecr.aws/gravitational/teleport-distroless:"+version)

			var config string
			for _, o := range objs {
				if o.GetKind() == "ConfigMap" && o.GetName() == spec.Release {
					config, _, _ = unstructured.NestedString(o.Object, "data", "teleport.yaml")
				}
			}
			assert.Contains(t, config, "kube_cluster_name: solo-weaver-local")
			assert.Contains(t, config, "192.0.2.10:3080")
		})
	}
}

func Test_TeleportKubeAgentChart_ChecksumMismatchIsRefused_Integration(t *testing.T) {
	spec, err := resolveCatalogChartVersion(teleportClusterAgent, "18.6.4")
	require.NoError(t, err)
	hm := isolatedHelm(t)
	_, err = hm.AddRepo(spec.RepoAlias, spec.Repo, helm.RepoAddOptions{})
	require.NoError(t, err)

	// The 18.11.3 checksum, so the archive is real but the wrong one.
	other, err := resolveCatalogChartVersion(teleportClusterAgent, "18.11.3")
	require.NoError(t, err)
	_, err = hm.PullAndVerify(context.Background(), t.TempDir(), spec.Chart, "18.6.4", spec.Algorithm, other.Checksum)
	require.True(t, errorx.IsOfType(err, helm.ErrChecksumMismatch), "got %v", err)
}
