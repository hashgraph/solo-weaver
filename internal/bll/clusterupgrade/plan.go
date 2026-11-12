// SPDX-License-Identifier: Apache-2.0

// Package clusterupgrade turns the live shape of a cluster into the ordered
// list of upgrade hops that takes it to its target. The plan is never stored:
// it is recomputed from the shape on every call, so a run that stops halfway
// resumes from wherever the cluster is.
package clusterupgrade

type HopKind string

const (
	// KindStructural replaces a chart the catalog no longer manages under its
	// release name. It runs before the version ladder.
	KindStructural HopKind = "structural"
	KindCilium     HopKind = "cilium"
	KindKubernetes HopKind = "kubernetes"
	KindCharts     HopKind = "charts"
)

type HopStep string

const (
	StepPatch HopStep = "patch"
	StepMinor HopStep = "minor"
	// StepResume finishes a Kubernetes hop that moved the control plane but
	// not the kubelet.
	StepResume HopStep = "resume"
	// StepReplace swaps a chart for another chart under the same release.
	StepReplace HopStep = "replace"
	// StepMigrate moves a chart across a major that changes its CRD API.
	StepMigrate HopStep = "migrate"
	// StepCoupled bumps the charts the next Kubernetes minor needs.
	StepCoupled HopStep = "coupled"
	// StepRefresh moves every remaining chart to its catalog default.
	StepRefresh HopStep = "refresh"
)

// Hop is one step of a plan. Component is "kubernetes", "cilium" or a chart
// name; it is empty for a charts hop, whose versions are in Charts.
type Hop struct {
	Kind      HopKind
	Step      HopStep
	Component string
	From      string
	To        string
	Charts    []ChartBump
}

type ChartBump struct {
	Chart string
	From  string
	To    string
}

type Target struct {
	Kubernetes string
	Cilium     string
}

type BlockerCode string

const (
	BlockerNotObserved         BlockerCode = "not-observed"
	BlockerMultipleNodes       BlockerCode = "multiple-nodes"
	BlockerKubernetesUnknown   BlockerCode = "kubernetes-unknown"
	BlockerKubernetesNotLadder BlockerCode = "kubernetes-not-in-ladder"
	BlockerCiliumUnknown       BlockerCode = "cilium-unknown"
	BlockerCiliumMismatch      BlockerCode = "cilium-mismatch"
	BlockerCiliumNotLadder     BlockerCode = "cilium-not-in-ladder"
	BlockerUnsupportedPairing  BlockerCode = "unsupported-pairing"
	BlockerChartUnknown        BlockerCode = "chart-unknown"
	BlockerNoMigration         BlockerCode = "no-migration"
)

// Blocker is a reason the cluster cannot be planned. It is data, not an
// error, so a caller can show every blocker at once.
type Blocker struct {
	Code      BlockerCode
	Component string
	Detail    string
}

// Plan is the ordered hops to Target. A plan with blockers has no hops.
type Plan struct {
	Target   Target
	Hops     []Hop
	Blockers []Blocker
}

type Options struct {
	// To bounds the Kubernetes target: a minor such as "1.35" means its rung,
	// a version such as "1.35.10" must be that rung. Empty means the default.
	To string
}
