// SPDX-License-Identifier: Apache-2.0

package clusterupgrade

import (
	"slices"

	"github.com/joomcode/errorx"
)

// Matrix is what the resolver knows about the versions this binary ships.
type Matrix struct {
	// Kubernetes has one rung per minor, ascending and with no gap: the latest
	// patch of each minor a cluster can upgrade through.
	Kubernetes []KubernetesRung
	// DefaultKubernetes is the version new installs get. Plans never go past it,
	// and it must be the rung of its minor.
	DefaultKubernetes string
	// Cilium has one rung per minor, ascending and with no gap.
	Cilium []CiliumRung
	// Charts are the catalog charts other than Cilium, by name.
	Charts map[string]ChartRung
}

type KubernetesRung struct {
	Version string
	// Charts is the lowest version of each chart this minor needs.
	Charts map[string]string
}

type CiliumRung struct {
	Version string
	// MinKubernetes and MaxKubernetes are the minors it supports, inclusive.
	MinKubernetes string
	MaxKubernetes string
}

type ChartRung struct {
	Default  string
	Versions []string
}

// Validate reports the first problem that would stop the resolver using m.
func (m Matrix) Validate() error {
	_, err := m.compile()
	return err
}

type kubeRung struct {
	v      version
	charts map[string]version
}

type ciliumRung struct {
	v        version
	min, max minorVersion
}

func (r ciliumRung) supports(m minorVersion) bool {
	return r.min.compare(m) <= 0 && m.compare(r.max) <= 0
}

type chartRung struct {
	def      version
	versions []version // ascending
}

// compiled is a validated Matrix with its versions parsed.
type compiled struct {
	kube   []kubeRung
	def    version
	cilium []ciliumRung
	charts map[string]chartRung
}

func (c compiled) kubeRung(m minorVersion) (kubeRung, bool) {
	for _, r := range c.kube {
		if r.v.minorOf() == m {
			return r, true
		}
	}
	return kubeRung{}, false
}

func (c compiled) ciliumRung(m minorVersion) (ciliumRung, bool) {
	for _, r := range c.cilium {
		if r.v.minorOf() == m {
			return r, true
		}
	}
	return ciliumRung{}, false
}

func (m Matrix) compile() (compiled, error) {
	c := compiled{charts: map[string]chartRung{}}

	for _, name := range sortedKeys(m.Charts) {
		r := m.Charts[name]
		cr := chartRung{}
		for _, s := range r.Versions {
			v, err := parseVersion(s)
			if err != nil {
				return compiled{}, errorx.IllegalFormat.Wrap(err, "chart %s", name)
			}
			cr.versions = append(cr.versions, v)
		}
		slices.SortFunc(cr.versions, version.compare)
		def, err := parseVersion(r.Default)
		if err != nil {
			return compiled{}, errorx.IllegalFormat.Wrap(err, "chart %s default", name)
		}
		if !slices.Contains(cr.versions, def) {
			return compiled{}, errorx.IllegalFormat.New("chart %s: default %s is not a listed version", name, r.Default)
		}
		cr.def = def
		c.charts[name] = cr
	}

	if len(m.Kubernetes) == 0 {
		return compiled{}, errorx.IllegalFormat.New("no Kubernetes rungs")
	}
	for i, r := range m.Kubernetes {
		v, err := parseVersion(r.Version)
		if err != nil {
			return compiled{}, errorx.IllegalFormat.Wrap(err, "Kubernetes rung")
		}
		if i > 0 && v.minorOf() != c.kube[i-1].v.minorOf().next() {
			return compiled{}, errorx.IllegalFormat.New("Kubernetes rung %s does not follow %s", v, c.kube[i-1].v)
		}
		kr := kubeRung{v: v, charts: map[string]version{}}
		for _, name := range sortedKeys(r.Charts) {
			min, err := parseVersion(r.Charts[name])
			if err != nil {
				return compiled{}, errorx.IllegalFormat.Wrap(err, "Kubernetes %s minimum for %s", v.minorOf(), name)
			}
			chart, ok := c.charts[name]
			if !ok {
				return compiled{}, errorx.IllegalFormat.New("Kubernetes %s needs chart %s, which the matrix does not list", v.minorOf(), name)
			}
			if !slices.Contains(chart.versions, min) {
				return compiled{}, errorx.IllegalFormat.New("Kubernetes %s needs %s %s, which is not a listed version", v.minorOf(), name, min)
			}
			kr.charts[name] = min
		}
		c.kube = append(c.kube, kr)
	}

	def, err := parseVersion(m.DefaultKubernetes)
	if err != nil {
		return compiled{}, errorx.IllegalFormat.Wrap(err, "default Kubernetes")
	}
	if r, ok := c.kubeRung(def.minorOf()); !ok || r.v != def {
		return compiled{}, errorx.IllegalFormat.New("default Kubernetes %s is not the rung of its minor", def)
	}
	c.def = def

	if len(m.Cilium) == 0 {
		return compiled{}, errorx.IllegalFormat.New("no Cilium rungs")
	}
	for i, r := range m.Cilium {
		v, err := parseVersion(r.Version)
		if err != nil {
			return compiled{}, errorx.IllegalFormat.Wrap(err, "Cilium rung")
		}
		if i > 0 && v.minorOf() != c.cilium[i-1].v.minorOf().next() {
			return compiled{}, errorx.IllegalFormat.New("Cilium rung %s does not follow %s", v, c.cilium[i-1].v)
		}
		lo, err := parseMinor(r.MinKubernetes)
		if err != nil {
			return compiled{}, errorx.IllegalFormat.Wrap(err, "Cilium %s", v)
		}
		hi, err := parseMinor(r.MaxKubernetes)
		if err != nil {
			return compiled{}, errorx.IllegalFormat.Wrap(err, "Cilium %s", v)
		}
		if hi.compare(lo) < 0 {
			return compiled{}, errorx.IllegalFormat.New("Cilium %s: Kubernetes range %s to %s is reversed", v, lo, hi)
		}
		c.cilium = append(c.cilium, ciliumRung{v: v, min: lo, max: hi})
	}

	// Every step of the Kubernetes ladder up to the default needs a Cilium
	// that runs on both of its minors.
	for _, r := range c.kube {
		k := r.v.minorOf()
		if k.compare(def.minorOf()) >= 0 {
			break
		}
		if !slices.ContainsFunc(c.cilium, func(cr ciliumRung) bool { return cr.supports(k) && cr.supports(k.next()) }) {
			return compiled{}, errorx.IllegalFormat.New("no Cilium rung supports both Kubernetes %s and %s", k, k.next())
		}
	}
	return c, nil
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
