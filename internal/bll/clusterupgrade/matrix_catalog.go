// SPDX-License-Identifier: Apache-2.0

package clusterupgrade

import (
	"slices"

	"github.com/joomcode/errorx"

	"github.com/hashgraph/solo-weaver/pkg/software"
)

// MatrixFromCatalog reads the upgrade matrix from the catalog: the latest
// kubelet version of each minor as the Kubernetes rungs, the latest version of
// each Cilium minor with the Kubernetes minors it supports, and every other
// chart's versions and default.
func MatrixFromCatalog(cat *software.InfrastructureCatalog) (Matrix, error) {
	if cat == nil {
		return Matrix{}, errorx.IllegalArgument.New("catalog is nil")
	}

	kubelet, err := cat.GetHostArtifact("kubelet")
	if err != nil {
		return Matrix{}, errorx.IllegalFormat.Wrap(err, "catalog")
	}
	minimums := map[string]map[string]software.Version{}
	for _, k := range cat.Kubernetes {
		minimums[k.Minor] = k.Charts
	}
	kubeVersions := make([]string, 0, len(kubelet.Versions))
	for v := range kubelet.Versions {
		kubeVersions = append(kubeVersions, string(v))
	}
	latest, err := latestPerMinor(kubeVersions)
	if err != nil {
		return Matrix{}, errorx.IllegalFormat.Wrap(err, "kubelet")
	}
	m := Matrix{DefaultKubernetes: string(kubelet.Default), Charts: map[string]ChartRung{}}
	for _, v := range latest {
		rung := KubernetesRung{Version: v.String(), Charts: map[string]string{}}
		for chart, min := range minimums[v.minorOf().String()] {
			rung.Charts[chart] = string(min)
		}
		m.Kubernetes = append(m.Kubernetes, rung)
	}

	cilium, err := cat.GetClusterComponent(software.CiliumComponentName)
	if err != nil {
		return Matrix{}, errorx.IllegalFormat.Wrap(err, "catalog")
	}
	latest, err = latestPerMinor(chartVersions(cilium))
	if err != nil {
		return Matrix{}, errorx.IllegalFormat.Wrap(err, "cilium")
	}
	for _, v := range latest {
		r := cilium.Versions[software.Version(v.String())].Kubernetes
		if r == nil {
			return Matrix{}, errorx.IllegalFormat.New("cilium %s has no Kubernetes range", v)
		}
		m.Cilium = append(m.Cilium, CiliumRung{Version: v.String(), MinKubernetes: r.Min, MaxKubernetes: r.Max})
	}

	for _, chart := range cat.Cluster {
		if chart.Name == software.CiliumComponentName {
			continue
		}
		m.Charts[chart.Name] = ChartRung{Default: string(chart.Default), Versions: chartVersions(&chart)}
	}

	if err := m.Validate(); err != nil {
		return Matrix{}, err
	}
	return m, nil
}

func chartVersions(chart *software.ChartMetadata) []string {
	out := make([]string, 0, len(chart.Versions))
	for v := range chart.Versions {
		out = append(out, string(v))
	}
	slices.Sort(out)
	return out
}

// latestPerMinor returns the highest version of each minor, ascending.
func latestPerMinor(versions []string) ([]version, error) {
	byMinor := map[minorVersion]version{}
	for _, s := range versions {
		v, err := parseVersion(s)
		if err != nil {
			return nil, err
		}
		if cur, ok := byMinor[v.minorOf()]; !ok || cur.less(v) {
			byMinor[v.minorOf()] = v
		}
	}
	out := make([]version, 0, len(byMinor))
	for _, v := range byMinor {
		out = append(out, v)
	}
	slices.SortFunc(out, version.compare)
	return out, nil
}
