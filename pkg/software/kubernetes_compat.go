// SPDX-License-Identifier: Apache-2.0

package software

import (
	"slices"
	"strconv"
	"strings"

	"github.com/joomcode/errorx"

	"github.com/hashgraph/solo-weaver/pkg/semver"
)

// KubernetesMinor is what one Kubernetes minor needs from the rest of the
// catalog: the lowest version of each chart that works on it. A chart it does
// not name works on it at any catalog version.
type KubernetesMinor struct {
	Minor  string             `yaml:"minor"`
	Charts map[string]Version `yaml:"charts"`
}

// KubernetesRange is an inclusive range of Kubernetes minors, such as 1.31 to 1.34.
type KubernetesRange struct {
	Min string `yaml:"min"`
	Max string `yaml:"max"`
}

func (r KubernetesRange) validate() error {
	lo, err := parseMinor(r.Min)
	if err != nil {
		return err
	}
	hi, err := parseMinor(r.Max)
	if err != nil {
		return err
	}
	if slices.Compare(hi[:], lo[:]) < 0 {
		return errorx.IllegalFormat.New("kubernetes range %s to %s is reversed", r.Min, r.Max)
	}
	return nil
}

// parseMinor parses a Kubernetes minor such as "1.34".
func parseMinor(s string) ([2]int, error) {
	major, minor, ok := strings.Cut(s, ".")
	if ok {
		a, errA := strconv.Atoi(major)
		b, errB := strconv.Atoi(minor)
		if errA == nil && errB == nil && a >= 0 && b >= 0 {
			return [2]int{a, b}, nil
		}
	}
	return [2]int{}, errorx.IllegalFormat.New("%q is not a Kubernetes minor such as 1.34", s)
}

// minorOf returns the minor of a version such as "1.34.13".
func minorOf(version string) (string, error) {
	parts := strings.SplitN(version, ".", 3)
	if len(parts) < 2 {
		return "", errorx.IllegalFormat.New("%q has no minor version", version)
	}
	minor := parts[0] + "." + parts[1]
	if _, err := parseMinor(minor); err != nil {
		return "", err
	}
	return minor, nil
}

// validateKubernetes checks the kubernetes: entries against the rest of the
// catalog. Every minor that has a kubelet version has exactly one entry and no
// entry names another minor; every chart minimum is a listed version of a
// catalog chart; and the chart defaults meet the minimums of the default
// kubelet's minor.
func (c *InfrastructureCatalog) validateKubernetes() error {
	entries := map[string]KubernetesMinor{}
	for _, k := range c.Kubernetes {
		if _, err := parseMinor(k.Minor); err != nil {
			return errorx.IllegalFormat.Wrap(err, "kubernetes[%s]", k.Minor)
		}
		for _, name := range sortedKeys(k.Charts) {
			chart, err := c.GetClusterComponent(name)
			if err != nil {
				return errorx.IllegalFormat.New("kubernetes[%s]: chart %q is not in the catalog", k.Minor, name)
			}
			if _, ok := chart.Versions[k.Charts[name]]; !ok {
				return errorx.IllegalFormat.New("kubernetes[%s]: %s %s is not a listed version", k.Minor, name, k.Charts[name])
			}
		}
		entries[k.Minor] = k
	}

	shipped := map[string]bool{}
	kubelet, _ := c.GetHostArtifact("kubelet")
	if kubelet != nil {
		for v := range kubelet.Versions {
			minor, err := minorOf(string(v))
			if err != nil {
				return errorx.IllegalFormat.Wrap(err, "host[kubelet]")
			}
			shipped[minor] = true
		}
	}
	for _, minor := range sortedKeys(shipped) {
		if _, ok := entries[minor]; !ok {
			return errorx.IllegalFormat.New("kubelet has %s versions but no kubernetes entry lists minor %s", minor, minor)
		}
	}
	for _, minor := range sortedKeys(entries) {
		if !shipped[minor] {
			return errorx.IllegalFormat.New("kubernetes[%s]: no kubelet version of this minor", minor)
		}
	}

	if kubelet == nil || kubelet.Default == "" {
		return nil
	}
	minor, err := minorOf(string(kubelet.Default))
	if err != nil {
		return errorx.IllegalFormat.Wrap(err, "host[kubelet] default")
	}
	return c.checkDefaultsMeet(entries[minor])
}

// checkDefaultsMeet requires each chart's default to be at least k's minimum.
func (c *InfrastructureCatalog) checkDefaultsMeet(k KubernetesMinor) error {
	for _, name := range sortedKeys(k.Charts) {
		chart, err := c.GetClusterComponent(name)
		if err != nil {
			return err
		}
		def, err := semver.NewSemver(string(chart.Default))
		if err != nil {
			return errorx.IllegalFormat.Wrap(err, "cluster[%s] default", name)
		}
		minimum, err := semver.NewSemver(string(k.Charts[name]))
		if err != nil {
			return errorx.IllegalFormat.Wrap(err, "kubernetes[%s]: %s minimum", k.Minor, name)
		}
		if def.LessThan(minimum) {
			return errorx.IllegalFormat.New("kubernetes[%s] is the default kubelet's minor and needs %s %s, but the default is %s",
				k.Minor, name, k.Charts[name], chart.Default)
		}
	}
	return nil
}

func sortedKeys[K ~string, V any](m map[K]V) []K {
	keys := make([]K, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
