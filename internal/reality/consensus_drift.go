// SPDX-License-Identifier: Apache-2.0

package reality

import (
	"fmt"

	"github.com/hashgraph/solo-weaver/internal/state"
	"k8s.io/apimachinery/pkg/api/resource"
)

// liveConsensusShape holds the managed-shape fields the reality checker reads back
// from a live ConsensusCapsule. Every field here is one the capsule step always
// sets explicitly (never left to an operator-side default), so a non-empty live
// value is authoritative and an empty one means "not read" — compared only when
// non-empty to avoid false drift. Volume backing, image-pull secrets, and
// provisionerDaemonEnabled are not read back yet (they need structural/bool reads
// the checker's client does not expose) — see the #1187 drift follow-up.
type liveConsensusShape struct {
	ImageRepo string
	ImageTag  string
	AccountId string
	Weight    int
	WeightSet bool
	LedgerId  string
	ChainId   string

	ContainerName string
	CPULimit      string
	CPURequest    string
	MemoryLimit   string
	MemoryRequest string
	JavaHeapMin   string
	JavaHeapMax   string
	JavaOpts      string
	UCImageRepo   string
	UCImageTag    string
}

// diffConsensusManaged compares the persisted baseline (what weaver recorded at
// install) against the live shape read from the cluster, returning one
// human-readable entry per field that drifted. Identity fields are always
// compared; the managed sizing/JVM/UC fields are compared only when a managed
// spec was recorded (nil = a pre-#1187 state file with no baseline to diff).
// An empty live value is skipped (not read, not "changed to empty"). The result
// is for surfacing (logging / status), so the persisted baseline is never
// mutated — drift keeps being reported until the operator reconciles.
func diffConsensusManaged(ns state.ConsensusNodeState, live liveConsensusShape) []string {
	var drift []string
	add := func(field, stateVal, liveVal string) {
		if liveVal != "" && liveVal != stateVal {
			drift = append(drift, fmt.Sprintf("%s: state=%q live=%q", field, stateVal, liveVal))
		}
	}
	addQuantity := func(field, stateVal, liveVal string) {
		if liveVal != "" && quantityDiffers(stateVal, liveVal) {
			drift = append(drift, fmt.Sprintf("%s: state=%q live=%q", field, stateVal, liveVal))
		}
	}

	add("imageRepo", ns.ImageRepo, live.ImageRepo)
	add("imageTag", ns.ImageTag, live.ImageTag)
	add("accountId", ns.AccountId, live.AccountId)
	add("ledgerId", ns.LedgerId, live.LedgerId)
	add("chainId", ns.ChainId, live.ChainId)
	if live.WeightSet && live.Weight != ns.Weight {
		drift = append(drift, fmt.Sprintf("weight: state=%d live=%d", ns.Weight, live.Weight))
	}

	if ns.ManagedSpec == nil {
		return drift
	}
	m := ns.ManagedSpec
	add("containerName", m.ContainerName, live.ContainerName)
	addQuantity("cpuLimit", m.CPULimit, live.CPULimit)
	addQuantity("cpuRequest", m.CPURequest, live.CPURequest)
	addQuantity("memoryLimit", m.MemoryLimit, live.MemoryLimit)
	addQuantity("memoryRequest", m.MemoryRequest, live.MemoryRequest)
	add("javaHeapMin", m.JavaHeapMin, live.JavaHeapMin)
	add("javaHeapMax", m.JavaHeapMax, live.JavaHeapMax)
	add("javaOpts", m.JavaOpts, live.JavaOpts)
	add("ucImageRepo", m.UCImageRepo, live.UCImageRepo)
	add("ucImageTag", m.UCImageTag, live.UCImageTag)

	return drift
}

// quantityDiffers reports whether two Kubernetes quantity strings represent
// different amounts. It compares semantically when both parse (so "2" and
// "2000m", or "16Gi" canonicalised by the API server, are not spurious drift),
// falling back to a string compare when either side is not a valid quantity.
func quantityDiffers(stateVal, liveVal string) bool {
	qs, errS := resource.ParseQuantity(stateVal)
	ql, errL := resource.ParseQuantity(liveVal)
	if errS != nil || errL != nil {
		return stateVal != liveVal
	}
	return qs.Cmp(ql) != 0
}
